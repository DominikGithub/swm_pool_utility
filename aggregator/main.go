package main

import (
	"database/sql"
	"fmt"
	"log"
	"math"
	"os"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

var db *sql.DB

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

func aggregate() error {
	fmt.Println("Starting daily average aggregation...")

	// Aggregate mean and variance components directly in SQL.
	// Slot index = dow*144 + (hour*60+minute)/10, where Monday=0..Sunday=6.
	// Population stddev: sqrt(E[X^2] - E[X]^2), computed in Go from SQL's AVG values.
	rows, err := db.Query(`
		SELECT
			name,
			((CAST(strftime('%w', dtime) AS INT) + 6) % 7) * 144
				+ (CAST(strftime('%H', dtime) AS INT) * 60
				   + CAST(strftime('%M', dtime) AS INT)) / 10 AS slot,
			AVG(100 - utility)                   AS mean_util,
			AVG((100 - utility) * (100 - utility)) AS mean_sq,
			COUNT(*)                 AS cnt
		FROM track_pools
		GROUP BY name, slot
		ORDER BY name, slot
	`)
	if err != nil {
		return fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("transaction failed: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO daily_avg_cache (pool_name, slot_index, mean_utilization, std_dev, sample_count, updated_at)
		VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(pool_name, slot_index) DO UPDATE SET
			mean_utilization = excluded.mean_utilization,
			std_dev          = excluded.std_dev,
			sample_count     = excluded.sample_count,
			updated_at       = excluded.updated_at
	`)
	if err != nil {
		return fmt.Errorf("prepare failed: %w", err)
	}
	defer stmt.Close()

	totalSlots := 0
	pools := map[string]bool{}
	for rows.Next() {
		var poolName string
		var slot, count int
		var mean, meanSq float64
		if err := rows.Scan(&poolName, &slot, &mean, &meanSq, &count); err != nil {
			log.Printf("scan failed: %v", err)
			continue
		}

		// Population stddev: sqrt(E[X^2] - E[X]^2)
		variance := meanSq - mean*mean
		if variance < 0 {
			variance = 0 // guard against floating-point rounding
		}
		stddev := math.Sqrt(variance)

		if _, err := stmt.Exec(poolName, slot, mean, stddev, count); err != nil {
			log.Printf("upsert failed for %s slot %d: %v", poolName, slot, err)
			continue
		}
		pools[poolName] = true
		totalSlots++
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit failed: %w", err)
	}

	fmt.Printf("Aggregation complete: %d pools, %d slots written\n", len(pools), totalSlots)
	return nil
}

func main() {
	interval := 1 // hours

	for _, arg := range os.Args[1:] {
		if arg == "--once" || arg == "-o" {
			initDB()
			defer db.Close()
			if err := aggregate(); err != nil {
				log.Printf("Aggregation failed: %v", err)
				os.Exit(1)
			}
			return
		}
	}

	initDB()
	defer db.Close()

	fmt.Println("Daily Average Aggregator starting (hourly)")

	if err := aggregate(); err != nil {
		log.Printf("Initial aggregation failed: %v", err)
	}

	ticker := time.NewTicker(time.Duration(interval) * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		if err := aggregate(); err != nil {
			log.Printf("Aggregation failed: %v", err)
		}
	}
}
