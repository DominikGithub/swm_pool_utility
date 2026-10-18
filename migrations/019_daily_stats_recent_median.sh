#!/usr/bin/env bash
# =============================================================================
# Migration 019: "Lately typical" line for the single-pool live chart
# =============================================================================
# Background
# ----------
# The live chart's grey dashed line showed the Daily Statistics median for the
# same weekday and time, taken over every recorded week.  That median has no
# notion of season: in October 2026 82–96% of the indoor pools' readings fell
# below it, while in May most fell above it.
#
# The line now shows what has been typical lately: the same median (closure
# days excluded, 50-minute window around each 10-minute slot) over the 28 full
# days before today (Berlin time) only.  Tested out of sample on the last four
# weeks, the share of readings below the line dropped from 78–96% to 52–68%.
#
# What it does
# ------------
# Adds one nullable column to daily_avg_cache:
#
#     recent_median_utilization
#
# and rebuilds the cache so it fills.  The aggregator also writes the columns
# of migration 018 (p25_, median_, p75_utilization), so any of those that are
# missing are added as well: this migration works on its own, whether or not
# 018 ran before.  All other columns are unchanged.
#
# Usage
# -----
#   docker compose build
#   bash migrations/019_daily_stats_recent_median.sh            # apply
#   bash migrations/019_daily_stats_recent_median.sh --dry-run  # preview only
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
      echo "Usage: bash migrations/019_daily_stats_recent_median.sh [--dry-run]" >&2
      exit 2
      ;;
  esac
done

echo "============================================================"
echo " Migration 019: Lately-typical line for the live chart"
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
  echo "[2/4] Adding missing statistics columns to daily_avg_cache..."
fi

docker compose run --rm --no-deps \
  --entrypoint python ml-training - "$DRY_RUN" <<'PY'
import os
import sqlite3
import sys

DRY_RUN = sys.argv[1] == "true"
DB = os.environ.get("DB_PATH", "/data/swm_pool_utility.db")
# 018's percentile columns first: the aggregator writes all four.
COLUMNS = ("p25_utilization", "median_utilization", "p75_utilization", "recent_median_utilization")

conn = sqlite3.connect(DB, timeout=30)
conn.execute("PRAGMA busy_timeout = 30000")
try:
    existing = {row[1] for row in conn.execute("PRAGMA table_info(daily_avg_cache)")}
    missing = [c for c in COLUMNS if c not in existing]
    for column in COLUMNS:
        state = "to add" if column in missing else "already present"
        print(f"  {column:<26s} {state}")

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
  echo " Migration 019 preview complete."
  echo "============================================================"
  exit 0
fi

echo ""
echo "[3/4] Rebuilding daily_avg_cache with the lately-typical median..."
docker compose run --rm --no-deps --entrypoint /app/daily-stats daily-stats --once
echo "  Cache rebuilt."

trap - EXIT
echo ""
echo "[4/4] Starting services..."
docker compose up -d
echo "  Services started."

echo ""
echo "============================================================"
echo " Migration 019 complete."
echo "============================================================"
