package core

import (
	"context"
	"errors"
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

type DynamicLimitPolicy struct {
	Enabled      bool
	ThresholdBPS int64
	ObserveFor   time.Duration
	LimitBPS     int64
	Cooldown     time.Duration
}

type UserLimitConfig struct {
	UpBPS   int64
	DownBPS int64
	Dynamic DynamicLimitPolicy
}

type DynamicLimitStatus struct {
	State            string `json:"state"`
	ObservedSeconds  int64  `json:"observedSeconds"`
	RemainingSeconds int64  `json:"remainingSeconds"`
}

type userLimiter struct {
	up               *rate.Limiter
	down             *rate.Limiter
	upContext        context.Context
	downContext      context.Context
	cancelUp         context.CancelFunc
	cancelDown       context.CancelFunc
	config           UserLimitConfig
	lastDown         int64
	lastSample       time.Time
	observedFor      time.Duration
	limitedUntil     time.Time
	dynamicLimited   bool
	effectiveUpBPS   int64
	effectiveDownBPS int64
}

type LimiterTracker struct {
	mu    sync.RWMutex
	users map[string]*userLimiter // key == metadata.User == client.Name
}

func NewLimiterTracker() *LimiterTracker {
	return &LimiterTracker{users: make(map[string]*userLimiter)}
}

func (t *LimiterTracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, user := range t.users {
		user.cancelWaits()
	}
	t.users = make(map[string]*userLimiter)
}

func burstFor(bps int64) int {
	if bps > minBurst {
		return int(bps)
	}
	return minBurst
}

func (u *userLimiter) cancelWaits() {
	if u.cancelUp != nil {
		u.cancelUp()
	}
	if u.cancelDown != nil {
		u.cancelDown()
	}
}

func updateLimiter(limiter **rate.Limiter, waitContext *context.Context, cancel *context.CancelFunc, oldBPS *int64, newBPS int64) {
	if *oldBPS == newBPS {
		return
	}
	if *cancel != nil {
		(*cancel)()
	}
	*oldBPS = newBPS
	if newBPS <= 0 {
		*limiter = nil
		*waitContext = nil
		*cancel = nil
		return
	}
	if *limiter == nil {
		*limiter = rate.NewLimiter(rate.Limit(newBPS), burstFor(newBPS))
	} else {
		(*limiter).SetLimit(rate.Limit(newBPS))
		(*limiter).SetBurst(burstFor(newBPS))
	}
	*waitContext, *cancel = context.WithCancel(context.Background())
}

func (u *userLimiter) applyLimits() {
	downBPS := u.config.DownBPS
	if u.dynamicLimited && (downBPS <= 0 || u.config.Dynamic.LimitBPS < downBPS) {
		downBPS = u.config.Dynamic.LimitBPS
	}
	updateLimiter(&u.up, &u.upContext, &u.cancelUp, &u.effectiveUpBPS, u.config.UpBPS)
	updateLimiter(&u.down, &u.downContext, &u.cancelDown, &u.effectiveDownBPS, downBPS)
}

func validDynamicPolicy(policy DynamicLimitPolicy) bool {
	return policy.Enabled && policy.ThresholdBPS > 0 && policy.ObserveFor > 0 && policy.LimitBPS > 0 && policy.Cooldown > 0
}

// SetUserLimit creates or updates a user's static limits. Values are bytes/sec.
func (t *LimiterTracker) SetUserLimit(name string, upBPS, downBPS int64) {
	t.SetUserLimitConfig(name, UserLimitConfig{UpBPS: upBPS, DownBPS: downBPS})
}

// SetUserLimitConfig creates or updates a user's static and dynamic limits.
func (t *LimiterTracker) SetUserLimitConfig(name string, config UserLimitConfig) {
	if name == "" {
		return
	}
	if !validDynamicPolicy(config.Dynamic) {
		config.Dynamic = DynamicLimitPolicy{}
	}
	if config.UpBPS <= 0 && config.DownBPS <= 0 && !config.Dynamic.Enabled {
		t.DeleteUser(name)
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	user := t.users[name]
	if user == nil {
		user = &userLimiter{}
		t.users[name] = user
	}
	if user.config.Dynamic != config.Dynamic {
		user.lastDown = 0
		user.lastSample = time.Time{}
		user.observedFor = 0
		user.limitedUntil = time.Time{}
		user.dynamicLimited = false
	}
	user.config = config
	user.applyLimits()
}

func (t *LimiterTracker) DeleteUser(name string) {
	if name == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if user := t.users[name]; user != nil {
		user.cancelWaits()
	}
	delete(t.users, name)
}

func (t *LimiterTracker) BulkLoad(limits map[string][2]int64) {
	configs := make(map[string]UserLimitConfig, len(limits))
	for name, limit := range limits {
		configs[name] = UserLimitConfig{UpBPS: limit[0], DownBPS: limit[1]}
	}
	t.BulkLoadConfigs(configs)
}

func (t *LimiterTracker) BulkLoadConfigs(limits map[string]UserLimitConfig) {
	t.Reset()
	for name, limit := range limits {
		t.SetUserLimitConfig(name, limit)
	}
}

func (t *LimiterTracker) DynamicUsers() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	users := make([]string, 0, len(t.users))
	for name, user := range t.users {
		if user.config.Dynamic.Enabled {
			users = append(users, name)
		}
	}
	return users
}

func (t *LimiterTracker) ObserveDownload(name string, totalDown int64, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	user := t.users[name]
	if user == nil || !user.config.Dynamic.Enabled {
		return
	}
	if user.dynamicLimited {
		if now.Before(user.limitedUntil) {
			return
		}
		user.dynamicLimited = false
		user.limitedUntil = time.Time{}
		user.observedFor = 0
		user.lastDown = totalDown
		user.lastSample = now
		user.applyLimits()
		return
	}
	if user.lastSample.IsZero() || !now.After(user.lastSample) || totalDown < user.lastDown {
		user.lastDown = totalDown
		user.lastSample = now
		user.observedFor = 0
		return
	}

	elapsed := now.Sub(user.lastSample)
	rateBPS := float64(totalDown-user.lastDown) / elapsed.Seconds()
	user.lastDown = totalDown
	user.lastSample = now
	if rateBPS < float64(user.config.Dynamic.ThresholdBPS) {
		user.observedFor = 0
		return
	}
	user.observedFor += elapsed
	if user.observedFor >= user.config.Dynamic.ObserveFor {
		user.dynamicLimited = true
		user.limitedUntil = now.Add(user.config.Dynamic.Cooldown)
		user.applyLimits()
	}
}

func (t *LimiterTracker) DynamicStatus(name string, now time.Time) DynamicLimitStatus {
	t.mu.RLock()
	defer t.mu.RUnlock()
	user := t.users[name]
	if user == nil || !user.config.Dynamic.Enabled {
		return DynamicLimitStatus{State: "disabled"}
	}
	if user.dynamicLimited {
		remaining := user.limitedUntil.Sub(now)
		if remaining < 0 {
			remaining = 0
		}
		return DynamicLimitStatus{State: "limited", RemainingSeconds: int64((remaining + time.Second - 1) / time.Second)}
	}
	return DynamicLimitStatus{State: "observing", ObservedSeconds: int64(user.observedFor / time.Second)}
}

func (t *LimiterTracker) currentLimit(name string, download bool) (*rate.Limiter, context.Context) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	user := t.users[name]
	if user == nil {
		return nil, nil
	}
	if download {
		return user.down, user.downContext
	}
	return user.up, user.upContext
}

func (t *LimiterTracker) RoutedConnection(ctx context.Context, conn net.Conn, metadata adapter.InboundContext, matchedRule adapter.Rule, matchOutbound adapter.Outbound) net.Conn {
	if metadata.User == "" {
		return conn
	}
	return &limitedConn{Conn: conn, tracker: t, user: metadata.User}
}

func (t *LimiterTracker) RoutedPacketConnection(ctx context.Context, conn network.PacketConn, metadata adapter.InboundContext, matchedRule adapter.Rule, matchOutbound adapter.Outbound) network.PacketConn {
	// UDP passthrough: no limiting.
	return conn
}

type limitedConn struct {
	net.Conn
	tracker *LimiterTracker
	user    string
}

func (w *limitedConn) wait(download bool, n int) {
	if n <= 0 {
		return
	}
	limiter, waitContext := w.tracker.currentLimit(w.user, download)
	if limiter == nil {
		return
	}
	if err := limiter.WaitN(waitContext, n); err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		// n exceeds the limiter's burst: fall back to non-blocking to avoid
		// stalling forever, then continue.
		logger.Debug("limiter waitN fallback: ", err.Error())
		limiter.AllowN(time.Now(), n)
	}
}

func (w *limitedConn) Read(b []byte) (int, error) {
	n, err := w.Conn.Read(b)
	if n > 0 {
		w.wait(false, n)
	}
	return n, err
}

func (w *limitedConn) Write(b []byte) (int, error) {
	w.wait(true, len(b))
	return w.Conn.Write(b)
}

func (w *limitedConn) Upstream() any {
	return w.Conn
}
