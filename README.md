# SWM Pool Utilization Monitor

A monitoring application that tracks historical pool utilization from SWM (Stadtwerke München) swimming pools and correlates it with weather conditions. The dashboard provides insights into how weather affects pool attendance.

[![Pool Dashboard](res/pool_dashboard_20260405.png)](http://grid.resolve.bar:8086/)

### Average Utilization Statistics
![Daily Average Statistics](res/dashboard_daily_average_michaelibad.png)

The daily average view derives aggregated statistics from a sliding window over the last 90 days.

## Services

| Service | Technology | Description |
|---------|------------|-------------|
| **api** | Go/Gin | REST API serving pool utilization and weather data |
| **pool-scraper** | Go | Collects real-time utilization data from the SWM website |
| **weather-scraper** | Go | Collects weather data from Open-Meteo API |
| **frontend** | Vue.js | Dashboard with historical charts and weather overlay |
| **db-init** | Debian | One-time setup: creates database file and tables (runs on first `./start.sh`) |

### Configuration

| Service | Setting | Default | Description |
|---------|---------|---------|-------------|
| pool-scraper | interval | 10 min | Pool data fetch frequency |
| weather-scraper | interval | 1 hour | Weather data fetch frequency |
| api | port | 8085 | REST API port |
| frontend | port | 8086 | Dashboard port |


## Quick Start

```bash
./start.sh
```

This will:
1. Initialize the SQLite database with required tables (via `db-init` service)
2. Build all Docker images
3. Start all services

## Data Sources

### Pool Utilization
Scrapes real-time utilization ("Auslastung") from [SWM Bäder](https://www.swm.de/baeder/auslastung).
- Sampling frequency **10 minutes**
- Collects utilization percentage for each pool

### Weather Data
Fetches current weather conditions from [Open-Meteo API](https://open-meteo.com/) for Munich coordinates (48.1372°N, 11.5755°E).
- Sampling frequency **1 hour**
- Records temperature, wind speed/direction, precipitation, cloud cover, and weather type

## API Endpoints

| Endpoint | Description |
|----------|-------------|
| `GET /api/health` | Health check |
| `GET /api/pools` | List all tracked pools |
| `GET /api/history?days=1` | Get pool history (default: 24 hours) |
| `GET /api/history?pool=X&days=30` | Filter by specific pool |
| `GET /api/weather?days=1` | Get weather history (default: 24 hours) |

## Dashboard Features

### Chart
- **Pool utilization** — one coloured line per pool showing utilization (%) over time
- **Temperature fill** — subtle amber area chart indicating temperature (normalized to the 0–100% axis, range –10°C to 35°C)
- **Weather icon overlay** — emoji icons, shown at weather-state change points:
  - Clear / ⛅ Partly cloudy / ☁️ Cloudy / 🌧️ Rain / 🌦️ Drizzle / ❄️ Snow / 🌨️ Sleet / ⛈️ Thunderstorm / 🌫️ Fog
  - 💨 Wind spike (≥15 km/h) / 🌬️ Very strong wind (≥30 km/h)

### Weather toggle
The toolbar button (☁️ / 🌤️) toggles weather overlays on/off:
- Enables/disables the temperature area fill in the chart
- Shows/hides weather icons on the chart
- Shows/hides the weather tile in the pool card list

### Daily Average view
The "Daily Average" option shows the **recurring weekly utilization pattern**:
- One line per pool showing the average utilization for each weekday and time slot across all weeks in the dataset
- In **single-pool mode**, a shaded confidence-interval band (±1 standard deviation) highlights variability

### Pool cards
- One card per pool showing the current (or hovered) utilization percentage
- Colour-coded: green < 40%, yellow 40–70%, red > 70%
- Star button to mark a favourite pool

### Weather tile
- Shows four metrics for the current or hovered timestamp: **Temp**, **Wind**, **Clouds**, **Precip**
- Wind speed is highlighted in red when ≥ 15 km/h

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

## Database Backup & Restore

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
docker compose stop api pool-scraper weather-scraper

# Replace the .db file on the hosts docker volume mount point
cp ./backup.db $(docker volume inspect swm_pool_utility_db_data --format '{{ .Mountpoint }}')/swm_pool_utility.db

# Restart services
docker compose start api pool-scraper weather-scraper
```
