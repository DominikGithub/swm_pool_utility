# SWM Pool Utility - Web Interface

A web interface for viewing historical swimming pool utility data from SWM (Stadtwerke München).

## Features

- **Interactive Charts**: Visualize pool utilization over time using Chart.js
- **Pool Selection**: View data for specific pools or all pools combined
- **Date Range Filtering**: Filter data by custom date ranges
- **Statistics Dashboard**: View average, peak, and minimum utility values
- **Responsive Design**: Works on desktop and mobile devices
- **Real-time Data**: Load the latest data from the SQLite database

## Usage

### Quick Demo (No Go Required)
Simply open `index.html` in your web browser to see a demo with sample data.

### Web Server Mode (Requires Go Installation)
```bash
# Option 1: Direct run (may have VCS stamping issue)
go run -buildvcs=false . -web

# Option 2: Use build script (recommended)
./build.sh
./swm_pool_utility -web

# Option 3: Manual build
go build -buildvcs=false -o swm_pool_utility .
./swm_pool_utility -web
```

### Troubleshooting Go Issues
If you get "VCS stamping" error:
1. Use `-buildvcs=false` flag
2. Install Go first: https://golang.org/dl/
3. Ensure git is initialized: `git init`

Then open your browser and navigate to: `http://localhost:8080`

### Scraping Mode (Original)
```bash
go run .
```
or
```bash
./swm_pool_utility
```

## API Endpoints

- `GET /` - Main web interface
- `GET /api/pools` - List all available pools
- `GET /api/data` - Get historical data with optional filters:
  - `pools` - Comma-separated list of pool names
  - `start` - Start date (YYYY-MM-DD)
  - `end` - End date (YYYY-MM-DD)

### Example API Calls
```bash
# Get all pools
curl http://localhost:8080/api/pools

# Get data for specific pools
curl "http://localhost:8080/api/data?pools=Pool1,Pool2"

# Get data for date range
curl "http://localhost:8080/api/data?start=2024-01-01&end=2024-01-07"
```

## Database Setup

The application uses SQLite database. If you need to recreate the database:

```sql
sqlite3 swm_pool_utility.db "VACUUM;"
sqlite3 swm_pool_utility.db "create table track_pools(id integer primary key AUTOINCREMENT, name varchar, dtime datetime default current_timestamp, utility int);"
```

To drop and recreate:
```sql
sqlite3 swm_pool_utility.db "drop table track_pools;"
```

## Dependencies

- Go 1.25.4+
- github.com/PuerkitoBio/goquery
- github.com/chromedp/chromedp
- github.com/mattn/go-sqlite3

## Web Interface Features

### Chart Visualization
- Line charts showing utility percentage over time
- Multiple pools can be displayed simultaneously
- Color-coded lines for easy pool identification
- Interactive tooltips on data points

### Filtering Options
- **Pool Selection**: Multi-select dropdown to choose specific pools
- **Date Range**: Start and end date pickers for custom time periods
- **Default View**: Shows last 7 days of data for all pools

### Statistics Dashboard
- **Average Utility**: Mean utilization across selected data
- **Peak Utility**: Highest utilization recorded
- **Minimum Utility**: Lowest utilization recorded
- **Data Points**: Total number of measurements

### Responsive Design
- Mobile-friendly layout
- Touch-enabled controls
- Adaptive chart sizing
- Clean, modern interface

## Development

The web interface is built with:
- **Backend**: Go standard library `net/http`
- **Frontend**: HTML5, CSS3, JavaScript
- **Charts**: Chart.js (CDN)
- **Database**: SQLite with go-sqlite3 driver

## File Structure

```
/home/swm_pool_utility/
├── swm_scraper.go      # Main application with scraping and web server
├── web_server.go       # Web server and API handlers
├── go.mod             # Go module dependencies
├── go.sum             # Dependency checksums
├── README.md          # This file
├── static/            # Static files (CSS, JS, images)
└── swm_pool_utility.db # SQLite database (auto-generated)
```

## Troubleshooting

1. **Database not found**: Run the scraper first to create the database
2. **Port 8080 in use**: Modify the port in `startWebServer()` function
3. **No data displayed**: Check if the database has records and try refreshing
4. **CORS issues**: The web interface serves from the same origin, so no CORS issues

## Future Enhancements

- Real-time data updates with WebSocket
- Export data to CSV/JSON
- Email alerts for high utilization
- Historical data comparison
- Pool-specific alerts and thresholds