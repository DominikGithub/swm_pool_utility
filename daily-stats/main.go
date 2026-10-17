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

	// Load pool IDs so the upsert can use integer foreign keys.
	poolIDs := map[string]int64{}
	pidRows, err := db.Query("SELECT id, name FROM pools")
	if err != nil {
		return fmt.Errorf("failed to load pool IDs: %w", err)
	}
	for pidRows.Next() {
		var id int64
		var name string
		if err := pidRows.Scan(&id, &name); err != nil {
			continue
		}
		poolIDs[name] = id
	}
	pidRows.Close()

	// Read all raw data. Slot computation is done in Go (not SQL) so we can
	// apply DST-aware timezone conversion before bucketing.
	//
	// Ordered by pool and time so the glitch guard below can compare each
	// reading against that pool's previous one.
	rows, err := db.Query(`
		SELECT p.name, tp.dtime, tp.utility
		FROM track_pools tp JOIN pools p ON tp.pool_id = p.id
		ORDER BY tp.pool_id, tp.dtime
	`)
	if err != nil {
		return fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	// poolName -> slotIndex -> accumulator
	data := map[string]map[int]*accumulator{}

	// Last reading kept per pool, so an implausible 0% can be recognised and
	// excluded from the averages.  SWM's widget intermittently serves 0%
	// "capacity remaining" for pools that are plainly not full; stored as
	// utilization that becomes a 100% reading, which is the worst possible
	// outlier for a mean.  Thresholds mirror pool-scraper's
	// dropImplausibleZeros, and a genuine gradual fill-up keeps its 0.
	const (
		glitchSpikeFloor = 60
		glitchLookback   = time.Hour
	)
	lastKept := map[string]int{}
	lastKeptAt := map[string]time.Time{}
	glitches := 0

	for rows.Next() {
		var name string
		var dtime time.Time // go-sqlite3 parses DATETIME columns into time.Time (UTC by default)
		var utility int
		if err := rows.Scan(&name, &dtime, &utility); err != nil {
			log.Printf("scan failed: %v", err)
			continue
		}

		if utility == 0 {
			if prev, ok := lastKept[name]; ok && prev > glitchSpikeFloor &&
				dtime.Sub(lastKeptAt[name]) <= glitchLookback {
				glitches++
				continue
			}
		}
		lastKept[name] = utility
		lastKeptAt[name] = dtime

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
	if glitches > 0 {
		fmt.Printf("  Excluded %d implausible 0%% sample(s) from the averages\n", glitches)
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("transaction failed: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO daily_avg_cache (pool_id, slot_index, mean_utilization, std_dev, sample_count, updated_at)
		VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(pool_id, slot_index) DO UPDATE SET
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
		poolID, ok := poolIDs[poolName]
		if !ok {
			log.Printf("warning: pool %q not found in pools table, skipping", poolName)
			continue
		}
		for slot, acc := range slots {
			mean := acc.sum / float64(acc.count)
			// Population stddev: sqrt(E[X²] - E[X]²)
			variance := acc.sumSq/float64(acc.count) - mean*mean
			if variance < 0 {
				variance = 0 // guard against floating-point rounding
			}
			stddev := math.Sqrt(variance)

			if _, err := stmt.Exec(poolID, slot, mean, stddev, acc.count); err != nil {
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
