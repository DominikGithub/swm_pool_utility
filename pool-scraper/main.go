/*
Pool scraper — collects live occupancy data from the SWM website.
*/
package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

var db *sql.DB

// poolIDs caches the pools.id for each pool name so every insert only needs
// a map lookup rather than a DB round-trip.  Populated at startup and updated
// on the rare occasion a previously-unseen pool name appears.
var poolIDs = map[string]int64{}

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

	loadPoolIDs()
}

// loadPoolIDs reads all existing pools into the in-memory cache.
func loadPoolIDs() {
	rows, err := db.Query("SELECT id, name FROM pools")
	if err != nil {
		// Table may not exist yet on a brand-new DB — not fatal.
		log.Printf("warning: could not load pool IDs: %v", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			continue
		}
		poolIDs[name] = id
	}
}

// getOrCreatePoolID returns the pools.id for name, inserting a new row if this
// is the first time the pool has been seen.
func getOrCreatePoolID(name string) (int64, error) {
	if id, ok := poolIDs[name]; ok {
		return id, nil
	}
	// INSERT OR IGNORE is safe to call concurrently (single goroutine here, but
	// harmless even if called twice due to restart races).
	if _, err := db.Exec("INSERT OR IGNORE INTO pools(name) VALUES (?)", name); err != nil {
		return 0, fmt.Errorf("insert pool %q: %w", name, err)
	}
	var id int64
	if err := db.QueryRow("SELECT id FROM pools WHERE name = ?", name).Scan(&id); err != nil {
		return 0, fmt.Errorf("lookup pool %q: %w", name, err)
	}
	poolIDs[name] = id
	return id, nil
}

func scrape() error {
	fmt.Println("Starting scrape...")

	configDir := "/tmp/chromium-data"
	os.RemoveAll(configDir)
	os.MkdirAll(configDir, 0755)

	cmd := exec.Command(
		"/usr/bin/chromium",
		"--headless=new",
		"--no-sandbox",
		"--disable-gpu",
		"--disable-dev-shm-usage",
		"--user-data-dir="+configDir,
		"--dump-dom",
		"--virtual-time-budget=20000",
		"https://www.swm.de/baeder/auslastung",
	)

	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("chromium failed: %w", err)
	}

	html := string(output)
	fmt.Printf("Got HTML length: %d bytes\n", len(html))

	if len(html) < 1000 {
		return fmt.Errorf("HTML too short: %d bytes", len(html))
	}

	poolStats := extractPoolData(html)
	fmt.Printf("Found %d pools\n", len(poolStats))

	if len(poolStats) == 0 {
		return fmt.Errorf("no pools found in HTML")
	}

	// Explicitly store the current UTC timestamp. All timestamps in the database
	// are UTC ("YYYY-MM-DD HH:MM:SS"). Timezone conversion (e.g. to Europe/Berlin)
	// happens at read time in the API and aggregator.
	now := time.Now().UTC().Format("2006-01-02 15:04:05")

	for name, utility := range poolStats {
		poolID, err := getOrCreatePoolID(name)
		if err != nil {
			log.Printf("failed to resolve pool ID for %s: %v", name, err)
			continue
		}
		_, err = db.Exec(
			"INSERT INTO track_pools (pool_id, dtime, utility) VALUES (?, ?, ?)",
			poolID, now, utility,
		)
		if err != nil {
			log.Printf("failed to insert %s: %v", name, err)
		} else {
			fmt.Printf("Logged: %s -> %d%%\n", name, utility)
		}
	}

	return nil
}

// extractPoolData returns a map of pool name → utilization percentage (0–100).
//
// Pool-name extraction strategies (tried in order, first non-empty wins):
//
//  1. bath-name="Pool Name"  — web-component attribute present in both the
//     static HTML and the Chromium-rendered DOM (current, 2026-05+).
//  2. class="headline-s">Pool Name</h3> — inline heading used in the
//     previous page design (legacy fallback).
//
// If neither strategy finds any names a diagnostic snippet of the pools
// section is printed to help identify the new structure quickly.
//
// Freibad normalization:
// The SWM website reports 0% capacity remaining when Freibäder are closed
// (instead of 100% like Hallenbäder).  Pools found in the id="freibad"
// section are normalized so all pools use the same "closed" convention.
// Hallenbad-section pools are NOT normalized because their 0% readings
// during operating hours can be genuine (pool at full capacity).
// The detection is fully dynamic — new pools added to the freibad section
// are handled automatically.
func extractPoolData(html string) map[string]int {
	poolStats := make(map[string]int)

	poolsSection := extractPoolsSection(html)
	if poolsSection == "" {
		fmt.Println("Could not find pools section")
		return poolStats
	}

	fmt.Printf("Pools section length: %d bytes\n", len(poolsSection))

	rePercent := regexp.MustCompile(`(\d+)\s*%`)

	type candidate struct {
		name   string
		endPos int // byte offset just past the name match — search for % from here
	}
	var candidates []candidate

	// --- Strategy 1: bath-name="…" attribute (current, 2026-05+) ---
	reBathName := regexp.MustCompile(`bath-name="([^"]+)"`)
	for _, m := range reBathName.FindAllStringSubmatchIndex(poolsSection, -1) {
		if len(m) >= 4 {
			if name := strings.TrimSpace(poolsSection[m[2]:m[3]]); name != "" {
				candidates = append(candidates, candidate{name, m[1]})
			}
		}
	}

	// --- Strategy 2: <h3 class="headline-s">…</h3> (legacy, pre-2026-05) ---
	if len(candidates) == 0 {
		fmt.Println("  bath-name attribute not found — trying headline-s fallback")
		reHeadline := regexp.MustCompile(`class="headline-s">([^<]+)</h3>`)
		for _, m := range reHeadline.FindAllStringSubmatchIndex(poolsSection, -1) {
			if len(m) >= 4 {
				if name := strings.TrimSpace(poolsSection[m[2]:m[3]]); name != "" {
					candidates = append(candidates, candidate{name, m[1]})
				}
			}
		}
	}

	fmt.Printf("  Found %d pool name candidates\n", len(candidates))

	if len(candidates) == 0 {
		// Diagnostic: print a snippet so the new structure can be identified quickly.
		snippet := poolsSection
		if len(snippet) > 800 {
			snippet = snippet[:800]
		}
		fmt.Printf("  Pools section snippet (first 800 bytes):\n%s\n", snippet)
	}

	for _, c := range candidates {
		if _, exists := poolStats[c.name]; exists {
			continue
		}

		endPos := c.endPos + 2000
		if endPos > len(poolsSection) {
			endPos = len(poolsSection)
		}
		searchArea := poolsSection[c.endPos:endPos]

		for _, pctMatch := range rePercent.FindAllStringSubmatch(searchArea, 5) {
			if len(pctMatch) >= 2 {
				pct, err := strconv.Atoi(pctMatch[1])
				if err == nil && pct >= 0 && pct <= 100 {
					poolStats[c.name] = pct
					fmt.Printf("  Found: %s -> %d%%\n", c.name, pct)
					break
				}
			}
		}
	}

	// Normalize Freibad closed-state values.
	// The SWM website reports 0% capacity remaining for closed Freibäder
	// (instead of 100% free like Hallenbäder).  Detect which pools appear
	// inside <div id="freibad"> and convert 0 → 100 so all pools use the
	// same "nobody there / closed" convention.
	//
	// Hallenbäder (id="hallenbad") are NOT normalized because their 0%
	// readings during operating hours can be genuine (pool at full capacity
	// on hot days, e.g. Cosimawellenbad on May 2 2026).
	freibadPools := freibadPoolNames(html)
	for name, utility := range poolStats {
		if utility == 0 && freibadPools[name] {
			poolStats[name] = 100
			fmt.Printf("  Normalized closed Freibad: %s 0%% → 100%%\n", name)
		}
	}

	return poolStats
}

// freibadPoolNames returns the set of pool names that appear in the
// id="freibad" HTML section.  These outdoor pools use an inverted convention
// where 0% means "closed" (instead of the Hallenbad convention where 100%
// means "closed").  The detection is dynamic: any pool whose bath-name
// attribute sits inside <div id="freibad"> is included, so future pool
// additions require no code changes.
//
// Hallenbad-section pools are deliberately excluded — see extractPoolData
// for the rationale.
func freibadPoolNames(html string) map[string]bool {
	names := map[string]bool{}
	reStart := regexp.MustCompile(`<div[^>]*\bid="freibad"[^>]*>`)
	loc := reStart.FindStringIndex(html)
	if loc == nil {
		return names
	}
	section := html[loc[0]:]

	// Clip at the next known section boundary.
	for _, stopID := range []string{"hallenbad", "sauna"} {
		reStop := regexp.MustCompile(`<div[^>]*\bid="` + regexp.QuoteMeta(stopID) + `"[^>]*>`)
		if stopLoc := reStop.FindStringIndex(section); stopLoc != nil {
			section = section[:stopLoc[0]]
			break
		}
	}

	reBathName := regexp.MustCompile(`bath-name="([^"]+)"`)
	for _, m := range reBathName.FindAllStringSubmatch(section, -1) {
		if len(m) >= 2 {
			if name := strings.TrimSpace(m[1]); name != "" {
				names[name] = true
			}
		}
	}
	if len(names) == 0 {
		reHeadline := regexp.MustCompile(`class="headline-s">([^<]+)</h3>`)
		for _, m := range reHeadline.FindAllStringSubmatch(section, -1) {
			if len(m) >= 2 {
				if name := strings.TrimSpace(m[1]); name != "" {
					names[name] = true
				}
			}
		}
	}

	if len(names) > 0 {
		fmt.Printf("  Freibad section pools: %v\n", getSortedKeys(names))
	}
	return names
}

// getSortedKeys returns the keys of m in alphabetical order for deterministic
// log output.
func getSortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// extractPoolsSection returns the HTML spanning all swimming-pool sections,
// stopping before the sauna section.  It tries several known section IDs so
// that a future page rename degrades gracefully instead of silently returning
// no data.
//
// Known page structures:
//
//	2026-05+  id="freibad"  (outdoor pools)
//	          id="hallenbad" (indoor pools)
//	          id="sauna"    (saunas — marks the end; excluded)
//
//	~2026-04  id="bad"      (all pools combined)
//	          id="sauna"    (saunas — marks the end; excluded)
func extractPoolsSection(html string) string {
	// IDs that mark the start of a pools section (tried from left-most occurrence).
	poolStartIDs := []string{"freibad", "hallenbad", "bad"}

	reID := func(id string) *regexp.Regexp {
		return regexp.MustCompile(`<div[^>]*\bid="` + regexp.QuoteMeta(id) + `"[^>]*>`)
	}

	// Find the earliest occurrence of any known pool-start section.
	start := -1
	foundID := ""
	for _, id := range poolStartIDs {
		loc := reID(id).FindStringIndex(html)
		if loc != nil && (start == -1 || loc[0] < start) {
			start = loc[0]
			foundID = id
		}
	}

	if start == -1 {
		fmt.Println("Could not find any pool section (tried: freibad, hallenbad, bad)")
		// Diagnostic: list every <div id="..."> seen, to speed up future debugging.
		reDivID := regexp.MustCompile(`<div[^>]*\bid="([^"]+)"`)
		for _, m := range reDivID.FindAllStringSubmatch(html, 40) {
			fmt.Printf("  Saw div id=%q\n", m[1])
		}
		return ""
	}

	fmt.Printf("  Found pool section start: id=%q\n", foundID)
	section := html[start:]

	// Clip at the sauna section (we never want sauna entries).
	if saunaLoc := reID("sauna").FindStringIndex(section); saunaLoc != nil {
		section = section[:saunaLoc[0]]
		fmt.Println("  Clipped section at id=\"sauna\"")
	}

	return section
}

func main() {
	interval := 10
	runOnce := false

	for _, arg := range os.Args[1:] {
		if arg == "--once" || arg == "-o" {
			runOnce = true
		} else if v, err := strconv.Atoi(arg); err == nil {
			interval = v
		}
	}

	initDB()
	defer db.Close()

	if runOnce {
		fmt.Println("SWM Pool Scraper running once...")
		if err := scrape(); err != nil {
			log.Printf("Scrape failed: %v", err)
			os.Exit(1)
		}
		return
	}

	fmt.Printf("SWM Pool Scraper starting (interval: %d min)\n", interval)

	ticker := time.NewTicker(time.Duration(interval) * time.Minute)
	defer ticker.Stop()

	if err := scrape(); err != nil {
		log.Printf("Initial scrape failed: %v", err)
	}

	for range ticker.C {
		if err := scrape(); err != nil {
			log.Printf("Scrape failed: %v", err)
		}
	}
}
