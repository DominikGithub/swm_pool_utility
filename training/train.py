#!/usr/bin/env python3
"""
Pool Utilization Prediction — Training Pipeline

Trains per-pool RandomForest models on historical pool utilization + weather data.
Saves one .joblib file per pool to the shared volume.

Usage:
    python train.py                          # train all pools
    python train.py --pool "Michaelibad"     # train single pool
    python train.py --validate-only          # evaluate without saving
"""

import argparse
import os
import sqlite3
import sys
from datetime import datetime, timedelta

import joblib
import numpy as np
import pandas as pd
from sklearn.ensemble import RandomForestRegressor
from sklearn.metrics import mean_absolute_error, mean_squared_error, r2_score
from sklearn.preprocessing import LabelEncoder

try:
    import holidays
except ImportError:
    print("Warning: 'holidays' package not found — German holidays disabled")
    holidays = None


DB_PATH = os.environ.get("DB_PATH", "/data/swm_pool_utility.db")
MODEL_DIR = os.environ.get("MODEL_DIR", "/models")
os.makedirs(MODEL_DIR, exist_ok=True)


# ── German (Bayern) holidays ─────────────────────────────────────────────────

def get_german_holidays(years):
    if holidays is None:
        return set()
    de = holidays.CountryHoliday("DE", prov="BY", years=years)
    return set(de.keys())


def is_holiday(dt, holiday_set):
    date_str = dt.strftime("%Y-%m-%d")
    dow = dt.weekday()
    if dow in (5, 6):
        return 1
    return 1 if date_str in holiday_set else 0


def days_to_nearest_holiday(dt, holiday_set, window=7):
    distances = [
        abs(d) for d in range(-window, window + 1)
        if d != 0 and (dt + timedelta(days=d)).strftime("%Y-%m-%d") in holiday_set
    ]
    return min(distances) if distances else 0


# ── Temporal feature engineering ─────────────────────────────────────────────

def add_temporal_features(df):
    df["hour"] = df["dtime"].dt.hour
    df["minute"] = df["dtime"].dt.minute
    df["day_of_week"] = df["dtime"].dt.dayofweek
    df["day_of_year"] = df["dtime"].dt.dayofyear
    df["is_weekend"] = (df["day_of_week"] >= 5).astype(int)

    def get_season(doy):
        if doy <= 59 or doy > 334:  return 0  # winter
        if doy <= 151:              return 1  # spring
        if doy <= 243:              return 2  # summer
                                    return 3  # autumn
    df["season"] = df["day_of_year"].apply(get_season)

    years = df["dtime"].dt.year.unique()
    hol_set = get_german_holidays(list(years))
    df["is_holiday"] = df["dtime"].apply(lambda t: is_holiday(t, hol_set))
    df["days_to_holiday"] = df["dtime"].apply(lambda t: days_to_nearest_holiday(t, hol_set))
    return df


# ── Weather join ─────────────────────────────────────────────────────────────

def join_weather(df, weather_df):
    wf = weather_df.copy()
    wf["dtime"] = pd.to_datetime(wf["dtime"])
    wf = wf.sort_values("dtime")

    wf_cols = ["temperature", "wind_speed", "precipitation", "cloud_cover", "weather_code"]
    df = df.sort_values(["pool_name", "dtime"])

    df_merged = pd.merge_asof(
        df.sort_values("dtime"),
        wf[wf_cols + ["dtime"]].sort_values("dtime"),
        on="dtime",
        direction="backward",
        tolerance=pd.Timedelta("61min"),
    )
    return df_merged


# ── Lag features ─────────────────────────────────────────────────────────────

def add_lag_features(df):
    df = df.sort_values(["pool_name", "dtime"])

    for lag in [1, 2, 6, 12]:
        mins = lag * 10
        df[f"util_lag_{mins}m"] = df.groupby("pool_name")["utilization"].shift(lag)

    df["util_rolling_3h"] = (
        df.groupby("pool_name")["utilization"]
        .transform(lambda s: s.rolling(18, min_periods=1).mean())
    )
    return df


# ── Main loading pipeline ────────────────────────────────────────────────────

def load_training_data(pool_name=None):
    conn = sqlite3.connect(DB_PATH)

    query = """
        SELECT name, dtime, utility FROM track_pools
        WHERE name IS NOT NULL
        ORDER BY name, dtime
    """
    if pool_name:
        query += f" AND name = '{pool_name}'"

    df = pd.read_sql_query(query, conn, parse_dates=["dtime"])
    conn.close()

    if df.empty:
        print(f"No data found{' for pool: ' + pool_name if pool_name else ''}")
        return None

    df["utilization"] = 100 - df["utility"]
    df = df.rename(columns={"name": "pool_name"})

    pool_names = df["pool_name"].unique()
    print(f"Loaded {len(df)} rows for {len(pool_names)} pool(s): {list(pool_names)}")

    conn = sqlite3.connect(DB_PATH)
    weather_query = "SELECT dtime, temperature, wind_speed, precipitation, cloud_cover, weather_code FROM weather ORDER BY dtime"
    weather_df = pd.read_sql_query(weather_query, conn, parse_dates=["dtime"])
    conn.close()

    print(f"Loaded {len(weather_df)} weather rows")

    df = join_weather(df, weather_df)
    df = add_temporal_features(df)
    df = add_lag_features(df)

    weather_features = ["temperature", "wind_speed", "precipitation", "cloud_cover", "weather_code"]
    for col in weather_features:
        if col in df.columns:
            df[col] = df[col].fillna(df[col].median())

    df = df.dropna()

    print(f"After feature engineering: {len(df)} rows")
    return df


# ── Feature columns ──────────────────────────────────────────────────────────

FEATURE_COLS = [
    "hour", "minute", "day_of_week", "day_of_year", "season",
    "is_weekend", "is_holiday", "days_to_holiday",
    "temperature", "wind_speed", "precipitation", "cloud_cover",
    "util_lag_10m", "util_lag_20m", "util_lag_60m", "util_lag_120m",
    "util_rolling_3h",
]


def split_train_val(df, val_days=7):
    cutoff = df["dtime"].max() - timedelta(days=val_days)
    train = df[df["dtime"] < cutoff].copy()
    val = df[df["dtime"] >= cutoff].copy()
    print(f"Train: {len(train)} rows, Val: {len(val)} rows")
    return train, val


def train_pool(df, pool_name):
    train, val = split_train_val(df)

    X_train = train[FEATURE_COLS].values
    y_train = train["utilization"].values
    X_val = val[FEATURE_COLS].values
    y_val = val["utilization"].values

    print(f"  Training RandomForest for '{pool_name}' on {len(X_train)} samples...")
    model = RandomForestRegressor(
        n_estimators=200,
        max_depth=15,
        min_samples_split=10,
        min_samples_leaf=5,
        n_jobs=-1,
        random_state=42,
    )
    model.fit(X_train, y_train)

    y_pred = model.predict(X_val)
    mae = mean_absolute_error(y_val, y_pred)
    rmse = np.sqrt(mean_squared_error(y_val, y_pred))
    r2 = r2_score(y_val, y_pred)

    print(f"  {pool_name} — MAE: {mae:.1f}%, RMSE: {rmse:.1f}%, R²: {r2:.3f}")

    return model, {"mae": mae, "rmse": rmse, "r2": r2}


# ── Entry point ──────────────────────────────────────────────────────────────

def main():
    parser = argparse.ArgumentParser(description="Train pool utilization prediction models")
    parser.add_argument("--pool", type=str, default=None, help="Train only a specific pool")
    parser.add_argument("--validate-only", action="store_true", help="Evaluate without saving models")
    args = parser.parse_args()

    df = load_training_data(pool_name=args.pool)
    if df is None:
        print(f'{args=}')
        sys.exit(1)

    conn = sqlite3.connect(DB_PATH)
    pool_names = df["pool_name"].unique()
    conn.close()

    results = {}
    for pool in sorted(pool_names):
        pool_df = df[df["pool_name"] == pool].copy()
        model, metrics = train_pool(pool_df, pool)
        results[pool] = metrics

        if not args.validate_only:
            out_path = os.path.join(MODEL_DIR, f"pool_{pool.replace(' ', '_').replace('-', '_')}.joblib")
            joblib.dump(model, out_path)
            print(f"  Saved: {out_path}")

    print("\n=== Summary ===")
    for pool, m in results.items():
        print(f"  {pool}: MAE={m['mae']:.1f}%  R²={m['r2']:.3f}")

    if args.validate_only:
        print("\n(validate-only mode — no models saved)")


if __name__ == "__main__":
    main()
