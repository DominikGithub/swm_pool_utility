#!/bin/bash
# Migration 004: Add pred_series column to predictions table
# Stores the full prediction horizon as a JSON array so the frontend
# can render every step, not just the +1h and +2h endpoints.
docker compose run --rm --entrypoint python training -c "
import sqlite3, os
conn = sqlite3.connect(os.environ.get('DB_PATH', '/data/swm_pool_utility.db'))
try:
    conn.execute('ALTER TABLE predictions ADD COLUMN pred_series TEXT')
    conn.commit()
    print('Added pred_series column to predictions table')
except Exception as e:
    print(f'Skipped (likely already exists): {e}')
conn.close()
"
