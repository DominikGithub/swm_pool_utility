/*
Daily stats aggregator — pre-computes per-pool weekly utilization averages from raw samples.
*/
package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"sort"
	"strings"
	"time"
	_ "time/tzdata" // embed IANA timezone database (no system tzdata package required)

	_ "github.com/mattn/go-sqlite3"
)

var db *sql.DB

// Closure detection for the percentile band.  A pool that is closed during its
// scheduled opening hours (season end, maintenance, early closing) reads 0%,
// which is indistinguishable from "empty" and would drag the typical-week band
// towards zero.  Judged per calendar day, open-hour readings count as a closure
// when the whole day reads 0%, or when 0% lasts at least closureMinRun on a day
// that otherwise reached closureBusyFloor.  A day hovering at 0–9% throughout is
// a quiet day (outdoor pools in bad weather), not a closure, and is kept.
const (
	closureMinRun    = 3 * time.Hour
	closureBusyFloor = 10.0
)

// Each slot's percentiles pool the readings of the slots within
// ±bandWindowSlots (±20 min, a 50-minute window).  A slot holds about one
// reading per week, so its own median jumps by whole percentage points from
// one slot to the next; ~5× the readings keep the line about as steady as the
// mean while still following the daily curve.
const bandWindowSlots = 2

// The live chart's "lately typical" line uses only the readings of the
// recentDays full days before today (Berlin time), so it follows seasonal
// change.  The all-time median does not: in October 2026 the indoor pools ran
// 5–11 points below it.  Today is left out so the live readings are compared
// with earlier days rather than with themselves, and the line stays put all day.
const recentDays = 28

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

// poolSchedule is one entry of frontend/src/data/pool-opening-hours.json, the
// opening-hours source of truth shared with the heatmap and migrations 013/014.
// Days are Monday-first [firstOpenSlot, firstClosedSlot) in 30-minute slots.
type poolSchedule struct {
	Match []string `json:"match"`
	Days  [][2]int `json:"days"`
}

func loadSchedules() ([]poolSchedule, error) {
	path := os.Getenv("SCHEDULE_PATH")
	if path == "" {
		path = "/schedules/pool-opening-hours.json"
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var schedules []poolSchedule
	if err := json.Unmarshal(raw, &schedules); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	for _, s := range schedules {
		if len(s.Days) != 7 {
			return nil, fmt.Errorf("schedule %v has %d days, want 7", s.Match, len(s.Days))
		}
	}
	return schedules, nil
}

// scheduleFor returns the first schedule whose match terms occur in the pool
// name, mirroring the frontend lookup (order matters: "dantebad" before "dante").
func scheduleFor(schedules []poolSchedule, poolName string) *poolSchedule {
	lower := strings.ToLower(poolName)
	for i := range schedules {
		for _, m := range schedules[i].Match {
			if strings.Contains(lower, m) {
				return &schedules[i]
			}
		}
	}
	return nil
}

// isOpen reports whether Berlin-local time t falls inside the scheduled hours.
func (s *poolSchedule) isOpen(t time.Time) bool {
	day := s.Days[(int(t.Weekday())+6)%7]
	slot := (t.Hour()*60 + t.Minute()) / 30
	return slot >= day[0] && slot < day[1]
}

// sample is one kept reading of a single pool-day, in Berlin local time.
type sample struct {
	at          time.Time
	slot        int
	utilization float64
}

// closureMask flags the readings of one pool-day that belong to a closure (see
// closureMinRun).  Without a schedule only whole-day closures are recognised,
// since normal closed hours would otherwise look like a long 0% run.
func closureMask(day []sample, sched *poolSchedule) []bool {
	closed := make([]bool, len(day))
	var open []int
	peak := 0.0
	for i, s := range day {
		if sched == nil || sched.isOpen(s.at) {
			open = append(open, i)
			if s.utilization > peak {
				peak = s.utilization
			}
		}
	}
	if len(open) == 0 {
		return closed
	}
	if peak == 0 {
		for _, i := range open {
			closed[i] = true
		}
		return closed
	}
	if sched == nil || peak < closureBusyFloor {
		return closed
	}

	runStart := -1
	for k := 0; k <= len(open); k++ {
		if k < len(open) && day[open[k]].utilization == 0 {
			if runStart < 0 {
				runStart = k
			}
			continue
		}
		if runStart >= 0 && day[open[k-1]].at.Sub(day[open[runStart]].at) >= closureMinRun {
			for _, i := range open[runStart:k] {
				closed[i] = true
			}
		}
		runStart = -1
	}
	return closed
}

// windowValues pools the readings of the slots within ±bandWindowSlots of slot.
// Wrapping at the week boundary is harmless: no pool is open around midnight.
func windowValues(bySlot map[int][]float64, slot int) []float64 {
	var vals []float64
	for d := -bandWindowSlots; d <= bandWindowSlots; d++ {
		vals = append(vals, bySlot[(slot+d+1008)%1008]...)
	}
	return vals
}

// quantile returns the q-quantile of sorted values with linear interpolation
// between the two nearest ranks (the numpy/Excel PERCENTILE.INC definition).
func quantile(sorted []float64, q float64) float64 {
	pos := q * float64(len(sorted)-1)
	lo := int(math.Floor(pos))
	hi := int(math.Ceil(pos))
	return sorted[lo] + (sorted[hi]-sorted[lo])*(pos-float64(lo))
}

func aggregate() error {
	fmt.Println("Starting daily average aggregation...")

	// Load the Berlin timezone so slot indices reflect local wall-clock time,
	// including correct DST transitions (CET = UTC+1, CEST = UTC+2).
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		return fmt.Errorf("failed to load Europe/Berlin timezone: %w", err)
	}

	schedules, err := loadSchedules()
	if err != nil {
		log.Printf("warning: opening hours unavailable (%v); percentile band only excludes whole-day closures", err)
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

	// poolName -> slotIndex -> readings outside closures, for the percentile
	// band (all readings) and the lately-typical line (recentDays only).
	// Buffered one pool-day at a time because closures are judged per day.
	bandValues := map[string]map[int][]float64{}
	recentValues := map[string]map[int][]float64{}
	now := time.Now().In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	recentFrom := today.AddDate(0, 0, -recentDays)
	var day []sample
	var dayPool, dayDate string
	closureSamples := 0
	flushDay := func() {
		closed := closureMask(day, scheduleFor(schedules, dayPool))
		for i, s := range day {
			if closed[i] {
				closureSamples++
				continue
			}
			if bandValues[dayPool] == nil {
				bandValues[dayPool] = map[int][]float64{}
				recentValues[dayPool] = map[int][]float64{}
			}
			bandValues[dayPool][s.slot] = append(bandValues[dayPool][s.slot], s.utilization)
			if !s.at.Before(recentFrom) && s.at.Before(today) {
				recentValues[dayPool][s.slot] = append(recentValues[dayPool][s.slot], s.utilization)
			}
		}
		day = day[:0]
	}

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

		if date := tBerlin.Format("2006-01-02"); name != dayPool || date != dayDate {
			flushDay()
			dayPool, dayDate = name, date
		}
		day = append(day, sample{at: tBerlin, slot: slot, utilization: utilization})
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("row iteration error: %w", err)
	}
	flushDay()
	if glitches > 0 {
		fmt.Printf("  Excluded %d implausible 0%% sample(s) from the averages\n", glitches)
	}
	if closureSamples > 0 {
		fmt.Printf("  Excluded %d closure sample(s) from the percentile band\n", closureSamples)
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("transaction failed: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO daily_avg_cache (pool_id, slot_index, mean_utilization, std_dev,
			p25_utilization, median_utilization, p75_utilization, recent_median_utilization,
			sample_count, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(pool_id, slot_index) DO UPDATE SET
			mean_utilization          = excluded.mean_utilization,
			std_dev                   = excluded.std_dev,
			p25_utilization           = excluded.p25_utilization,
			median_utilization        = excluded.median_utilization,
			p75_utilization           = excluded.p75_utilization,
			recent_median_utilization = excluded.recent_median_utilization,
			sample_count              = excluded.sample_count,
			updated_at                = excluded.updated_at
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

			// Typical-week band and lately-typical line for the dashboard; each
			// stays NULL when every reading in its window fell on a closure.
			var p25, median, p75, recentMedian interface{}
			if vals := windowValues(bandValues[poolName], slot); len(vals) > 0 {
				sort.Float64s(vals)
				p25, median, p75 = quantile(vals, 0.25), quantile(vals, 0.5), quantile(vals, 0.75)
			}
			if vals := windowValues(recentValues[poolName], slot); len(vals) > 0 {
				sort.Float64s(vals)
				recentMedian = quantile(vals, 0.5)
			}

			if _, err := stmt.Exec(poolID, slot, mean, stddev, p25, median, p75, recentMedian, acc.count); err != nil {
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
