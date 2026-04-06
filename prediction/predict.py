#!/usr/bin/env python3
"""
Pool Utilization Prediction — Batch Inference Service

Runs every 10 minutes:
1. Loads per-pool models from disk
2. Fetches latest readings from track_pools (lag features)
3. Fetches weather forecast from weather_forecast table
4. Builds feature matrix and runs prediction for next 6h per pool
5. Upserts results to predictions table

Usage:
    python predict.py               # run once and exit
    python predict.py --daemon      # run continuously every 10min
"""

import argparse
import os
import sqlite3
import sys
from datetime import datetime, timedelta

import joblib
import numpy as np
import pandas as pd

try:
    import holidays
except ImportError:
    holidays = None


DB_PATH = os.environ.get("DB_PATH", "/data/swm_pool_utility.db")
MODEL_DIR = os.environ.get("MODEL_DIR", "/models")
PREDICTION_HORIZON_HOURS = 6
PREDICTION_INTERVAL_MINUTES = 10
MODEL_VERSION = datetime.now().strftime("%Y%m%d%H%M%S")


# ── Holiday helpers ──────────────────────────────────────────────────────────

def get_german_holidays(years):
    if holidays is None:
        return set()
    return set(holidays.CountryHoliday("DE", prov="BY", years=years).keys())


def is_holiday(dt, holiday_set):
    if dt.weekday() >= 5:
        return 1
    return 1 if dt.strftime("%Y-%m-%d") in holiday_set else 0


def days_to_nearest_holiday(dt, holiday_set, window=7):
    distances = [
        abs(d) for d in range(-window, window + 1)
        if d != 0 and (dt + timedelta(days=d)).strftime("%Y-%m-%d") in holiday_set
    ]
    return min(distances) if distances else 0


def add_temporal_features(df):
    df["hour"] = df["dtime"].dt.hour
    df["minute"] = df["dtime"].dt.minute
    df["day_of_week"] = df["dtime"].dt.dayofweek
    df["day_of_year"] = df["dtime"].dt.dayofyear
    df["is_weekend"] = (df["day_of_week"] >= 5).astype(int)

    def get_season(doy):
        if doy <= 59 or doy > 334:
            return 0  # winter
        if doy <= 151:
            return 1  # spring
        if doy <= 243:
            return 2  # summer
        return 3  # autumn
    df["season"] = df["day_of_year"].apply(get_season)

    years = df["dtime"].dt.year.unique()
    hol_set = get_german_holidays(list(years))
    df["is_holiday"] = df["dtime"].apply(lambda t: is_holiday(t, hol_set))
    df["days_to_holiday"] = df["dtime"].apply(lambda t: days_to_nearest_holiday(t, hol_set))
    return df


# ── Feature columns (must match training) ────────────────────────────────────

FEATURE_COLS = [
    "hour", "minute", "day_of_week", "day_of_year", "season",
    "is_weekend", "is_holiday", "days_to_holiday",
    "temperature", "wind_speed", "precipitation", "cloud_cover",
    "util_lag_10m", "util_lag_20m", "util_lag_60m", "util_lag_120m",
    "util_rolling_3h",
]


# ── Load models ──────────────────────────────────────────────────────────────

def load_models():
    models = {}
    if not os.path.isdir(MODEL_DIR):
        print(f"Model directory not found: {MODEL_DIR}")
        return models

    for fname in os.listdir(MODEL_DIR):
        if not fname.endswith(".joblib"):
            continue
        # Normalize: replace _ with space (trained filenames use _ for spaces/hyphens)
        # Then also strip hyphens and compare against DB names
        raw = fname.replace("pool_", "").replace(".joblib", "")
        normalized = raw.replace("_", " ").replace("-", " ").lower()
        path = os.path.join(MODEL_DIR, fname)
        try:
            model = joblib.load(path)
            models[raw] = model
            print(f"  Loaded model: {raw}")
        except Exception as e:
            print(f"  Failed to load {fname}: {e}")

    # Map normalized names to DB pool names
    conn = sqlite3.connect(DB_PATH)
    db_pools = [r[0] for r in conn.execute("SELECT DISTINCT name FROM track_pools").fetchall()]
    conn.close()

    def norm_key(s):
        return s.lower().replace("-", " ").replace("_", " ")

    name_map = {}
    for pool in db_pools:
        pool_norm = norm_key(pool)
        for raw in models.keys():
            if norm_key(raw) == pool_norm:
                name_map[raw] = pool

    # Rebuild models dict with actual DB pool names as keys
    remapped = {}
    for raw, m in models.items():
        db_name = name_map.get(raw, raw)
        remapped[db_name] = m
    return remapped

    for fname in os.listdir(MODEL_DIR):
        if not fname.endswith(".joblib"):
            continue
        pool_name = fname.replace("pool_", "").replace("_", " ").replace(".joblib", "")
        path = os.path.join(MODEL_DIR, fname)
        try:
            models[pool_name] = joblib.load(path)
            print(f"  Loaded model: {pool_name}")
        except Exception as e:
            print(f"  Failed to load {fname}: {e}")
    return models


# ── Load latest readings per pool ────────────────────────────────────────────

def load_latest_readings():
    conn = sqlite3.connect(DB_PATH)
    df = pd.read_sql_query("""
        SELECT name, dtime, utility FROM track_pools
        WHERE name IS NOT NULL
        ORDER BY name, dtime DESC
    """, conn, parse_dates=["dtime"])
    conn.close()

    if df["dtime"].dt.tz is not None:
        df["dtime"] = df["dtime"].dt.tz_convert("UTC").dt.tz_localize(None)
    df["utilization"] = 100 - df["utility"]

    latest = {}
    for pool_name, grp in df.groupby("name"):
        grp = grp.sort_values("dtime")
        readings = grp.tail(20).sort_values("dtime")
        latest[pool_name] = readings.tail(20).reset_index(drop=True)
    return latest


# ── Load weather forecast ────────────────────────────────────────────────────

def load_weather_forecast():
    conn = sqlite3.connect(DB_PATH)
    df = pd.read_sql_query("""
        SELECT dtime, temperature, wind_speed, precipitation, cloud_cover, weather_code
        FROM weather_forecast
        ORDER BY dtime
    """, conn, parse_dates=["dtime"])
    conn.close()
    if df["dtime"].dt.tz is not None:
        df["dtime"] = df["dtime"].dt.tz_convert("UTC").dt.tz_localize(None)
    return df


# ── Build future timestamps ────────────────────────────────────────────────────

def future_timestamps(now_utc, horizon_hours, interval_minutes):
    steps = horizon_hours * 60 // interval_minutes
    return [now_utc + timedelta(minutes=i * interval_minutes) for i in range(1, steps + 1)]


# ── Predict for one pool ─────────────────────────────────────────────────────

def predict_pool(pool_name, model, latest_readings, weather_df):
    readings = latest_readings.get(pool_name)
    if readings is None or len(readings) < 3:
        return None

    weather_df = weather_df.sort_values("dtime")
    now_utc = pd.Timestamp.now("UTC").floor("10min").tz_localize(None)

    future_times = future_timestamps(now_utc, PREDICTION_HORIZON_HOURS, PREDICTION_INTERVAL_MINUTES)

    rows = []
    for ft in future_times:
        wf_row = weather_df[weather_df["dtime"] <= ft]
        if wf_row.empty:
            continue
        wf = wf_row.iloc[-1]

        ft_utc = ft.tz_localize("UTC")

        util_lag_10m = readings[readings["dtime"] <= ft - timedelta(minutes=10)]["utilization"]
        util_lag_20m = readings[readings["dtime"] <= ft - timedelta(minutes=20)]["utilization"]
        util_lag_60m = readings[readings["dtime"] <= ft - timedelta(minutes=60)]["utilization"]
        util_lag_120m = readings[readings["dtime"] <= ft - timedelta(minutes=120)]["utilization"]

        util_t0 = util_lag_10m.iloc[-1] if len(util_lag_10m) > 0 else readings["utilization"].iloc[-1]
        util_t1 = util_lag_20m.iloc[-1] if len(util_lag_20m) > 0 else util_t0
        util_t6 = util_lag_60m.iloc[-1] if len(util_lag_60m) > 0 else util_t0
        util_t12 = util_lag_120m.iloc[-1] if len(util_lag_120m) > 0 else util_t0

        recent = readings[readings["dtime"] >= ft - timedelta(hours=3)]["utilization"]
        util_rolling = recent.mean() if len(recent) > 0 else util_t0

        row = {
            "pool_name": pool_name,
            "dtime": ft_utc,
            "hour": ft.hour,
            "minute": ft.minute,
            "day_of_week": ft.weekday(),
            "day_of_year": ft.dayofyear,
            "is_weekend": 1 if ft.weekday() >= 5 else 0,
            "season": 0 if ft.dayofyear <= 59 or ft.dayofyear > 334 else (1 if ft.dayofyear <= 151 else (2 if ft.dayofyear <= 243 else 3)),
            "is_holiday": 0,
            "days_to_holiday": 0,
            "temperature": wf["temperature"],
            "wind_speed": wf["wind_speed"],
            "precipitation": wf["precipitation"],
            "cloud_cover": wf["cloud_cover"],
            "util_lag_10m": util_t0,
            "util_lag_20m": util_t1,
            "util_lag_60m": util_t6,
            "util_lag_120m": util_t12,
            "util_rolling_3h": util_rolling,
        }
        rows.append(row)

    if not rows: return None

    pred_df = pd.DataFrame(rows)
    pred_df = add_temporal_features(pred_df)

    X = pred_df[FEATURE_COLS].values
    preds = model.predict(X)

    pred_df["predicted_utilization"] = preds.clip(0, 100)
    return pred_df[["pool_name", "dtime", "predicted_utilization"]]


# ── Save predictions ─────────────────────────────────────────────────────────

def save_predictions(pred_df):
    if pred_df is None or pred_df.empty:
        print("No predictions to save")
        return

    conn = sqlite3.connect(DB_PATH)
    now = datetime.utcnow().strftime("%Y-%m-%d %H:%M:%S")
    count = 0
    for _, row in pred_df.iterrows():
        try:
            conn.execute(
                "INSERT OR REPLACE INTO predictions "
                "(pool_name, dtime, predicted_utilization, model_version, created_at) "
                "VALUES (?, ?, ?, ?, ?)",
                (
                    row["pool_name"],
                    row["dtime"].strftime("%Y-%m-%d %H:%M:%S"),
                    round(float(row["predicted_utilization"]), 1),
                    MODEL_VERSION,
                    now,
                ),
            )
            count += 1
        except Exception as e:
            print(f"  Insert failed: {e}")

    conn.commit()
    conn.close()
    print(f"Saved {count} predictions")


# ── Run one prediction cycle ─────────────────────────────────────────────────

def run_prediction_cycle(models, latest_readings, weather_df):
    total = 0
    for pool_name, model in sorted(models.items()):
        pred_df = predict_pool(pool_name, model, latest_readings, weather_df)
        if pred_df is not None:
            save_predictions(pred_df)
            total += len(pred_df)
    print(f"Prediction cycle complete: {total} total predictions")


# ── Entry point ──────────────────────────────────────────────────────────────

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--daemon", action="store_true", help="Run continuously every 10min")
    args = parser.parse_args()

    print(f"[{datetime.now()}] Starting prediction service...")

    models = load_models()
    if not models:
        print("No models found in", MODEL_DIR)
        sys.exit(1)

    print(f"Loaded {len(models)} model(s)")

    latest_readings = load_latest_readings()
    print(f"Loaded latest readings for {len(latest_readings)} pool(s)")

    weather_df = load_weather_forecast()
    print(f"Loaded {len(weather_df)} weather forecast rows")

    if args.daemon:
        import time
        interval = 10 * 60
        while True:
            try:
                latest_readings = load_latest_readings()
                weather_df = load_weather_forecast()
                run_prediction_cycle(models, latest_readings, weather_df)
            except Exception as e:
                print(f"Prediction cycle failed: {e}")
            time.sleep(interval)
    else:
        run_prediction_cycle(models, latest_readings, weather_df)


if __name__ == "__main__":
    main()
