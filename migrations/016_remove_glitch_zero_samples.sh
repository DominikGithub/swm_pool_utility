#!/usr/bin/env bash
# =============================================================================
# Migration 016: Remove site-wide-glitch 0% samples from track_pools
# =============================================================================
# Background
# ----------
# The SWM capacity widget intermittently serves 0% "capacity remaining" for
# pools that are plainly not full.  Pools in the scraper's inverted-convention
# set have their 0 mapped to 100 and so absorb the glitch invisibly, but the
# remaining pools (Olympia-Schwimmhalle, Cosimawellenbad, Nordbad, Bad
# Giesing-Harlaching, Dante-Winter-Warmfreibad) stored the raw 0.  Each one is
# a 100-point cliff in the history graphs, and it also drags down
# daily_avg_cache and the ML training targets.
#
# Observed example — 2026-10-17 07:04:46, five pools in the same instant:
#
#     pool                      prev  value  next
#     Bad Giesing-Harlaching      93      0    94
#     Cosimawellenbad             93      0    93
#     Dante-Winter-Warmfreibad    97      0    98
#     Nordbad                     91      0    93
#     Olympia-Schwimmhalle        96      0    96
#
# Five pools across the city cannot reach exactly 0% free capacity in the same
# ten-minute slot and recover.  The longest observed episode ran 30 minutes
# through midday (2026-10-12 12:50 → 13:10).
#
# What it does
# ------------
# Deletes only 0% samples that the pool could not physically have reached: a
# run of zeros is removed when the reading immediately before it was above
# SPIKE_FLOOR (default 60) and no older than LOOKBACK_MIN (default 60) minutes.
#
# NOT deleted: zeros approached gradually (93 → … → 20 → 5 → 1 → 0).  Those are
# real full-capacity events on hot May/June afternoons and must stay.  This is
# the same rule the pool-scraper now applies at write time via
# dropImplausibleZeros, so history and new data are judged identically.
#
# Rows are removed rather than rewritten: the site never reported a real value,
# and a single missing ten-minute point is bridged invisibly by the charts,
# whereas a 0 reads as "completely full".
#
# Afterwards daily_avg_cache is rebuilt (daily-stats --once recomputes every
# slot from scratch), so the glitch is cleared out of the statistics too.
#
# Usage
# -----
#   bash migrations/016_remove_glitch_zero_samples.sh            # apply
#   bash migrations/016_remove_glitch_zero_samples.sh --dry-run  # preview only
#
# Idempotency
# -----------
# Safe to run repeatedly: once the glitch rows are gone there is nothing left
# matching the rule, and the cache rebuild is a pure recomputation.
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$PROJECT_DIR"

SPIKE_FLOOR="${SPIKE_FLOOR:-60}"
LOOKBACK_MIN="${LOOKBACK_MIN:-60}"

DRY_RUN=false
for arg in "$@"; do
  case "$arg" in
    --dry-run) DRY_RUN=true ;;
    *)
      echo "Unknown argument: $arg" >&2
      echo "Usage: bash migrations/016_remove_glitch_zero_samples.sh [--dry-run]" >&2
      exit 2
      ;;
  esac
done

echo "============================================================"
echo " Migration 016: Remove glitch 0% samples from track_pools"
echo "============================================================"
echo " spike floor : previous reading must exceed ${SPIKE_FLOOR}%"
echo " lookback    : previous reading at most ${LOOKBACK_MIN} min old"
echo ""

if [[ "$DRY_RUN" == "true" ]]; then
  echo "[1/1] Previewing affected rows (services left running)..."
else
  echo "[1/4] Stopping services to prevent concurrent database writes..."
  docker compose stop
  echo "  Services stopped."
  echo ""
  echo "[2/4] Removing glitch 0% samples..."
fi

docker compose run --rm --no-deps \
  --entrypoint python ml-training - "$DRY_RUN" "$SPIKE_FLOOR" "$LOOKBACK_MIN" <<'PY'
import os
import sqlite3
import sys
from collections import Counter
from datetime import datetime, timedelta

DRY_RUN = sys.argv[1] == "true"
SPIKE_FLOOR = int(sys.argv[2])
LOOKBACK = timedelta(minutes=int(sys.argv[3]))
DB = os.environ.get("DB_PATH", "/data/swm_pool_utility.db")


def parse(dtime):
    for fmt in ("%Y-%m-%d %H:%M:%S", "%Y-%m-%d %H:%M:%S.%f", "%Y-%m-%dT%H:%M:%SZ"):
        try:
            return datetime.strptime(dtime, fmt)
        except ValueError:
            continue
    return None


conn = sqlite3.connect(DB, timeout=30)
conn.execute("PRAGMA busy_timeout = 30000")
if not DRY_RUN:
    conn.execute("BEGIN IMMEDIATE")

try:
    names = dict(conn.execute("SELECT id, name FROM pools").fetchall())
    total = conn.execute("SELECT COUNT(*) FROM track_pools").fetchone()[0]

    # One ordered pass per pool: a zero run is judged by the sample that
    # precedes it, exactly as the scraper judges a live reading.
    glitch_ids = []
    glitch_by_pool = Counter()
    glitch_by_month = Counter()
    kept_runs = 0
    kept_rows = 0

    cur_pool = None
    prev_val = prev_at = None
    in_run = run_is_glitch = False

    for row_id, pool_id, dtime, utility in conn.execute(
        "SELECT id, pool_id, dtime, utility FROM track_pools ORDER BY pool_id, dtime"
    ):
        if pool_id != cur_pool:
            cur_pool, prev_val, prev_at, in_run = pool_id, None, None, False

        at = parse(dtime)

        if utility == 0:
            if not in_run:
                in_run = True
                run_is_glitch = (
                    prev_val is not None
                    and prev_val > SPIKE_FLOOR
                    and at is not None
                    and prev_at is not None
                    and (at - prev_at) <= LOOKBACK
                )
                if not run_is_glitch:
                    kept_runs += 1
            if run_is_glitch:
                glitch_ids.append(row_id)
                glitch_by_pool[names.get(pool_id, f"pool {pool_id}")] += 1
                glitch_by_month[dtime[:7]] += 1
            else:
                kept_rows += 1
        else:
            in_run = False

        prev_val, prev_at = utility, at

    print(f"  track_pools rows            : {total:,}")
    print(f"  glitch 0% rows to remove    : {len(glitch_ids):,}")
    print(f"  genuine 0% rows kept        : {kept_rows:,} in {kept_runs} run(s)")
    print()

    if glitch_by_pool:
        print("  By pool:")
        for name, count in glitch_by_pool.most_common():
            print(f"    {name:<32s} {count:>5} rows")
        print()
        print("  By month:")
        for month in sorted(glitch_by_month):
            print(f"    {month}  {glitch_by_month[month]:>5} rows")
        print()

    if DRY_RUN:
        print("  Dry run only; no rows were deleted.")
    elif glitch_ids:
        conn.executemany(
            "DELETE FROM track_pools WHERE id = ?", [(i,) for i in glitch_ids]
        )
        conn.commit()
        print(f"  Deleted {len(glitch_ids):,} glitch 0% rows.")
    else:
        print("  Nothing to delete; history is already clean.")

    print()
    print("  Migration 016 complete.")
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
  echo " Migration 016 preview complete."
  echo "============================================================"
  exit 0
fi

echo ""
echo "[3/4] Rebuilding daily_avg_cache from the cleaned history..."
docker compose run --rm --no-deps --entrypoint /app/daily-stats daily-stats --once
echo "  Cache rebuilt."

echo ""
echo "[4/4] Starting services..."
docker compose up -d
echo "  Services started."

echo ""
echo "============================================================"
echo " Migration 016 complete."
echo "============================================================"
