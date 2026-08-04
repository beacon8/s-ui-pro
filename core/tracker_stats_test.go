package core

import "testing"

func TestUserTrafficSnapshotSurvivesStatsFlush(t *testing.T) {
	tracker := NewStatsTracker()
	readCounters, writeCounters := tracker.getReadCounters("inbound", "outbound", "alice")
	for _, counter := range readCounters {
		counter.Add(120)
	}
	for _, counter := range writeCounters {
		counter.Add(240)
	}

	before := tracker.UserTrafficSnapshot([]string{"alice"})
	if len(before) != 1 || before[0].Up != 120 || before[0].Down != 240 {
		t.Fatalf("unexpected snapshot before flush: %#v", before)
	}

	tracker.GetStats()

	after := tracker.UserTrafficSnapshot([]string{"alice"})
	if len(after) != 1 || after[0].Up != 120 || after[0].Down != 240 {
		t.Fatalf("snapshot changed after flush: %#v", after)
	}
}
