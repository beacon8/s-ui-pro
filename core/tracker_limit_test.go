package core

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/sagernet/sing-box/adapter"
)

func TestDynamicDownloadLimitState(t *testing.T) {
	tracker := NewLimiterTracker()
	tracker.SetUserLimitConfig("alice", UserLimitConfig{
		DownBPS: 80,
		Dynamic: DynamicLimitPolicy{
			Enabled: true, ThresholdBPS: 100, ObserveFor: 2 * time.Second,
			LimitBPS: 50, Cooldown: 3 * time.Second,
		},
	})
	t0 := time.Unix(100, 0)

	tracker.ObserveDownload("alice", 0, t0)
	tracker.ObserveDownload("alice", 100, t0.Add(time.Second))
	if got := tracker.DynamicStatus("alice", t0.Add(time.Second)); got.State != "observing" || got.ObservedSeconds != 1 {
		t.Fatalf("first high sample = %+v", got)
	}

	tracker.ObserveDownload("alice", 199, t0.Add(2*time.Second))
	if got := tracker.DynamicStatus("alice", t0.Add(2*time.Second)); got.ObservedSeconds != 0 {
		t.Fatalf("low sample did not reset observation: %+v", got)
	}

	tracker.ObserveDownload("alice", 299, t0.Add(3*time.Second))
	tracker.ObserveDownload("alice", 399, t0.Add(4*time.Second))
	if got := tracker.DynamicStatus("alice", t0.Add(4*time.Second)); got.State != "limited" || got.RemainingSeconds != 3 {
		t.Fatalf("continuous high samples did not trigger: %+v", got)
	}
	if limiter, _ := tracker.currentLimit("alice", true); limiter == nil || limiter.Limit() != 50 {
		t.Fatalf("dynamic/static minimum not applied: %v", limiter)
	}

	tracker.ObserveDownload("alice", 700, t0.Add(7*time.Second))
	if got := tracker.DynamicStatus("alice", t0.Add(7*time.Second)); got.State != "observing" || got.ObservedSeconds != 0 {
		t.Fatalf("cooldown did not reset observation: %+v", got)
	}
	if limiter, _ := tracker.currentLimit("alice", true); limiter == nil || limiter.Limit() != 80 {
		t.Fatalf("static limit was not restored: %v", limiter)
	}

	tracker.ObserveDownload("alice", 800, t0.Add(8*time.Second))
	if got := tracker.DynamicStatus("alice", t0.Add(8*time.Second)); got.State != "observing" || got.ObservedSeconds != 1 {
		t.Fatalf("full observation did not restart: %+v", got)
	}
	tracker.ObserveDownload("alice", 900, t0.Add(9*time.Second))
	if got := tracker.DynamicStatus("alice", t0.Add(9*time.Second)); got.State != "limited" {
		t.Fatalf("second observation did not trigger: %+v", got)
	}
}

func TestExistingConnectionSeesRuntimeLimitChanges(t *testing.T) {
	tracker := NewLimiterTracker()
	left, right := net.Pipe()
	t.Cleanup(func() { left.Close(); right.Close() })
	wrapped := tracker.RoutedConnection(context.Background(), left, adapter.InboundContext{User: "alice"}, nil, nil)
	conn, ok := wrapped.(*limitedConn)
	if !ok {
		t.Fatalf("unlimited user connection was not prepared for runtime limits: %T", wrapped)
	}
	if limiter, _ := conn.tracker.currentLimit(conn.user, true); limiter != nil {
		t.Fatal("new connection unexpectedly limited")
	}

	tracker.SetUserLimit("alice", 0, 100)
	_, oldContext := conn.tracker.currentLimit(conn.user, true)
	if limiter, _ := conn.tracker.currentLimit(conn.user, true); limiter == nil || limiter.Limit() != 100 {
		t.Fatalf("existing connection missed new limit: %v", limiter)
	}

	tracker.SetUserLimit("alice", 0, 200)
	select {
	case <-oldContext.Done():
		if oldContext.Err() != context.Canceled {
			t.Fatalf("old wait context error = %v", oldContext.Err())
		}
	default:
		t.Fatal("runtime update did not release an existing wait")
	}

	tracker.SetUserLimit("alice", 0, 0)
	if limiter, _ := conn.tracker.currentLimit(conn.user, true); limiter != nil {
		t.Fatal("existing connection missed limit removal")
	}
}
