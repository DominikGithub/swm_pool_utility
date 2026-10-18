#!/usr/bin/env bash
# =============================================================================
# Migration 018: Percentile band for the Daily Statistics view
# =============================================================================
# Background
# ----------
# The Daily Statistics chart drew mean ± 1σ per weekday slot.  That band was
# labelled a confidence interval but measured week-to-week spread, mostly
# tracked the mean, was clipped at 0% for outdoor pools, and counted closure
# days (season end, maintenance, early closing) as 0% readings.
#
# The chart now shows the median week with a P25–P75 band (the middle half of
# observed weeks).  daily-stats computes both from readings outside closures,
# judged per day against frontend/src/data/pool-opening-hours.json:
#
#   * a whole open day reading 0%, or
#   * 0% for at least 3 h on a day that was otherwise at least 10% busy.
#
# What it does
# ------------
# Adds three nullable columns to daily_avg_cache:
#
#     p25_utilization, median_utilization, p75_utilization
#
# mean_utilization and std_dev are unchanged (the ML feature avg_weekday_delta
# reads mean_utilization), then the cache is rebuilt so the new columns fill.
#
# Usage
# -----
#   docker compose build
#   bash migrations/018_daily_stats_percentile_band.sh            # apply
#   bash migrations/018_daily_stats_percentile_band.sh --dry-run  # preview only
#
# Idempotency
# -----------
# Safe to run repeatedly: existing columns are skipped and the cache rebuild
# recomputes every slot.  If any step fails, the services are started again
# before the script exits, so the dashboard is never left down.
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$PROJECT_DIR"

DRY_RUN=false
for arg in "$@"; do
  case "$arg" in
    --dry-run) DRY_RUN=true ;;
    *)
      echo "Unknown argument: $arg" >&2
      echo "Usage: bash migrations/018_daily_stats_percentile_band.sh [--dry-run]" >&2
      exit 2
      ;;
  esac
done

echo "============================================================"
echo " Migration 018: Percentile band for Daily Statistics"
echo "============================================================"
echo ""

if [[ "$DRY_RUN" == "true" ]]; then
  echo "[1/1] Previewing schema change (services left running)..."
else
  echo "[1/4] Stopping services to prevent concurrent database writes..."
  docker compose stop
  echo "  Services stopped."
  # From here on, a failed step must not leave the dashboard down.
  trap 'echo ""; echo "  Migration failed; starting services again so the dashboard stays up..."; docker compose up -d' EXIT
  echo ""
  echo "[2/4] Adding percentile columns to daily_avg_cache..."
fi

docker compose run --rm --no-deps \
  --entrypoint python ml-training - "$DRY_RUN" <<'PY'
import os
import sqlite3
import sys

DRY_RUN = sys.argv[1] == "true"
DB = os.environ.get("DB_PATH", "/data/swm_pool_utility.db")
COLUMNS = ("p25_utilization", "median_utilization", "p75_utilization")

conn = sqlite3.connect(DB, timeout=30)
conn.execute("PRAGMA busy_timeout = 30000")
try:
    existing = {row[1] for row in conn.execute("PRAGMA table_info(daily_avg_cache)")}
    missing = [c for c in COLUMNS if c not in existing]
    for column in COLUMNS:
        state = "to add" if column in missing else "already present"
        print(f"  {column:<20s} {state}")

    if DRY_RUN:
        print("\n  Dry run only; schema left unchanged.")
    elif missing:
        for column in missing:
            conn.execute(f"ALTER TABLE daily_avg_cache ADD COLUMN {column} REAL")
        conn.commit()
        print(f"\n  Added {len(missing)} column(s).")
    else:
        print("\n  Nothing to add; schema is already up to date.")
finally:
    conn.close()
PY

if [[ "$DRY_RUN" == "true" ]]; then
  echo ""
  echo "============================================================"
  echo " Migration 018 preview complete."
  echo "============================================================"
  exit 0
fi

echo ""
echo "[3/4] Rebuilding daily_avg_cache with the percentile band..."
docker compose run --rm --no-deps --entrypoint /app/daily-stats daily-stats --once
echo "  Cache rebuilt."

trap - EXIT
echo ""
echo "[4/4] Starting services..."
docker compose up -d
echo "  Services started."

echo ""
echo "============================================================"
echo " Migration 018 complete."
echo "============================================================"
