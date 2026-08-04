#!/usr/bin/env bash
# =============================================================================
# Migration 012: Backfill missing weather history
# =============================================================================
#
# The weather collector previously ran through a cron wrapper and left gaps in
# the weather table. This one-time repair fetches the last 30 days of hourly
# Open-Meteo data and inserts only slots that have no observation within 30
# minutes. Existing collected observations are preserved.
#
# This migration is intentionally separate from the weather collector. It is
# safe to run again, but can be removed after the affected installations have
# been repaired.
#
# Usage:
#   bash migrations/012_backfill_weather_history.sh
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$PROJECT_DIR"

echo "============================================================"
echo " Migration 012: Backfill missing weather history"
echo "============================================================"
echo ""

docker compose run --rm --no-deps --entrypoint python ml-training - 30 <<'PY'
import json
import os
import sqlite3
import sys
from datetime import datetime, timedelta, timezone
from urllib.parse import urlencode
from urllib.request import urlopen

DAYS = int(sys.argv[1])
DB = os.environ.get("DB_PATH", "/data/swm_pool_utility.db")


def weather_type(code):
    if code == 0:
        return "clear"
    if code in (1, 2, 3):
        return "partly_cloudy"
    if code in (45, 48):
        return "foggy"
    if 51 <= code <= 67 or 80 <= code <= 82:
        return "rain"
    if 71 <= code <= 77 or 85 <= code <= 86:
        return "snow"
    if 95 <= code <= 99:
        return "thunderstorm"
    return "unknown"


params = {
    "latitude": "48.1372",
    "longitude": "11.5755",
    "hourly": "temperature_2m,wind_speed_10m,wind_direction_10m,precipitation,cloud_cover,weather_code",
    "past_days": DAYS,
    "forecast_days": 1,
    "timezone": "GMT",
}
url = "https://api.open-meteo.com/v1/forecast?" + urlencode(params)
with urlopen(url, timeout=30) as response:
    if response.status != 200:
        raise RuntimeError(f"Open-Meteo returned HTTP {response.status}")
    hourly = json.load(response).get("hourly", {})

fields = (
    "time",
    "temperature_2m",
    "wind_speed_10m",
    "wind_direction_10m",
    "precipitation",
    "cloud_cover",
    "weather_code",
)
lengths = {name: len(hourly.get(name, [])) for name in fields}
if not lengths["time"] or len(set(lengths.values())) != 1:
    raise RuntimeError(f"Open-Meteo returned mismatched hourly arrays: {lengths}")

conn = sqlite3.connect(DB, timeout=30)
conn.execute("PRAGMA busy_timeout = 30000")
type_ids = dict(conn.execute("SELECT type, id FROM weather_types"))
unknown_type_id = type_ids.get("unknown")
if unknown_type_id is None:
    raise RuntimeError("weather_types is missing the required 'unknown' row")

now = datetime.now(timezone.utc)
inserted = 0
preserved = 0
try:
    conn.execute("BEGIN IMMEDIATE")
    for index, raw_timestamp in enumerate(hourly["time"]):
        timestamp = datetime.strptime(raw_timestamp, "%Y-%m-%dT%H:%M").replace(tzinfo=timezone.utc)
        if timestamp >= now:
            continue

        dtime = timestamp.strftime("%Y-%m-%d %H:%M:%S")
        window_start = (timestamp - timedelta(minutes=30)).strftime("%Y-%m-%d %H:%M:%S")
        window_end = (timestamp + timedelta(minutes=30)).strftime("%Y-%m-%d %H:%M:%S")
        exists = conn.execute(
            "SELECT 1 FROM weather WHERE dtime >= ? AND dtime < ? LIMIT 1",
            (window_start, window_end),
        ).fetchone()
        if exists:
            preserved += 1
            continue

        code = hourly["weather_code"][index]
        conn.execute(
            """
            INSERT INTO weather
                (dtime, temperature, wind_speed, wind_direction, precipitation,
                 cloud_cover, weather_code, weather_type_id)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?)
            """,
            (
                dtime,
                hourly["temperature_2m"][index],
                hourly["wind_speed_10m"][index],
                hourly["wind_direction_10m"][index],
                hourly["precipitation"][index],
                hourly["cloud_cover"][index],
                code,
                type_ids.get(weather_type(code), unknown_type_id),
            ),
        )
        inserted += 1
    conn.commit()
except Exception:
    conn.rollback()
    raise
finally:
    conn.close()

print(f"Backfilled {inserted} missing hourly weather rows; preserved {preserved} existing slots.")
PY

echo ""
echo "============================================================"
echo " Migration 012 complete."
echo "============================================================"
