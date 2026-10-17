#!/usr/bin/env bash
# =============================================================================
# Migration 017: Remove false-empty ("closed") glitch samples from track_pools
# =============================================================================
# Background
# ----------
# Migration 016 removed the SWM glitch where it stores as a raw 0.  The same
# glitch also reaches pools whose 0 means "closed": the scraper's
# normalizePoolStats maps it to 100, so instead of a visible zero the history
# records a false "pool empty" reading.
#
# It cannot be judged at write time, because closing time and a glitch look
# identical in the moment.  What separates them is what happens next: a real
# closure persists until the pool reopens hours later, a glitch recovers within
# a sample or two.  Examples found in this database:
#
#     Müller'sches Volksbad  2026-10-05 10:10   76 → 100 → 62
#     Südbad                 2026-10-04 15:10   72 → 100 → 67
#     Michaelibad            2026-10-03 10:30   64 → 100 → 79
#     Südbad                 2026-05-03 14:50    2 → 100 →  2   (packed, "empty", packed)
#
# These are worse than the visible zeros: they are not spikes anyone would
# notice, they drag daily_avg_cache down, and they teach the predictor that a
# busy pool emptied for ten minutes.
#
# This affects every pool, not just the inverted-convention ones — pools
# outside that set show the same signature (Bad Giesing-Harlaching, Nordbad,
# Cosimawellenbad, Freibad West).
#
# What it does
# ------------
# Deletes a run of 100s only when ALL of the following hold, which is the same
# rule pool-scraper now applies live in correctFalseEmptySamples:
#
#   1. The run is at most MAX_RUN samples long (default 2 = 20 minutes).
#      A genuine closure lasts hours.
#   2. The readings immediately before AND after the run both show the pool
#      clearly in use (at most BUSY_MAX, default 80 = at least 20% utilized).
#      Not higher on purpose: in the 80–90 band a brief genuine closure is an
#      equally good explanation, and BUSY_MAX=90 would delete 87 more rows on
#      that guess.
#   3. Every step across the run is at most MAX_STEP_MIN minutes (default 20),
#      so a scraper gap cannot be mistaken for consecutive readings.
#
# NOT deleted: overnight closures, long closures, closures approached from an
# already-quiet pool, and anything bracketing a data gap.
#
# Rows are removed rather than rewritten, for the same reason as 016: the site
# never reported a usable value for that interval.
#
# Afterwards daily_avg_cache is rebuilt so the glitch leaves the statistics too.
#
# Usage
# -----
#   bash migrations/017_remove_false_empty_samples.sh            # apply
#   bash migrations/017_remove_false_empty_samples.sh --dry-run  # preview only
#
# Idempotency
# -----------
# Safe to run repeatedly: once the islands are gone nothing matches the rule.
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$PROJECT_DIR"

BUSY_MAX="${BUSY_MAX:-80}"
MAX_RUN="${MAX_RUN:-2}"
MAX_STEP_MIN="${MAX_STEP_MIN:-20}"

DRY_RUN=false
for arg in "$@"; do
  case "$arg" in
    --dry-run) DRY_RUN=true ;;
    *)
      echo "Unknown argument: $arg" >&2
      echo "Usage: bash migrations/017_remove_false_empty_samples.sh [--dry-run]" >&2
      exit 2
      ;;
  esac
done

echo "============================================================"
echo " Migration 017: Remove false-empty samples from track_pools"
echo "============================================================"
echo " busy threshold : bracketing readings must be <= ${BUSY_MAX}"
echo " max run        : ${MAX_RUN} sample(s)"
echo " max step       : ${MAX_STEP_MIN} min between consecutive samples"
echo ""

if [[ "$DRY_RUN" == "true" ]]; then
  echo "[1/1] Previewing affected rows (services left running)..."
else
  echo "[1/4] Stopping services to prevent concurrent database writes..."
  docker compose stop
  echo "  Services stopped."
  echo ""
  echo "[2/4] Removing false-empty samples..."
fi

docker compose run --rm --no-deps \
  --entrypoint python ml-training - "$DRY_RUN" "$BUSY_MAX" "$MAX_RUN" "$MAX_STEP_MIN" <<'PY'
import os
import sqlite3
import sys
from collections import Counter
from datetime import datetime, timedelta

DRY_RUN = sys.argv[1] == "true"
BUSY_MAX = int(sys.argv[2])
MAX_RUN = int(sys.argv[3])
MAX_STEP = timedelta(minutes=int(sys.argv[4]))
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

    doomed = []
    by_pool = Counter()
    by_month = Counter()
    examples = []
    kept_long = 0
    kept_unbracketed = 0

    for pool_id in sorted(names):
        rows = [
            (rid, parse(dtime), util)
            for rid, dtime, util in conn.execute(
                "SELECT id, dtime, utility FROM track_pools "
                "WHERE pool_id = ? ORDER BY dtime",
                (pool_id,),
            )
        ]

        i = 0
        while i < len(rows):
            if rows[i][2] != 100:
                i += 1
                continue

            # maximal run of 100s starting at i
            j = i
            while j + 1 < len(rows) and rows[j + 1][2] == 100:
                j += 1
            run = rows[i:j + 1]

            if len(run) > MAX_RUN:
                kept_long += 1
                i = j + 1
                continue

            before = rows[i - 1] if i > 0 else None
            after = rows[j + 1] if j + 1 < len(rows) else None

            ok = (
                before is not None and after is not None
                and before[2] <= BUSY_MAX and after[2] <= BUSY_MAX
                and all(t is not None for _, t, _ in run)
                and before[1] is not None and after[1] is not None
                # contiguity across the whole island, including both brackets
                and all(
                    (b[1] - a[1]) <= MAX_STEP
                    for a, b in zip([before] + run, run + [after])
                )
            )

            if ok:
                for rid, t, _ in run:
                    doomed.append(rid)
                    by_pool[names[pool_id]] += 1
                    by_month[t.strftime("%Y-%m")] += 1
                if len(examples) < 12:
                    examples.append(
                        (names[pool_id], run[0][1].strftime("%Y-%m-%d %H:%M"),
                         before[2], len(run), after[2])
                    )
            else:
                kept_unbracketed += 1

            i = j + 1

    print(f"  track_pools rows             : {total:,}")
    print(f"  false-empty rows to remove   : {len(doomed):,}")
    print(f"  closures kept (too long)     : {kept_long:,}")
    print(f"  closures kept (not bracketed): {kept_unbracketed:,}")
    print()

    if examples:
        print("  Examples (before → closed run → after):")
        for name, when, prev, runlen, nxt in examples:
            run_desc = "100" if runlen == 1 else f"100 x{runlen}"
            print(f"    {name:<26s} {when}   {prev:>3} → {run_desc:<7s} → {nxt:>3}")
        print()

    if by_pool:
        print("  By pool:")
        for name, count in by_pool.most_common():
            print(f"    {name:<32s} {count:>5} rows")
        print()
        print("  By month:")
        for month in sorted(by_month):
            print(f"    {month}  {by_month[month]:>5} rows")
        print()

    if DRY_RUN:
        print("  Dry run only; no rows were deleted.")
    elif doomed:
        conn.executemany("DELETE FROM track_pools WHERE id = ?", [(i,) for i in doomed])
        conn.commit()
        print(f"  Deleted {len(doomed):,} false-empty rows.")
    else:
        print("  Nothing to delete; history is already clean.")

    print()
    print("  Migration 017 complete.")
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
  echo " Migration 017 preview complete."
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
echo " Migration 017 complete."
echo "============================================================"
