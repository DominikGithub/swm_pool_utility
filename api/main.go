package main

import (
	"database/sql"
	"fmt"
	"log"
	"strconv"
	"time"
	_ "time/tzdata" // embed IANA timezone database for Europe/Berlin

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
)

var (
	db        *sql.DB
	berlinLoc *time.Location
)

type DataPoint struct {
	Name      string `json:"name"`
	Timestamp string `json:"timestamp"`
	Utility   int    `json:"utility"`
}

type WeatherPoint struct {
	Timestamp     string  `json:"timestamp"`
	Temperature   float64 `json:"temperature"`
	WindSpeed     float64 `json:"wind_speed"`
	CloudCover    int     `json:"cloud_cover"`
	WeatherType   string  `json:"weather_type"`
	Precipitation float64 `json:"precipitation"`
}

func getPools(c *gin.Context) {
	rows, err := db.Query("SELECT DISTINCT name FROM track_pools ORDER BY name")
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	var pools []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			continue
		}
		pools = append(pools, name)
	}
	c.JSON(200, pools)
}

func getHistory(c *gin.Context) {
	pool := c.Query("pool")
	daysStr := c.DefaultQuery("days", "1")
	days, _ := strconv.Atoi(daysStr)

	query := "SELECT name, dtime, utility FROM track_pools"
	var args []interface{}

	if days > 0 {
		// Format cutoff as the same "YYYY-MM-DD HH:MM:SS" UTC string that SQLite
		// stores, so the string comparison is unambiguous.
		cutoff := time.Now().UTC().AddDate(0, 0, -days).Format("2006-01-02 15:04:05")
		query += " WHERE dtime >= ?"
		args = append(args, cutoff)
		if pool != "" {
			query += " AND name = ?"
			args = append(args, pool)
		}
	} else {
		if pool != "" {
			query += " WHERE name = ?"
			args = append(args, pool)
		}
	}

	query += " ORDER BY dtime ASC"

	rows, err := db.Query(query, args...)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	var data []DataPoint
	for rows.Next() {
		var d DataPoint
		var dtime time.Time // go-sqlite3 parses DATETIME columns into time.Time (UTC)
		if err := rows.Scan(&d.Name, &dtime, &d.Utility); err != nil {
			continue
		}
		// Output as Berlin local time with UTC offset — unambiguous and display-ready.
		// e.g. "2026-04-06T10:30:00+02:00" (CEST) or "2026-01-15T09:30:00+01:00" (CET)
		d.Timestamp = dtime.In(berlinLoc).Format(time.RFC3339)
		data = append(data, d)
	}
	c.JSON(200, data)
}

func getWeather(c *gin.Context) {
	daysStr := c.DefaultQuery("days", "1")
	days, _ := strconv.Atoi(daysStr)

	query := "SELECT dtime, temperature, wind_speed, cloud_cover, weather_type, precipitation FROM weather"
	var args []interface{}

	if days > 0 {
		cutoff := time.Now().UTC().AddDate(0, 0, -days).Format("2006-01-02 15:04:05")
		query += " WHERE dtime >= ?"
		args = append(args, cutoff)
	}

	query += " ORDER BY dtime ASC"

	rows, err := db.Query(query, args...)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	var data []WeatherPoint
	for rows.Next() {
		var d WeatherPoint
		var dtime time.Time
		if err := rows.Scan(&dtime, &d.Temperature, &d.WindSpeed, &d.CloudCover, &d.WeatherType, &d.Precipitation); err != nil {
			continue
		}
		d.Timestamp = dtime.In(berlinLoc).Format(time.RFC3339)
		data = append(data, d)
	}
	c.JSON(200, data)
}

func health(c *gin.Context) {
	c.JSON(200, gin.H{"status": "ok"})
}

func getDailyAvg(c *gin.Context) {
	pool := c.Query("pool")

	query := `SELECT pool_name, slot_index, mean_utilization, std_dev, sample_count, updated_at FROM daily_avg_cache`
	var args []interface{}
	if pool != "" {
		query += ` WHERE pool_name = ?`
		args = append(args, pool)
	}
	query += ` ORDER BY pool_name, slot_index`

	rows, err := db.Query(query, args...)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	type slotData struct {
		mean        float64
		stddev      float64
		sampleCount int
	}

	// poolName -> slotIndex -> slotData
	byPool := map[string]map[int]slotData{}
	var updatedAt time.Time
	var totalSamples int

	for rows.Next() {
		var poolName string
		var si int
		var mean, stddev float64
		var count int
		var ts time.Time
		if err := rows.Scan(&poolName, &si, &mean, &stddev, &count, &ts); err != nil {
			continue
		}
		if byPool[poolName] == nil {
			byPool[poolName] = map[int]slotData{}
		}
		byPool[poolName][si] = slotData{mean: mean, stddev: stddev, sampleCount: count}
		totalSamples += count
		if ts.After(updatedAt) {
			updatedAt = ts
		}
	}

	if len(byPool) == 0 {
		c.JSON(200, gin.H{"labels": []string{}, "datasets": []interface{}{}})
		return
	}

	// Build ordered labels: "Mon 00:00", "Mon 00:10", ..., "Sun 23:50"
	shortDays := [7]string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}
	slotsPerDay := 144
	labels := make([]string, 0, 7*slotsPerDay)
	for d := 0; d < 7; d++ {
		for h := 0; h < 24; h++ {
			for m := 0; m < 60; m += 10 {
				labels = append(labels, fmt.Sprintf("%s %02d:%02d", shortDays[d], h, m))
			}
		}
	}

	type dataset struct {
		Label       string    `json:"label"`
		Data        []float64 `json:"data"`
		StdDev      []float64 `json:"stddev"`
		SampleCount []int     `json:"sample_count"`
	}

	var datasets []dataset
	for poolName, slots := range byPool {
		ds := dataset{
			Label:       poolName,
			Data:        make([]float64, len(labels)),
			StdDev:      make([]float64, len(labels)),
			SampleCount: make([]int, len(labels)),
		}
		for i := range ds.Data {
			ds.Data[i] = -1 // sentinel: no data
		}
		for si, sd := range slots {
			if si >= 0 && si < len(labels) {
				ds.Data[si] = sd.mean
				ds.StdDev[si] = sd.stddev
				ds.SampleCount[si] = sd.sampleCount
			}
		}
		datasets = append(datasets, ds)
	}

	// Compute date range from raw timestamps, converting to Berlin time.
	// MIN/MAX aggregates don't carry the DATETIME column type, so go-sqlite3
	// returns them as plain strings — scan into string and parse manually.
	var dtimeMinStr, dtimeMaxStr string
	db.QueryRow(`SELECT MIN(dtime), MAX(dtime) FROM track_pools`).Scan(&dtimeMinStr, &dtimeMaxStr)

	dtimeMin, _ := time.Parse("2006-01-02 15:04:05", dtimeMinStr)
	dtimeMax, _ := time.Parse("2006-01-02 15:04:05", dtimeMaxStr)
	dateFrom := dtimeMin.In(berlinLoc).Format("2006-01-02")
	dateTo := dtimeMax.In(berlinLoc).Format("2006-01-02")

	weeksDuration := dtimeMax.Sub(dtimeMin).Hours() / (24 * 7)
	if weeksDuration < 1.0 {
		weeksDuration = 1.0
	}

	// Format updated_at in Berlin time
	var updatedAtStr string
	if !updatedAt.IsZero() {
		updatedAtStr = updatedAt.In(berlinLoc).Format(time.RFC3339)
	}

	c.JSON(200, gin.H{
		"labels":        labels,
		"datasets":      datasets,
		"updated_at":    updatedAtStr,
		"total_samples": totalSamples,
		"weeks":         fmt.Sprintf("%.1f", weeksDuration),
		"date_from":     dateFrom,
		"date_to":       dateTo,
	})
}

func getPredictions(c *gin.Context) {
	pool := c.Query("pool")
	hoursStr := c.DefaultQuery("hours", "6")
	hours, _ := strconv.Atoi(hoursStr)
	if hours <= 0 || hours > 24 {
		hours = 6
	}

	cutoff := time.Now().UTC().Format("2006-01-02 15:04:05")

	query := "SELECT pool_name, dtime, predicted_utilization FROM predictions WHERE dtime >= ?"
	var args []interface{}
	args = append(args, cutoff)
	if pool != "" {
		query += " AND pool_name = ?"
		args = append(args, pool)
	}
	query += " ORDER BY pool_name, dtime ASC"

	rows, err := db.Query(query, args...)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	type PredPoint struct {
		Pool     string  `json:"pool"`
		Time     string  `json:"time"`
		Value    float64 `json:"value"`
	}

	var predictions []PredPoint
	for rows.Next() {
		var poolName string
		var dtimeStr string
		var value float64
		if err := rows.Scan(&poolName, &dtimeStr, &value); err != nil {
			continue
		}
		dtime, _ := time.Parse(time.RFC3339, dtimeStr)
		predictions = append(predictions, PredPoint{
			Pool:  poolName,
			Time:  dtime.In(berlinLoc).Format(time.RFC3339),
			Value: value,
		})
	}
	c.JSON(200, predictions)
}

type PoolStatus struct {
	Name       string  `json:"name"`
	Current    int     `json:"current_util"`
	Predicted  float64 `json:"predicted_util"`
	Arrow      string  `json:"arrow"`
}

func getPoolStatus(c *gin.Context) {
	rows, err := db.Query(`
		SELECT name, utility FROM track_pools
		WHERE (name, dtime) IN (
			SELECT name, MAX(dtime) FROM track_pools GROUP BY name
		)
	`)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	current := map[string]int{}
	for rows.Next() {
		var name string
		var util int
		if err := rows.Scan(&name, &util); err != nil {
			continue
		}
		current[name] = 100 - util
	}

	cutoff := time.Now().UTC().Format("2006-01-02 15:04:05")
	predRows, err := db.Query(`
		SELECT pool_name, MIN(dtime), predicted_utilization FROM predictions
		WHERE dtime >= ? GROUP BY pool_name
	`, cutoff)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer predRows.Close()

	nextPred := map[string]float64{}
	for predRows.Next() {
		var poolName string
		var dtimeStr string
		var value float64
		if err := predRows.Scan(&poolName, &dtimeStr, &value); err != nil {
			continue
		}
		nextPred[poolName] = value
	}

	var statuses []PoolStatus
	for name, cur := range current {
		pred := nextPred[name]
		var arrow string
		diff := pred - float64(cur)
		switch {
		case diff > 5:
			arrow = "up"
		case diff < -5:
			arrow = "down"
		default:
			arrow = "stable"
		}
		statuses = append(statuses, PoolStatus{
			Name:      name,
			Current:   cur,
			Predicted: pred,
			Arrow:     arrow,
		})
	}
	c.JSON(200, statuses)
}

func main() {
	var err error

	berlinLoc, err = time.LoadLocation("Europe/Berlin")
	if err != nil {
		log.Fatal("failed to load Europe/Berlin timezone:", err)
	}

	db, err = sql.Open("sqlite3", "/data/swm_pool_utility.db")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	if _, err := db.Query("SELECT 1"); err != nil {
		log.Fatal("Database not accessible:", err)
	}

	r := gin.Default()
	r.Use(gin.Logger())

	r.GET("/api/pool-status", getPoolStatus)
	r.GET("/api/pools", getPools)
	r.GET("/api/history", getHistory)
	r.GET("/api/weather", getWeather)
	r.GET("/api/daily-avg", getDailyAvg)
	r.GET("/api/predictions", getPredictions)

	log.Println("API server running on 0.0.0.0:8085")
	if err := r.Run("0.0.0.0:8085"); err != nil {
		log.Fatal(err)
	}
}
