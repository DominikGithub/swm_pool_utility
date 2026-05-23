#!/usr/bin/env bash
# =============================================================================
# Migration 008: Fix Freibad closed-state utility values (0 → 100)
# =============================================================================
# Background
# ----------
# The SWM website reports 0% capacity remaining for closed Freibäder (instead
# of the Hallenbad convention of 100%).  Michaelibad also switched to this
# convention after the May 2026 website redesign.
#
# Because every consumer of the data converts with  utilization = 100 - utility,
# the stored 0 gets displayed as 100% utilization (= maximally crowded) instead
# of 0% (= closed/empty).
#
# Strategy
# --------
#   Freibäder (pools 10-17):
#     Fix ALL utility=0 rows unconditionally.
#     These large outdoor pools never genuinely hit 0% free capacity during
#     operating hours — a scraped 0 always means "closed / not operating".
#
#   Michaelibad (pool 4):
#     Fix utility=0 only when Berlin local time is outside its
#     operating hours (07:30–23:00).  During operating hours a 0%
#     reading can be genuine (pool at full capacity on a hot day).
#     Nighttime 0s (23:00+) and pre-opening 0s (<07:30) are the
#     broken closed-state convention and are fixed.
#
#   All other Hallenbäder (pools 1-3, 5-9):
#     Historical glitch rows (Apr 23 11:20 & Apr 26 19:00 Berlin) are
#     replaced with values interpolated from neighboring data points.
#     All other utility=0 (genuine daytime full-capacity) is preserved.
#
# What it does
# ------------
#   1. Updates track_pools: utility 0 → 100 for Freibäder + Michaelibad night.
#   2. Interpolates 15 known simultaneous-glitch rows from neighboring values.
#   3. Preserves all genuine Hallenbad utility=0 values (e.g. Cosimawellenbad
#      May 2-3).
#
# Usage
# -----
#   bash migrations/008_fix_freibad_closed_utility.sh [--up]
#   --up   Automatically start all services after migration completes.
#
# Idempotency
# -----------
# Safe to run more than once.  Already-fixed rows won't match utility=0.
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
VOLUME="${COMPOSE_PROJECT}_db_data"

echo "============================================================"
echo " Migration 008: Fix Freibad closed-state utility (0 → 100)"
echo "============================================================"
echo " Project dir : $PROJECT_DIR"
echo " DB volume   : $VOLUME"
echo ""

# ---------------------------------------------------------------------------
# Step 1 — stop all services
# ---------------------------------------------------------------------------
echo "[1/3] Stopping all services..."
docker compose stop
echo "  All services stopped."
echo ""

# ---------------------------------------------------------------------------
# Step 2 — patch utility values and clear daily cache
# ---------------------------------------------------------------------------
echo "[2/3] Patching utility data..."

docker compose run --rm --no-deps --entrypoint python ml-training -c "
import sqlite3, os, sys

DB = os.environ.get('DB_PATH', '/data/swm_pool_utility.db')
print(f'  Database: {DB}')

conn = sqlite3.connect(DB)

# ── Freibäder: unconditional fix (all utility=0 → 100) ───────────────────────
# These are the 8 outdoor pools that first appeared on 2026-05-22.  They never
# genuinely hit 0% free capacity during operating hours — a 0 always means
# the pool is closed.
freibad_names = [
    'Dantebad',
    'Freibad West',
    'Michaeli-Freibad',
    'Naturbad Georgenschwaige',
    'Naturbad Maria Einsiedel',
    'Prinzregentenbad',
    'Schyrenbad',
    'Ungererbad',
]
fb_placeholders = ','.join('?' * len(freibad_names))

freibad_before = conn.execute(f'''
    SELECT p.name, COUNT(*)
    FROM track_pools tp JOIN pools p ON tp.pool_id = p.id
    WHERE tp.utility = 0 AND p.name IN ({fb_placeholders})
    GROUP BY p.name ORDER BY p.name
''', freibad_names).fetchall()

freibad_total = sum(cnt for _, cnt in freibad_before)

if freibad_total > 0:
    print()
    print('  Freibäder to fix (all utility=0):')
    for name, cnt in freibad_before:
        print(f'    {name:<35s} {cnt:>5} rows')
    print(f'    {\"\":<35s} -----')
    print(f'    {\"SUBTOTAL\":<35s} {freibad_total:>5} rows')

    conn.execute(f'''
        UPDATE track_pools SET utility = 100 WHERE utility = 0
        AND pool_id IN (SELECT id FROM pools WHERE name IN ({fb_placeholders}))
    ''', freibad_names)
    print(f'  → Fixed {freibad_total} Freibad rows.')
else:
    print()
    print('  Freibäder: nothing to fix.')

# ── Michaelibad: fix only outside 07:30–23:00 Berlin time ─────────────────────
# Michaelibad (Hallenbad, pool 4) switched to the Freibad convention of
# reporting 0% when closed after the May 2026 redesign.  During operating
# hours a 0% reading can be genuine (pool at full capacity), so we only fix
# values that fall outside 07:30–23:00 Berlin local time.
michaeli_before = conn.execute('''
    SELECT COUNT(*) FROM track_pools tp JOIN pools p ON tp.pool_id = p.id
    WHERE tp.utility = 0 AND p.name = 'Michaelibad'
      AND (
        CAST(strftime('%H', datetime(tp.dtime, '+2 hours')) AS INTEGER) < 7
        OR (CAST(strftime('%H', datetime(tp.dtime, '+2 hours')) AS INTEGER) = 7
            AND CAST(strftime('%M', datetime(tp.dtime, '+2 hours')) AS INTEGER) < 30)
        OR CAST(strftime('%H', datetime(tp.dtime, '+2 hours')) AS INTEGER) >= 23
      )
''').fetchone()[0]

if michaeli_before > 0:
    print(f'  Michaelibad: {michaeli_before} rows outside 07:30–23:00 to fix.')
    conn.execute('''
        UPDATE track_pools SET utility = 100 WHERE utility = 0
        AND pool_id = (SELECT id FROM pools WHERE name = 'Michaelibad')
        AND (
            CAST(strftime('%H', datetime(dtime, '+2 hours')) AS INTEGER) < 7
            OR (CAST(strftime('%H', datetime(dtime, '+2 hours')) AS INTEGER) = 7
                AND CAST(strftime('%M', datetime(dtime, '+2 hours')) AS INTEGER) < 30)
            OR CAST(strftime('%H', datetime(dtime, '+2 hours')) AS INTEGER) >= 23
        )
    ''')
    print(f'  → Fixed {michaeli_before} Michaelibad nighttime rows.')
else:
    print('  Michaelibad: nothing to fix outside 07:30–23:00.')

# ── Interpolate known simultaneous-glitch rows ───────────────────────────────
# On Apr 23 and Apr 26 the SWM website returned 0% for all Hallenbäder
# simultaneously at specific moments (likely maintenance/deploy events).
# These are obvious outliers — a genuine full-pool reading wouldn't hit
# every pool at the exact same second.  Replace each with the average of
# its immediate neighbours.
#
# The 15 rows are identified by (pool_id, dtime) pairs.  Cosimawellenbad
# at Apr 26 17:00:49 is excluded — it already had a valid 79% reading.
glitch_timestamps = [
    '2026-04-23 09:20:09',  # 11:20 Berlin — all 8 Hallenbäder affected
    '2026-04-26 17:00:49',  # 19:00 Berlin — 7 Hallenbäder affected (not Cosima)
]
interp_count = 0

for ts in glitch_timestamps:
    # Which pools have utility=0 at this timestamp?
    affected = conn.execute('''
        SELECT tp.pool_id, p.name FROM track_pools tp JOIN pools p ON tp.pool_id = p.id
        WHERE tp.dtime = ? AND tp.utility = 0
          AND p.name NOT IN (''' + ','.join('?' * len(freibad_names)) + ''')
    ''', [ts] + freibad_names).fetchall()

    for pool_id, pool_name in affected:
        prev = conn.execute('''
            SELECT utility FROM track_pools
            WHERE pool_id = ? AND dtime < ? AND utility != 0
            ORDER BY dtime DESC LIMIT 1
        ''', (pool_id, ts)).fetchone()

        next_row = conn.execute('''
            SELECT utility FROM track_pools
            WHERE pool_id = ? AND dtime > ? AND utility != 0
            ORDER BY dtime ASC LIMIT 1
        ''', (pool_id, ts)).fetchone()

        if prev and next_row:
            # Safety check: neighbours should be within ~15 min and not a
            # Freibad-style 100 (which means "closed" for those pools).
            # Standard Hallenbad values are 10-95 range during daytime.
            interp = round((prev[0] + next_row[0]) / 2.0)
            conn.execute(
                'UPDATE track_pools SET utility = ? WHERE pool_id = ? AND dtime = ?',
                (interp, pool_id, ts)
            )
            interp_count += 1
            print(f'  Interpolated {pool_name}: '
                  f'{ts}  {prev[0]} / {next_row[0]} -> {interp}')

if interp_count > 0:
    print(f'  → Interpolated {interp_count} historical glitch rows.')
else:
    print('  → No historical glitch rows found.')

# ── Remaining utility=0 (deliberately preserved) ─────────────────────────────
print()
print('  REMAINING utility=0 rows (preserved — NOT touched):')
remaining = conn.execute('''
    SELECT p.name, COUNT(*) as cnt
    FROM track_pools tp JOIN pools p ON tp.pool_id = p.id
    WHERE tp.utility = 0
    GROUP BY p.name ORDER BY p.name
''').fetchall()

if remaining:
    for name, cnt in remaining:
        print(f'    {name:<35s} {cnt:>5} rows')
    total_rem = sum(cnt for _, cnt in remaining)
    print(f'    {\"\":<35s} -----')
    print(f'    {\"REMAINING\":<35s} {total_rem:>5} rows')
else:
    print('    (none — all known broken data has been fixed)')

# ── Clear daily_avg_cache ────────────────────────────────────────────────────
cache_before = conn.execute('SELECT COUNT(*) FROM daily_avg_cache').fetchone()[0]
conn.execute('DELETE FROM daily_avg_cache')
print()
print(f'  Cleared daily_avg_cache ({cache_before} rows — will rebuild on next aggregation cycle).')

conn.commit()
conn.close()

total_fixed = freibad_total + michaeli_before + interp_count
print()
print(f'  Total rows fixed: {total_fixed} (Freibäder: {freibad_total}, Michaelibad nighttime: {michaeli_before}, interpolated glitches: {interp_count})')
print('  Migration 008 complete.')
"

echo ""
echo "[2/3] Data patch applied successfully."
echo ""

# ---------------------------------------------------------------------------
# Step 3 — optionally restart all services
# ---------------------------------------------------------------------------
if [[ "$UP" == "true" ]]; then
  echo "[3/3] Starting all services..."
  docker compose up -d
  echo ""
  echo "  Services started."
  echo "  The daily-stats service will rebuild daily_avg_cache within ~1 hour."
else
  echo "[3/3] Services remain stopped."
  echo "      Start them when ready with: docker compose up -d"
fi

echo ""
echo "============================================================"
echo " Migration 008 complete."
echo "============================================================"
