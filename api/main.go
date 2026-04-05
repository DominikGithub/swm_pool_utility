package main

import (
	"database/sql"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
)

var db *sql.DB

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
		cutoff := time.Now().AddDate(0, 0, -days)
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
		if err := rows.Scan(&d.Name, &d.Timestamp, &d.Utility); err != nil {
			continue
		}
		d.Timestamp = formatTimestamp(d.Timestamp)
		data = append(data, d)
	}
	c.JSON(200, data)
}

func formatTimestamp(ts string) string {
	// Golang format template from numeric example date
	t, err := time.Parse("2006-01-02 15:04:05", ts)
	if err != nil {
		return ts
	}
	return t.Format(time.RFC3339)
}

func getWeather(c *gin.Context) {
	daysStr := c.DefaultQuery("days", "1")
	days, _ := strconv.Atoi(daysStr)

	query := "SELECT dtime, temperature, wind_speed, cloud_cover, weather_type, precipitation FROM weather"
	var args []interface{}

	if days > 0 {
		cutoff := time.Now().AddDate(0, 0, -days)
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
		var ts string
		if err := rows.Scan(&ts, &d.Temperature, &d.WindSpeed, &d.CloudCover, &d.WeatherType, &d.Precipitation); err != nil {
			continue
		}
		d.Timestamp = formatTimestamp(ts)
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
		mean       float64
		stddev     float64
		sampleCount int
	}

	// poolName -> slotIndex -> slotData
	byPool := map[string]map[int]slotData{}
	var updatedAt string
	var totalSamples int

	for rows.Next() {
		var poolName string
		var si int
		var mean, stddev float64
		var count int
		var ts string
		if err := rows.Scan(&poolName, &si, &mean, &stddev, &count, &ts); err != nil {
			continue
		}
		if byPool[poolName] == nil {
			byPool[poolName] = map[int]slotData{}
		}
		byPool[poolName][si] = slotData{mean: mean, stddev: stddev, sampleCount: count}
		totalSamples += count
		if updatedAt == "" || ts > updatedAt {
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

	// Compute date range and weeks in SQL to avoid go-sqlite3 datetime parsing issues.
	var dateFrom, dateTo string
	var weeks float64
	db.QueryRow(`
		SELECT
			strftime('%Y-%m-%d', MIN(dtime)),
			strftime('%Y-%m-%d', MAX(dtime)),
			ROUND(MAX(1.0, (julianday(MAX(dtime)) - julianday(MIN(dtime))) / 7.0), 1)
		FROM track_pools
	`).Scan(&dateFrom, &dateTo, &weeks)

	c.JSON(200, gin.H{
		"labels":        labels,
		"datasets":      datasets,
		"updated_at":    updatedAt,
		"total_samples": totalSamples,
		"weeks":         weeks,
		"date_from":     dateFrom,
		"date_to":       dateTo,
	})
}

func main() {
	var err error
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

	r.GET("/api/health", health)
	r.GET("/api/pools", getPools)
	r.GET("/api/history", getHistory)
	r.GET("/api/weather", getWeather)
	r.GET("/api/daily-avg", getDailyAvg)

	log.Println("API server running on 0.0.0.0:8085")
	if err := r.Run("0.0.0.0:8085"); err != nil {
		log.Fatal(err)
	}
}
