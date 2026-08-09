package main

import "testing"

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
