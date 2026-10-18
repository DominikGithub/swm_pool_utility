/*
REST API — single data gateway between the SQLite database and the frontend dashboard.
*/
package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
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

// glitchSpikeFloor and glitchLookback mirror pool-scraper's dropImplausibleZeros.
// SWM's capacity widget intermittently serves 0% for pools that are plainly not
// full; the scraper now discards those at write time, but this read-side guard
// keeps a glitch that predates the fix — or one that arrives through any other
// path — from drawing a 100-point cliff in the dashboard graphs.
//
// A 0% reading is dropped only when the pool's previous sample was above the
// floor and recent enough to be comparable.  A gradual fill-up
// (93 → … → 20 → 5 → 1 → 0) keeps its 0, because that is a real
// full-capacity event.
const (
	glitchSpikeFloor = 60
	glitchLookback   = time.Hour
)

// glitchGuard applies that rule to a chronological stream of readings,
// tracking the last believable reading per pool.
type glitchGuard struct {
	lastKept   map[string]int
	lastKeptAt map[string]time.Time
	dropped    int
}

func newGlitchGuard() *glitchGuard {
	return &glitchGuard{lastKept: map[string]int{}, lastKeptAt: map[string]time.Time{}}
}

// keep reports whether a reading is believable and remembers it if so.  A
// dropped reading deliberately does NOT update the state: during a glitch
// lasting several intervals every 0 is still compared against the last
// believable reading.  Should such a run outlast glitchLookback the guard
// stands down, since by then a sustained 0 may well be real.  A 0 without a
// predecessor (the very start of a requested window) is passed through.
func (g *glitchGuard) keep(pool string, dtime time.Time, utility int) bool {
	if utility == 0 {
		if prev, ok := g.lastKept[pool]; ok && prev > glitchSpikeFloor &&
			dtime.Sub(g.lastKeptAt[pool]) <= glitchLookback {
			g.dropped++
			return false
		}
	}
	g.lastKept[pool] = utility
	g.lastKeptAt[pool] = dtime
	return true
}

// dbTimeLayout is the UTC format every dtime column uses.
const dbTimeLayout = "2006-01-02 15:04:05"

// queryWindow reads the parameters shared by the history and weather
// endpoints: `days` calendar days (Berlin time) up to `end`, an RFC3339
// timestamp such as 2026-06-14T00:00:00+02:00, or up to now when end is
// absent.  The bounds come back as dtime strings; an empty bound is open.
func queryWindow(c *gin.Context) (from, to string, err error) {
	days, _ := strconv.Atoi(c.DefaultQuery("days", "1"))
	end := time.Now()
	if s := c.Query("end"); s != "" {
		t, perr := time.Parse(time.RFC3339, s)
		if perr != nil {
			return "", "", fmt.Errorf("invalid end %q: want RFC3339, e.g. 2026-06-14T00:00:00+02:00", s)
		}
		end = t
		to = t.UTC().Format(dbTimeLayout)
	}
	if days > 0 {
		from = end.In(berlinLoc).AddDate(0, 0, -days).UTC().Format(dbTimeLayout)
	}
	return from, to, nil
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
	rows, err := db.Query("SELECT name FROM pools ORDER BY name")
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	pools := make([]string, 0)
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
	from, to, err := queryWindow(c)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// The bounds use the same "YYYY-MM-DD HH:MM:SS" UTC strings that SQLite
	// stores, so the string comparison is unambiguous.
	query := `SELECT p.name, tp.dtime, tp.utility
		FROM track_pools tp JOIN pools p ON tp.pool_id = p.id`
	var conds []string
	var args []interface{}
	if from != "" {
		conds = append(conds, "tp.dtime >= ?")
		args = append(args, from)
	}
	if to != "" {
		conds = append(conds, "tp.dtime < ?")
		args = append(args, to)
	}
	if pool != "" {
		conds = append(conds, "p.name = ?")
		args = append(args, pool)
	}
	if len(conds) > 0 {
		query += " WHERE " + strings.Join(conds, " AND ")
	}
	query += " ORDER BY tp.dtime ASC"

	rows, err := db.Query(query, args...)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	data := make([]DataPoint, 0)

	// Rows arrive in dtime order, so per pool they are already chronological.
	guard := newGlitchGuard()
	for rows.Next() {
		var d DataPoint
		var dtime time.Time // go-sqlite3 parses DATETIME columns into time.Time (UTC)
		if err := rows.Scan(&d.Name, &dtime, &d.Utility); err != nil {
			continue
		}
		if !guard.keep(d.Name, dtime, d.Utility) {
			continue
		}
		// Output as Berlin local time with UTC offset — unambiguous and display-ready.
		// e.g. "2026-04-06T10:30:00+02:00" (CEST) or "2026-01-15T09:30:00+01:00" (CET)
		d.Timestamp = dtime.In(berlinLoc).Format(time.RFC3339)
		data = append(data, d)
	}
	if guard.dropped > 0 {
		log.Printf("history: filtered %d implausible 0%% sample(s)", guard.dropped)
	}
	c.JSON(200, data)
}

func getWeather(c *gin.Context) {
	from, to, err := queryWindow(c)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	query := `SELECT w.dtime, w.temperature, w.wind_speed, w.cloud_cover, wt.type, w.precipitation
		FROM weather w JOIN weather_types wt ON w.weather_type_id = wt.id`
	var conds []string
	var args []interface{}
	if from != "" {
		conds = append(conds, "w.dtime >= ?")
		args = append(args, from)
	}
	if to != "" {
		conds = append(conds, "w.dtime < ?")
		args = append(args, to)
	}
	if len(conds) > 0 {
		query += " WHERE " + strings.Join(conds, " AND ")
	}
	query += " ORDER BY w.dtime ASC"

	rows, err := db.Query(query, args...)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	data := make([]WeatherPoint, 0)
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

// The time navigator under the live chart draws one bar per day: each pool's
// daily peak over the whole history.  Building it reads every stored reading,
// so the result is cached for overviewTTL instead of rebuilt per request.
const overviewTTL = 10 * time.Minute

type dailyOverview struct {
	Days  []string       `json:"days"` // Berlin midnight of each day, RFC3339
	Pools []overviewPool `json:"pools"`
}

type overviewPool struct {
	Label string     `json:"label"`
	Peak  []*float64 `json:"peak"` // null: no readings that day
}

var (
	overviewMu      sync.Mutex
	overviewCache   *dailyOverview
	overviewBuiltAt time.Time
)

func getDailyOverview(c *gin.Context) {
	overviewMu.Lock()
	if overviewCache == nil || time.Since(overviewBuiltAt) > overviewTTL {
		ov, err := buildDailyOverview()
		if err != nil {
			overviewMu.Unlock()
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		overviewCache, overviewBuiltAt = ov, time.Now()
	}
	ov := overviewCache
	overviewMu.Unlock()

	pool := c.Query("pool")
	if pool == "" {
		c.JSON(200, ov)
		return
	}
	out := dailyOverview{Days: ov.Days, Pools: []overviewPool{}}
	for _, p := range ov.Pools {
		if p.Label == pool {
			out.Pools = append(out.Pools, p)
		}
	}
	c.JSON(200, out)
}

// buildDailyOverview returns, for every Berlin calendar day from the first
// stored reading up to today, each pool's daily peak: the 90th percentile of
// that day's readings, so a single spike does not set the bar.
func buildDailyOverview() (*dailyOverview, error) {
	rows, err := db.Query(`SELECT p.name, tp.dtime, tp.utility
		FROM track_pools tp JOIN pools p ON tp.pool_id = p.id
		ORDER BY tp.pool_id, tp.dtime`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// pool -> "YYYY-MM-DD" (Berlin) -> utilization readings
	byPool := map[string]map[string][]float64{}
	var first time.Time
	guard := newGlitchGuard()
	for rows.Next() {
		var name string
		var dtime time.Time
		var utility int
		if err := rows.Scan(&name, &dtime, &utility); err != nil {
			continue
		}
		if !guard.keep(name, dtime, utility) {
			continue
		}
		if first.IsZero() || dtime.Before(first) {
			first = dtime
		}
		if byPool[name] == nil {
			byPool[name] = map[string][]float64{}
		}
		day := dtime.In(berlinLoc).Format("2006-01-02")
		byPool[name][day] = append(byPool[name][day], float64(100-utility))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	ov := &dailyOverview{Days: []string{}, Pools: []overviewPool{}}
	if first.IsZero() {
		return ov, nil
	}
	var keys []string
	f, now := first.In(berlinLoc), time.Now().In(berlinLoc)
	for d := time.Date(f.Year(), f.Month(), f.Day(), 0, 0, 0, 0, berlinLoc); !d.After(now); d = d.AddDate(0, 0, 1) {
		ov.Days = append(ov.Days, d.Format(time.RFC3339))
		keys = append(keys, d.Format("2006-01-02"))
	}

	names := make([]string, 0, len(byPool))
	for name := range byPool {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		peaks := make([]*float64, len(keys))
		for i, key := range keys {
			vals := byPool[name][key]
			if len(vals) == 0 {
				continue
			}
			sort.Float64s(vals)
			pos := 0.9 * float64(len(vals)-1)
			lo := int(pos)
			hi := int(math.Ceil(pos))
			v := math.Round((vals[lo]+(vals[hi]-vals[lo])*(pos-float64(lo)))*10) / 10
			peaks[i] = &v
		}
		ov.Pools = append(ov.Pools, overviewPool{Label: name, Peak: peaks})
	}
	return ov, nil
}

func health(c *gin.Context) {
	c.JSON(200, gin.H{"status": "ok"})
}

func getDailyAvg(c *gin.Context) {
	pool := c.Query("pool")

	query := `SELECT p.name, dac.slot_index, dac.p25_utilization, dac.median_utilization, dac.p75_utilization,
		dac.recent_median_utilization, dac.sample_count, dac.updated_at
		FROM daily_avg_cache dac JOIN pools p ON dac.pool_id = p.id`
	var args []interface{}
	if pool != "" {
		query += ` WHERE p.name = ?`
		args = append(args, pool)
	}
	query += ` ORDER BY p.name, dac.slot_index`

	rows, err := db.Query(query, args...)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	// Percentiles are NULL for slots where every reading fell on a closure.
	type slotData struct {
		p25, median, p75, recentMedian sql.NullFloat64
		sampleCount                    int
	}

	// poolName -> slotIndex -> slotData
	byPool := map[string]map[int]slotData{}
	var updatedAt time.Time
	var totalSamples int

	for rows.Next() {
		var poolName string
		var si int
		var sd slotData
		var ts time.Time
		if err := rows.Scan(&poolName, &si, &sd.p25, &sd.median, &sd.p75, &sd.recentMedian, &sd.sampleCount, &ts); err != nil {
			continue
		}
		if byPool[poolName] == nil {
			byPool[poolName] = map[int]slotData{}
		}
		byPool[poolName][si] = sd
		totalSamples += sd.sampleCount
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

	// Median line with a P25–P75 band: the middle half of observed weeks,
	// with closures excluded by the daily-stats aggregator.  RecentMedian is
	// the same median over the 28 full days before today only (the live
	// chart's "lately typical" line).
	type dataset struct {
		Label        string    `json:"label"`
		Median       []float64 `json:"median"`
		P25          []float64 `json:"p25"`
		P75          []float64 `json:"p75"`
		RecentMedian []float64 `json:"recent_median"`
		SampleCount  []int     `json:"sample_count"`
	}

	// noData marks a slot without a value (no readings, or all on closures).
	const noData = -1
	valueOr := func(v sql.NullFloat64) float64 {
		if !v.Valid {
			return noData
		}
		return v.Float64
	}

	var datasets []dataset
	for poolName, slots := range byPool {
		ds := dataset{
			Label:        poolName,
			Median:       make([]float64, len(labels)),
			P25:          make([]float64, len(labels)),
			P75:          make([]float64, len(labels)),
			RecentMedian: make([]float64, len(labels)),
			SampleCount:  make([]int, len(labels)),
		}
		for i := range ds.Median {
			ds.Median[i], ds.P25[i], ds.P75[i], ds.RecentMedian[i] = noData, noData, noData, noData
		}
		for si, sd := range slots {
			if si >= 0 && si < len(labels) {
				ds.Median[si] = valueOr(sd.median)
				ds.P25[si] = valueOr(sd.p25)
				ds.P75[si] = valueOr(sd.p75)
				ds.RecentMedian[si] = valueOr(sd.recentMedian)
				ds.SampleCount[si] = sd.sampleCount
			}
		}
		datasets = append(datasets, ds)
	}
	// byPool is a map, so fix the order: the frontend colours pools by index.
	sort.Slice(datasets, func(i, j int) bool { return datasets[i].Label < datasets[j].Label })

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

func getHourlyAvg(c *gin.Context) {
	pool := c.Query("pool")

	// Median aggregation via CTEs + SQLite window functions.
	//
	// base:   extracts pool name, day-of-week, 30-min slot, and raw utility.
	//         The slot/dow expressions are written once here and reused by name.
	// ranked: assigns a rank ordered by utility within each (pool, dow, slot)
	//         partition and records the partition size.
	// final:  keeps only the middle row(s) — one for odd counts, two for even —
	//         and averages them.  This is the standard median definition.
	baseFilter := ""
	var args []interface{}
	if pool != "" {
		baseFilter = " WHERE p.name = ?"
		args = append(args, pool)
	}

	query := `
		WITH base AS (
			SELECT
				p.name AS pool_name,
				strftime('%w', datetime(tp.dtime, '+2 hours')) AS dow,
				strftime('%H', datetime(tp.dtime, '+2 hours')) * 2 +
					CASE WHEN CAST(strftime('%M', datetime(tp.dtime, '+2 hours'))
					     AS INTEGER) >= 30 THEN 1 ELSE 0 END  AS slot,
				tp.utility
			FROM track_pools tp JOIN pools p ON tp.pool_id = p.id
			` + baseFilter + `
		),
		ranked AS (
			SELECT *,
				ROW_NUMBER() OVER (PARTITION BY pool_name, dow, slot ORDER BY utility) AS rn,
				COUNT(*)       OVER (PARTITION BY pool_name, dow, slot)                AS cnt,
				SUM(CASE WHEN utility >= 99 THEN 1 ELSE 0 END)
				               OVER (PARTITION BY pool_name, dow, slot)                AS closed_sum
			FROM base
		)
		SELECT
			pool_name,
			dow,
			slot,
			AVG(utility)                      AS median_util,
			MAX(cnt)                          AS sample_count,
			MAX(closed_sum) * 1.0 / MAX(cnt)  AS closed_fraction
		FROM ranked
		WHERE rn IN ((cnt + 1) / 2, (cnt + 2) / 2)
		GROUP BY pool_name, dow, slot
		ORDER BY pool_name, dow, slot`

	rows, err := db.Query(query, args...)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	type hourlyData struct {
		Pool           string  `json:"pool"`
		DayOfWeek      int     `json:"day_of_week"`
		Slot           int     `json:"slot"`
		Mean           float64 `json:"mean"`
		Samples        int     `json:"samples"`
		ClosedFraction float64 `json:"closed_fraction"`
	}

	data := make([]hourlyData, 0)
	for rows.Next() {
		var hd hourlyData
		if err := rows.Scan(&hd.Pool, &hd.DayOfWeek, &hd.Slot, &hd.Mean, &hd.Samples, &hd.ClosedFraction); err != nil {
			continue
		}
		data = append(data, hd)
	}

	c.JSON(200, data)
}

func getPredictions(c *gin.Context) {
	pool := c.Query("pool")

	query := "SELECT pool_name, current_util, pred_1h, pred_2h, delta_1h, delta_2h, trend_strength, trend_direction FROM predictions"
	var args []interface{}
	if pool != "" {
		query += " WHERE pool_name = ?"
		args = append(args, pool)
	}
	query += " ORDER BY pool_name"

	rows, err := db.Query(query, args...)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	type TrendPoint struct {
		Pool          string  `json:"pool"`
		Current       float64 `json:"current"`
		Pred1H        float64 `json:"pred_1h"`
		Pred2H        float64 `json:"pred_2h"`
		Delta1H       float64 `json:"delta_1h"`
		Delta2H       float64 `json:"delta_2h"`
		TrendStrength float64 `json:"trend_strength"`
		Direction     string  `json:"direction"`
	}

	trends := make([]TrendPoint, 0)
	for rows.Next() {
		var tp TrendPoint
		if err := rows.Scan(&tp.Pool, &tp.Current, &tp.Pred1H, &tp.Pred2H, &tp.Delta1H, &tp.Delta2H, &tp.TrendStrength, &tp.Direction); err != nil {
			continue
		}
		trends = append(trends, tp)
	}
	c.JSON(200, trends)
}

type PoolStatus struct {
	Name          string          `json:"name"`
	CurrentUtil   float64         `json:"current_util"`
	Pred1H        float64         `json:"pred_1h"`
	Pred2H        float64         `json:"pred_2h"`
	Delta1H       float64         `json:"delta_1h"`
	Delta2H       float64         `json:"delta_2h"`
	TrendStrength float64         `json:"trend_strength"`
	Arrow         string          `json:"arrow"`
	PredSeries    json.RawMessage `json:"pred_series"`
}

func getPoolStatus(c *gin.Context) {
	rows, err := db.Query(`
		SELECT p.name, tp.utility
		FROM track_pools tp JOIN pools p ON tp.pool_id = p.id
		WHERE (tp.pool_id, tp.dtime) IN (
			SELECT pool_id, MAX(dtime) FROM track_pools GROUP BY pool_id
		)
	`)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	current := map[string]float64{}
	for rows.Next() {
		var name string
		var util int
		if err := rows.Scan(&name, &util); err != nil {
			continue
		}
		current[name] = float64(100 - util)
	}

	predRows, err := db.Query(`
		SELECT pool_name, current_util, pred_1h, pred_2h, delta_1h, delta_2h, trend_strength, trend_direction, pred_series
		FROM predictions
	`)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer predRows.Close()

	preds := map[string]PoolStatus{}
	for predRows.Next() {
		var ps PoolStatus
		var seriesStr *string
		if err := predRows.Scan(&ps.Name, &ps.CurrentUtil, &ps.Pred1H, &ps.Pred2H, &ps.Delta1H, &ps.Delta2H, &ps.TrendStrength, &ps.Arrow, &seriesStr); err != nil {
			continue
		}
		if seriesStr != nil {
			ps.PredSeries = json.RawMessage(*seriesStr)
		} else {
			ps.PredSeries = json.RawMessage("null")
		}
		preds[ps.Name] = ps
	}

	statuses := make([]PoolStatus, 0)
	for name, cur := range current {
		if pred, ok := preds[name]; ok {
			statuses = append(statuses, pred)
		} else {
			statuses = append(statuses, PoolStatus{Name: name, CurrentUtil: cur, Arrow: "stable"})
		}
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

	r.GET("/api/health", health)
	r.GET("/api/pool-status", getPoolStatus)
	r.GET("/api/pools", getPools)
	r.GET("/api/history", getHistory)
	r.GET("/api/weather", getWeather)
	r.GET("/api/daily-avg", getDailyAvg)
	r.GET("/api/daily-overview", getDailyOverview)
	r.GET("/api/hourly-avg", getHourlyAvg)
	r.GET("/api/predictions", getPredictions)

	log.Println("API server running on 0.0.0.0:8085")
	if err := r.Run("0.0.0.0:8085"); err != nil {
		log.Fatal(err)
	}
}
