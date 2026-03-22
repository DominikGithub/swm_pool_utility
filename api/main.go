package main

import (
	"database/sql"
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
	daysStr := c.DefaultQuery("days", "7")
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

func health(c *gin.Context) {
	c.JSON(200, gin.H{"status": "ok"})
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

	log.Println("API server running on 0.0.0.0:8085")
	if err := r.Run("0.0.0.0:8085"); err != nil {
		log.Fatal(err)
	}
}
