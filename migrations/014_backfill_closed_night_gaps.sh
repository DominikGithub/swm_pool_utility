#!/usr/bin/env bash
# =============================================================================
# Migration 014: Backfill confirmed closed-hours pool gaps
# =============================================================================
#
# track_pools.utility stores remaining capacity, while the dashboard displays
# utilization as 100 - utility. A closed pool must therefore be stored as
# utility=100 so it displays as 0% utilization.
#
# The scraper previously skipped named closed cards, leaving gaps before
# opening and after closing for pools that were otherwise active. This
# migration fills only those gaps under a deliberately conservative rule:
#
#   * The pool must have at least three samples in the selected recent window.
#     Seasonal pools with no recent data are excluded.
#   * The missing timestamp must be bracketed by real samples from that same
#     pool within 24 hours before and after it.
#   * The pool must have an official opening-hours schedule and the timestamp
#     must fall outside that schedule in Europe/Berlin time.
#   * The timestamp must already exist for another tracked pool. No new time
#     grid is invented.
#   * Existing rows are never changed.
#
# It is idempotent: rerunning with --apply inserts only still-missing rows.
#
# Usage:
#   bash migrations/014_backfill_closed_night_gaps.sh
#   bash migrations/014_backfill_closed_night_gaps.sh --dry-run
#   bash migrations/014_backfill_closed_night_gaps.sh --days 14
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
SCHEDULE_FILE="$PROJECT_DIR/frontend/src/data/pool-opening-hours.json"
cd "$PROJECT_DIR"

DAYS=7
APPLY=true

while [[ $# -gt 0 ]]; do
  case "$1" in
    --dry-run)
      APPLY=false
      shift
      ;;
    --days)
      if [[ $# -lt 2 || ! "$2" =~ ^[1-9][0-9]*$ ]]; then
        echo "--days requires a positive integer" >&2
        exit 2
      fi
      DAYS="$2"
      shift 2
      ;;
    *)
      echo "Unknown argument: $1" >&2
      echo "Usage: bash migrations/014_backfill_closed_night_gaps.sh [--dry-run] [--days N]" >&2
      exit 2
      ;;
  esac
done

MODE="dry run"
if [[ "$APPLY" == "true" ]]; then
  MODE="apply"
fi

if [[ ! -f "$SCHEDULE_FILE" ]]; then
  echo "Opening-hours schedule not found: $SCHEDULE_FILE" >&2
  exit 1
fi

echo "============================================================"
echo " Migration 014: Backfill confirmed closed-hours pool gaps"
echo "============================================================"
echo " Mode: $MODE"
echo " Window: last $DAYS days of available pool data"
echo " Schedule: frontend/src/data/pool-opening-hours.json"
echo ""

docker compose run --rm --no-deps -T \
  -e "BACKFILL_DAYS=$DAYS" \
  -e "BACKFILL_APPLY=$APPLY" \
  --volume "$SCHEDULE_FILE:/schedules/pool-opening-hours.json:ro" \
  --entrypoint python ml-training - <<'PY'
import json
import os
import sqlite3
from collections import defaultdict
from datetime import datetime, timezone
from zoneinfo import ZoneInfo

DAYS = int(os.environ["BACKFILL_DAYS"])
APPLY = os.environ["BACKFILL_APPLY"] == "true"
DB = os.environ.get("DB_PATH", "/data/swm_pool_utility.db")
BERLIN = ZoneInfo("Europe/Berlin")


with open("/schedules/pool-opening-hours.json", encoding="utf-8") as source:
    schedules = json.load(source)


def to_berlin(raw_dtime):
    return datetime.strptime(raw_dtime, "%Y-%m-%d %H:%M:%S").replace(
        tzinfo=timezone.utc
    ).astimezone(BERLIN)


def schedule_for(pool_name):
    lower_name = pool_name.lower()
    for entry in schedules:
        if any(match in lower_name for match in entry["match"]):
            return entry["days"]
    return None


conn = sqlite3.connect(DB, timeout=30)
conn.execute("PRAGMA busy_timeout = 30000")

try:
    latest = conn.execute("SELECT MAX(dtime) FROM track_pools").fetchone()[0]
    if latest is None:
        raise RuntimeError("track_pools contains no data")

    cutoff = conn.execute(
        "SELECT datetime(?, ?)", (latest, f"-{DAYS} days")
    ).fetchone()[0]

    if APPLY:
        conn.execute("BEGIN IMMEDIATE")

    # A pool that has not been sampled recently is seasonal, retired, or
    # otherwise unknown; never create data for it.
    missing_rows = conn.execute(
        """
        WITH active_pools AS (
            SELECT pool_id
            FROM track_pools
            WHERE dtime >= ?
            GROUP BY pool_id
            HAVING COUNT(*) >= 3
        ), sample_times AS (
            SELECT DISTINCT dtime
            FROM track_pools
            WHERE dtime >= ?
        )
        SELECT active_pools.pool_id, pools.name, sample_times.dtime
        FROM active_pools
        JOIN pools ON pools.id = active_pools.pool_id
        CROSS JOIN sample_times
        LEFT JOIN track_pools existing
          ON existing.pool_id = active_pools.pool_id
         AND existing.dtime = sample_times.dtime
        WHERE existing.id IS NULL
          AND EXISTS (
              SELECT 1
              FROM track_pools previous
              WHERE previous.pool_id = active_pools.pool_id
                AND previous.dtime >= datetime(sample_times.dtime, '-24 hours')
                AND previous.dtime < sample_times.dtime
          )
          AND EXISTS (
              SELECT 1
              FROM track_pools next_sample
              WHERE next_sample.pool_id = active_pools.pool_id
                AND next_sample.dtime > sample_times.dtime
                AND next_sample.dtime <= datetime(sample_times.dtime, '+24 hours')
          )
        ORDER BY pools.name, sample_times.dtime
        """,
        (cutoff, cutoff),
    )

    inserts = []
    summary = defaultdict(lambda: [0, None, None])
    unscheduled_pools = set()
    for pool_id, pool_name, dtime in missing_rows:
        local_time = to_berlin(dtime)
        schedule = schedule_for(pool_name)
        if schedule is None:
            unscheduled_pools.add(pool_name)
            continue

        first_open_slot, first_closed_slot = schedule[local_time.weekday()]
        slot = (local_time.hour * 60 + local_time.minute) // 30
        if first_open_slot <= slot < first_closed_slot:
            continue

        inserts.append((pool_id, dtime, 100))
        pool_summary = summary[pool_name]
        pool_summary[0] += 1
        pool_summary[1] = dtime if pool_summary[1] is None else min(pool_summary[1], dtime)
        pool_summary[2] = dtime if pool_summary[2] is None else max(pool_summary[2], dtime)

    print(f"  Latest sample: {latest} UTC")
    print(f"  Recent-data cutoff: {cutoff} UTC")
    print(f"  Missing confirmed closed-night samples: {len(inserts):,}")
    for pool_name, (count, first, last) in sorted(summary.items()):
        print(f"    {pool_name:<35s} {count:>6,}  {first} to {last}")
    if unscheduled_pools:
        print("  Skipped active pools without an opening-hours schedule:")
        for pool_name in sorted(unscheduled_pools):
            print(f"    {pool_name}")

    if not APPLY:
        print("  Dry run only; no data was changed.")
    elif not inserts:
        conn.commit()
        print("  No missing closed-night samples found.")
    else:
        conn.executemany(
            "INSERT INTO track_pools (pool_id, dtime, utility) VALUES (?, ?, ?)",
            inserts,
        )
        conn.commit()
        print("  Inserted closed-night rows as utility=100 (displayed utilization=0%).")
except Exception:
    if APPLY:
        conn.rollback()
    raise
finally:
    conn.close()
PY

echo ""
echo "============================================================"
if [[ "$APPLY" == "true" ]]; then
  echo " Migration 014 complete."
else
  echo " Migration 014 preview complete."
fi
echo "============================================================"
