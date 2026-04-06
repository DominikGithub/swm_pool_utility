#!/usr/bin/env bash
# =============================================================================
# Migration 001: Fix daily_avg_cache timezone (UTC → Europe/Berlin)
# =============================================================================
# Background
# ----------
# The aggregator previously computed slot_index using SQLite's strftime() on
# raw UTC timestamps.  Because Munich uses CET (UTC+1) in winter and CEST
# (UTC+2) in summer, every slot was shifted by 1–2 hours in the daily average
# chart.
#
# The fixed aggregator now converts timestamps to Europe/Berlin local time
# before bucketing, so slot_index values change.  Old rows in daily_avg_cache
# must be deleted; the aggregator will rebuild the table correctly on its next
# run (or you can trigger an immediate rebuild — see below).
#
# Raw data (track_pools, weather) is NOT touched: it is already stored in UTC
# via SQLite's CURRENT_TIMESTAMP and requires no migration.
#
# Usage
# -----
# IMPORTANT: This migration is part of a larger change that also updated the
# API, both scrapers, and the frontend.  Rebuild and restart ALL services
# before running this script, otherwise the cache is rebuilt by the old binary.
#
#   cd /path/to/swm_pool_utility
#   docker compose build
#   docker compose up -d
#   bash migrations/001_fix_daily_avg_cache_timezone.sh --rebuild
#
# The --rebuild flag clears the cache AND immediately triggers the new binary
# to repopulate it.  Without it the cache is cleared and the hourly cron job
# inside the container will rebuild within 60 minutes.
#
# Idempotency
# -----------
# Safe to run multiple times.  Deleting an already-empty table is a no-op.
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$PROJECT_DIR"

REBUILD=false
for arg in "$@"; do
  [[ "$arg" == "--rebuild" ]] && REBUILD=true
done

# Docker Compose prefixes volume names with the project name, which defaults
# to the lowercase directory name.  Override with COMPOSE_PROJECT_NAME if needed.
COMPOSE_PROJECT="${COMPOSE_PROJECT_NAME:-$(basename "$PROJECT_DIR" | tr '[:upper:]' '[:lower:]' | tr -cd '[:alnum:]_-')}"
VOLUME="${COMPOSE_PROJECT}_db_data"

echo "============================================================"
echo " Migration 001: Fix daily_avg_cache timezone"
echo "============================================================"
echo " Project dir : $PROJECT_DIR"
echo " DB volume   : $VOLUME"
echo ""

# ---------------------------------------------------------------------------
# Step 1 — clear stale cache rows
# ---------------------------------------------------------------------------
echo "[1/2] Clearing stale daily_avg_cache rows..."

docker run --rm \
  -v "${VOLUME}:/data" \
  debian:12.9-slim \
  bash -c '
    apt-get update -qq > /dev/null 2>&1
    apt-get install -y -qq sqlite3 > /dev/null 2>&1
    COUNT=$(sqlite3 /data/swm_pool_utility.db "SELECT COUNT(*) FROM daily_avg_cache;")
    sqlite3 /data/swm_pool_utility.db "DELETE FROM daily_avg_cache;"
    echo "  Deleted ${COUNT} rows from daily_avg_cache."
  '

# ---------------------------------------------------------------------------
# Step 2 — optionally rebuild immediately
# ---------------------------------------------------------------------------
if [[ "$REBUILD" == "true" ]]; then
  echo ""
  echo "[2/2] Triggering immediate cache rebuild..."
  echo "      (requires the daily-avg-aggregator container to be running)"
  docker compose exec daily-avg-aggregator /app/aggregator --once
  echo ""
  echo "  Cache rebuilt with corrected Europe/Berlin timezone."
else
  echo ""
  echo "[2/2] Skipping immediate rebuild."
  echo "      The aggregator cron job will rebuild the cache within the next hour."
  echo "      To rebuild right now, run:"
  echo "        docker compose exec daily-avg-aggregator /app/aggregator --once"
fi

echo ""
echo "============================================================"
echo " Migration 001 complete."
echo "============================================================"
