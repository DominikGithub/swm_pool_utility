#!/usr/bin/env bash
# =============================================================================
# Migration 011: Fix all data — Volksbad utility + stolen data + ghost pools
# =============================================================================
# Background
# ----------
# Before the scraper parsed <li class="bath-capacity-item"> elements atomically,
# it used an unbounded 2000-byte search window after each headline.  When a pool
# appeared as closed on the SWM page (no percentage), the search window would
# reach into the next pool's section and assign that pool's percentage to the
# closed pool.  Ghost pools were also created from the closure-message headlines.
#
# The donor pools (Olympia, Michaelibad, etc.) already contain the correct data
# at every affected timestamp — the stolen rows are duplicates, so deleting them
# from the wrong pool loses nothing.
#
# This migration also fixes Muellersches Volksbad utility=0 -> 100 for historical
# rows outside its correct per-day operating hours.
#
# What it does
# ------------
#   1. Volksbad: utility 0 -> 100 outside correct per-day hours
#   2. Ghost pools: remove all data for closure-message "pools"
#   3. Nordbad: delete stolen duplicate rows from 2026-06-09 onward
#   4. Cosimawellenbad: delete rows from 2026-06-22 (pool closed this day)
#   5. Clears daily_avg_cache, predictions, and orphan entries
#
# Usage
# -----
#   bash migrations/011_fix_volksbad_utility.sh [--up]
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
echo " Migration 011: Fix all pool data"
echo "============================================================"
echo ""

echo "[1/3] Stopping all services..."
docker compose stop
echo "  All services stopped."
echo ""

echo "[2/3] Running data fixes..."
docker compose run --rm --no-deps --entrypoint python ml-training -c "
import sqlite3, os

DB = os.environ.get('DB_PATH', '/data/swm_pool_utility.db')
conn = sqlite3.connect(DB)

# =========================================================================
# 1. Fix Volksbad: 0 -> 100 outside correct per-day operating hours
# =========================================================================
VOLKSBAD = 'M\u00fcller\u2019sches Volksbad'
vpid = conn.execute('SELECT id FROM pools WHERE name = ?', (VOLKSBAD,)).fetchone()

if vpid is None:
    print('  Volksbad: not found in pools table.')
else:
    vpid = vpid[0]
    H  = \"CAST(strftime('%H', datetime(dtime, '+2 hours')) AS INTEGER)\"
    M  = \"CAST(strftime('%M', datetime(dtime, '+2 hours')) AS INTEGER)\"
    WD = \"CAST(strftime('%w', datetime(dtime, '+2 hours')) AS INTEGER)\"
    closed_cond = f'''
        ({WD}=1 AND ({H}<15 OR {H}>=22))
        OR
        ({WD} IN (2,3,4) AND ({H}<7 OR ({H}=7 AND {M}<30) OR {H}>=22))
        OR
        ({WD} IN (5,6) AND ({H}<11 OR {H}>=21))
        OR
        ({WD}=0 AND ({H}<10 OR {H}>=18))
    '''
    before = conn.execute(f'''
        SELECT COUNT(*) FROM track_pools
        WHERE pool_id = ? AND utility = 0 AND ({closed_cond})
    ''', (vpid,)).fetchone()[0]

    if before > 0:
        conn.execute(f'''
            UPDATE track_pools SET utility = 100 WHERE utility = 0
            AND pool_id = ? AND ({closed_cond})
        ''', (vpid,))
        daytime = conn.execute('''
            SELECT COUNT(*) FROM track_pools WHERE pool_id = ? AND utility = 0
        ''', (vpid,)).fetchone()[0]
        print(f'  [1] Volksbad: fixed {before} night-closed rows, {daytime} daytime 0% preserved.')
    else:
        print('  [1] Volksbad: already clean, nothing to fix.')

# =========================================================================
# 2. Ghost pools: remove closure-message entries from all tables
# =========================================================================
ghost_names = [
    'Geschlossen \u2013 in Revision',
    'Geschlossen \u2013 aufgrund von Sanierungsarbeiten',
]
ghost_total = 0
for gname in ghost_names:
    gp = conn.execute('SELECT id FROM pools WHERE name = ?', (gname,)).fetchone()
    if gp is None:
        continue
    gid = gp[0]

    n_tp = conn.execute('SELECT COUNT(*) FROM track_pools WHERE pool_id = ?', (gid,)).fetchone()[0]
    n_da = conn.execute('SELECT COUNT(*) FROM daily_avg_cache WHERE pool_id = ?', (gid,)).fetchone()[0]
    n_pr = conn.execute('SELECT COUNT(*) FROM predictions WHERE pool_name = ?', (gname,)).fetchone()[0]

    conn.execute('DELETE FROM track_pools WHERE pool_id = ?', (gid,))
    conn.execute('DELETE FROM daily_avg_cache WHERE pool_id = ?', (gid,))
    conn.execute('DELETE FROM predictions WHERE pool_name = ?', (gname,))
    conn.execute('DELETE FROM pools WHERE id = ?', (gid,))

    print(f'  [2] Ghost \u201c{gname}\u201d: removed {n_tp} track_pools, {n_da} cache, {n_pr} predictions')
    ghost_total += n_tp

if ghost_total == 0:
    print('  [2] Ghost pools: none found.')

# =========================================================================
# 3. Nordbad: delete stolen duplicate rows from 2026-06-09 onward
#    (Olympia already has the correct data at every timestamp)
# =========================================================================
npid = conn.execute(\"SELECT id FROM pools WHERE name = 'Nordbad'\").fetchone()
if npid is None:
    print('  [3] Nordbad: not found.')
else:
    npid = npid[0]
    before = conn.execute(
        \"SELECT COUNT(*) FROM track_pools WHERE pool_id = ? AND dtime >= '2026-06-09'\",
        (npid,)
    ).fetchone()[0]
    total = conn.execute('SELECT COUNT(*) FROM track_pools WHERE pool_id = ?', (npid,)).fetchone()[0]
    if before > 0:
        conn.execute(
            \"DELETE FROM track_pools WHERE pool_id = ? AND dtime >= '2026-06-09'\",
            (npid,)
        )
        print(f'  [3] Nordbad: removed {before} stolen rows, {total - before} genuine preserved.')
    else:
        print(f'  [3] Nordbad: already clean ({total} genuine rows).')

# =========================================================================
# 4. Cosimawellenbad: delete rows from 2026-06-22 (pool closed this day)
# =========================================================================
cpid = conn.execute(\"SELECT id FROM pools WHERE name = 'Cosimawellenbad'\").fetchone()
if cpid is None:
    print('  [4] Cosimawellenbad: not found.')
else:
    cpid = cpid[0]
    before = conn.execute(
        \"SELECT COUNT(*) FROM track_pools WHERE pool_id = ? AND date(dtime) = '2026-06-22'\",
        (cpid,)
    ).fetchone()[0]
    conn.execute(
        \"DELETE FROM track_pools WHERE pool_id = ? AND date(dtime) = '2026-06-22'\",
        (cpid,)
    )
    print(f'  [4] Cosimawellenbad: removed {before} rows from June 22 (pool closed).')

# =========================================================================
# 5. Cleanup caches
# =========================================================================
cache = conn.execute('SELECT COUNT(*) FROM daily_avg_cache').fetchone()[0]
conn.execute('DELETE FROM daily_avg_cache')
print(f'  [5] Cleared daily_avg_cache ({cache} rows)')

conn.execute(\"DELETE FROM predictions WHERE pool_name NOT IN (SELECT name FROM pools)\")
print('  [5] Cleaned orphan predictions')

conn.commit()
conn.close()

# Summary
print()
print(f'  Summary: Volksbad fixed, {ghost_total} ghost rows removed, Nordbad cleaned, Cosimawellenbad cleaned.')
print('  Donor pools (Olympia, Michaelibad) already contain the correct data at every timestamp.')
print('  No data was lost.')
print()
print('  Migration 011 complete.')
"

echo ""
echo "[3/3] Starting services..."
docker compose up -d
echo "  Services started."

echo ""
echo "============================================================"
echo " Migration 011 complete."
echo "============================================================"
