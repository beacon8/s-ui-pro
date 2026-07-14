package core

import (
	"context"
	"net"
	"sync"
	"time"

	"github.com/admin8800/s-ui/logger"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing/common/network"
	"golang.org/x/time/rate"
)

// minBurst is the lower bound for the token bucket burst size. It avoids small
// limits forcing tiny packets to wait, which would hurt interactive latency.
const minBurst = 64 * 1024

type userLimiter struct {
	up   *rate.Limiter // client -> server (Read side)
	down *rate.Limiter // server -> client (Write side)
}

type LimiterTracker struct {
	mu    sync.RWMutex
	users map[string]*userLimiter // key == metadata.User == client.Name
}

func NewLimiterTracker() *LimiterTracker {
	return &LimiterTracker{
		users: make(map[string]*userLimiter),
	}
}

func (t *LimiterTracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.users = make(map[string]*userLimiter)
}

func burstFor(bps int64) int {
	if bps > minBurst {
		return int(bps)
	}
	return minBurst
}

// SetUserLimit creates or updates a user's limiter. upBPS/downBPS are bytes/sec.
// If both are 0 the user entry is removed.
func (t *LimiterTracker) SetUserLimit(name string, upBPS, downBPS int64) {
	if name == "" {
		return
	}
	if upBPS == 0 && downBPS == 0 {
		t.DeleteUser(name)
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	ul, ok := t.users[name]
	if !ok {
		ul = &userLimiter{}
		t.users[name] = ul
	}

	if upBPS > 0 {
		if ul.up == nil {
			ul.up = rate.NewLimiter(rate.Limit(upBPS), burstFor(upBPS))
		} else {
			ul.up.SetLimit(rate.Limit(upBPS))
			ul.up.SetBurst(burstFor(upBPS))
		}
	} else {
		ul.up = nil
	}

	if downBPS > 0 {
		if ul.down == nil {
			ul.down = rate.NewLimiter(rate.Limit(downBPS), burstFor(downBPS))
		} else {
			ul.down.SetLimit(rate.Limit(downBPS))
			ul.down.SetBurst(burstFor(downBPS))
		}
	} else {
		ul.down = nil
	}
}

func (t *LimiterTracker) DeleteUser(name string) {
	if name == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.users, name)
}

// BulkLoad replaces the limiter set with the given limits ([2]int64{upBPS, downBPS}).
func (t *LimiterTracker) BulkLoad(limits map[string][2]int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.users = make(map[string]*userLimiter)
	for name, l := range limits {
		if name == "" || (l[0] == 0 && l[1] == 0) {
			continue
		}
		ul := &userLimiter{}
		if l[0] > 0 {
			ul.up = rate.NewLimiter(rate.Limit(l[0]), burstFor(l[0]))
		}
		if l[1] > 0 {
			ul.down = rate.NewLimiter(rate.Limit(l[1]), burstFor(l[1]))
		}
		t.users[name] = ul
	}
}

func (t *LimiterTracker) getUser(name string) *userLimiter {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.users[name]
}

func (t *LimiterTracker) RoutedConnection(ctx context.Context, conn net.Conn, metadata adapter.InboundContext, matchedRule adapter.Rule, matchOutbound adapter.Outbound) net.Conn {
	if metadata.User == "" {
		return conn
	}
	ul := t.getUser(metadata.User)
	if ul == nil || (ul.up == nil && ul.down == nil) {
		return conn
	}
	return &limitedConn{Conn: conn, up: ul.up, down: ul.down}
}

func (t *LimiterTracker) RoutedPacketConnection(ctx context.Context, conn network.PacketConn, metadata adapter.InboundContext, matchedRule adapter.Rule, matchOutbound adapter.Outbound) network.PacketConn {
	// UDP passthrough: no limiting.
	return conn
}

type limitedConn struct {
	net.Conn
	up   *rate.Limiter
	down *rate.Limiter
}

func (w *limitedConn) wait(l *rate.Limiter, n int) {
	if l == nil || n <= 0 {
		return
	}
	if err := l.WaitN(context.Background(), n); err != nil {
		// n exceeds the limiter's burst: fall back to non-blocking to avoid
		// stalling forever, then continue.
		logger.Debug("limiter waitN fallback: ", err.Error())
		l.AllowN(time.Now(), n)
	}
}

func (w *limitedConn) Read(b []byte) (int, error) {
	n, err := w.Conn.Read(b)
	if n > 0 {
		w.wait(w.up, n)
	}
	return n, err
}

func (w *limitedConn) Write(b []byte) (int, error) {
	w.wait(w.down, len(b))
	return w.Conn.Write(b)
}

func (w *limitedConn) Upstream() any {
	return w.Conn
}
