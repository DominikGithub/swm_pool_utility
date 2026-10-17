#!/usr/bin/env bash
# =============================================================================
# Migration 015: Add UNIQUE(pool_id, dtime) index to track_pools
# =============================================================================
# Background
# ----------
# track_pools historically used a plain INSERT with no uniqueness guarantee, so
# overlapping scrapes or manual --once runs could store two readings for the
# same pool in the same second.  The pool-scraper is being changed to
# `INSERT ... ON CONFLICT(pool_id, dtime) DO NOTHING`, which requires a UNIQUE
# index on (pool_id, dtime) to exist.
#
# What it does
# ------------
#   1. Detects existing duplicate (pool_id, dtime) rows.
#   2. Removes duplicates, keeping the earliest row per pool+second (MIN(id)),
#      matching the "first write wins" semantics of ON CONFLICT DO NOTHING.
#   3. Creates UNIQUE index idx_track_pools_pool_dtime ON track_pools(pool_id, dtime).
#
# Ordering with the scraper code change
# -------------------------------------
# Apply this migration BEFORE deploying the new pool-scraper binary:
#   - The OLD binary's plain INSERT keeps working after the index exists (a
#     genuine same-second collision would just log "UNIQUE constraint failed"
#     and continue — harmless).
#   - The NEW binary's ON CONFLICT clause raises an error if the index is
#     missing, so the index must exist first.
#
# Usage
# -----
#   bash migrations/015_add_track_pools_unique_index.sh            # apply
#   bash migrations/015_add_track_pools_unique_index.sh --dry-run  # preview only
#
# Idempotency
# -----------
# Safe to run repeatedly.  Duplicate removal is a no-op once data is unique,
# and the index uses IF NOT EXISTS.
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
      echo "Usage: bash migrations/015_add_track_pools_unique_index.sh [--dry-run]" >&2
      exit 2
      ;;
  esac
done

echo "============================================================"
echo " Migration 015: Add UNIQUE(pool_id, dtime) to track_pools"
echo "============================================================"
echo ""

if [[ "$DRY_RUN" == "true" ]]; then
  echo "[1/1] Previewing duplicates (services left running)..."
else
  echo "[1/3] Stopping services to prevent concurrent database writes..."
  docker compose stop
  echo "  Services stopped."
  echo ""
  echo "[2/3] Deduplicating track_pools and creating the unique index..."
fi

docker compose run --rm --no-deps \
  --entrypoint python ml-training - "$DRY_RUN" <<'PY'
import os
import sqlite3
import sys

DRY_RUN = sys.argv[1] == "true"
DB = os.environ.get("DB_PATH", "/data/swm_pool_utility.db")

conn = sqlite3.connect(DB, timeout=30)
conn.execute("PRAGMA busy_timeout = 30000")
if not DRY_RUN:
    conn.execute("BEGIN IMMEDIATE")

try:
    total = conn.execute("SELECT COUNT(*) FROM track_pools").fetchone()[0]
    distinct_pairs = conn.execute(
        "SELECT COUNT(*) FROM (SELECT 1 FROM track_pools GROUP BY pool_id, dtime)"
    ).fetchone()[0]
    duplicates = total - distinct_pairs

    print(f"  track_pools rows         : {total:,}")
    print(f"  distinct (pool, dtime)   : {distinct_pairs:,}")
    print(f"  duplicate rows to remove : {duplicates:,}")
    print()

    if duplicates > 0:
        rows = conn.execute("""
            SELECT p.name, COUNT(*) AS groups, SUM(d.cnt - 1) AS to_remove
            FROM (
                SELECT pool_id, COUNT(*) AS cnt
                FROM track_pools
                GROUP BY pool_id, dtime
                HAVING COUNT(*) > 1
            ) d
            JOIN pools p ON p.id = d.pool_id
            GROUP BY p.name
            ORDER BY to_remove DESC
        """).fetchall()
        print("  Duplicate groups by pool:")
        for name, groups, to_remove in rows:
            print(f"    {name:<32s} {groups:>5} groups  {to_remove:>6} rows to remove")
        print()

    idx_exists = conn.execute(
        "SELECT 1 FROM sqlite_master "
        "WHERE type = 'index' AND name = 'idx_track_pools_pool_dtime'"
    ).fetchone() is not None

    if DRY_RUN:
        print("  Dry run only; no data was changed and no index was created.")
        if idx_exists:
            print("  Unique index idx_track_pools_pool_dtime: already exists")
        else:
            print("  Unique index idx_track_pools_pool_dtime: would be created")
    else:
        if duplicates > 0:
            conn.execute("""
                DELETE FROM track_pools
                WHERE id NOT IN (
                    SELECT MIN(id) FROM track_pools GROUP BY pool_id, dtime
                )
            """)
            print(f"  Removed {duplicates:,} duplicate rows "
                  "(kept the earliest row per pool+second).")

        if idx_exists:
            print("  Unique index idx_track_pools_pool_dtime already exists; left as-is.")
        else:
            conn.execute(
                "CREATE UNIQUE INDEX idx_track_pools_pool_dtime "
                "ON track_pools(pool_id, dtime)"
            )
            print("  Created UNIQUE index idx_track_pools_pool_dtime "
                  "ON track_pools(pool_id, dtime).")

        conn.commit()

    print()
    print("  Migration 015 complete.")
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
  echo " Migration 015 preview complete."
  echo "============================================================"
  exit 0
fi

echo ""
echo "[3/3] Starting services..."
docker compose up -d
echo "  Services started."

echo ""
echo "============================================================"
echo " Migration 015 complete."
echo "============================================================"
