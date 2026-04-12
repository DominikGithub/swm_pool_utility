/*
Weather forecast collector — fetches the 7-day Open-Meteo forecast.
Supplies the future weather context the prediction service needs to estimate upcoming pool utilization.
*/
package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

var db *sql.DB

const (
	munichLat        = 48.1372
	munichLon        = 11.5755
	openMeteoURL     = "https://api.open-meteo.com/v1/forecast"
	forecastDays     = 7
	intervalMinutes  = 10
)

type OpenMeteoHourlyResponse struct {
	Hourly struct {
		Time           []string  `json:"time"`
		Temperature    []float64 `json:"temperature_2m"`
		WindSpeed      []float64 `json:"wind_speed_10m"`
		Precipitation  []float64 `json:"precipitation"`
		CloudCover     []int     `json:"cloud_cover"`
		WeatherCode    []int     `json:"weather_code"`
	} `json:"hourly"`
}

type ForecastPoint struct {
	Dtime         string
	Temperature   float64
	WindSpeed     float64
	Precipitation float64
	CloudCover    int
	WeatherCode   int
	WeatherType   string
}

func initDB() {
	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "/data/swm_pool_utility.db"
	}

	var err error
	db, err = sql.Open("sqlite3", dbPath)
	if err != nil {
		log.Fatal(err)
	}
}

func getWeatherType(code int) string {
	switch {
	case code == 0:
		return "clear"
	case code == 1 || code == 2 || code == 3:
		return "partly_cloudy"
	case code == 45 || code == 48:
		return "foggy"
	case code >= 51 && code <= 67:
		return "rain"
	case code >= 71 && code <= 77:
		return "snow"
	case code >= 80 && code <= 82:
		return "rain"
	case code >= 85 && code <= 86:
		return "snow"
	case code >= 95 && code <= 99:
		return "thunderstorm"
	default:
		return "unknown"
	}
}

func fetchHourlyForecast() (*OpenMeteoHourlyResponse, error) {
	url := fmt.Sprintf(
		"%s?latitude=%.4f&longitude=%.4f&hourly=temperature_2m,wind_speed_10m,precipitation,cloud_cover,weather_code&forecast_days=%d&timezone=Europe/Berlin",
		openMeteoURL, munichLat, munichLon, forecastDays,
	)

	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	var result OpenMeteoHourlyResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("JSON decode failed: %w", err)
	}

	return &result, nil
}

func parseBerlinTime(s string) (time.Time, error) {
	s = strings.TrimSuffix(s, "Z")
	return time.ParseInLocation("2006-01-02T15:04", s, berlinLoc)
}

var berlinLoc *time.Location

func init() {
	var err error
	berlinLoc, err = time.LoadLocation("Europe/Berlin")
	if err != nil {
		log.Fatalf("failed to load Europe/Berlin timezone: %v", err)
	}
}

func interpolateLinear(a, b float64, t float64) float64 {
	return a + (b-a)*t
}

func interpolateInt(a, b int, t float64) int {
	return int(float64(a) + float64(b-a)*t)
}

func roundDown10Min(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(),
		(t.Minute()/intervalMinutes)*intervalMinutes, 0, 0, t.Location())
}

func fetchAndSaveForecast() error {
	result, err := fetchHourlyForecast()
	if err != nil {
		return fmt.Errorf("fetch failed: %w", err)
	}

	if _, err := db.Exec("DELETE FROM weather_forecast WHERE dtime < datetime('now', '-8 days')"); err != nil {
		log.Printf("warning: failed to clean old forecast rows: %v", err)
	}

	h := result.Hourly
	if len(h.Time) == 0 {
		return fmt.Errorf("no hourly data returned")
	}

	nowUTC := time.Now().UTC()
	fetchedAt := nowUTC.Format("2006-01-02 15:04:05")

	var points []ForecastPoint
	for i := 0; i < len(h.Time)-1; i++ {
		tStart, err := parseBerlinTime(h.Time[i])
		if err != nil {
			continue
		}
		if _, err := parseBerlinTime(h.Time[i+1]); err != nil {
			continue
		}

		tStartUTC := tStart.UTC()
		if tStartUTC.Before(nowUTC.Add(-1 * time.Hour)) {
			continue
		}

		steps := 60 / intervalMinutes
		for step := 0; step < steps; step++ {
			t := float64(step) / float64(steps)
			t10 := roundDown10Min(tStart.Add(time.Duration(step*intervalMinutes) * time.Minute))

			point := ForecastPoint{
				Dtime:         t10.UTC().Format("2006-01-02 15:04:05"),
				Temperature:   interpolateLinear(h.Temperature[i], h.Temperature[i+1], t),
				WindSpeed:     interpolateLinear(h.WindSpeed[i], h.WindSpeed[i+1], t),
				Precipitation: interpolateLinear(h.Precipitation[i], h.Precipitation[i+1], t),
				CloudCover:    interpolateInt(h.CloudCover[i], h.CloudCover[i+1], t),
				WeatherCode:   h.WeatherCode[i],
				WeatherType:   getWeatherType(h.WeatherCode[i]),
			}
			points = append(points, point)
		}
	}

	if len(points) == 0 {
		return fmt.Errorf("no future forecast points generated")
	}

	fmt.Printf("Generated %d forecast points for next %d days\n", len(points), forecastDays)

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT OR REPLACE INTO weather_forecast
		(dtime, temperature, wind_speed, precipitation, cloud_cover, weather_code, weather_type, fetched_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	count := 0
	for _, p := range points {
		_, err := stmt.Exec(p.Dtime, p.Temperature, p.WindSpeed, p.Precipitation,
			p.CloudCover, p.WeatherCode, p.WeatherType, fetchedAt)
		if err != nil {
			log.Printf("failed to insert forecast for %s: %v", p.Dtime, err)
			continue
		}
		count++
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	fmt.Printf("Saved %d forecast points\n", count)

	if time.Now().UTC().Weekday() == time.Sunday && time.Now().UTC().Hour() == 3 {
		fmt.Println("Running weekly VACUUM...")
		if _, err := db.Exec("VACUUM"); err != nil {
			log.Printf("warning: VACUUM failed: %v", err)
		} else {
			fmt.Println("VACUUM complete")
		}
	}

	return nil
}

func main() {
	runOnce := false
	for _, arg := range os.Args[1:] {
		if arg == "--once" || arg == "-o" {
			runOnce = true
		}
	}

	initDB()
	defer db.Close()

	fmt.Println("Weather forecast scraper starting...")

	if err := fetchAndSaveForecast(); err != nil {
		log.Printf("Failed to fetch forecast: %v", err)
		os.Exit(1)
	}

	if !runOnce {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()

		for range ticker.C {
			if err := fetchAndSaveForecast(); err != nil {
				log.Printf("Failed to fetch forecast: %v", err)
			}
		}
	}
}
