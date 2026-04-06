package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

var db *sql.DB

const (
	munichLat  = 48.1372
	munichLon  = 11.5755
	openMeteoURL = "https://api.open-meteo.com/v1/forecast"
)

type OpenMeteoResponse struct {
	Current struct {
		Time           string  `json:"time"`
		Temperature    float64 `json:"temperature_2m"`
		WindSpeed      float64 `json:"wind_speed_10m"`
		WindDirection  float64 `json:"wind_direction_10m"`
		Precipitation  float64 `json:"precipitation"`
		CloudCover     int     `json:"cloud_cover"`
		WeatherCode    int     `json:"weather_code"`
	} `json:"current"`
}

type WeatherData struct {
	Temperature   float64 `json:"temperature"`
	WindSpeed     float64 `json:"wind_speed"`
	WindDirection float64 `json:"wind_direction"`
	Precipitation float64 `json:"precipitation"`
	CloudCover    int     `json:"cloud_cover"`
	WeatherCode   int     `json:"weather_code"`
	WeatherType   string  `json:"weather_type"`
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

func fetchWeather() (*WeatherData, error) {
	url := fmt.Sprintf("%s?latitude=%.4f&longitude=%.4f&current=temperature_2m,wind_speed_10m,wind_direction_10m,precipitation,cloud_cover,weather_code&timezone=Europe/Berlin",
		openMeteoURL, munichLat, munichLon)

	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	var result OpenMeteoResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("JSON decode failed: %w", err)
	}

	weatherType := getWeatherType(result.Current.WeatherCode)

	return &WeatherData{
		Temperature:   result.Current.Temperature,
		WindSpeed:     result.Current.WindSpeed,
		WindDirection: result.Current.WindDirection,
		Precipitation: result.Current.Precipitation,
		CloudCover:    result.Current.CloudCover,
		WeatherCode:   result.Current.WeatherCode,
		WeatherType:   weatherType,
	}, nil
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

func saveWeather(w *WeatherData) error {
	// Explicitly store the current UTC timestamp. All timestamps in the database
	// are UTC ("YYYY-MM-DD HH:MM:SS"). Timezone conversion (e.g. to Europe/Berlin)
	// happens at read time in the API.
	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	_, err := db.Exec(`
		INSERT INTO weather (dtime, temperature, wind_speed, wind_direction, precipitation, cloud_cover, weather_code, weather_type)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		now, w.Temperature, w.WindSpeed, w.WindDirection, w.Precipitation, w.CloudCover, w.WeatherCode, w.WeatherType)
	return err
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

	fmt.Println("Weather scraper starting...")

	if err := fetchAndSaveWeather(); err != nil {
		log.Printf("Failed to fetch weather: %v", err)
		os.Exit(1)
	}

	if !runOnce {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()

		for range ticker.C {
			if err := fetchAndSaveWeather(); err != nil {
				log.Printf("Failed to fetch weather: %v", err)
			}
		}
	}
}

func fetchAndSaveWeather() error {
	weather, err := fetchWeather()
	if err != nil {
		return err
	}

	fmt.Printf("Weather: %.1f°C, %.1f km/h wind, %d%% clouds, %s\n",
		weather.Temperature, weather.WindSpeed, weather.CloudCover, weather.WeatherType)

	if err := saveWeather(weather); err != nil {
		return fmt.Errorf("failed to save weather: %w", err)
	}

	fmt.Println("Weather data saved successfully")
	return nil
}
