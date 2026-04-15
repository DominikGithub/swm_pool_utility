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

func extractPoolData(html string) map[string]int {
	poolStats := make(map[string]int)

	poolsSection := extractPoolsSection(html)
	if poolsSection == "" {
		fmt.Println("Could not find pools section")
		return poolStats
	}

	fmt.Printf("Pools section length: %d bytes\n", len(poolsSection))

	rePoolName := regexp.MustCompile(`class="headline-s">([^<]+)</h3>`)
	rePercent := regexp.MustCompile(`(\d+)\s*%`)

	matches := rePoolName.FindAllStringSubmatchIndex(poolsSection, -1)
	fmt.Printf("  Found %d pool name matches\n", len(matches))

	for _, match := range matches {
		if len(match) >= 4 {
			name := poolsSection[match[2]:match[3]]
			name = strings.TrimSpace(name)

			if name == "" {
				continue
			}

			if _, exists := poolStats[name]; exists {
				continue
			}

			startPos := match[1]
			endPos := startPos + 2000
			if endPos > len(poolsSection) {
				endPos = len(poolsSection)
			}
			searchArea := poolsSection[startPos:endPos]

			pctMatches := rePercent.FindAllStringSubmatch(searchArea, 5)
			for _, pctMatch := range pctMatches {
				if len(pctMatch) >= 2 {
					pct, err := strconv.Atoi(pctMatch[1])
					if err == nil && pct >= 0 && pct <= 100 {
						poolStats[name] = pct
						fmt.Printf("  Found: %s -> %d%%\n", name, pct)
						break
					}
				}
			}
		}
	}

	return poolStats
}

// extractPoolsSection returns only the HTML between the pools container
// (id="bad") and the saunas container (id="sauna"). This is purely
// structural — no pool/sauna name matching needed.
//
// Page structure (as of 2026-04):
//
//	<div ... id="bad">
//	  <h2 ...>Echtzeit-Auslastung der Bäder</h2>
//	  ... pool <h3> entries ...
//	</div>
//	<div ... id="sauna">
//	  <h2 ...>Echtzeit-Auslastung der Saunen</h2>
//	  ... sauna <h3> entries ...
//	</div>
func extractPoolsSection(html string) string {
	// Find the pools container: <div ... id="bad">
	reBadStart := regexp.MustCompile(`<div[^>]*\bid="bad"[^>]*>`)
	badLoc := reBadStart.FindStringIndex(html)
	if badLoc == nil {
		fmt.Println("Could not find id=\"bad\" section")
		return ""
	}

	section := html[badLoc[0]:]

	// Find the saunas container: <div ... id="sauna">
	// This marks the end of the pools section.
	reSaunaStart := regexp.MustCompile(`<div[^>]*\bid="sauna"[^>]*>`)
	saunaLoc := reSaunaStart.FindStringIndex(section)
	if saunaLoc != nil {
		section = section[:saunaLoc[0]]
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
