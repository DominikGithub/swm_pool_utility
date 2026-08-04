/*
Pool scraper — collects live occupancy data from the SWM REST API.
*/
package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
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

// normalizeName replaces Unicode smart quotes and other visually-similar
// characters with their ASCII equivalents.  The SWM website sometimes uses
// typographic quotes (\u2019) while the REST API uses ASCII apostrophes (\u0027).
// Normalizing ensures we never create duplicate pool entries because of
// invisible Unicode differences.
func normalizeName(name string) string {
	name = strings.ReplaceAll(name, "\u2018", "'") // left single quotation mark
	name = strings.ReplaceAll(name, "\u2019", "'") // right single quotation mark
	name = strings.ReplaceAll(name, "\u201c", `"`) // left double quotation mark
	name = strings.ReplaceAll(name, "\u201d", `"`) // right double quotation mark
	return name
}

// loadPoolIDs reads all existing pools into the in-memory cache.
// Names are normalized so that the in-memory lookup is always consistent.
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
		key := normalizeName(name)
		if existing, ok := poolIDs[key]; ok {
			// Two pool rows with different Unicode but the same normalized
			// name — keep the one with more track_pools data.
			var cnt1, cnt2 int
			db.QueryRow("SELECT COUNT(*) FROM track_pools WHERE pool_id = ?", existing).Scan(&cnt1)
			db.QueryRow("SELECT COUNT(*) FROM track_pools WHERE pool_id = ?", id).Scan(&cnt2)
			if cnt2 > cnt1 {
				poolIDs[key] = id
			}
			log.Printf("warning: duplicate pool names %q (ids %d, %d) — keeping id %d",
				key, existing, id, poolIDs[key])
		} else {
			poolIDs[key] = id
		}
	}
}

// getOrCreatePoolID returns the pools.id for name, inserting a new row if this
// is the first time the pool has been seen.  The name is normalized before
// lookup and insert to avoid duplicates from invisible Unicode differences.
func getOrCreatePoolID(name string) (int64, error) {
	name = normalizeName(name)
	if id, ok := poolIDs[name]; ok {
		return id, nil
	}
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

// --- API response types ---

type apiResponse struct {
	ReturnCode string     `json:"return_code"`
	Data       []location `json:"data"`
}

type location struct {
	LocationID       int    `json:"location_id"`
	LocationName     string `json:"location_name"`
	CustomerAmount   int    `json:"customer_amount"`
	CustomerAmountMax int   `json:"customer_amount_max"`
	AreaList          []area `json:"area_list"`
}

type area struct {
	AreaID           int    `json:"area_id"`
	AreaName         string `json:"area_name"`
	CustomerAmount   int    `json:"customer_amount"`
	CustomerAmountMax int   `json:"customer_amount_max"`
}

// --- Pool name mapping ---
//
// Maps API area_name → database pool name.  Sauna areas are excluded.
// A missing entry means the area is skipped (e.g. unknown areas, saunas).
var areaToPool = map[string]string{
	// Freibäder (outdoor pools)
	"Bad Georgenschwaige Freibad":       "Naturbad Georgenschwaige",
	"Dantebad Freibad":                 "Dantebad",
	"Freibad West":                     "Freibad West",
	"Naturbad Maria Einsiedel Freibad": "Naturbad Maria Einsiedel",
	"Michaelibad Freibad":              "Michaeli-Freibad",
	"Prinzregentenbad Freibad":         "Prinzregentenbad",
	"Schyrenbad Freibad":               "Schyrenbad",
	"Ungererbad Freibad":               "Ungererbad",

	// Hallenbäder (indoor pools)
	"Cosimawellenbad Hallenbad":        "Cosimawellenbad",
	"Dante-Winter-Warmfreibad":         "Dante-Winter-Warmfreibad",
	"Michaelibad Hallenbad":            "Michaelibad",
	"Müller'sches Volksbad Hallenbad":  "Müller'sches Volksbad",
	"Nordbad Hallenbad":                "Nordbad",
	"Südbad Hallenbad":                 "Südbad",
	"Westbad Hallenbad":                "Westbad",
}

var httpClient = &http.Client{
	Timeout: 30 * time.Second,
}

func scrape() error {
	fmt.Println("Starting scrape via REST API...")

	apiURL := os.Getenv("SWM_API_URL")
	if apiURL == "" {
		apiURL = "https://www.swm.de/.rest/bath/visitorCount"
	}

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", "swm-pool-scraper/1.0")
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read API response: %w", err)
	}
	fmt.Printf("Got API response: %d bytes\n", len(body))

	var data apiResponse
	if err := json.Unmarshal(body, &data); err != nil {
		return fmt.Errorf("failed to parse JSON: %w", err)
	}
	if data.ReturnCode != "success" {
		return fmt.Errorf("API return_code = %q", data.ReturnCode)
	}

	// Walk all locations and their areas; pick the ones we track.
	poolStats := make(map[string]int)

	for _, loc := range data.Data {
		for _, a := range loc.AreaList {
		dbName, ok := areaToPool[a.AreaName]
		if !ok {
				continue
			}
			if a.CustomerAmountMax == 0 {
				fmt.Printf("  Skipping %s: max capacity is 0\n", dbName)
				continue
			}
			// DB stores free capacity: 100 = empty, 0 = completely full.
			// Compute as 100 - filled_pct.
			pct := int(math.Round((1.0 - float64(a.CustomerAmount)/float64(a.CustomerAmountMax)) * 100))
			if pct < 0 {
				pct = 0
			}
			if pct > 100 {
				pct = 100
			}
			poolStats[dbName] = pct
			fmt.Printf("  Found: %s -> %d%% (%d/%d)\n", dbName, pct, a.CustomerAmount, a.CustomerAmountMax)
		}
	}

	fmt.Printf("Found %d pools\n", len(poolStats))

	if len(poolStats) == 0 {
		return fmt.Errorf("no pools found in API response")
	}


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
