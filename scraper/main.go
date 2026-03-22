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
		"--virtual-time-budget=15000",
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

	for name, utility := range poolStats {
		_, err := db.Exec("INSERT INTO track_pools (name, utility) VALUES (?, ?)", name, utility)
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

	reList := regexp.MustCompile(`bath-capacity__item-list[\s\S]*?headline-s[^>]*>([^<]+)[\s\S]*?(\d+)%`)
	listMatches := reList.FindAllStringSubmatch(html, -1)

	for _, match := range listMatches {
		if len(match) >= 3 {
			name := strings.TrimSpace(match[1])
			utilityStr := strings.TrimSpace(match[2])
			utility, err := strconv.Atoi(utilityStr)
			if err == nil && name != "" {
				poolStats[name] = utility
			}
		}
	}

	if len(poolStats) == 0 {
		fmt.Println("Trying alternative extraction...")
		lines := strings.Split(html, "\n")
		for i, line := range lines {
			if strings.Contains(line, "headline-s") && !strings.Contains(line, "nav-card") {
				name := extractTextFromTag(line)
				if name != "" && len(name) > 2 {
					for j := i; j < i+20 && j < len(lines); j++ {
						if strings.Contains(lines[j], "%") {
							rePct := regexp.MustCompile(`(\d+)\s*%`)
							pctMatch := rePct.FindStringSubmatch(lines[j])
							if len(pctMatch) >= 2 {
								utility, _ := strconv.Atoi(pctMatch[1])
								if utility > 0 && utility <= 100 {
									poolStats[name] = utility
									fmt.Printf("  Found: %s -> %d%%\n", name, utility)
									break
								}
							}
						}
					}
				}
			}
		}
	}

	return poolStats
}

func extractTextFromTag(tag string) string {
	start := strings.Index(tag, ">")
	if start == -1 {
		return ""
	}
	tag = tag[start+1:]
	end := strings.Index(tag, "<")
	if end == -1 {
		return ""
	}
	return strings.TrimSpace(tag[:end])
}

func main() {
	interval := 15
	if len(os.Args) > 1 {
		if v, err := strconv.Atoi(os.Args[1]); err == nil {
			interval = v
		}
	}

	initDB()
	defer db.Close()

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
