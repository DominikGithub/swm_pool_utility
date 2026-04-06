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
from zoneinfo import ZoneInfo

import joblib
import numpy as np
import pandas as pd
from sklearn.ensemble import RandomForestRegressor
from sklearn.metrics import mean_absolute_error, mean_squared_error, r2_score

try:
    import holidays
except ImportError:
    print("Warning: 'holidays' package not found — German holidays disabled")
    holidays = None


DB_PATH = os.environ.get("DB_PATH", "/data/swm_pool_utility.db")
MODEL_DIR = os.environ.get("MODEL_DIR", "/models")
os.makedirs(MODEL_DIR, exist_ok=True)

FEATURE_COLS = [
    "hour", "minute", "day_of_week", "day_of_year", "season",
    "is_weekend", "is_holiday", "days_to_holiday",
    "temperature", "wind_speed", "precipitation", "cloud_cover",
    "util_lag_10m", "util_lag_20m", "util_lag_30m",
    "util_lag_60m", "util_lag_120m", "util_rolling_3h",
    "util_change_30m", "util_momentum",
    "avg_weekday_delta",
]

# ── German (Bayern) holidays ─────────────────────────────────────────────────

def get_german_holidays(years):
    if holidays is None:
        return set()
    de = holidays.CountryHoliday("DE", prov="BY", years=years)
    return set(de.keys())


def is_holiday(dt, holiday_set):
    date_str = dt.strftime("%Y-%m-%d")
    dow = dt.weekday()
    # Weekend
    if dow in (5, 6):
        return 1
    # National holiday
    return 1 if date_str in holiday_set else 0


def days_to_nearest_holiday(dt, holiday_set, window=7):
    """Return the number of days to the closest holiday within ±window days.

    Searches past and future holidays, so the model can learn the lead-up
    effect (people visit pools before a holiday) and the tail-off after one.
    """
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
        if doy <= 59 or doy > 334: return 0  # winter
        if doy <= 151:             return 1  # spring
        if doy <= 243:             return 2  # summer
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


# ── Historical weekday-average trend ─────────────────────────────────────────

BERLIN = ZoneInfo("Europe/Berlin")
SLOTS_PER_WEEK = 1008  # 7 days × 144 ten-minute slots


def load_avg_cache():
    """Load daily_avg_cache into a dict keyed by (pool_name, slot_index)."""
    conn = sqlite3.connect(DB_PATH)
    rows = conn.execute(
        "SELECT pool_name, slot_index, mean_utilization FROM daily_avg_cache"
    ).fetchall()
    conn.close()
    return {(pool, slot): mean for pool, slot, mean in rows}


def _utc_to_berlin_slot(dtime_utc):
    """Convert a naive-UTC timestamp to its Berlin-local weekly slot index.

    Slot index matches the aggregator: Mon=0..Sun=6, 144 ten-minute slots/day.
    """
    dt_berlin = dtime_utc.tz_localize("UTC").tz_convert(BERLIN)
    dow = dt_berlin.weekday()  # Mon=0..Sun=6, same as aggregator
    return dow * 144 + (dt_berlin.hour * 60 + dt_berlin.minute) // 10


def add_avg_weekday_delta(df, avg_cache, steps_ahead=3):
    """Add avg_weekday_delta: typical utilization change over the next
    `steps_ahead` slots (30 min at 10-min resolution) based on the
    historical weekday averages stored in daily_avg_cache.

    Falls back to 0.0 when cache data is missing (e.g. first deployment
    before the aggregator has populated the table).
    """
    slots = df["dtime"].apply(_utc_to_berlin_slot)
    slots_ahead = (slots + steps_ahead) % SLOTS_PER_WEEK

    def _delta(pool, s, sa):
        cur = avg_cache.get((pool, int(s)))
        ahe = avg_cache.get((pool, int(sa)))
        if cur is not None and ahe is not None:
            return ahe - cur
        return 0.0

    df["avg_weekday_delta"] = [
        _delta(p, s, sa)
        for p, s, sa in zip(df["pool_name"], slots, slots_ahead)
    ]
    return df


# ── Lag features ─────────────────────────────────────────────────────────────

def add_lag_features(df):
    df = df.sort_values(["pool_name", "dtime"])

    # Data is scraped at 10-minute intervals, so shift(1) = 10m ago, shift(2) = 20m ago, etc.
    df["util_lag_10m"] = df.groupby("pool_name")["utilization"].shift(1)
    df["util_lag_20m"] = df.groupby("pool_name")["utilization"].shift(2)
    df["util_lag_30m"] = df.groupby("pool_name")["utilization"].shift(3)
    df["util_lag_60m"] = df.groupby("pool_name")["utilization"].shift(6)
    df["util_lag_120m"] = df.groupby("pool_name")["utilization"].shift(12)

    df["util_rolling_3h"] = (
        df.groupby("pool_name")["utilization"]
        .transform(lambda s: s.rolling(18, min_periods=1).mean())
    )
    df["util_change_30m"] = df["util_lag_10m"] - df["util_lag_30m"]
    df["util_momentum"] = df["util_lag_10m"] - df["util_rolling_3h"]
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

    avg_cache = load_avg_cache()
    print(f"Loaded {len(avg_cache)} daily-avg cache entries")
    df = add_avg_weekday_delta(df, avg_cache)

    weather_features = ["temperature", "wind_speed", "precipitation", "cloud_cover", "weather_code"]
    for col in weather_features:
        if col in df.columns:
            df[col] = df[col].fillna(df[col].median())

    df = df.dropna()
    print(f"After feature engineering: {len(df)} rows")
    return df

def split_train_val(df, val_days=7):                                        # TODO is this really a valid temporal split?
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
    model = RandomForestRegressor(                                                  # TODO check model parameters for improvement
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

def run_training(pool_name=None, validate_only=False):
    """Run one full training cycle. Returns False if no data was found."""
    print(f"[{datetime.now()}] Starting training cycle...")
    df = load_training_data(pool_name=pool_name)
    if df is None:
        print("No training data found — skipping cycle.")
        return False

    pool_names = df["pool_name"].unique()
    results = {}
    for pool in sorted(pool_names):
        pool_df = df[df["pool_name"] == pool].copy()
        model, metrics = train_pool(pool_df, pool)
        results[pool] = metrics

        if not validate_only:
            out_path = os.path.join(MODEL_DIR, f"pool_{pool.replace(' ', '_').replace('-', '_')}.joblib")
            joblib.dump(model, out_path)
            print(f"  Saved: {out_path}")

    print("\n=== Summary ===")
    for pool, m in results.items():
        print(f"  {pool}: MAE={m['mae']:.1f}%  R²={m['r2']:.3f}")

    if validate_only:
        print("\n(validate-only mode — no models saved)")

    print(f"[{datetime.now()}] Training cycle complete.")
    return True


def main():
    import time

    parser = argparse.ArgumentParser(description="Train pool utilization prediction models")
    parser.add_argument("--pool", type=str, default=None, help="Train only a specific pool")
    parser.add_argument("--validate-only", action="store_true", help="Evaluate without saving models")
    parser.add_argument("--daemon", action="store_true",
                        help="Run continuously: retrain immediately then every 24 hours")
    args = parser.parse_args()

    if args.daemon:
        retrain_hour = int(os.environ.get("RETRAIN_HOUR", "2"))  # default 02:00 Berlin

        def seconds_until_next_run():
            now = datetime.now(BERLIN)
            target = now.replace(hour=retrain_hour, minute=0, second=0, microsecond=0)
            if now >= target:
                target += timedelta(days=1)
            return (target - now).total_seconds()

        while True:
            try:
                run_training(pool_name=args.pool, validate_only=args.validate_only)
            except Exception as e:
                print(f"[{datetime.now()}] Training cycle failed: {e}")
            wait = seconds_until_next_run()
            next_run = datetime.now(BERLIN) + timedelta(seconds=wait)
            print(f"[{datetime.now()}] Next retraining at {next_run.strftime('%Y-%m-%d %H:%M')} Europe/Berlin ({wait/3600:.1f}h from now).")
            time.sleep(wait)
    else:
        ok = run_training(pool_name=args.pool, validate_only=args.validate_only)
        if not ok:
            sys.exit(1)


if __name__ == "__main__":
    main()
