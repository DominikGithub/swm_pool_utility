#!/bin/bash
# Migration 006: Extend idx_track_pools_name_dtime to a covering index
#
# The previous index was (name, dtime). Every query that also needs the
# utility column (getHistory, getPoolStatus, getHourlyAvg, ml queries)
# had to follow a pointer back to the table row for each match.
#
# Adding utility as a third column makes the index "covering" for all
# common read paths — SQLite can satisfy those queries from the index
# alone without touching the heap at all.
#
# SQLite's CREATE INDEX ... IF NOT EXISTS is NOT sensitive to column
# changes, so the old narrower index must be dropped first.
#
# Idempotent: safe to run more than once.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR/.."

echo "============================================================"
echo " Migration 006: Covering index on track_pools(name, dtime, utility)"
echo "============================================================"

docker compose run --rm --entrypoint python ml-training -c "
import sqlite3, os

db = os.environ.get('DB_PATH', '/data/swm_pool_utility.db')
conn = sqlite3.connect(db)

# Check current index definition
rows = conn.execute(\"SELECT name, sql FROM sqlite_master WHERE type='index' AND name='idx_track_pools_name_dtime'\").fetchall()
if rows:
    current_sql = rows[0][1]
    print(f'  Current index: {current_sql}')
    if 'utility' in (current_sql or '').lower():
        print('  Already a covering index — nothing to do.')
        conn.close()
        exit(0)
    print('  Dropping old (name, dtime) index...')
    conn.execute('DROP INDEX IF EXISTS idx_track_pools_name_dtime')
else:
    print('  Index not found, will create fresh.')

print('  Creating covering index (name, dtime, utility)...')
conn.execute('CREATE INDEX idx_track_pools_name_dtime ON track_pools(name, dtime, utility)')
conn.commit()

result = conn.execute(\"SELECT sql FROM sqlite_master WHERE type='index' AND name='idx_track_pools_name_dtime'\").fetchone()
print(f'  Done. New index: {result[0]}')
conn.close()
"

echo ""
echo "============================================================"
echo " Migration 006 complete."
echo "============================================================"
