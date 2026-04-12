/*
Daily stats aggregator — pre-computes per-pool weekly utilization averages from raw samples.
*/
package main

import (
	"database/sql"
	"fmt"
	"log"
	"math"
	"os"
	"time"
	_ "time/tzdata" // embed IANA timezone database (no system tzdata package required)

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

type accumulator struct {
	sum   float64
	sumSq float64
	count int
}

func aggregate() error {
	fmt.Println("Starting daily average aggregation...")

	// Load the Berlin timezone so slot indices reflect local wall-clock time,
	// including correct DST transitions (CET = UTC+1, CEST = UTC+2).
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		return fmt.Errorf("failed to load Europe/Berlin timezone: %w", err)
	}

	// Read all raw data. Slot computation is done in Go (not SQL) so we can
	// apply DST-aware timezone conversion before bucketing.
	rows, err := db.Query(`SELECT name, dtime, utility FROM track_pools`)
	if err != nil {
		return fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	// poolName -> slotIndex -> accumulator
	data := map[string]map[int]*accumulator{}

	for rows.Next() {
		var name string
		var dtime time.Time // go-sqlite3 parses DATETIME columns into time.Time (UTC by default)
		var utility int
		if err := rows.Scan(&name, &dtime, &utility); err != nil {
			log.Printf("scan failed: %v", err)
			continue
		}

		// Convert to Berlin local time — this is the key DST-aware step.
		tBerlin := dtime.In(loc)

		// slot_index: Monday=0 .. Sunday=6, 144 ten-minute slots per day (6*24=144).
		// time.Weekday(): Sunday=0, Monday=1 .. Saturday=6 → remap with (+6)%7.
		dow := (int(tBerlin.Weekday()) + 6) % 7
		slot := dow*144 + (tBerlin.Hour()*60+tBerlin.Minute())/10

		// Utilization = 100 - reported "capacity remaining" value.
		utilization := float64(100 - utility)

		if data[name] == nil {
			data[name] = map[int]*accumulator{}
		}
		if data[name][slot] == nil {
			data[name][slot] = &accumulator{}
		}
		acc := data[name][slot]
		acc.sum += utilization
		acc.sumSq += utilization * utilization
		acc.count++
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("row iteration error: %w", err)
	}

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
	for poolName, slots := range data {
		for slot, acc := range slots {
			mean := acc.sum / float64(acc.count)
			// Population stddev: sqrt(E[X²] - E[X]²)
			variance := acc.sumSq/float64(acc.count) - mean*mean
			if variance < 0 {
				variance = 0 // guard against floating-point rounding
			}
			stddev := math.Sqrt(variance)

			if _, err := stmt.Exec(poolName, slot, mean, stddev, acc.count); err != nil {
				log.Printf("upsert failed for %s slot %d: %v", poolName, slot, err)
				continue
			}
			pools[poolName] = true
			totalSlots++
		}
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
