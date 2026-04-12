# SWM Pool Utilization Monitor

A monitoring application that tracks historical pool utilization from SWM (Stadtwerke München) swimming pools and correlates it with weather conditions. The dashboard provides insights into how weather affects pool attendance.

[![Pool Dashboard](res/pool_dashboard_20260412_1.png)](http://grid.resolve.bar:8086/)

### Heatmap
![Heatmap](res/heatmap.png)

The heatmap view shows utilization patterns in 30 minute intervals (based on data over last 60 days).

### Daily Statistics
![Daily Statistics](res/dashboard_daily_average_michaelibad.png)

The Daily Statistics view derives general per week day statistics from historical data.


## Quick Start

```bash
./start.sh
```

This will:
1. Initialize the SQLite database with required tables (via `db-init` service)
2. Build all Docker images
3. Start all services

See the `Backend maintenance` section below for troubleshooting setup. First time setup may require manual data collection and aggregations services manually, to fill gaps in the backend datastore due to delayed scheduled service execution.

## Dashboard

### Chart
- **Pool utilization** — one coloured line per pool showing utilization (%) over time
- **Temperature** — subtle amber area chart indicating temperature (normalized to the 0–100% axis, range –10°C to 35°C)
- **Weather icon** — emoji icons, shown at weather-state change points:
  - Clear / ⛅ Partly cloudy / ☁️ Cloudy / 🌧️ Rain / 🌦️ Drizzle / ❄️ Snow / 🌨️ Sleet / ⛈️ Thunderstorm / 🌫️ Fog
  - 💨 Wind spike (≥15 km/h) / 🌬️ Very strong wind (≥30 km/h)

### Weather toggle
The toolbar button (☁️ / 🌤️) toggles weather overlays on/off:
- Enables/disables the temperature area fill in the chart
- Shows/hides weather icons on the chart
- Shows/hides the weather tile in the pool card list

### Daily Statistics
The "Daily Statistics" option shows the **recurring weekly utilization pattern**:
- In _single-pool mode_, the confidence-interval band (mean ± 1σ) highlights utilization variability
- The statistics tile shows data coverage: 
  - **Coverage** (historic time horizon taken into account for the statistics calculation)
  - **Samples** (total measurements)
  - **Last Update** (last cache refresh)

### Heatmap
The heatmap view shows the **weekly utilization pattern** across 30min time slots:
- **Rows**: Monday to Sunday
- **Columns**: Time of day (00:00 to 23:00, 30-minute resolution)
- **Colors**: Green (low crowds) → Yellow → Red (high crowds)
- **Gray cells**: Pool is closed (based on official SWM opening hours) or insufficient data

### Pool cards
- One card per pool showing the current (or hovered) utilization percentage
- Colour-coded: green < 40%, yellow 40–70%, red > 70%
- Star button to mark a favourite pool

### Weather tile
- Shows four metrics for the current or hovered timestamp: **Temp**, **Wind**, **Clouds**, **Precip**
- Wind speed is highlighted in red when ≥ 15 km/h

## Services

| Service | Technology | Description |
|---------|------------|-------------|
| **api** | Go/Gin | REST API serving pool utilization, weather, and prediction data |
| **pool-scraper** | Go | Collects real-time utilization data from the SWM website |
| **weather-scraper** | Go | Collects current weather data from Open-Meteo API |
| **weather-forecast-scraper** | Go | Collects 7-day weather forecast from Open-Meteo API |
| **daily-stats** | Go | Computes and caches daily statistics |
| **ml-prediction** | Python (scikit-learn) | Runs utilization predictions every 10 minutes |
| **ml-training** | Python (scikit-learn) | Retrains prediction models daily (runs as daemon) |
| **frontend** | Vue.js | Dashboard with historical charts and weather overlay |

### Configuration

| Service | Setting | Default | Description |
|---------|---------|---------|-------------|
| db-init | — | once | One-time setup: creates database file, tables, indexes (runs on first `./start.sh`) |
| pool-scraper | interval | 10 min | Pool data fetch frequency |
| weather-scraper | interval | 1 hour | Current weather fetch frequency |
| weather-forecast-scraper | interval | 1 hour | Weather forecast fetch frequency |
| daily-stats | interval | 1 hour | Daily statistics cache refresh frequency |
| ml-prediction | interval | 10 min | Prediction generation frequency |
| ml-training | interval | 24 hours | Model retraining frequency (runs as daemon) |
| api | port | 8085 | REST API port |
| frontend | port | 8086 | Dashboard port |

## Data Sources

### Pool Utilization
Scrapes real-time utilization ("Auslastung") from [SWM Bäder](https://www.swm.de/baeder/auslastung).
- Sampling frequency **10 minutes**
- Collects utilization percentage for each pool

### Weather Data
Fetches current weather conditions from [Open-Meteo API](https://open-meteo.com/) for Munich coordinates (48.1372°N, 11.5755°E).
- Sampling frequency **1 hour**
- Records temperature, wind speed/direction, precipitation, cloud cover, and weather type

### Weather Forecast
Fetches 7-day weather forecast from [Open-Meteo Forecast API](https://open-meteo.com/).
- Sampling frequency **1 hour**
- Stores hourly forecasts for predicting future pool utilization

### Timezone Handling

| Layer | Format | Timezone | Example |
|-------|--------|----------|---------|
| SWM website (source) | — | Europe/Berlin | "10:30" (local wall-clock) |
| Open-Meteo API (source) | ISO 8601 | Europe/Berlin (requested via `&timezone=`) | `2026-04-06T10:30` |
| SQLite storage (`dtime`) | `YYYY-MM-DD HH:MM:SS` | UTC | `2026-04-06 08:30:00` |
| daily-stats (slot computation) | `time.Time` → slot index | UTC → Europe/Berlin via `time.In()` | slot 189 = Mon 07:30 Berlin |
| REST API output | RFC 3339 | Europe/Berlin (with UTC offset) | `2026-04-06T10:30:00+02:00` |
| Frontend display | `toLocaleString` | Europe/Berlin (pinned via `timeZone`) | `06.04., 10:30` |

## API Endpoints

| Endpoint | Description |
|----------|-------------|
| `GET /api/health` | Health check |
| `GET /api/pools` | List all tracked pools |
| `GET /api/pool-status` | Current utilization status for all pools |
| `GET /api/history?days=1` | Get pool history (default: 24 hours) |
| `GET /api/history?pool=X&days=30` | Filter by specific pool |
| `GET /api/weather?days=1` | Get weather history (default: 24 hours) |
| `GET /api/daily-avg` | Get cached daily statistics |
| `GET /api/daily-avg?pool=X` | Get cached daily statistics for a specific pool |
| `GET /api/hourly-avg` | Get hourly average utilization per day-of-week (heatmap data) |
| `GET /api/hourly-avg?pool=X` | Get heatmap data for a specific pool |
| `GET /api/predictions` | Get current predictions for all pools |

## Data Storage

SQLite database stored in a Docker volume (`db_data`), which is mounted to the host system at `/var/lib/docker/volumes/swm_pool_utility_db_data/_data`.

### Tables

**track_pools**
| Column | Type | Description |
|--------|------|-------------|
| name | VARCHAR | Pool name |
| dtime | DATETIME | Timestamp of measurement |
| utility | INT | Utilization percentage (0-100) |

**weather**
| Column | Type | Description |
|--------|------|-------------|
| dtime | DATETIME | Timestamp of measurement |
| temperature | REAL | Temperature in °C |
| wind_speed | REAL | Wind speed in km/h |
| wind_direction | REAL | Wind direction in degrees |
| precipitation | REAL | Precipitation in mm |
| cloud_cover | INT | Cloud cover percentage (0-100) |
| weather_code | INT | WMO weather code |
| weather_type | VARCHAR | Simplified weather type (clear, partly_cloudy, cloudy, rain, drizzle, snow, sleet, thunderstorm, fog) |

**daily_avg_cache**
| Column | Type | Description |
|--------|------|-------------|
| pool_name | VARCHAR | Pool name |
| slot_index | INT | Time slot index (0–1007, representing Mon 00:00 to Sun 23:50 in 10-min steps) |
| mean_utilization | REAL | Mean utilization percentage for this pool and slot |
| std_dev | REAL | Population standard deviation |
| sample_count | INT | Number of data points contributing to the mean |
| updated_at | DATETIME | Timestamp of the last cache refresh |

**weather_forecast**
| Column | Type | Description |
|--------|------|-------------|
| dtime | DATETIME | Forecast timestamp |
| temperature | REAL | Forecast temperature in °C |
| wind_speed | REAL | Forecast wind speed in km/h |
| precipitation | REAL | Forecast precipitation in mm |
| cloud_cover | INT | Cloud cover percentage (0-100) |
| weather_code | INT | WMO weather code |
| weather_type | VARCHAR | Simplified weather type |
| fetched_at | DATETIME | When the forecast was fetched |

**predictions**
| Column | Type | Description |
|--------|------|-------------|
| pool_name | VARCHAR | Pool name |
| current_util | REAL | Current utilization percentage |
| pred_1h | REAL | Predicted utilization in 1 hour |
| pred_2h | REAL | Predicted utilization in 2 hours |
| delta_1h | REAL | Change in utilization after 1 hour |
| delta_2h | REAL | Change in utilization after 2 hours |
| trend_strength | REAL | Magnitude of predicted swing (0-1) |
| trend_direction | VARCHAR | "up", "down", or "stable" |
| model_version | VARCHAR | Version identifier of the model |
| created_at | DATETIME | When the prediction was generated |
| pred_series | TEXT | JSON array of 12 predictions (2h in 10-min steps) |

## Backend Maintenance

The SQLite database is stored in a Docker volume (`db_data`). Use the following commands to back up and restore.

### Backup

```bash
# Copy the database file from the volume to the host
docker cp swm_pool_utility-api-1:/data/swm_pool_utility.db ./backup.db
```

### Restore

```bash
# Verify host volume mount point
docker volume inspect swm_pool_utility_db_data --format '{{ .Mountpoint }}'

# Stop the containers to ensure file consistency
docker compose stop api pool-scraper weather-scraper daily-stats

# Replace the .db file on the hosts docker volume mount point
cp ./backup.db $(docker volume inspect swm_pool_utility_db_data --format '{{ .Mountpoint }}')/swm_pool_utility.db

# Restart services
docker compose start api pool-scraper weather-scraper daily-stats
```

### Trigger Average Statistics Update

```bash
# Run backend statistics updater manually
docker compose exec daily-stats /app/daily-stats --once
```

### Model Retraining

The `ml-training` service runs as a daemon: it retrains all pool models on startup, then repeats every 24 hours. The `ml-prediction` service detects updated `.joblib` files at the start of the next 10-minute cycle and hot-reloads them — no restart needed.

To force an immediate retrain (retrains now, then resumes the daily schedule):

```bash
# Retrain and redeploy now:
docker compose up -d --build ml-training ml-prediction
docker compose run --rm ml-training          # retrain now
docker compose exec ml-prediction python predict.py
```

For targeted runs (single pool, validation):

```bash
# Single pool — overrides the daemon command with an explicit flag
docker compose run --rm ml-training --pool "Michaelibad"

# Validate metrics without saving models
docker compose run --rm ml-training --validate-only
```

> **Note:** The `avg_weekday_delta` feature depends on `daily_avg_cache`. Make sure `daily-stats` has run at least once before the first training cycle (`docker compose exec daily-stats /app/daily-stats --once`).

#### Changing the Prediction Interval

The prediction step size is controlled by `PREDICTION_INTERVAL_MINUTES` in `ml-prediction/predict.py`.

| Constant | Formula | Example (10 min) | Example (30 min) |
|---|---|---|---|
| `PREDICTION_INTERVALS_1H` | `60 // interval` | `6` | `2` |
| `PREDICTION_INTERVALS_2H` | `120 // interval` | `12` | `4` |

After editing `predict.py`, restart the prediction service to apply the new code:

```bash
docker compose restart ml-prediction
```

_The service does **not** hot-reload code changes — only model file changes (`.joblib`) are detected automatically._


## Predictions

### Overview

Each pool tile shows a trend indicator — a circular icon with a directional arrow — giving a short-term forecast of utilization changes. The prediction service runs every 10 minutes and stores one row per pool in the `predictions` table.

### Trend Direction

The arrow indicates whether utilization is expected to **increase**, **decrease**, or remain **stable** in the next hour:

- **Up** — utilization is predicted to rise by more than 5% in the next hour
- **Down** — utilization is predicted to fall by more than 5% in the next hour
- **Stable** — change is less than 5% (hidden)

### Trend Strength

The thin bar below the arrow shows the **magnitude** of the predicted swing. It is proportional to `trend_strength`, which is calculated as:

```
trend_strength = (|delta_1h| + |delta_2h|) / 2
```

Where:
- `delta_1h` = predicted_utilization(1h) − current_utilization
- `delta_2h` = predicted_utilization(2h) − current_utilization

A bar at 50% width means the model expects roughly a 10% swing in either direction over the next two hours. A bar at 20% means only a ~4% change is expected. This helps distinguish a firm signal (large bar) from noise (small bar) even when the direction arrow is the same.

### Prediction Services

| Service | Technology | Interval |
|---------|-----------|----------|
| **weather-forecast-scraper** | Go | hourly — fetches 7-day weather forecast from Open-Meteo |
| **ml-prediction** | Python (scikit-learn) | 10 min — loads RandomForest models, runs inference, upserts to `predictions` |
| **ml-training** | Python (scikit-learn) | manual — retrains models from historical data |

### Model Architecture

Per-pool **RandomForestRegressor** (one model per pool), trained on:
- **Temporal features** — hour, day-of-week, day-of-year, season, is-weekend, is-holiday, days-to-holiday
- **Weather features** — temperature, wind speed, precipitation, cloud cover
- **Lag features** — utilization at 10/20/30/60 minutes ago, 30-minute and 1-hour rolling means, change/momentum, acceleration
- **Seasonality feature** — `avg_weekday_delta`: typical utilization change over the next 30 min at this weekday+time of day, derived from `daily_avg_cache`

Prediction horizon: 2 hours ahead in 10-minute steps (12 steps per cycle). The service extracts step +1h and +2h, computes the delta from current utilization, and stores `delta_1h`, `delta_2h`, and `trend_strength` per pool.

### Model Training

```
Loaded 44721 rows for 9 pools
Loaded 340 weather rows
Loaded 9072 daily-avg cache entries
After feature engineering: 44613 rows
  Train: 3999 rows, Val: 1000 rows  (80/20 split)
  Training RandomForest for 'Dante-Winter-Warmfreibad' on 3999 samples...

Example feature distribution:
  Dante-Winter-Warmfreibad — delta MAE: 0.36pp, RMSE: 0.71pp, R²: 0.476
  Feature importances:
    util_momentum             0.2704  █████████████████████████████████████████████████████████████████████████████████
    avg_weekday_delta         0.1790  ██████████████████████████████████████████████████████
    util_change_30m           0.0810  ████████████████████████
    hour                      0.0748  ██████████████████████
    util_accel                0.0640  ███████████████████
    util_rolling_30m          0.0509  ███████████████
    util_lag_60m              0.0465  ██████████████
    day_of_year               0.0385  ████████████
    util_lag_30m              0.0318  ██████████
    util_rolling_1h           0.0238  ███████
    temperature               0.0227  ███████
    util_lag_20m              0.0203  ██████
    util_lag_10m              0.0177  █████
    util_change_10m           0.0173  █████
    wind_speed                0.0162  █████
    minute                    0.0156  █████
    cloud_cover               0.0146  ████
    day_of_week               0.0126  ████
    is_holiday                0.0009  █
    precipitation             0.0009  █
    is_weekend                0.0006  █
    season                    0.0000  █
    days_to_holiday           0.0000  █
=== Summary ===
  Bad Giesing-Harlaching: MAE=0.4%  R²=0.345
  Cosimawellenbad: MAE=0.4%  R²=0.367
  Dante-Winter-Warmfreibad: MAE=0.4%  R²=0.476
  Michaelibad: MAE=0.5%  R²=0.571
  Müller’sches Volksbad: MAE=0.6%  R²=0.417
  Nordbad: MAE=0.5%  R²=0.444
  Olympia-Schwimmhalle: MAE=0.3%  R²=0.422
  Südbad: MAE=0.5%  R²=0.343
  Westbad: MAE=0.4%  R²=0.466
```