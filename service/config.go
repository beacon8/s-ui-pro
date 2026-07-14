package service

import (
	"encoding/json"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/admin8800/s-ui/core"
	"github.com/admin8800/s-ui/database"
	"github.com/admin8800/s-ui/database/model"
	"github.com/admin8800/s-ui/logger"
	"github.com/admin8800/s-ui/util/common"
)

var (
	LastUpdate          int64
	corePtr             *core.Core
	coreLifecycleMu     sync.Mutex
	trafficAccountingMu sync.Mutex
	// pendingRestartStats retains counters if a deplete-triggered full restart
	// cannot start a replacement core. It is only accessed while
	// coreLifecycleMu is held and is restored on the next successful start.
	pendingRestartStats []model.Stats
	lastStartFailTime   time.Time
	startCooldown       = 15 * time.Second
)

type ConfigService struct {
	ClientService
	TlsService
	SettingService
	InboundService
	OutboundService
	ServicesService
	EndpointService
}

type SingBoxConfig struct {
	Log          json.RawMessage   `json:"log"`
	Dns          json.RawMessage   `json:"dns"`
	Ntp          json.RawMessage   `json:"ntp"`
	Inbounds     []json.RawMessage `json:"inbounds"`
	Outbounds    []json.RawMessage `json:"outbounds"`
	Services     []json.RawMessage `json:"services"`
	Endpoints    []json.RawMessage `json:"endpoints"`
	Route        json.RawMessage   `json:"route"`
	Experimental json.RawMessage   `json:"experimental"`
}

func NewConfigService(core *core.Core) *ConfigService {
	corePtr = core
	return &ConfigService{}
}

func (s *ConfigService) GetConfig(data string) (*[]byte, error) {
	var err error
	if len(data) == 0 {
		data, err = s.SettingService.GetConfig()
		if err != nil {
			return nil, err
		}
	}
	singboxConfig := SingBoxConfig{}
	err = json.Unmarshal([]byte(data), &singboxConfig)
	if err != nil {
		return nil, err
	}

	singboxConfig.Inbounds, err = s.InboundService.GetAllConfig(database.GetDB())
	if err != nil {
		return nil, err
	}
	singboxConfig.Outbounds, err = s.OutboundService.GetAllConfig(database.GetDB())
	if err != nil {
		return nil, err
	}
	singboxConfig.Services, err = s.ServicesService.GetAllConfig(database.GetDB())
	if err != nil {
		return nil, err
	}
	singboxConfig.Endpoints, err = s.EndpointService.GetAllConfig(database.GetDB())
	if err != nil {
		return nil, err
	}
	rawConfig, err := json.MarshalIndent(singboxConfig, "", "  ")
	if err != nil {
		return nil, err
	}
	return &rawConfig, nil
}

func (s *ConfigService) StartCore() error {
	coreLifecycleMu.Lock()
	defer coreLifecycleMu.Unlock()
	return s.startCoreLocked()
}

func (s *ConfigService) startCoreLocked() error {
	if corePtr.IsRunning() {
		return nil
	}
	if time.Since(lastStartFailTime) < startCooldown {
		logger.Info("start core cooldown ", startCooldown/time.Second, " seconds")
		return common.NewError("sing-box start is cooling down after a failure")
	}

	logger.Info("starting core")
	rawConfig, err := s.GetConfig("")
	if err != nil {
		return err
	}
	err = s.startCoreWithRawConfigLocked(*rawConfig)
	if err != nil {
		return err
	}
	s.restorePendingRestartStatsLocked()
	logger.Info("sing-box started")
	return nil
}

func (s *ConfigService) startCoreWithRawConfigLocked(rawConfig []byte) error {
	if err := corePtr.Start(rawConfig); err != nil {
		lastStartFailTime = time.Now()
		logger.Error("start sing-box err:", err.Error())
		return err
	}
	lastStartFailTime = time.Time{}
	s.loadClientLimits()
	return nil
}

func (s *ConfigService) restorePendingRestartStatsLocked() {
	if len(pendingRestartStats) == 0 {
		return
	}
	if box := corePtr.GetInstance(); box != nil && box.StatsTracker() != nil {
		box.StatsTracker().RestoreStats(pendingRestartStats)
		pendingRestartStats = nil
	}
}

// loadClientLimits reloads all enabled clients' bandwidth limits into the
// freshly started core's limiter. Called after each successful core start.
func (s *ConfigService) loadClientLimits() {
	box := corePtr.GetInstance()
	if box == nil || box.LimiterTracker() == nil {
		return
	}
	var clients []model.Client
	err := database.GetDB().Model(model.Client{}).Where("enable = ?", true).
		Select("name, up_limit, down_limit, limit_unit").Find(&clients).Error
	if err != nil {
		logger.Warning("load client limits err:", err.Error())
		return
	}
	limits := make(map[string][2]int64, len(clients))
	for _, c := range clients {
		up := toBytesPerSec(c.UpLimit, c.LimitUnit)
		down := toBytesPerSec(c.DownLimit, c.LimitUnit)
		if up == 0 && down == 0 {
			continue
		}
		limits[c.Name] = [2]int64{up, down}
	}
	box.LimiterTracker().BulkLoad(limits)
}

func (s *ConfigService) RestartCore() error {
	coreLifecycleMu.Lock()
	defer coreLifecycleMu.Unlock()
	return s.restartCoreLocked()
}

func (s *ConfigService) restartCoreLocked() error {
	rawConfig, err := s.GetConfig("")
	if err != nil {
		return err
	}
	if err := s.restartCoreWithRawConfigLocked(*rawConfig); err != nil {
		return err
	}
	s.restorePendingRestartStatsLocked()
	return nil
}

func (s *ConfigService) restartCoreWithRawConfigLocked(rawConfig []byte) error {
	if err := corePtr.ValidateConfig(rawConfig); err != nil {
		return err
	}
	var oldTracker *core.StatsTracker
	var saved []model.Stats
	if corePtr.IsRunning() {
		if box := corePtr.GetInstance(); box != nil {
			oldTracker = box.StatsTracker()
			if oldTracker != nil {
				saved = append(saved, (*oldTracker.GetStats())...)
			}
		}
	}
	if corePtr.IsRunning() {
		if err := corePtr.Stop(); err != nil {
			if oldTracker != nil {
				saved = append(saved, (*oldTracker.GetStats())...)
			}
			pendingRestartStats = append(pendingRestartStats, saved...)
			logger.Error("restart sing-box err (stop):", err.Error())
			return err
		}
	}
	if oldTracker != nil {
		saved = append(saved, (*oldTracker.GetStats())...)
	}
	if err := s.startCoreWithRawConfigLocked(rawConfig); err != nil {
		pendingRestartStats = append(pendingRestartStats, saved...)
		logger.Error("restart sing-box err (start):", err.Error())
		return err
	}
	if len(saved) > 0 {
		if box := corePtr.GetInstance(); box != nil && box.StatsTracker() != nil {
			box.StatsTracker().RestoreStats(saved)
		} else {
			pendingRestartStats = append(pendingRestartStats, saved...)
		}
	}
	logger.Info("sing-box restarted with new config")
	return nil
}

func (s *ConfigService) StopCore() error {
	coreLifecycleMu.Lock()
	defer coreLifecycleMu.Unlock()
	trafficAccountingMu.Lock()
	defer trafficAccountingMu.Unlock()

	trafficAge, err := s.SettingService.GetTrafficAge()
	if err != nil {
		return common.NewErrorf("read traffic retention before core stop: %v", err)
	}
	bucketSeconds, err := s.SettingService.GetStatsBucketSeconds()
	if err != nil {
		return common.NewErrorf("read stats bucket before core stop: %v", err)
	}
	statsService := &StatsService{}
	if err := statsService.saveStatsLocked(trafficAge > 0, bucketSeconds); err != nil {
		return common.NewErrorf("flush stats before core stop: %v", err)
	}
	stopErr := s.stopCoreLocked()
	flushErr := s.flushPendingRestartStatsLocked(statsService, trafficAge > 0, bucketSeconds)
	if stopErr != nil && flushErr != nil {
		return common.NewErrorf("stop core: %v; flush closing stats: %v", stopErr, flushErr)
	}
	if stopErr != nil {
		return stopErr
	}
	if flushErr != nil {
		return common.NewErrorf("flush closing stats: %v", flushErr)
	}
	return nil
}

// StopCoreDiscardStats is reserved for database restore. At that point the
// global DB is already the restored snapshot, so writing counters owned by the
// old runtime would corrupt the exact restore result.
func (s *ConfigService) StopCoreDiscardStats() error {
	coreLifecycleMu.Lock()
	defer coreLifecycleMu.Unlock()
	trafficAccountingMu.Lock()
	defer trafficAccountingMu.Unlock()

	pendingRestartStats = nil
	err := corePtr.Stop()
	pendingRestartStats = nil
	if err != nil {
		return err
	}
	logger.Info("sing-box stopped after database restore")
	return nil
}

func (s *ConfigService) stopCoreLocked() error {
	var tracker *core.StatsTracker
	var saved []model.Stats
	if corePtr != nil {
		if box := corePtr.GetInstance(); box != nil {
			tracker = box.StatsTracker()
			if tracker != nil {
				saved = append(saved, (*tracker.GetStats())...)
			}
		}
	}
	err := corePtr.Stop()
	if tracker != nil {
		saved = append(saved, (*tracker.GetStats())...)
	}
	pendingRestartStats = append(pendingRestartStats, saved...)
	if err != nil {
		return err
	}
	logger.Info("sing-box stopped")
	return nil
}

func (s *ConfigService) flushPendingRestartStatsLocked(statsService *StatsService, enableTraffic bool, bucketSeconds int64) error {
	if len(pendingRestartStats) == 0 {
		return nil
	}
	pending := pendingRestartStats
	pendingRestartStats = nil
	tracker := core.NewStatsTracker()
	tracker.RestoreStats(pending)
	if err := statsService.saveTrackerStatsLocked(tracker, enableTraffic, bucketSeconds); err != nil {
		pendingRestartStats = append(pendingRestartStats, pending...)
		return err
	}
	return nil
}

func (s *ConfigService) CheckOutbound(tag string, link string) core.CheckOutboundResult {
	if tag == "" {
		return core.CheckOutboundResult{Error: "missing query parameter: tag"}
	}
	if corePtr == nil || !corePtr.IsRunning() {
		return core.CheckOutboundResult{Error: "core not running"}
	}
	return core.CheckOutbound(corePtr.GetCtx(), tag, link)
}

func (s *ConfigService) DepleteClients() error {
	coreLifecycleMu.Lock()
	defer coreLifecycleMu.Unlock()

	inboundIds, err := s.ClientService.DepleteClients()
	if err != nil || len(inboundIds) == 0 {
		return err
	}
	if err := s.InboundService.RestartInbounds(database.GetDB(), inboundIds); err != nil {
		if restartErr := s.restartCoreLocked(); restartErr != nil {
			return common.NewErrorf("restart depleted inbounds: %v; full core restart also failed: %v", err, restartErr)
		}
		logger.Warning("partial inbound restart failed; full core restart restored database state: ", err)
	}
	return nil
}

// ResetTraffic keeps the in-memory counters, database reset and core restart
// in one critical section so pre-reset traffic cannot be written back later.
func (s *ConfigService) ResetTraffic() error {
	coreLifecycleMu.Lock()
	defer coreLifecycleMu.Unlock()
	trafficAccountingMu.Lock()
	defer trafficAccountingMu.Unlock()

	if err := s.ClientService.resetAllClientsTrafficLocked(); err != nil {
		return err
	}
	// A global reset intentionally establishes a new user-accounting boundary.
	// Retain endpoint history captured by an earlier failed core restart.
	pendingKept := pendingRestartStats[:0]
	for _, stat := range pendingRestartStats {
		if stat.Resource != "user" {
			pendingKept = append(pendingKept, stat)
		}
	}
	pendingRestartStats = pendingKept
	// Drop only pre-reset user counters. Keep endpoint counters so a failed
	// restart cannot erase unrelated traffic history; bytes arriving after this
	// point belong to the new reset period and remain countable.
	if corePtr != nil {
		if box := corePtr.GetInstance(); box != nil && box.StatsTracker() != nil {
			stats := *box.StatsTracker().GetStats()
			kept := stats[:0]
			for _, stat := range stats {
				if stat.Resource != "user" {
					kept = append(kept, stat)
				}
			}
			box.StatsTracker().RestoreStats(kept)
		}
	}
	return s.restartCoreLocked()
}

func (s *ConfigService) applyConfigLocked(configData, changeData json.RawMessage, loginUser, action string) error {
	oldConfig, err := s.SettingService.GetConfig()
	if err != nil {
		return err
	}
	oldRawConfig, err := s.GetConfig(oldConfig)
	if err != nil {
		return err
	}
	candidateRawConfig, err := s.GetConfig(string(configData))
	if err != nil {
		return err
	}
	if err := corePtr.ValidateConfig(*candidateRawConfig); err != nil {
		return common.NewErrorf("invalid sing-box config: %v", err)
	}

	wasRunning := corePtr.IsRunning()
	restoreRuntime := func(cause error) error {
		var restoreErr error
		if wasRunning {
			restoreErr = s.restartCoreWithRawConfigLocked(*oldRawConfig)
		} else {
			restoreErr = s.stopCoreLocked()
		}
		if restoreErr != nil {
			return common.NewErrorf("%v; failed to restore previous runtime: %v", cause, restoreErr)
		}
		s.restorePendingRestartStatsLocked()
		return common.NewErrorf("%v; previous config restored", cause)
	}

	if err := s.restartCoreWithRawConfigLocked(*candidateRawConfig); err != nil {
		return restoreRuntime(common.NewErrorf("new config failed: %v", err))
	}

	db := database.GetDB()
	tx := db.Begin()
	if tx.Error != nil {
		return restoreRuntime(common.NewErrorf("begin config transaction: %v", tx.Error))
	}
	rollback := func(cause error) error {
		if rollbackErr := tx.Rollback().Error; rollbackErr != nil {
			cause = common.NewErrorf("%v; database rollback failed: %v", cause, rollbackErr)
		}
		return restoreRuntime(cause)
	}

	if err := s.SettingService.SaveConfig(tx, configData); err != nil {
		return rollback(err)
	}
	if err := tx.Create(&model.Changes{
		DateTime: time.Now().Unix(),
		Actor:    loginUser,
		Key:      "config",
		Action:   action,
		Obj:      changeData,
	}).Error; err != nil {
		return rollback(err)
	}
	if err := tx.Commit().Error; err != nil {
		return rollback(err)
	}

	s.restorePendingRestartStatsLocked()
	LastUpdate = time.Now().Unix()
	return nil
}

func (s *ConfigService) Save(obj string, act string, data json.RawMessage, initUsers string, loginUser string, hostname string) (objs []string, err error) {
	coreLifecycleMu.Lock()
	defer coreLifecycleMu.Unlock()

	if obj == "config" {
		if err := s.applyConfigLocked(data, data, loginUser, act); err != nil {
			return nil, err
		}
		return []string{obj}, nil
	}

	objs = []string{obj}

	db := database.GetDB()
	tx := db.Begin()
	if tx.Error != nil {
		return nil, tx.Error
	}
	runtimeWasRunning := corePtr != nil && corePtr.IsRunning()
	runtimeTouched := false
	restartAfterCommit := false
	var pendingInboundIds []uint
	restoreRuntimeAfterRollback := func(cause error) error {
		if !runtimeWasRunning || !runtimeTouched {
			return cause
		}
		if restartErr := s.restartCoreLocked(); restartErr != nil {
			return common.NewErrorf("%v; database rolled back but runtime restore failed: %v", cause, restartErr)
		}
		return cause
	}
	defer func() {
		if err != nil {
			if rollbackErr := tx.Rollback().Error; rollbackErr != nil {
				err = common.NewErrorf("%v; database rollback failed: %v", err, rollbackErr)
			}
			pendingHotSwaps = nil
			err = restoreRuntimeAfterRollback(err)
			return
		}
		if commitErr := tx.Commit().Error; commitErr != nil {
			_ = tx.Rollback().Error
			pendingHotSwaps = nil
			err = restoreRuntimeAfterRollback(commitErr)
			return
		}
		LastUpdate = time.Now().Unix()

		// 事务提交后执行出站热替换（editbulk 收集的任务）
		if len(pendingHotSwaps) > 0 {
			var hotSwapErr error
			for _, hs := range pendingHotSwaps {
				if e := corePtr.RemoveOutbound(hs.oldTag); e != nil && e != os.ErrInvalid {
					hotSwapErr = common.NewErrorf("remove outbound %s: %v", hs.oldTag, e)
					break
				}
				if e := corePtr.AddOutbound(hs.config); e != nil {
					hotSwapErr = common.NewErrorf("add outbound: %v", e)
					break
				}
			}
			pendingHotSwaps = nil
			if hotSwapErr != nil {
				if restartErr := s.restartCoreLocked(); restartErr != nil {
					err = common.NewErrorf("database committed, but outbound reload failed: %v; full core restart also failed: %v", hotSwapErr, restartErr)
					return
				}
				logger.Warning("outbound reload failed; full core restart restored committed state: ", hotSwapErr)
			}
		}
		if len(pendingInboundIds) > 0 {
			if restartErr := s.InboundService.RestartInbounds(database.GetDB(), pendingInboundIds); restartErr != nil {
				if fullRestartErr := s.restartCoreLocked(); fullRestartErr != nil {
					err = common.NewErrorf("database committed, but inbound reload failed: %v; full core restart also failed: %v", restartErr, fullRestartErr)
					return
				}
				logger.Warning("partial inbound reload failed; full core restart restored committed state: ", restartErr)
			}
		}
		if restartAfterCommit {
			if restartErr := s.restartCoreLocked(); restartErr != nil {
				err = common.NewErrorf("database committed, but core restart failed: %v", restartErr)
				return
			}
		}
		// Try to start core if it is not running
		if !corePtr.IsRunning() {
			_ = s.startCoreLocked()
		}
	}()

	switch obj {
	case "clients":
		runtimeTouched = runtimeWasRunning
		var inboundIds []uint
		inboundIds, err = s.ClientService.Save(tx, act, data, hostname)
		if err == nil && len(inboundIds) > 0 {
			objs = append(objs, "inbounds")
			pendingInboundIds = inboundIds
		}
	case "tls":
		restartAfterCommit = runtimeWasRunning && act == "edit"
		err = s.TlsService.Save(tx, act, data, hostname)
		objs = append(objs, "clients", "inbounds")
	case "inbounds":
		runtimeTouched = runtimeWasRunning
		err = s.InboundService.Save(tx, act, data, initUsers, hostname)
		objs = append(objs, "clients")
	case "outbounds":
		runtimeTouched = runtimeWasRunning && act != "editbulk"
		err = s.OutboundService.Save(tx, act, data)
	case "services":
		runtimeTouched = runtimeWasRunning
		err = s.ServicesService.Save(tx, act, data)
	case "endpoints":
		runtimeTouched = runtimeWasRunning
		err = s.EndpointService.Save(tx, act, data)
	case "settings":
		err = s.SettingService.Save(tx, data)
	default:
		return nil, common.NewError("unknown object: ", obj)
	}
	if err != nil {
		return nil, err
	}

	dt := time.Now().Unix()
	err = tx.Create(&model.Changes{
		DateTime: dt,
		Actor:    loginUser,
		Key:      obj,
		Action:   act,
		Obj:      data,
	}).Error
	if err != nil {
		return nil, err
	}

	return objs, nil
}

func (s *ConfigService) CheckChanges(lu string) (bool, error) {
	if lu == "" {
		return true, nil
	}
	intLu, err := strconv.ParseInt(lu, 10, 64)
	if err != nil {
		return false, err
	}
	if LastUpdate == 0 {
		db := database.GetDB()
		var count int64
		err := db.Model(model.Changes{}).Where("date_time > ?", intLu).Count(&count).Error
		if err == nil {
			LastUpdate = time.Now().Unix()
		}
		return count > 0, err
	}
	return LastUpdate > intLu, nil
}

// ImportRuleResult is the return value of ImportRouteRules.
type ImportRuleResult struct {
	Added         int                   `json:"added"`
	Skipped       int                   `json:"skipped"`
	SkippedRules  []ImportSkippedDetail `json:"skippedRules,omitempty"`
	AddedRulesets int                   `json:"addedRulesets"`
	TotalRules    int                   `json:"totalRules"`
	Restarted     bool                  `json:"restarted"`
}

type ImportSkippedDetail struct {
	Index            int      `json:"index"`
	ConflictUsers    []string `json:"conflictUsers"`
	ExistingOutbound []string `json:"existingOutbound,omitempty"`
}

type importRulesPayload struct {
	Rules   []map[string]interface{} `json:"rules"`
	RuleSet []map[string]interface{} `json:"rule_set,omitempty"`
	Final   string                   `json:"final,omitempty"`
}

// ImportRouteRules batch-appends route rules with conflict-skip semantics, then
// safely applies the updated config.
func (s *ConfigService) ImportRouteRules(data json.RawMessage, loginUser string) (*ImportRuleResult, error) {
	var payload importRulesPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, common.NewErrorf("invalid payload: %v", err)
	}
	if len(payload.Rules) == 0 && len(payload.RuleSet) == 0 && payload.Final == "" {
		return nil, common.NewError("nothing to import")
	}

	coreLifecycleMu.Lock()
	defer coreLifecycleMu.Unlock()

	cfgStr, err := s.SettingService.GetConfig()
	if err != nil {
		return nil, err
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal([]byte(cfgStr), &cfg); err != nil {
		return nil, common.NewErrorf("invalid stored config: %v", err)
	}

	route, _ := cfg["route"].(map[string]interface{})
	if route == nil {
		route = map[string]interface{}{}
		cfg["route"] = route
	}

	existingRules := castMapSlice(route["rules"])
	existingSets := castMapSlice(route["rule_set"])

	occupied := collectOccupiedUsers(existingRules)

	result := &ImportRuleResult{}

	for i, r := range payload.Rules {
		incoming := collectRuleUsers(r)
		if len(incoming) == 0 {
			existingRules = append(existingRules, r)
			result.Added++
			continue
		}

		var conflicts []string
		outboundSet := map[string]struct{}{}
		for u := range incoming {
			if outbound, ok := occupied[u]; ok {
				conflicts = append(conflicts, u)
				if outbound != "" {
					outboundSet[outbound] = struct{}{}
				}
			}
		}
		if len(conflicts) > 0 {
			existingOuts := make([]string, 0, len(outboundSet))
			for o := range outboundSet {
				existingOuts = append(existingOuts, o)
			}
			result.Skipped++
			result.SkippedRules = append(result.SkippedRules, ImportSkippedDetail{
				Index:            i,
				ConflictUsers:    conflicts,
				ExistingOutbound: existingOuts,
			})
			continue
		}

		ob, _ := r["outbound"].(string)
		for u := range incoming {
			occupied[u] = ob
		}
		existingRules = append(existingRules, r)
		result.Added++
	}
	route["rules"] = existingRules

	if len(payload.RuleSet) > 0 {
		tagSet := map[string]bool{}
		for _, rs := range existingSets {
			if t, ok := rs["tag"].(string); ok {
				tagSet[t] = true
			}
		}
		for _, rs := range payload.RuleSet {
			tag, _ := rs["tag"].(string)
			if tag == "" || tagSet[tag] {
				continue
			}
			existingSets = append(existingSets, rs)
			tagSet[tag] = true
			result.AddedRulesets++
		}
		route["rule_set"] = existingSets
	}

	if payload.Final != "" {
		route["final"] = payload.Final
	}

	result.TotalRules = len(existingRules)

	// No substantive change — skip write and restart.
	if result.Added == 0 && result.AddedRulesets == 0 && payload.Final == "" {
		return result, nil
	}

	newCfg, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, err
	}

	if err = s.applyConfigLocked(newCfg, data, loginUser, "importRules"); err != nil {
		return nil, err
	}
	result.Restarted = true

	return result, nil
}

func castMapSlice(v interface{}) []map[string]interface{} {
	if v == nil {
		return nil
	}
	arr, ok := v.([]interface{})
	if !ok {
		return nil
	}
	out := make([]map[string]interface{}, 0, len(arr))
	for _, x := range arr {
		if m, ok := x.(map[string]interface{}); ok {
			out = append(out, m)
		}
	}
	return out
}

// collectOccupiedUsers collects user/auth_user from rules whose action is
// absent (defaults to "route") or explicitly "route".
// Returns map[username] = first occupying outbound tag.
func collectOccupiedUsers(rules []map[string]interface{}) map[string]string {
	occupied := map[string]string{}
	for _, r := range rules {
		if act, ok := r["action"].(string); ok && act != "" && act != "route" {
			continue
		}
		ob, _ := r["outbound"].(string)
		for u := range collectRuleUsers(r) {
			if _, exists := occupied[u]; !exists {
				occupied[u] = ob
			}
		}
	}
	return occupied
}

// collectRuleUsers recursively collects all user/auth_user values from a rule,
// including logical sub-rules.
func collectRuleUsers(r map[string]interface{}) map[string]struct{} {
	out := map[string]struct{}{}
	pickArr := func(key string) {
		arr, ok := r[key].([]interface{})
		if !ok {
			return
		}
		for _, v := range arr {
			if s, ok := v.(string); ok && s != "" {
				out[s] = struct{}{}
			}
		}
	}
	pickArr("user")
	pickArr("auth_user")
	if t, _ := r["type"].(string); t == "logical" {
		if subs, ok := r["rules"].([]interface{}); ok {
			for _, sub := range subs {
				if m, ok := sub.(map[string]interface{}); ok {
					for u := range collectRuleUsers(m) {
						out[u] = struct{}{}
					}
				}
			}
		}
	}
	return out
}

func (s *ConfigService) GetChanges(actor string, chngKey string, count string) []model.Changes {
	c, _ := strconv.Atoi(count)
	db := database.GetDB()
	tx := db.Model(model.Changes{}).Where("`id` > 0")
	if len(actor) > 0 {
		tx = tx.Where("`actor` = ?", actor)
	}
	if len(chngKey) > 0 {
		tx = tx.Where("`key` = ?", chngKey)
	}
	var chngs []model.Changes
	err := tx.Order("`id` desc").Limit(c).Scan(&chngs).Error
	if err != nil {
		logger.Warning(err)
	}
	return chngs
}
