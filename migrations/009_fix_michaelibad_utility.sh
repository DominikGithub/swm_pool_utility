#!/usr/bin/env bash
# =============================================================================
# Migration 009: Fix Michaelibad closed-state utility (0 → 100)
# =============================================================================
# Background
# ----------
# Michaelibad switched to the inverted convention (0% = closed) after the
# SWM website redesign on 2026-05-22.  The pool-scraper now normalizes its
# scraped 0 values to 100 via the normalizePools set.  This migration is a
# one-time catch-up for rows inserted before the scraper fix.
#
# What it does
# ------------
#   Michaelibad utility 0 → 100 since 2026-05-23 (idempotent — already-fixed
#   rows are skipped).  Clears daily_avg_cache for rebuild.
#
# Usage
# -----
#   bash migrations/009_fix_michaelibad_utility.sh [--up]
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$PROJECT_DIR"

UP=false
for arg in "$@"; do
  [[ "$arg" == "--up" ]] && UP=true
done

COMPOSE_PROJECT="${COMPOSE_PROJECT_NAME:-$(basename "$PROJECT_DIR" | tr '[:upper:]' '[:lower:]' | tr -cd '[:alnum:]_-')}"

echo "============================================================"
echo " Migration 009: Fix Michaelibad utility values (0 → 100)"
echo "============================================================"
echo ""

echo "[1/3] Stopping all services..."
docker compose stop
echo "  All services stopped."
echo ""

echo "[2/3] Fixing Michaelibad data..."
docker compose run --rm --no-deps --entrypoint python ml-training -c "
import sqlite3, os, sys

DB = os.environ.get('DB_PATH', '/data/swm_pool_utility.db')
conn = sqlite3.connect(DB)

before = conn.execute('''
    SELECT COUNT(*) FROM track_pools
    WHERE pool_id = (SELECT id FROM pools WHERE name = 'Michaelibad')
      AND utility = 0 AND dtime >= '2026-05-23'
''').fetchone()[0]

print(f'  Michaelibad utility=0 rows since May 23: {before}')

if before == 0:
    print('  Nothing to fix.')
    conn.close()
    sys.exit(0)

conn.execute('''
    UPDATE track_pools SET utility = 100 WHERE utility = 0
    AND pool_id = (SELECT id FROM pools WHERE name = 'Michaelibad')
    AND dtime >= '2026-05-23'
''')

remaining = conn.execute('''
    SELECT COUNT(*) FROM track_pools
    WHERE pool_id = (SELECT id FROM pools WHERE name = 'Michaelibad')
      AND utility = 0 AND dtime >= '2026-05-23'
''').fetchone()[0]

print(f'  → Fixed {before} rows, {remaining} still at 0.')

cache = conn.execute('SELECT COUNT(*) FROM daily_avg_cache').fetchone()[0]
conn.execute('DELETE FROM daily_avg_cache')
print(f'  Cleared daily_avg_cache ({cache} rows).')

conn.commit()
conn.close()
print()
print('  Migration 009 complete.')
"

echo ""
echo "[3/3] Starting services..."
docker compose up -d
echo "  Services started."

echo ""
echo "============================================================"
echo " Migration 009 complete."
echo "============================================================"
