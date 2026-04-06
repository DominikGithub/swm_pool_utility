#!/bin/bash
# Migration 003: Replace predictions table with new trend-schema
docker compose run --rm --entrypoint python training -c "
import sqlite3, os
conn = sqlite3.connect(os.environ.get('DB_PATH', '/data/swm_pool_utility.db'))
conn.execute('DROP TABLE IF EXISTS predictions')
conn.execute('''CREATE TABLE predictions(
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    pool_name VARCHAR NOT NULL UNIQUE,
    current_util REAL NOT NULL,
    pred_1h REAL NOT NULL,
    pred_2h REAL NOT NULL,
    delta_1h REAL NOT NULL,
    delta_2h REAL NOT NULL,
    trend_strength REAL NOT NULL,
    trend_direction VARCHAR NOT NULL,
    model_version VARCHAR,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
)''')
conn.commit()
print('Recreated predictions table')
conn.close()
"
