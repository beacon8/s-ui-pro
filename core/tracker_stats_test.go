package core

import (
	"testing"

	"github.com/admin8800/s-ui/database/model"
)

func TestRestoreStatsReturnsFailedBatchToCounters(t *testing.T) {
	tracker := NewStatsTracker()
	failed := []model.Stats{
		{Resource: "user", Tag: "alice", Direction: false, Traffic: 12},
		{Resource: "user", Tag: "alice", Direction: true, Traffic: 34},
	}
	tracker.RestoreStats(failed)

	restored := *tracker.GetStats()
	if len(restored) != 2 {
		t.Fatalf("restored stats count = %d, want 2", len(restored))
	}
	traffic := map[bool]int64{}
	for _, stat := range restored {
		traffic[stat.Direction] = stat.Traffic
	}
	if traffic[false] != 12 || traffic[true] != 34 {
		t.Fatalf("restored traffic = %#v", traffic)
	}
}

func TestResetUsersKeepsExistingConnectionCountersUsable(t *testing.T) {
	tracker := NewStatsTracker()
	reads, writes := tracker.getReadCounters("", "", "alice")
	reads[0].Add(100)
	writes[0].Add(200)

	tracker.ResetUsers([]string{"alice"})
	reads[0].Add(3)
	writes[0].Add(4)

	stats := *tracker.GetStats()
	traffic := map[bool]int64{}
	for _, stat := range stats {
		traffic[stat.Direction] = stat.Traffic
	}
	if traffic[true] != 3 || traffic[false] != 4 {
		t.Fatalf("post-reset traffic = %#v", traffic)
	}
}
