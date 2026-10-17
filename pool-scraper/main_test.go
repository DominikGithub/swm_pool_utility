package main

import (
	"database/sql"
	"testing"
)

func TestExtractPoolDataRecordsNamedClosedPool(t *testing.T) {
	html := `<div id="freibad">
		<li class="bath-capacity-item"><h3 class="headline-s">Bad Giesing-Harlaching</h3><p class="closed-msg">Closed</p></li>
		<li class="bath-capacity-item"><h3 class="headline-s">Olympia-Schwimmhalle</h3><div style="width: 38%"></div></li>
	</div>`

	stats, unavailablePools := extractPoolData(html)
	if got := stats["Bad Giesing-Harlaching"]; got != 100 {
		t.Fatalf("closed pool = %d, want 100", got)
	}
	if got := stats["Olympia-Schwimmhalle"]; got != 38 {
		t.Fatalf("active pool = %d, want 38", got)
	}
	if len(unavailablePools) != 0 {
		t.Fatalf("unavailable pools = %v, want none", unavailablePools)
	}
}

func TestExtractPoolDataSkipsStatusHeadingsAndCanonicalizesNames(t *testing.T) {
	html := `<div id="hallenbad">
		<li class="bath-capacity-item"><h3 class="headline-s">Geschlossen – in Revision</h3><p class="closed-msg">Closed</p></li>
		<li class="bath-capacity-item"><h3 class="headline-s">Müller’sches Volksbad</h3><div style="width: 41%"></div></li>
	</div>`

	stats, unavailablePools := extractPoolData(html)
	if _, ok := stats["Geschlossen – in Revision"]; ok {
		t.Fatal("status heading was recorded as a pool")
	}
	if got := stats["Müller'sches Volksbad"]; got != 41 {
		t.Fatalf("canonical pool = %d, want 41", got)
	}
	if len(unavailablePools) != 0 {
		t.Fatalf("unavailable pools = %v, want none", unavailablePools)
	}
}

func TestExtractPoolDataMarksRenovationAsUnavailable(t *testing.T) {
	html := `<div id="hallenbad">
		<li class="bath-capacity-item"><h3 class="headline-s">Westbad</h3><div class="closed-msg"><h3>Geschlossen – aufgrund von Sanierungsarbeiten</h3></div></li>
	</div>`

	stats, unavailablePools := extractPoolData(html)
	if _, ok := stats["Westbad"]; ok {
		t.Fatal("long-term closure was recorded as an ordinary closed pool")
	}
	if len(unavailablePools) != 1 || unavailablePools[0] != "Westbad" {
		t.Fatalf("unavailable pools = %v, want [Westbad]", unavailablePools)
	}
}

func TestValidatePoolStatsAcceptsNormalScrape(t *testing.T) {
	stats := map[string]int{
		"Nordbad":              40,
		"Westbad":              55,
		"Südbad":               0,
		"Michaelibad":          0,
		"Olympia-Schwimmhalle": 20,
		"Cosimawellenbad":      70,
	}
	if err := validatePoolStats(stats); err != nil {
		t.Fatalf("normal scrape rejected: %v", err)
	}
}

func TestValidatePoolStatsAcceptsAllClosed(t *testing.T) {
	// Overnight: every pool closed → all 100. Must not be treated as a glitch.
	stats := map[string]int{
		"Nordbad": 100, "Westbad": 100, "Südbad": 100,
		"Michaelibad": 100, "Olympia-Schwimmhalle": 100, "Cosimawellenbad": 100,
	}
	if err := validatePoolStats(stats); err != nil {
		t.Fatalf("all-closed scrape rejected: %v", err)
	}
}

func TestValidatePoolStatsRejectsTooFewPools(t *testing.T) {
	stats := map[string]int{"Nordbad": 40, "Westbad": 55}
	if err := validatePoolStats(stats); err == nil {
		t.Fatal("too-few-pools scrape was accepted")
	}
}

func TestValidatePoolStatsRejectsSiteWideZero(t *testing.T) {
	stats := map[string]int{
		"Nordbad": 0, "Westbad": 0, "Südbad": 0,
		"Michaelibad": 0, "Olympia-Schwimmhalle": 0, "Cosimawellenbad": 0,
	}
	if err := validatePoolStats(stats); err == nil {
		t.Fatal("site-wide 0% glitch was accepted")
	}
}

func TestNormalizePoolStatsMapsInvertedConvention(t *testing.T) {
	stats := map[string]int{
		"Michaelibad":          0, // inverted: closed → 100
		"Cosimawellenbad":      0, // genuine full capacity → stays 0
		"Olympia-Schwimmhalle": 42,
	}
	normalizePoolStats(stats)
	if got := stats["Michaelibad"]; got != 100 {
		t.Fatalf("Michaelibad = %d, want 100", got)
	}
	if got := stats["Cosimawellenbad"]; got != 0 {
		t.Fatalf("Cosimawellenbad = %d, want 0 (genuine full)", got)
	}
	if got := stats["Olympia-Schwimmhalle"]; got != 42 {
		t.Fatalf("Olympia-Schwimmhalle = %d, want 42", got)
	}
}

// newTestDB points the package-level db/poolIDs at a throwaway in-memory
// database holding just the two tables the zero-guard reads.
func newTestDB(t *testing.T, pools ...string) {
	t.Helper()
	conn, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	schema := `CREATE TABLE pools(id INTEGER PRIMARY KEY, name VARCHAR NOT NULL UNIQUE);
		CREATE TABLE track_pools(id INTEGER PRIMARY KEY AUTOINCREMENT,
			pool_id INTEGER NOT NULL, dtime DATETIME, utility INT);`
	if _, err := conn.Exec(schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}

	prevDB, prevIDs := db, poolIDs
	db, poolIDs = conn, map[string]int64{}
	for i, name := range pools {
		id := int64(i + 1)
		if _, err := conn.Exec("INSERT INTO pools(id, name) VALUES (?, ?)", id, name); err != nil {
			t.Fatalf("insert pool %q: %v", name, err)
		}
		poolIDs[name] = id
	}
	t.Cleanup(func() {
		conn.Close()
		db, poolIDs = prevDB, prevIDs
	})
}

func addSample(t *testing.T, pool, dtime string, utility int) {
	t.Helper()
	if _, err := db.Exec("INSERT INTO track_pools(pool_id, dtime, utility) VALUES (?, ?, ?)",
		poolIDs[pool], dtime, utility); err != nil {
		t.Fatalf("insert sample: %v", err)
	}
}

// A partial glitch: only some pools read 0, so validatePoolStats cannot see it,
// but no pool goes from 93% free capacity to completely full in ten minutes.
func TestDropImplausibleZerosRemovesPartialGlitch(t *testing.T) {
	newTestDB(t, "Cosimawellenbad", "Olympia-Schwimmhalle")
	addSample(t, "Cosimawellenbad", "2026-10-17 06:54:46", 93)
	addSample(t, "Olympia-Schwimmhalle", "2026-10-17 06:54:46", 96)

	stats := map[string]int{"Cosimawellenbad": 0, "Olympia-Schwimmhalle": 96}
	if dropped := dropImplausibleZeros(stats, "2026-10-17 07:04:46"); dropped != 1 {
		t.Fatalf("dropped = %d, want 1", dropped)
	}
	if _, ok := stats["Cosimawellenbad"]; ok {
		t.Error("implausible 0% was kept")
	}
	if got := stats["Olympia-Schwimmhalle"]; got != 96 {
		t.Errorf("Olympia-Schwimmhalle = %d, want 96 (untouched)", got)
	}
}

// A real full-capacity event is approached gradually, so its 0 must survive —
// these readings genuinely occurred on hot May/June afternoons.
func TestDropImplausibleZerosKeepsGradualFill(t *testing.T) {
	newTestDB(t, "Cosimawellenbad")
	addSample(t, "Cosimawellenbad", "2026-05-24 11:20:05", 1)

	stats := map[string]int{"Cosimawellenbad": 0}
	if dropped := dropImplausibleZeros(stats, "2026-05-24 11:30:05"); dropped != 0 {
		t.Fatalf("dropped = %d, want 0", dropped)
	}
	if got, ok := stats["Cosimawellenbad"]; !ok || got != 0 {
		t.Error("genuine full-capacity 0% was discarded")
	}
}

// For inverted-convention pools a raw 0 means "closed"; normalizePoolStats
// turns it into 100, so the guard must leave it alone.
func TestDropImplausibleZerosSkipsInvertedConvention(t *testing.T) {
	newTestDB(t, "Michaelibad")
	addSample(t, "Michaelibad", "2026-10-17 00:51:00", 100)

	stats := map[string]int{"Michaelibad": 0}
	if dropped := dropImplausibleZeros(stats, "2026-10-17 01:00:28"); dropped != 0 {
		t.Fatalf("dropped = %d, want 0", dropped)
	}
	normalizePoolStats(stats)
	if got := stats["Michaelibad"]; got != 100 {
		t.Fatalf("Michaelibad = %d, want 100 (closed overnight)", got)
	}
}

// After a scraper outage the last stored reading says nothing about the pool's
// present state, so the guard stands down instead of discarding good data.
func TestDropImplausibleZerosStandsDownOnStaleHistory(t *testing.T) {
	newTestDB(t, "Cosimawellenbad")
	addSample(t, "Cosimawellenbad", "2026-10-17 04:00:00", 93)

	stats := map[string]int{"Cosimawellenbad": 0}
	if dropped := dropImplausibleZeros(stats, "2026-10-17 07:04:46"); dropped != 0 {
		t.Fatalf("dropped = %d, want 0 (history older than lookback)", dropped)
	}
}

// An unseen pool has no history to judge against; its reading must be stored.
func TestDropImplausibleZerosKeepsUnknownPool(t *testing.T) {
	newTestDB(t)

	stats := map[string]int{"Brand-New-Bad": 0}
	if dropped := dropImplausibleZeros(stats, "2026-10-17 07:04:46"); dropped != 0 {
		t.Fatalf("dropped = %d, want 0", dropped)
	}
}

func countSamples(t *testing.T, pool string) int {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM track_pools WHERE pool_id = ?",
		poolIDs[pool]).Scan(&n); err != nil {
		t.Fatalf("count samples: %v", err)
	}
	return n
}

// The glitch signature: busy, a lone "closed", busy again ten minutes later.
func TestCorrectFalseEmptyRemovesLoneIsland(t *testing.T) {
	newTestDB(t, "Müller'sches Volksbad")
	addSample(t, "Müller'sches Volksbad", "2026-10-05 10:00:08", 76)
	addSample(t, "Müller'sches Volksbad", "2026-10-05 10:10:08", 100) // glitch

	stats := map[string]int{"Müller'sches Volksbad": 62}
	if got := correctFalseEmptySamples(stats, "2026-10-05 10:20:08"); got != 1 {
		t.Fatalf("corrected = %d, want 1", got)
	}
	if got := countSamples(t, "Müller'sches Volksbad"); got != 1 {
		t.Fatalf("%d samples left, want 1 (only the busy reading)", got)
	}
}

// Two consecutive glitch samples are still within the island limit.
func TestCorrectFalseEmptyRemovesTwoSampleIsland(t *testing.T) {
	newTestDB(t, "Nordbad")
	addSample(t, "Nordbad", "2026-10-05 14:00:00", 70)
	addSample(t, "Nordbad", "2026-10-05 14:10:00", 100)
	addSample(t, "Nordbad", "2026-10-05 14:20:00", 100)

	stats := map[string]int{"Nordbad": 68}
	if got := correctFalseEmptySamples(stats, "2026-10-05 14:30:00"); got != 2 {
		t.Fatalf("corrected = %d, want 2", got)
	}
	if got := countSamples(t, "Nordbad"); got != 1 {
		t.Fatalf("%d samples left, want 1", got)
	}
}

// The case that must NOT fire: a real closure the pool reopens from hours later.
// Every closed reading is legitimate and has to survive.
func TestCorrectFalseEmptyKeepsOvernightClosure(t *testing.T) {
	newTestDB(t, "Nordbad")
	addSample(t, "Nordbad", "2026-10-15 20:50:00", 32) // busy, just before closing
	for _, ts := range []string{
		"2026-10-15 21:00:00", "2026-10-15 21:10:00", "2026-10-15 21:20:00",
		"2026-10-15 21:30:00", "2026-10-15 21:40:00",
	} {
		addSample(t, "Nordbad", ts, 100) // genuinely closed
	}

	stats := map[string]int{"Nordbad": 88}
	if got := correctFalseEmptySamples(stats, "2026-10-15 21:50:00"); got != 0 {
		t.Fatalf("corrected = %d, want 0 — a real closure was deleted", got)
	}
	if got := countSamples(t, "Nordbad"); got != 6 {
		t.Fatalf("%d samples left, want 6 (nothing removed)", got)
	}
}

// A closure longer than falseEmptyMaxRun is taken at face value even when the
// surrounding readings are busy.
func TestCorrectFalseEmptyKeepsLongIsland(t *testing.T) {
	newTestDB(t, "Südbad")
	addSample(t, "Südbad", "2026-10-05 12:00:00", 60)
	addSample(t, "Südbad", "2026-10-05 12:10:00", 100)
	addSample(t, "Südbad", "2026-10-05 12:20:00", 100)
	addSample(t, "Südbad", "2026-10-05 12:30:00", 100)

	stats := map[string]int{"Südbad": 58}
	if got := correctFalseEmptySamples(stats, "2026-10-05 12:40:00"); got != 0 {
		t.Fatalf("corrected = %d, want 0", got)
	}
}

// A scraper gap means the samples are not consecutive, so nothing can be
// concluded and the closed reading stays.
func TestCorrectFalseEmptyStandsDownAcrossGap(t *testing.T) {
	newTestDB(t, "Cosimawellenbad")
	addSample(t, "Cosimawellenbad", "2026-10-05 09:00:00", 70)
	addSample(t, "Cosimawellenbad", "2026-10-05 09:10:00", 100)

	// 50 minutes later — the scraper was down in between.
	stats := map[string]int{"Cosimawellenbad": 65}
	if got := correctFalseEmptySamples(stats, "2026-10-05 10:00:00"); got != 0 {
		t.Fatalf("corrected = %d, want 0 (gap breaks contiguity)", got)
	}
}

// If the pool is not clearly in use again, the island proves nothing.
func TestCorrectFalseEmptyRequiresBusyPresent(t *testing.T) {
	newTestDB(t, "Nordbad")
	addSample(t, "Nordbad", "2026-10-05 23:00:00", 70)
	addSample(t, "Nordbad", "2026-10-05 23:10:00", 100)

	stats := map[string]int{"Nordbad": 97} // nearly empty: indistinguishable
	if got := correctFalseEmptySamples(stats, "2026-10-05 23:20:00"); got != 0 {
		t.Fatalf("corrected = %d, want 0", got)
	}
}

// The reading entering the island must itself be busy; a closure approached
// from an already-quiet pool is normal.
func TestCorrectFalseEmptyRequiresBusyBefore(t *testing.T) {
	newTestDB(t, "Nordbad")
	addSample(t, "Nordbad", "2026-10-05 22:00:00", 95) // nearly empty already
	addSample(t, "Nordbad", "2026-10-05 22:10:00", 100)

	stats := map[string]int{"Nordbad": 80}
	if got := correctFalseEmptySamples(stats, "2026-10-05 22:20:00"); got != 0 {
		t.Fatalf("corrected = %d, want 0", got)
	}
}
