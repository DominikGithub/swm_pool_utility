#!/usr/bin/env bash
# =============================================================================
# Migration 007: Normalise pool names and weather types
# =============================================================================
# Background
# ----------
# track_pools, daily_avg_cache, weather, and weather_forecast all stored
# repeated VARCHAR strings for pool names (avg ~20 bytes) and weather types
# (avg ~14 bytes).  Because these are low-cardinality values (9 pools, 7
# weather types), replacing them with integer foreign keys saves ~16 bytes
# per row in track_pools (~1,152 inserts/day) and ~10 bytes per row in the
# weather tables (~1,032 inserts/day) — roughly 28 KB/day / 10 MB/year going
# forward, plus an immediate reduction of ~1 MB on the existing 52k track_pools
# rows once VACUUM runs.
#
# Two new lookup tables (created by this migration, maintained by db-init on
# fresh deployments):
#   pools        (id INTEGER PK, name VARCHAR UNIQUE)   -- 9 rows, constant
#   weather_types(id INTEGER PK, type VARCHAR UNIQUE)   -- 7 rows, constant
#
# Tables rebuilt (original VARCHAR column dropped, integer FK added):
#   track_pools      name          → pool_id
#   daily_avg_cache  pool_name     → pool_id
#   weather          weather_type  → weather_type_id
#   weather_forecast weather_type  → weather_type_id
#
# Usage
# -----
#   # 1. Build new images first so services are ready after migration:
#   docker compose build
#
#   # 2. Run the migration (stops services, migrates, optionally restarts):
#   bash migrations/007_normalize_pool_names_and_weather_types.sh [--up]
#
#   --up   Automatically start all services after migration completes.
#          Without this flag services remain stopped so you can verify first.
#
# Idempotency
# -----------
# Safe to run more than once.  The script checks whether the pools table and
# pool_id column already exist; if so it exits immediately without touching data.
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
echo " Migration 007: Normalise pool names and weather types"
echo "============================================================"
echo " Project dir : $PROJECT_DIR"
echo " DB volume   : $VOLUME"
echo ""

# ---------------------------------------------------------------------------
# Step 1 — stop all services so no writers are active during schema change
# ---------------------------------------------------------------------------
echo "[1/3] Stopping all services..."
docker compose stop
echo "  All services stopped."
echo ""

# ---------------------------------------------------------------------------
# Step 2 — run the Python migration inside a temporary ml-training container
#          (has sqlite3 in its Python environment and the DB volume mounted)
# ---------------------------------------------------------------------------
echo "[2/3] Applying schema migration..."

docker compose run --rm --no-deps --entrypoint python ml-training -c "
import sqlite3, os, sys

DB = os.environ.get('DB_PATH', '/data/swm_pool_utility.db')
print(f'  Database: {DB}')

conn = sqlite3.connect(DB)

# ── Idempotency check ────────────────────────────────────────────────────────
tables  = {r[0] for r in conn.execute(\"SELECT name FROM sqlite_master WHERE type='table'\").fetchall()}
tp_cols = {r[1] for r in conn.execute('PRAGMA table_info(track_pools)').fetchall()}

if 'pools' in tables and 'pool_id' in tp_cols:
    print('  Already migrated — nothing to do.')
    conn.close()
    sys.exit(0)

def row_count(tbl):
    return conn.execute(f'SELECT COUNT(*) FROM {tbl}').fetchone()[0]

# ── Begin exclusive transaction ───────────────────────────────────────────────
# EXCLUSIVE prevents any other reader/writer from touching the DB while we
# rebuild tables.  All DDL in SQLite is transactional, so a failure at any
# step rolls back the entire migration.
conn.isolation_level = None   # manual transaction control
conn.execute('BEGIN EXCLUSIVE')
try:
    # ── 1. Create lookup tables ───────────────────────────────────────────────
    print('  Creating lookup tables...')
    conn.execute('''
        CREATE TABLE IF NOT EXISTS pools (
            id   INTEGER PRIMARY KEY,
            name VARCHAR NOT NULL UNIQUE
        )
    ''')
    conn.execute('''
        CREATE TABLE IF NOT EXISTS weather_types (
            id   INTEGER PRIMARY KEY,
            type VARCHAR NOT NULL UNIQUE
        )
    ''')

    # Populate pools from all sources (union covers any divergence between tables)
    conn.execute('''
        INSERT OR IGNORE INTO pools(name)
        SELECT name      FROM track_pools     WHERE name       IS NOT NULL
        UNION
        SELECT pool_name FROM daily_avg_cache WHERE pool_name  IS NOT NULL
        ORDER BY 1
    ''')
    n_pools = conn.execute('SELECT COUNT(*) FROM pools').fetchone()[0]
    print(f'  pools: {n_pools} entries')

    # Seed the full canonical set of weather type strings unconditionally so
    # that a fresh DB (no weather rows yet) still gets all 7 entries, then
    # absorb any extra values already present in the data.
    for wtype in ('clear', 'partly_cloudy', 'foggy', 'rain', 'snow', 'thunderstorm', 'unknown'):
        conn.execute('INSERT OR IGNORE INTO weather_types(type) VALUES (?)', (wtype,))
    conn.execute('''
        INSERT OR IGNORE INTO weather_types(type)
        SELECT DISTINCT weather_type FROM weather          WHERE weather_type IS NOT NULL
        UNION
        SELECT DISTINCT weather_type FROM weather_forecast WHERE weather_type IS NOT NULL
    ''')
    n_wtypes = conn.execute('SELECT COUNT(*) FROM weather_types').fetchone()[0]
    print(f'  weather_types: {n_wtypes} entries')

    # ── 2. Rebuild track_pools ────────────────────────────────────────────────
    n_before = row_count('track_pools')
    print(f'  Rebuilding track_pools ({n_before:,} rows)...')
    conn.execute('''
        CREATE TABLE track_pools_new (
            id      INTEGER PRIMARY KEY AUTOINCREMENT,
            pool_id INTEGER NOT NULL REFERENCES pools(id),
            dtime   DATETIME DEFAULT CURRENT_TIMESTAMP,
            utility INT
        )
    ''')
    conn.execute('''
        INSERT INTO track_pools_new (id, pool_id, dtime, utility)
        SELECT tp.id, p.id, tp.dtime, tp.utility
        FROM   track_pools tp
        JOIN   pools p ON p.name = tp.name
    ''')
    n_after = row_count('track_pools_new')
    if n_after != n_before:
        raise RuntimeError(f'track_pools row count mismatch: {n_before} -> {n_after}')
    conn.execute('DROP TABLE track_pools')
    conn.execute('ALTER TABLE track_pools_new RENAME TO track_pools')
    print(f'  track_pools rebuilt ({n_after:,} rows transferred).')

    # ── 3. Rebuild daily_avg_cache ────────────────────────────────────────────
    n_before = row_count('daily_avg_cache')
    print(f'  Rebuilding daily_avg_cache ({n_before:,} rows)...')
    conn.execute('''
        CREATE TABLE daily_avg_cache_new (
            id               INTEGER PRIMARY KEY AUTOINCREMENT,
            pool_id          INTEGER NOT NULL REFERENCES pools(id),
            slot_index       INT     NOT NULL,
            mean_utilization REAL    NOT NULL,
            std_dev          REAL    NOT NULL,
            sample_count     INT     NOT NULL,
            updated_at       DATETIME DEFAULT CURRENT_TIMESTAMP,
            UNIQUE(pool_id, slot_index)
        )
    ''')
    conn.execute('''
        INSERT INTO daily_avg_cache_new
               (id, pool_id, slot_index, mean_utilization, std_dev, sample_count, updated_at)
        SELECT dac.id, p.id, dac.slot_index,
               dac.mean_utilization, dac.std_dev, dac.sample_count, dac.updated_at
        FROM   daily_avg_cache dac
        JOIN   pools p ON p.name = dac.pool_name
    ''')
    n_after = row_count('daily_avg_cache_new')
    if n_after != n_before:
        raise RuntimeError(f'daily_avg_cache row count mismatch: {n_before} -> {n_after}')
    conn.execute('DROP TABLE daily_avg_cache')
    conn.execute('ALTER TABLE daily_avg_cache_new RENAME TO daily_avg_cache')
    print(f'  daily_avg_cache rebuilt ({n_after:,} rows transferred).')

    # ── 4. Rebuild weather ────────────────────────────────────────────────────
    n_before = row_count('weather')
    print(f'  Rebuilding weather ({n_before:,} rows)...')
    conn.execute('''
        CREATE TABLE weather_new (
            id              INTEGER PRIMARY KEY AUTOINCREMENT,
            dtime           DATETIME DEFAULT CURRENT_TIMESTAMP,
            temperature     REAL,
            wind_speed      REAL,
            wind_direction  REAL,
            precipitation   REAL,
            cloud_cover     INT,
            weather_code    INT,
            weather_type_id INTEGER NOT NULL REFERENCES weather_types(id)
        )
    ''')
    # LEFT JOIN with COALESCE so any legacy NULL weather_type rows map to
    # 'unknown' instead of being silently dropped.
    conn.execute('''
        INSERT INTO weather_new
               (id, dtime, temperature, wind_speed, wind_direction,
                precipitation, cloud_cover, weather_code, weather_type_id)
        SELECT w.id, w.dtime, w.temperature, w.wind_speed, w.wind_direction,
               w.precipitation, w.cloud_cover, w.weather_code,
               COALESCE(wt.id, (SELECT id FROM weather_types WHERE type = 'unknown'))
        FROM   weather w
        LEFT JOIN weather_types wt ON wt.type = w.weather_type
    ''')
    n_after = row_count('weather_new')
    if n_after != n_before:
        raise RuntimeError(f'weather row count mismatch: {n_before} -> {n_after}')
    conn.execute('DROP TABLE weather')
    conn.execute('ALTER TABLE weather_new RENAME TO weather')
    print(f'  weather rebuilt ({n_after:,} rows transferred).')

    # ── 5. Rebuild weather_forecast ───────────────────────────────────────────
    n_before = row_count('weather_forecast')
    print(f'  Rebuilding weather_forecast ({n_before:,} rows)...')
    conn.execute('''
        CREATE TABLE weather_forecast_new (
            id              INTEGER PRIMARY KEY AUTOINCREMENT,
            dtime           DATETIME NOT NULL,
            temperature     REAL,
            wind_speed      REAL,
            precipitation   REAL,
            cloud_cover     INT,
            weather_code    INT,
            weather_type_id INTEGER NOT NULL REFERENCES weather_types(id),
            fetched_at      DATETIME
        )
    ''')
    conn.execute('''
        INSERT INTO weather_forecast_new
               (id, dtime, temperature, wind_speed, precipitation,
                cloud_cover, weather_code, weather_type_id, fetched_at)
        SELECT wf.id, wf.dtime, wf.temperature, wf.wind_speed, wf.precipitation,
               wf.cloud_cover, wf.weather_code,
               COALESCE(wt.id, (SELECT id FROM weather_types WHERE type = 'unknown')),
               wf.fetched_at
        FROM   weather_forecast wf
        LEFT JOIN weather_types wt ON wt.type = wf.weather_type
    ''')
    n_after = row_count('weather_forecast_new')
    if n_after != n_before:
        raise RuntimeError(f'weather_forecast row count mismatch: {n_before} -> {n_after}')
    conn.execute('DROP TABLE weather_forecast')
    conn.execute('ALTER TABLE weather_forecast_new RENAME TO weather_forecast')
    print(f'  weather_forecast rebuilt ({n_after:,} rows transferred).')

    # ── 6. Recreate indexes ───────────────────────────────────────────────────
    print('  Recreating indexes...')
    # track_pools: covering index now keyed on pool_id (integer) instead of name
    conn.execute('CREATE INDEX idx_track_pools_pool_id_dtime ON track_pools(pool_id, dtime, utility)')
    conn.execute('CREATE INDEX idx_track_pools_dtime         ON track_pools(dtime)')
    conn.execute('CREATE INDEX idx_weather_dtime             ON weather(dtime)')
    conn.execute('CREATE UNIQUE INDEX idx_weather_forecast_dtime ON weather_forecast(dtime)')

    conn.execute('COMMIT')
    print('  Schema changes committed successfully.')

except Exception as exc:
    conn.execute('ROLLBACK')
    print(f'  MIGRATION FAILED — rolled back. Error: {exc}', file=__import__(\"sys\").stderr)
    __import__(\"sys\").exit(1)

# ── 7. VACUUM ─────────────────────────────────────────────────────────────────
# Must run outside a transaction.  Rewrites the entire DB file, immediately
# returning the pages freed by dropping the old VARCHAR columns.
print('  Running VACUUM to reclaim freed space...')
conn.isolation_level = ''   # restore default isolation level for VACUUM
conn.execute('VACUUM')
print('  VACUUM complete.')

# ── Summary ───────────────────────────────────────────────────────────────────
print()
print('  Final table sizes:')
for tbl in ('track_pools', 'daily_avg_cache', 'weather', 'weather_forecast',
            'pools', 'weather_types', 'predictions'):
    n = conn.execute(f'SELECT COUNT(*) FROM {tbl}').fetchone()[0]
    print(f'    {tbl:<22s}  {n:>8,} rows')

conn.close()
print()
print('  Migration 007 complete.')
"

echo ""
echo "[2/3] Schema migration applied successfully."
echo ""

# ---------------------------------------------------------------------------
# Step 3 — optionally restart all services with the new images
# ---------------------------------------------------------------------------
if [[ "$UP" == "true" ]]; then
  echo "[3/3] Starting all services..."
  docker compose up -d
  echo ""
  echo "  Services started."
else
  echo "[3/3] Services remain stopped."
  echo "      Start them when ready with: docker compose up -d"
fi

echo ""
echo "============================================================"
echo " Migration 007 complete."
echo "============================================================"
