package main

import (
	"testing"
	"time"
)

func init() {
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		panic(err)
	}
	berlinLoc = loc
}

func TestBerlinSlotFollowsDaylightSavingTime(t *testing.T) {
	cases := []struct {
		name     string
		utc      string
		wantDow  int
		wantSlot int
	}{
		// 07:30 Berlin is slot 15 in both seasons, although the UTC hour differs.
		{"summer (CEST, UTC+2)", "2026-07-10 05:30:00", 5, 15},
		{"winter (CET, UTC+1)", "2026-11-20 06:30:00", 5, 15},
		// Saturday 22:45 UTC is 23:45 CET: the last slot of the same Berlin day.
		{"late evening stays on the local day", "2026-11-21 22:45:00", 6, 47},
		// Shortly after midnight Berlin time the UTC date is still the previous day.
		{"just after local midnight", "2026-11-21 23:10:00", 0, 0},
		// The night of the switch back (25 Oct 2026): 00:30 UTC = 02:30 CEST, 01:30 UTC = 02:30 CET.
		{"before switch back", "2026-10-25 00:30:00", 0, 5},
		{"after switch back", "2026-10-25 01:30:00", 0, 5},
	}
	for _, tc := range cases {
		ts, err := time.ParseInLocation(dbTimeLayout, tc.utc, time.UTC)
		if err != nil {
			t.Fatal(err)
		}
		dow, slot := berlinSlot(ts)
		if dow != tc.wantDow || slot != tc.wantSlot {
			t.Errorf("%s: berlinSlot(%s UTC) = (%d, %d), want (%d, %d)", tc.name, tc.utc, dow, slot, tc.wantDow, tc.wantSlot)
		}
	}
}

func TestAggregateHourlyMedianSamplesAndClosedFraction(t *testing.T) {
	cells := map[heatmapKey][]int{
		{pool: "Südbad", dow: 1, slot: 20}:     {80, 60, 70},            // odd count: middle value
		{pool: "Südbad", dow: 1, slot: 19}:     {90, 70, 80, 60},        // even count: mean of 70 and 80
		{pool: "Michaelibad", dow: 6, slot: 2}: {100, 100, 99, 40, 100}, // mostly "closed" readings
	}
	got := aggregateHourly(cells)
	want := []hourlyData{
		{Pool: "Michaelibad", DayOfWeek: 6, Slot: 2, Mean: 100, Samples: 5, ClosedFraction: 0.8},
		{Pool: "Südbad", DayOfWeek: 1, Slot: 19, Mean: 75, Samples: 4, ClosedFraction: 0},
		{Pool: "Südbad", DayOfWeek: 1, Slot: 20, Mean: 70, Samples: 3, ClosedFraction: 0},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d cells, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("cell %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}
