#!/bin/bash
# Migration 005: Enable PRAGMA auto_vacuum = INCREMENTAL
#
# The db-init script sets auto_vacuum = INCREMENTAL for fresh databases, but
# existing databases created before this was added still have auto_vacuum = 0
# (NONE).  This migration enables incremental auto-vacuum on the live DB.
#
# SQLite requires a VACUUM to be run after changing the auto_vacuum mode on an
# existing non-empty database — the mode change alone has no effect until then.
# The VACUUM rewrites the entire database file, so it may take a few minutes on
# large databases.  All services should remain running; SQLite's WAL mode
# allows concurrent reads during VACUUM.
#
# Idempotent: running this more than once is safe (VACUUM on a file already in
# INCREMENTAL mode is a no-op for the mode, and VACUUM itself is always safe).

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR/.."

echo "============================================================"
echo " Migration 005: Enable auto_vacuum = INCREMENTAL"
echo "============================================================"

docker compose run --rm --entrypoint python training -c "
import sqlite3, os

db = os.environ.get('DB_PATH', '/data/swm_pool_utility.db')
conn = sqlite3.connect(db)

current = conn.execute('PRAGMA auto_vacuum').fetchone()[0]
modes = {0: 'NONE', 1: 'FULL', 2: 'INCREMENTAL'}
print(f'  Current auto_vacuum mode: {modes.get(current, current)}')

if current == 2:
    print('  Already INCREMENTAL — nothing to do.')
else:
    print('  Setting auto_vacuum = INCREMENTAL and running VACUUM...')
    conn.execute('PRAGMA auto_vacuum = INCREMENTAL')
    conn.execute('VACUUM')
    conn.commit()
    result = conn.execute('PRAGMA auto_vacuum').fetchone()[0]
    print(f'  Done. New mode: {modes.get(result, result)}')

conn.close()
"

echo ""
echo "============================================================"
echo " Migration 005 complete."
echo "============================================================"
