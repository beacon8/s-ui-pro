package service

import (
	"encoding/json"
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
	startCoreMu         sync.Mutex
	startCoreInProgress bool
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
	if corePtr.IsRunning() {
		return nil
	}
	startCoreMu.Lock()
	if startCoreInProgress {
		startCoreMu.Unlock()
		return nil
	}
	if time.Since(lastStartFailTime) < startCooldown {
		logger.Info("start core cooldown ", startCooldown/time.Second, " seconds")
		startCoreMu.Unlock()
		return nil
	}
	startCoreInProgress = true
	startCoreMu.Unlock()
	defer func() {
		startCoreMu.Lock()
		startCoreInProgress = false
		startCoreMu.Unlock()
	}()

	logger.Info("starting core")
	rawConfig, err := s.GetConfig("")
	if err != nil {
		return err
	}
	err = corePtr.Start(*rawConfig)
	if err != nil {
		startCoreMu.Lock()
		lastStartFailTime = time.Now()
		startCoreMu.Unlock()
		logger.Error("start sing-box err:", err.Error())
		return err
	}
	s.loadClientLimits()
	logger.Info("sing-box started")
	return nil
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
	err := s.StopCore()
	if err != nil {
		return err
	}
	return s.StartCore()
}

func (s *ConfigService) restartCoreWithConfig(config json.RawMessage) error {
	startCoreMu.Lock()
	if startCoreInProgress {
		startCoreMu.Unlock()
		return nil
	}
	startCoreInProgress = true
	startCoreMu.Unlock()
	defer func() {
		startCoreMu.Lock()
		startCoreInProgress = false
		startCoreMu.Unlock()
	}()

	if corePtr.IsRunning() {
		if err := corePtr.Stop(); err != nil {
			logger.Error("restart sing-box err (stop):", err.Error())
			return err
		}
	}
	rawConfig, err := s.GetConfig(string(config))
	if err != nil {
		logger.Error("restart sing-box err (get config):", err.Error())
		return err
	}
	if err := corePtr.Start(*rawConfig); err != nil {
		logger.Error("restart sing-box err (start):", err.Error())
		return err
	}
	s.loadClientLimits()
	logger.Info("sing-box restarted with new config")
	return nil
}

func (s *ConfigService) StopCore() error {
	err := corePtr.Stop()
	if err != nil {
		return err
	}
	logger.Info("sing-box stopped")
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

func (s *ConfigService) Save(obj string, act string, data json.RawMessage, initUsers string, loginUser string, hostname string) ([]string, error) {
	var err error
	var objs []string = []string{obj}

	db := database.GetDB()
	tx := db.Begin()
	defer func() {
		if err == nil {
			tx.Commit()
			// Try to start core if it is not running
			if !corePtr.IsRunning() {
				s.StartCore()
			}
		} else {
			tx.Rollback()
		}
	}()

	switch obj {
	case "clients":
		var inboundIds []uint
		inboundIds, err = s.ClientService.Save(tx, act, data, hostname)
		if err == nil && len(inboundIds) > 0 {
			objs = append(objs, "inbounds")
			err = s.InboundService.RestartInbounds(tx, inboundIds)
			if err != nil {
				return nil, common.NewErrorf("failed to update users for inbounds: %v", err)
			}
		}
	case "tls":
		err = s.TlsService.Save(tx, act, data, hostname)
		objs = append(objs, "clients", "inbounds")
	case "inbounds":
		err = s.InboundService.Save(tx, act, data, initUsers, hostname)
		objs = append(objs, "clients")
	case "outbounds":
		err = s.OutboundService.Save(tx, act, data)
	case "services":
		err = s.ServicesService.Save(tx, act, data)
	case "endpoints":
		err = s.EndpointService.Save(tx, act, data)
	case "config":
		err = s.SettingService.SaveConfig(tx, data)
		if err != nil {
			return nil, err
		}
		configData := make(json.RawMessage, len(data))
		copy(configData, data)
		go func() { _ = s.restartCoreWithConfig(configData) }()
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

	LastUpdate = time.Now().Unix()

	return objs, nil
}

func (s *ConfigService) CheckChanges(lu string) (bool, error) {
	if lu == "" {
		return true, nil
	}
	if LastUpdate == 0 {
		db := database.GetDB()
		var count int64
		err := db.Model(model.Changes{}).Where("date_time > " + lu).Count(&count).Error
		if err == nil {
			LastUpdate = time.Now().Unix()
		}
		return count > 0, err
	} else {
		intLu, err := strconv.ParseInt(lu, 10, 64)
		return LastUpdate > intLu, err
	}
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
// writes the updated config and asynchronously restarts the core.
func (s *ConfigService) ImportRouteRules(data json.RawMessage, loginUser string) (*ImportRuleResult, error) {
	var payload importRulesPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, common.NewErrorf("invalid payload: %v", err)
	}
	if len(payload.Rules) == 0 && len(payload.RuleSet) == 0 && payload.Final == "" {
		return nil, common.NewError("nothing to import")
	}

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

	db := database.GetDB()
	tx := db.Begin()
	committed := false
	defer func() {
		if !committed {
			tx.Rollback()
		}
	}()

	if err = s.SettingService.SaveConfig(tx, newCfg); err != nil {
		return nil, err
	}
	if err = tx.Create(&model.Changes{
		DateTime: time.Now().Unix(),
		Actor:    loginUser,
		Key:      "config",
		Action:   "importRules",
		Obj:      data,
	}).Error; err != nil {
		return nil, err
	}
	if err = tx.Commit().Error; err != nil {
		return nil, err
	}
	committed = true

	LastUpdate = time.Now().Unix()

	cfgCopy := make(json.RawMessage, len(newCfg))
	copy(cfgCopy, newCfg)
	go func() { _ = s.restartCoreWithConfig(cfgCopy) }()
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
	whereString := "`id`>0"
	if len(actor) > 0 {
		whereString += " and `actor`='" + actor + "'"
	}
	if len(chngKey) > 0 {
		whereString += " and `key`='" + chngKey + "'"
	}
	db := database.GetDB()
	var chngs []model.Changes
	err := db.Model(model.Changes{}).Where(whereString).Order("`id` desc").Limit(c).Scan(&chngs).Error
	if err != nil {
		logger.Warning(err)
	}
	return chngs
}
