#!/usr/bin/env bash
# =============================================================================
# Migration 013: Normalize closed-hours pool utilization
# =============================================================================
#
# track_pools.utility stores capacity remaining, while the dashboard displays
# utilization as 100 - utility. A pool closed under its official schedule must
# therefore be stored as utility=100 so it displays as 0% utilization.
#
# This migration reads frontend/src/data/pool-opening-hours.json, the existing
# source of truth used by the heatmap. It updates every historical row outside
# the corresponding Berlin-local opening interval to utility=100, preserving
# all values during scheduled opening hours. The daily statistics cache is
# cleared after a change so it is rebuilt from the corrected history.
#
# Usage:
#   bash migrations/013_normalize_closed_hours_utility.sh
#   bash migrations/013_normalize_closed_hours_utility.sh --dry-run
#
# Idempotency:
#   Safe to run repeatedly. Rows already stored as utility=100 are skipped.
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
SCHEDULE_FILE="$PROJECT_DIR/frontend/src/data/pool-opening-hours.json"
cd "$PROJECT_DIR"

DRY_RUN=false
for arg in "$@"; do
  case "$arg" in
    --dry-run) DRY_RUN=true ;;
    *)
      echo "Unknown argument: $arg" >&2
      echo "Usage: bash migrations/013_normalize_closed_hours_utility.sh [--dry-run]" >&2
      exit 2
      ;;
  esac
done

if [[ ! -f "$SCHEDULE_FILE" ]]; then
  echo "Opening-hours schedule not found: $SCHEDULE_FILE" >&2
  exit 1
fi

echo "============================================================"
echo " Migration 013: Normalize closed-hours pool utilization"
echo "============================================================"
echo " Schedule source: frontend/src/data/pool-opening-hours.json"
echo ""

if [[ "$DRY_RUN" == "true" ]]; then
  echo "[1/1] Previewing affected rows..."
else
  echo "[1/3] Stopping services to prevent concurrent database writes..."
  docker compose stop
  echo "  Services stopped."
  echo ""
  echo "[2/3] Normalizing historical pool data..."
fi

docker compose run --rm --no-deps \
  --volume "$SCHEDULE_FILE:/schedules/pool-opening-hours.json:ro" \
  --entrypoint python ml-training - "$DRY_RUN" <<'PY'
import json
import os
import sqlite3
import sys
from collections import defaultdict
from datetime import datetime, timezone
from zoneinfo import ZoneInfo

DRY_RUN = sys.argv[1] == "true"
DB = os.environ.get("DB_PATH", "/data/swm_pool_utility.db")
BERLIN = ZoneInfo("Europe/Berlin")

with open("/schedules/pool-opening-hours.json", encoding="utf-8") as source:
    schedules = json.load(source)


def schedule_for(pool_name):
    lower_name = pool_name.lower()
    for entry in schedules:
        if any(match in lower_name for match in entry["match"]):
            days = entry["days"]
            if len(days) != 7 or any(len(day) != 2 for day in days):
                raise RuntimeError(f"invalid schedule for {pool_name!r}")
            return days
    return None


def to_utc(raw_dtime):
    parsed = datetime.fromisoformat(raw_dtime)
    if parsed.tzinfo is None:
        return parsed.replace(tzinfo=timezone.utc)
    return parsed.astimezone(timezone.utc)


conn = sqlite3.connect(DB, timeout=30)
conn.execute("PRAGMA busy_timeout = 30000")
if not DRY_RUN:
    conn.execute("BEGIN IMMEDIATE")

try:
    rows = conn.execute("""
        SELECT tp.id, p.name, tp.dtime, tp.utility
        FROM track_pools tp JOIN pools p ON p.id = tp.pool_id
        ORDER BY tp.dtime
    """)

    affected_ids = []
    affected_by_pool = defaultdict(lambda: [0, None, None])
    unknown_pools = set()

    for row_id, pool_name, raw_dtime, utility in rows:
        schedule = schedule_for(pool_name)
        if schedule is None:
            unknown_pools.add(pool_name)
            continue

        local_time = to_utc(raw_dtime).astimezone(BERLIN)
        slot = (local_time.hour * 60 + local_time.minute) // 30
        first_open_slot, first_closed_slot = schedule[local_time.weekday()]
        is_closed = slot < first_open_slot or slot >= first_closed_slot

        if is_closed and utility != 100:
            affected_ids.append((row_id,))
            summary = affected_by_pool[pool_name]
            summary[0] += 1
            summary[1] = raw_dtime if summary[1] is None else min(summary[1], raw_dtime)
            summary[2] = raw_dtime if summary[2] is None else max(summary[2], raw_dtime)

    if unknown_pools:
        raise RuntimeError(
            "opening-hours schedule missing for: " + ", ".join(sorted(unknown_pools))
        )

    print(f"  Rows outside official opening hours: {len(affected_ids):,}")
    for pool_name, (count, first, last) in sorted(affected_by_pool.items()):
        print(f"    {pool_name:<35s} {count:>6,}  {first} to {last}")

    if DRY_RUN:
        print("  Dry run only; no data was changed.")
    elif affected_ids:
        conn.executemany(
            "UPDATE track_pools SET utility = 100 WHERE id = ? AND utility != 100",
            affected_ids,
        )
        cache_rows = conn.execute("SELECT COUNT(*) FROM daily_avg_cache").fetchone()[0]
        conn.execute("DELETE FROM daily_avg_cache")
        conn.commit()
        print("  Stored closed rows as utility=100 (displayed utilization=0%).")
        print(f"  Cleared {cache_rows:,} daily_avg_cache rows for rebuild.")
    else:
        conn.commit()
        print("  No rows required correction.")
except Exception:
    if not DRY_RUN:
        conn.rollback()
    raise
finally:
    conn.close()
PY

if [[ "$DRY_RUN" == "true" ]]; then
  echo ""
  echo "============================================================"
  echo " Migration 013 preview complete."
  echo "============================================================"
  exit 0
fi

echo ""
echo "[3/3] Starting services..."
docker compose up -d
echo "  Services started; daily-stats rebuilds the cleared cache on startup."

echo ""
echo "============================================================"
echo " Migration 013 complete."
echo "============================================================"
