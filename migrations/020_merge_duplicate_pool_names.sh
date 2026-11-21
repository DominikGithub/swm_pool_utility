#!/usr/bin/env bash
# =============================================================================
# Migration 020: Merge pools that differ only in apostrophe typography
# =============================================================================
# Background
# ----------
# pool-scraper folds the typographic apostrophes ‘ and ’ to a plain ' before it
# looks a pool up (canonicalPoolName), but the pools table kept the spelling
# from before that change: "Müller’sches Volksbad".  The lookup only worked
# through the scraper's in-memory ID cache.
#
# On 2026-11-21 one scraper run failed to load that cache (the database was
# briefly locked and the failure was only logged).  With the cache empty, it
# could not find "Müller'sches Volksbad", so it inserted a NEW pool under that
# name.  From the next run on, both rows had the same cache key and the newer
# one won, so from 23:20 every Volksbad reading went to the new, empty pool and
# the live chart for the real one stopped.
#
# pool-scraper is fixed (busy timeout, a failed cache load aborts the run, the
# oldest row wins, and an uncached lookup matches by canonical name).  This
# migration repairs the data.
#
# What it does
# ------------
# For every group of pools whose names are equal after canonicalisation
# (trim, ‘ and ’ → '):
#
#   1. Keeps the OLDEST row (lowest id): it holds the history.
#   2. Moves the readings of the other rows onto it.  A reading whose timestamp
#      the kept pool already has is dropped (UNIQUE(pool_id, dtime)).
#   3. Deletes the other rows and their daily_avg_cache rows.
#   4. Renames the kept row to the canonical name, e.g. "Müller'sches Volksbad".
#      Stored this way, the scraper's exact-name lookup can never miss it again,
#      even without its cache.
#   5. Moves the kept pool's predictions row and model file
#      (/models/pool_<name>.joblib) to the new name and removes the duplicates',
#      so predictions continue without waiting for the nightly retrain.
#
# A pool stored under a non-canonical name with no duplicate is only renamed
# (steps 4 and 5), so all databases end up with the same spelling.
#
# Afterwards daily_avg_cache is rebuilt from the merged history.
#
# Usage
# -----
#   # 1. Build the fixed images first (pool-scraper, frontend):
#   docker compose build
#
#   # 2. Preview, then apply (stops services, migrates, restarts them):
#   bash migrations/020_merge_duplicate_pool_names.sh --dry-run
#   bash migrations/020_merge_duplicate_pool_names.sh
#
# Idempotency
# -----------
# Safe to run repeatedly: once every name is canonical and unique there is
# nothing to merge or rename.
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
      echo "Usage: bash migrations/020_merge_duplicate_pool_names.sh [--dry-run]" >&2
      exit 2
      ;;
  esac
done

echo "============================================================"
echo " Migration 020: Merge pools differing only in apostrophes"
echo "============================================================"
echo ""

if [[ "$DRY_RUN" == "true" ]]; then
  echo "[1/1] Previewing changes (services left running)..."
else
  echo "[1/4] Stopping services to prevent concurrent database writes..."
  docker compose stop
  echo "  Services stopped."
  echo ""
  echo "[2/4] Merging pools..."
fi

# ml-training mounts both the database and the model volume.
docker compose run --rm --no-deps \
  --entrypoint python ml-training - "$DRY_RUN" <<'PY'
import os
import sqlite3
import sys

DRY_RUN = sys.argv[1] == "true"
DB = os.environ.get("DB_PATH", "/data/swm_pool_utility.db")
MODEL_DIR = os.environ.get("MODEL_DIR", "/models")


def canonical(name):
    """Mirror of pool-scraper's canonicalPoolName."""
    return name.strip().replace("‘", "'").replace("’", "'")


def model_path(name):
    """Mirror of the file name ml-training saves a pool's model under."""
    return os.path.join(MODEL_DIR, f"pool_{name.replace(' ', '_').replace('-', '_')}.joblib")


def count(conn, pool_id):
    return conn.execute(
        "SELECT COUNT(*) FROM track_pools WHERE pool_id = ?", (pool_id,)
    ).fetchone()[0]


conn = sqlite3.connect(DB, timeout=30)
conn.execute("PRAGMA busy_timeout = 30000")
if not DRY_RUN:
    conn.execute("BEGIN IMMEDIATE")

model_moves = []    # (old path, new path)
model_removals = []  # paths

try:
    groups = {}
    for pool_id, name in conn.execute("SELECT id, name FROM pools ORDER BY id"):
        groups.setdefault(canonical(name), []).append((pool_id, name))

    plan = [
        (canon, members) for canon, members in groups.items()
        if len(members) > 1 or members[0][1] != canon
    ]

    if not plan:
        print("  Every pool name is canonical and unique; nothing to do.")

    for canon, members in plan:
        keep_id, keep_name = members[0]
        dupes = members[1:]
        print(f"  {canon}")
        print(f"    keep   id {keep_id:<4} {keep_name!r:<28} {count(conn, keep_id):>7,} readings")

        for dupe_id, dupe_name in dupes:
            total = count(conn, dupe_id)
            if DRY_RUN:
                clashes = conn.execute(
                    "SELECT COUNT(*) FROM track_pools d JOIN track_pools k "
                    "ON k.pool_id = ? AND k.dtime = d.dtime WHERE d.pool_id = ?",
                    (keep_id, dupe_id),
                ).fetchone()[0]
                moved = total - clashes
            else:
                moved = conn.execute(
                    "UPDATE OR IGNORE track_pools SET pool_id = ? WHERE pool_id = ?",
                    (keep_id, dupe_id),
                ).rowcount
                # Left behind = a reading at a timestamp the kept pool already has.
                clashes = conn.execute(
                    "DELETE FROM track_pools WHERE pool_id = ?", (dupe_id,)
                ).rowcount
                conn.execute("DELETE FROM daily_avg_cache WHERE pool_id = ?", (dupe_id,))
                conn.execute("DELETE FROM pools WHERE id = ?", (dupe_id,))
            print(f"    merge  id {dupe_id:<4} {dupe_name!r:<28} {moved:>7,} readings moved, "
                  f"{clashes} duplicate timestamps dropped")

        if keep_name != canon:
            print(f"    rename {keep_name!r} -> {canon!r}")
            if not DRY_RUN:
                conn.execute("UPDATE pools SET name = ? WHERE id = ?", (canon, keep_id))

        # predictions is keyed by pool name.  The kept pool's row is the valid
        # one; a row under the canonical name came from the near-empty duplicate.
        stale = [n for _, n in dupes if n != canon]
        if not DRY_RUN:
            if keep_name != canon:
                conn.execute("DELETE FROM predictions WHERE pool_name = ?", (canon,))
                conn.execute(
                    "UPDATE predictions SET pool_name = ? WHERE pool_name = ?",
                    (canon, keep_name),
                )
            conn.executemany(
                "DELETE FROM predictions WHERE pool_name = ?", [(n,) for n in stale]
            )

        new_model = model_path(canon)
        if keep_name != canon and os.path.exists(model_path(keep_name)):
            model_moves.append((model_path(keep_name), new_model))
        for n in stale:
            if os.path.exists(model_path(n)) and model_path(n) != new_model:
                model_removals.append(model_path(n))
        print()

    if DRY_RUN:
        for old, new in model_moves:
            print(f"  would move model {os.path.basename(old)} -> {os.path.basename(new)}")
        for path in model_removals:
            print(f"  would remove model {os.path.basename(path)}")
        print("  Dry run only; nothing was changed.")
    else:
        conn.commit()
        # Files only after the database change is committed.
        for old, new in model_moves:
            os.replace(old, new)
            print(f"  moved model {os.path.basename(old)} -> {os.path.basename(new)}")
        for path in model_removals:
            os.remove(path)
            print(f"  removed model {os.path.basename(path)}")
        print()
        print("  Pools now:")
        for pool_id, name in conn.execute("SELECT id, name FROM pools ORDER BY id"):
            print(f"    {pool_id:>3}  {name}")
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
  echo " Migration 020 preview complete."
  echo "============================================================"
  exit 0
fi

echo ""
echo "[3/4] Rebuilding daily_avg_cache from the merged history..."
docker compose run --rm --no-deps --entrypoint /app/daily-stats daily-stats --once
echo "  Cache rebuilt."

echo ""
echo "[4/4] Starting services..."
docker compose up -d
echo "  Services started."

echo ""
echo "============================================================"
echo " Migration 020 complete."
echo "============================================================"
