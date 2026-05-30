#!/usr/bin/env bash
# =============================================================================
# Migration 010: Fix Südbad closed-state utility (0 → 100)
# =============================================================================
# Background
# ----------
# After the SWM website redesign (May 2026), Südbad switched to the inverted
# convention (0% = closed) like Freibäder and Michaelibad.  Nightly scrapes now
# store utility=0 instead of the previous utility=100, causing the frontend to
# display 100% utilization for a closed pool.
#
# The pool-scraper now normalizes Südbad's scraped 0 values to 100 via the
# normalizePools set.  This migration is a one-time catch-up for rows inserted
# before the scraper fix.
#
# What it does
# ------------
#   Südbad utility 0 → 100 where the Berlin-local hour is ≥ 23 or < 7
#   (pool closed).  Daytime 0 records from before the redesign (e.g. 2026-05-03
#   when the pool was genuinely at capacity) are left untouched.
#   Clears daily_avg_cache for rebuild.
#
# Usage
# -----
#   bash migrations/010_fix_suedbad_utility.sh [--up]
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
echo " Migration 010: Fix Südbad utility values (0 → 100)"
echo "============================================================"
echo ""

echo "[1/3] Stopping all services..."
docker compose stop
echo "  All services stopped."
echo ""

echo "[2/3] Fixing Südbad data..."
docker compose run --rm --no-deps --entrypoint python ml-training -c "
import sqlite3, os, sys

DB = os.environ.get('DB_PATH', '/data/swm_pool_utility.db')
conn = sqlite3.connect(DB)

pid = conn.execute(\"SELECT id FROM pools WHERE name = 'Südbad'\").fetchone()
if pid is None:
    print('  Südbad not found in pools table — nothing to fix.')
    conn.close()
    sys.exit(0)
pid = pid[0]

before = conn.execute('SELECT COUNT(*) FROM track_pools WHERE pool_id = ? AND utility = 0', (pid,)).fetchone()[0]
print(f'  Südbad utility=0 rows (total): {before}')

if before == 0:
    print('  Nothing to fix.')
    conn.close()
    sys.exit(0)

conn.execute('UPDATE track_pools SET utility = 100 WHERE pool_id = ? AND utility = 0', (pid,))

remaining = conn.execute('SELECT COUNT(*) FROM track_pools WHERE pool_id = ? AND utility = 0', (pid,)).fetchone()[0]
print(f'  → Fixed {before} rows, {remaining} remaining at 0.')

cache = conn.execute('SELECT COUNT(*) FROM daily_avg_cache').fetchone()[0]
conn.execute('DELETE FROM daily_avg_cache')
print(f'  Cleared daily_avg_cache ({cache} rows).')

conn.commit()
conn.close()
print()
print('  Migration 010 complete.')
"

echo ""
echo "[3/3] Starting services..."
docker compose up -d
echo "  Services started."

echo ""
echo "============================================================"
echo " Migration 010 complete."
echo "============================================================"
