#!/usr/bin/env python3
"""
ML Prediction — runs trained models every 10 minutes to forecast near-future pool utilization.
Gives dashboard users an early signal on whether a pool is about to get busier or quieter.

Usage:
    python predict.py               # run once and exit
    python predict.py --daemon      # run continuously every 10min
"""

import argparse
import json
import os
import sqlite3
import sys
from datetime import datetime, timedelta
from zoneinfo import ZoneInfo

import joblib
import numpy as np
import pandas as pd

try:
    import holidays
except ImportError:
    holidays = None


DB_PATH = os.environ.get("DB_PATH", "/data/swm_pool_utility.db")
MODEL_DIR = os.environ.get("MODEL_DIR", "/models")
DIRECTION_THRESHOLD = 2.0  # pp — delta_1h must exceed this to show an up/down arrow
PREDICTION_HORIZON_HOURS = 2
PREDICTION_INTERVAL_MINUTES = 10
PREDICTION_INTERVALS_1H = 60 // PREDICTION_INTERVAL_MINUTES  # 1 hour ahead  → preds index PREDICTION_INTERVALS_1H-1
PREDICTION_INTERVALS_2H = 120 // PREDICTION_INTERVAL_MINUTES  # 2 hours ahead → preds index PREDICTION_INTERVALS_2H-1
MODEL_VERSION = datetime.now().strftime("%Y%m%d%H%M%S")
FEATURE_COLS = [
    "hour", "minute", "day_of_week", "day_of_year", "season",
    "is_weekend", "is_holiday", "days_to_holiday",
    "temperature", "wind_speed", "precipitation", "cloud_cover",
    "util_lag_10m", "util_lag_20m", "util_lag_30m", "util_lag_60m",
    "util_rolling_30m", "util_rolling_1h",
    "util_change_10m", "util_change_30m", "util_momentum",
    "util_accel",
    "avg_weekday_delta",
]

BERLIN = ZoneInfo("Europe/Berlin")
SLOTS_PER_WEEK = 1008  # 7 days × 144 ten-minute slots

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


# ── Historical weekday-average trend ─────────────────────────────────────────

def load_avg_cache():
    """Load daily_avg_cache into a dict keyed by (pool_name, slot_index)."""
    conn = sqlite3.connect(DB_PATH)
    rows = conn.execute("""
        SELECT p.name, dac.slot_index, dac.mean_utilization
        FROM daily_avg_cache dac JOIN pools p ON dac.pool_id = p.id
    """).fetchall()
    conn.close()
    return {(pool, slot): mean for pool, slot, mean in rows}


def _berlin_slot_delta(pool_name, ft_utc, avg_cache, steps_ahead=3):
    """Typical utilization change over `steps_ahead` slots (30 min at 10-min resolution)
    for this weekday+time, derived from daily_avg_cache.  Falls back to 0.0 when no
    historical data exists for this (pool, slot) pair."""
    dt_berlin = ft_utc.tz_localize("UTC").tz_convert(BERLIN)
    dow = dt_berlin.weekday()          # Mon=0..Sun=6, same as aggregator
    slot = dow * 144 + (dt_berlin.hour * 60 + dt_berlin.minute) // 10
    slot_ahead = (slot + steps_ahead) % SLOTS_PER_WEEK
    cur = avg_cache.get((pool_name, slot))
    ahe = avg_cache.get((pool_name, slot_ahead))
    return (ahe - cur) if (cur is not None and ahe is not None) else 0.0


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
        path = os.path.join(MODEL_DIR, fname)
        try:
            model = joblib.load(path)
            models[raw] = model
            print(f"  Loaded model: {raw}")
        except Exception as e:
            print(f"  Failed to load {fname}: {e}")

    # Map normalized names to DB pool names
    conn = sqlite3.connect(DB_PATH)
    db_pools = [r[0] for r in conn.execute("SELECT name FROM pools").fetchall()]
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


# ── Load latest readings per pool ────────────────────────────────────────────

def load_latest_readings():
    conn = sqlite3.connect(DB_PATH)
    df = pd.read_sql_query("""
        SELECT p.name, tp.dtime, tp.utility
        FROM track_pools tp JOIN pools p ON tp.pool_id = p.id
        ORDER BY p.name, tp.dtime DESC
    """, conn, parse_dates=["dtime"])
    conn.close()

    if df["dtime"].dt.tz is not None:
        df["dtime"] = df["dtime"].dt.tz_convert("UTC").dt.tz_localize(None)
    df["utilization"] = 100 - df["utility"]

    latest = {}
    for pool_name, grp in df.groupby("name"):
        grp = grp.sort_values("dtime")
        readings = grp.tail(40).sort_values("dtime")
        latest[pool_name] = readings.tail(40).reset_index(drop=True)
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

def build_features(pool_name, ft, wf, readings, hol_set, avg_cache):
    util_lag_10m = readings[readings["dtime"] <= ft - timedelta(minutes=10)]["utilization"]
    util_lag_20m = readings[readings["dtime"] <= ft - timedelta(minutes=20)]["utilization"]
    util_lag_30m = readings[readings["dtime"] <= ft - timedelta(minutes=30)]["utilization"]
    util_lag_60m = readings[readings["dtime"] <= ft - timedelta(minutes=60)]["utilization"]

    util_t10 = util_lag_10m.iloc[-1] if len(util_lag_10m) > 0 else readings["utilization"].iloc[-1]
    util_t20 = util_lag_20m.iloc[-1] if len(util_lag_20m) > 0 else util_t10
    util_t30 = util_lag_30m.iloc[-1] if len(util_lag_30m) > 0 else util_t20
    util_t60 = util_lag_60m.iloc[-1] if len(util_lag_60m) > 0 else util_t30

    # Shorter rolling windows - 30min and 1h instead of 3h
    recent_30m = readings[readings["dtime"] >= ft - timedelta(minutes=30)]["utilization"]
    util_rolling_30m = recent_30m.mean() if len(recent_30m) > 0 else util_t10

    recent_1h = readings[readings["dtime"] >= ft - timedelta(hours=1)]["utilization"]
    util_rolling_1h = recent_1h.mean() if len(recent_1h) > 0 else util_t10

    return {
        "hour": ft.hour,
        "minute": ft.minute,
        "day_of_week": ft.weekday(),
        "day_of_year": ft.dayofyear,
        "is_weekend": 1 if ft.weekday() >= 5 else 0,
        "season": 0 if ft.dayofyear <= 59 or ft.dayofyear > 334 else (1 if ft.dayofyear <= 151 else (2 if ft.dayofyear <= 243 else 3)),
        "is_holiday": is_holiday(ft, hol_set),
        "days_to_holiday": days_to_nearest_holiday(ft, hol_set),
        "temperature": wf["temperature"],
        "wind_speed": wf["wind_speed"],
        "precipitation": wf["precipitation"],
        "cloud_cover": wf["cloud_cover"],
        "util_lag_10m": util_t10,
        "util_lag_20m": util_t20,
        "util_lag_30m": util_t30,
        "util_lag_60m": util_t60,
        "util_rolling_30m": util_rolling_30m,
        "util_rolling_1h": util_rolling_1h,
        "util_change_10m": util_t10 - util_t20,
        "util_change_30m": util_t10 - util_t30,
        "util_momentum": util_t10 - util_rolling_1h,
        "util_accel": (util_t10 - util_t20) - (util_t20 - util_t30),  # 2nd derivative
        "avg_weekday_delta": _berlin_slot_delta(pool_name, ft, avg_cache),
    }


def predict_pool(pool_name, model, latest_readings, weather_df):
    readings = latest_readings.get(pool_name)
    if readings is None or len(readings) < 3:
        return None

    weather_df = weather_df.sort_values("dtime")
    now_utc = pd.Timestamp.now("UTC").floor("10min").tz_localize(None)
    future_times = future_timestamps(now_utc, PREDICTION_HORIZON_HOURS, PREDICTION_INTERVAL_MINUTES)

    hol_set = get_german_holidays([now_utc.year, now_utc.year + 1])
    avg_cache = load_avg_cache()

    current_util = float(readings["utilization"].iloc[-1])

    # ── Autoregressive chaining ───────────────────────────────────────────────
    # Root cause of the U / inverted-U shape in predictions:
    #
    # The old approach batch-predicted all N future steps from the same frozen
    # snapshot of actual readings.  For step t+20m, util_lag_10m was still the
    # current actual reading (no predicted point existed at t+10m yet).  Only
    # the temporal features (hour, minute) differed between steps, so the model
    # had to produce deltas from an inconsistent context — causing oscillation
    # between positive and negative deltas across the horizon.
    #
    # Fix: after each step, append the predicted value to an augmented copy of
    # the readings.  The next step then sees a lag window that reflects where
    # the model predicted the pool to be, not a frozen snapshot of the past.
    #
    # This is consistent with the training objective (delta = util_t − util_t-10m):
    # at step k, base_util is the predicted value from step k-1, so
    # pred_util = base_util + delta is exactly the same relationship.
    #
    # No retraining required — the model still predicts Δ(utilisation); only
    # the lag features it receives are now internally consistent across steps.
    augmented = readings[["dtime", "utilization"]].copy()

    preds = []
    for ft in future_times:
        wf_row = weather_df[weather_df["dtime"] <= ft]
        # Fall back to the most recent available forecast rather than skipping,
        # so a momentary forecast gap does not break the chain.
        if wf_row.empty:
            wf_row = weather_df
        wf = wf_row.iloc[-1]

        features = build_features(pool_name, ft, wf, augmented, hol_set, avg_cache)
        X = np.array([features[c] for c in FEATURE_COLS]).reshape(1, -1)

        # base_util mirrors what build_features used as util_lag_10m for ft,
        # keeping the delta→absolute conversion consistent with training.
        recent_for_base = augmented[augmented["dtime"] <= ft - timedelta(minutes=10)]
        base_util = (
            float(recent_for_base["utilization"].iloc[-1])
            if len(recent_for_base) > 0
            else current_util
        )

        delta = float(model.predict(X)[0])
        pred_util = float(np.clip(base_util + delta, 0, 100))
        preds.append(pred_util)

        # Extend the history window with this prediction so the next step's
        # lag features (util_lag_10m, util_rolling_30m, util_momentum, …)
        # reflect the predicted trajectory rather than the frozen past.
        augmented = pd.concat(
            [augmented, pd.DataFrame({"dtime": [ft], "utilization": [pred_util]})],
            ignore_index=True,
        )

    if len(preds) < PREDICTION_INTERVALS_2H:
        return None

    pred_1h = float(preds[PREDICTION_INTERVALS_1H - 1])
    pred_2h = float(preds[PREDICTION_INTERVALS_2H - 1])
    delta_1h = pred_1h - current_util
    delta_2h = pred_2h - current_util
    trend_strength = (abs(delta_1h) + abs(delta_2h)) / 2

    if delta_1h > DIRECTION_THRESHOLD:    direction = "up"
    elif delta_1h < -DIRECTION_THRESHOLD: direction = "down"
    else:                                 direction = "stable"

    # Full prediction series — one value per step over the horizon.
    # Stored as JSON so the API and frontend can render every step.
    pred_series = [round(float(v), 1) for v in preds]

    return {
        "pool_name": pool_name,
        "current_util": round(current_util, 1),
        "pred_1h": round(pred_1h, 1),
        "pred_2h": round(pred_2h, 1),
        "delta_1h": round(delta_1h, 1),
        "delta_2h": round(delta_2h, 1),
        "trend_strength": round(trend_strength, 1),
        "trend_direction": direction,
        "pred_series": pred_series,
    }


# ── Save predictions ─────────────────────────────────────────────────────────

def save_predictions(pred_dict):
    if pred_dict is None:
        return

    conn = sqlite3.connect(DB_PATH)
    now = datetime.utcnow().strftime("%Y-%m-%d %H:%M:%S")
    try:
        conn.execute(
            "INSERT OR REPLACE INTO predictions "
            "(pool_name, current_util, pred_1h, pred_2h, delta_1h, delta_2h, trend_strength, trend_direction, model_version, created_at, pred_series) "
            "VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
            (
                pred_dict["pool_name"],
                pred_dict["current_util"],
                pred_dict["pred_1h"],
                pred_dict["pred_2h"],
                pred_dict["delta_1h"],
                pred_dict["delta_2h"],
                pred_dict["trend_strength"],
                pred_dict["trend_direction"],
                MODEL_VERSION,
                now,
                json.dumps(pred_dict.get("pred_series", [])),
            ),
        )
        conn.commit()
    except Exception as e:
        print(f"  Insert failed: {e}")
    conn.close()


# ── Run one prediction cycle ─────────────────────────────────────────────────

def model_mtimes():
    """Return a dict of {filename: mtime} for all .joblib files in MODEL_DIR."""
    if not os.path.isdir(MODEL_DIR):
        return {}
    return {
        f: os.path.getmtime(os.path.join(MODEL_DIR, f))
        for f in os.listdir(MODEL_DIR)
        if f.endswith(".joblib")
    }


def run_prediction_cycle(models, latest_readings, weather_df):
    count = 0
    for pool_name, model in sorted(models.items()):
        pred = predict_pool(pool_name, model, latest_readings, weather_df)
        if pred is not None:
            save_predictions(pred)
            count += 1
    print(f"Prediction cycle complete: {count} pools predicted")


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
        known_mtimes = model_mtimes()
        while True:
            try:
                current_mtimes = model_mtimes()
                if current_mtimes != known_mtimes:
                    print(f"[{datetime.now()}] Model files changed — reloading models...")
                    models = load_models()
                    known_mtimes = current_mtimes
                    print(f"Reloaded {len(models)} model(s)")
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
