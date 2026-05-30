# Database Schema

SQLite database stored in a Docker volume (`db_data`), which is mounted to the host system at `/var/lib/docker/volumes/swm_pool_utility_db_data/_data`.

## Tables

**pools** *(lookup)*
| Column | Type | Description |
|--------|------|-------------|
| id | INTEGER PK | Auto-assigned surrogate key |
| name | VARCHAR UNIQUE | Pool name (e.g. "Michaelibad") |

**weather_types** *(lookup)*
| Column | Type | Description |
|--------|------|-------------|
| id | INTEGER PK | Auto-assigned surrogate key |
| type | VARCHAR UNIQUE | Weather type string (clear, partly_cloudy, foggy, rain, snow, thunderstorm, unknown) |

**track_pools**
| Column | Type | Description |
|--------|------|-------------|
| id | INTEGER PK | Auto-increment |
| pool_id | INTEGER FK → pools.id | Pool reference |
| dtime | DATETIME | Timestamp of measurement (UTC) |
| utility | INT | Capacity remaining, 0–100 (100 = full / closed) |

**weather**
| Column | Type | Description |
|--------|------|-------------|
| id | INTEGER PK | Auto-increment |
| dtime | DATETIME | Timestamp of measurement (UTC) |
| temperature | REAL | Temperature in °C |
| wind_speed | REAL | Wind speed in km/h |
| wind_direction | REAL | Wind direction in degrees |
| precipitation | REAL | Precipitation in mm |
| cloud_cover | INT | Cloud cover percentage (0–100) |
| weather_code | INT | WMO weather code |
| weather_type_id | INTEGER FK → weather_types.id | Weather type reference |

**daily_avg_cache**
| Column | Type | Description |
|--------|------|-------------|
| id | INTEGER PK | Auto-increment |
| pool_id | INTEGER FK → pools.id | Pool reference |
| slot_index | INT | Time slot index (0–1007: Mon 00:00 → Sun 23:50 in 10-min steps) |
| mean_utilization | REAL | Mean utilization percentage for this pool and slot |
| std_dev | REAL | Population standard deviation |
| sample_count | INT | Number of data points contributing to the mean |
| updated_at | DATETIME | Timestamp of the last cache refresh |

**weather_forecast**
| Column | Type | Description |
|--------|------|-------------|
| id | INTEGER PK | Auto-increment |
| dtime | DATETIME UNIQUE | Forecast timestamp (UTC) |
| temperature | REAL | Forecast temperature in °C |
| wind_speed | REAL | Forecast wind speed in km/h |
| precipitation | REAL | Forecast precipitation in mm |
| cloud_cover | INT | Cloud cover percentage (0–100) |
| weather_code | INT | WMO weather code |
| weather_type_id | INTEGER FK → weather_types.id | Weather type reference |
| fetched_at | DATETIME | When the forecast was fetched |

**predictions**
| Column | Type | Description |
|--------|------|-------------|
| pool_name | VARCHAR UNIQUE | Pool name |
| current_util | REAL | Current utilization percentage |
| pred_1h | REAL | Predicted utilization in 1 hour |
| pred_2h | REAL | Predicted utilization in 2 hours |
| delta_1h | REAL | Change in utilization after 1 hour |
| delta_2h | REAL | Change in utilization after 2 hours |
| trend_strength | REAL | Magnitude of predicted swing (0–1) |
| trend_direction | VARCHAR | "up", "down", or "stable" |
| model_version | VARCHAR | Version identifier of the model |
| created_at | DATETIME | When the prediction was generated |
| pred_series | TEXT | JSON array of 12 predictions (2 h in 10-min steps) |

## Migrations

Schema changes are applied as numbered migration scripts in `migrations/`. Each script is idempotent (safe to run more than once) and follows the same pattern:

1. Stop running services
2. Apply schema changes via a Python or SQLite session inside a container
3. Restart services

```bash
# Apply the migrations
docker compose build
bash migrations/<NNN>_<name>.sh --up
```
