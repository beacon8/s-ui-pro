package service

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/admin8800/s-ui/core"
	"github.com/admin8800/s-ui/database"
	"github.com/admin8800/s-ui/database/model"
	"github.com/admin8800/s-ui/logger"
	"github.com/op/go-logging"
)

func TestConfigSaveRestoresPreviousConfigWhenCoreRejectsCandidate(t *testing.T) {
	logger.InitLogger(logging.ERROR)
	if err := database.InitDB(filepath.Join(t.TempDir(), "config.db")); err != nil {
		t.Fatal(err)
	}
	if _, err := (&SettingService{}).GetAllSetting(); err != nil {
		t.Fatal(err)
	}

	box := core.NewCore()
	configService := NewConfigService(box)
	if err := configService.StartCore(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = configService.StopCore()
		if sqlDB, err := database.GetDB().DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	previous, err := configService.SettingService.GetConfig()
	if err != nil {
		t.Fatal(err)
	}
	candidate := json.RawMessage(`{
  "log": {"level": "info"},
  "dns": {"servers": [], "rules": []},
  "route": {
    "rules": [],
    "rule_set": [
      {"type": "inline", "tag": "duplicate", "rules": [{"domain_suffix": ["a.example"]}]},
      {"type": "inline", "tag": "duplicate", "rules": [{"domain_suffix": ["b.example"]}]}
    ]
  },
  "experimental": {}
}`)
	fullCandidate, err := configService.GetConfig(string(candidate))
	if err != nil {
		t.Fatal(err)
	}
	if err := box.ValidateConfig(*fullCandidate); err != nil {
		t.Fatalf("candidate should pass option parsing: %v", err)
	}

	if _, err := configService.Save("config", "edit", candidate, "", "test", "localhost"); err == nil {
		t.Fatal("Save accepted a config that sing-box cannot start")
	}
	current, err := configService.SettingService.GetConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !jsonEqual([]byte(current), []byte(previous)) {
		t.Fatalf("saved config was not rolled back: %s", current)
	}
	if !box.IsRunning() {
		t.Fatal("previous sing-box config was not restored")
	}
}

func TestFallbackFullRestartPreservesPendingTraffic(t *testing.T) {
	logger.InitLogger(logging.ERROR)
	if err := database.InitDB(filepath.Join(t.TempDir(), "restart.db")); err != nil {
		t.Fatal(err)
	}
	if _, err := (&SettingService{}).GetAllSetting(); err != nil {
		t.Fatal(err)
	}

	box := core.NewCore()
	configService := NewConfigService(box)
	if err := configService.StartCore(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = configService.StopCore()
		if sqlDB, err := database.GetDB().DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	box.GetInstance().StatsTracker().RestoreStats([]model.Stats{{
		Resource: "user", Tag: "alice", Direction: true, Traffic: 123,
	}})

	coreLifecycleMu.Lock()
	err := configService.restartCoreLocked()
	coreLifecycleMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}

	stats := *box.GetInstance().StatsTracker().GetStats()
	for _, stat := range stats {
		if stat.Resource == "user" && stat.Tag == "alice" && stat.Direction && stat.Traffic == 123 {
			return
		}
	}
	t.Fatalf("pending traffic was lost across full restart: %+v", stats)
}

func TestClosedCoreKeepsFinalCountersForRestartHandoff(t *testing.T) {
	logger.InitLogger(logging.ERROR)
	if err := database.InitDB(filepath.Join(t.TempDir(), "close.db")); err != nil {
		t.Fatal(err)
	}
	if _, err := (&SettingService{}).GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	box := core.NewCore()
	configService := NewConfigService(box)
	if err := configService.StartCore(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = configService.StopCore()
		if sqlDB, err := database.GetDB().DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	tracker := box.GetInstance().StatsTracker()
	tracker.RestoreStats([]model.Stats{{Resource: "user", Tag: "late", Traffic: 77}})
	coreLifecycleMu.Lock()
	err := box.Stop()
	coreLifecycleMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	stats := *tracker.GetStats()
	if len(stats) != 1 || stats[0].Tag != "late" || stats[0].Traffic != 77 {
		t.Fatalf("closing core erased final counters: %+v", stats)
	}
}

func TestStopCoreFlushesFinalTraffic(t *testing.T) {
	logger.InitLogger(logging.ERROR)
	if err := database.InitDB(filepath.Join(t.TempDir(), "flush.db")); err != nil {
		t.Fatal(err)
	}
	if _, err := (&SettingService{}).GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	client := model.Client{Name: "flush-user", Enable: true}
	if err := database.GetDB().Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	box := core.NewCore()
	configService := NewConfigService(box)
	if err := configService.StartCore(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = configService.StopCore()
		if sqlDB, err := database.GetDB().DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	box.GetInstance().StatsTracker().RestoreStats([]model.Stats{{
		Resource: "user", Tag: client.Name, Direction: true, Traffic: 88,
	}})

	if err := configService.StopCore(); err != nil {
		t.Fatal(err)
	}
	var saved model.Client
	if err := database.GetDB().First(&saved, client.Id).Error; err != nil {
		t.Fatal(err)
	}
	if saved.Up != 88 {
		t.Fatalf("final traffic was not flushed before shutdown: up=%d", saved.Up)
	}
}

func TestRestoreStopDiscardsOldRuntimeTraffic(t *testing.T) {
	logger.InitLogger(logging.ERROR)
	if err := database.InitDB(filepath.Join(t.TempDir(), "restore-stop.db")); err != nil {
		t.Fatal(err)
	}
	if _, err := (&SettingService{}).GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	client := model.Client{Name: "restored-user", Enable: true, Up: 5}
	if err := database.GetDB().Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	box := core.NewCore()
	configService := NewConfigService(box)
	if err := configService.StartCore(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pendingRestartStats = nil
		_ = configService.StopCore()
		if sqlDB, err := database.GetDB().DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	box.GetInstance().StatsTracker().RestoreStats([]model.Stats{{
		Resource: "user", Tag: client.Name, Direction: true, Traffic: 99,
	}})
	pendingRestartStats = []model.Stats{{Resource: "user", Tag: client.Name, Traffic: 11}}

	if err := configService.StopCoreDiscardStats(); err != nil {
		t.Fatal(err)
	}
	var saved model.Client
	if err := database.GetDB().First(&saved, client.Id).Error; err != nil {
		t.Fatal(err)
	}
	if saved.Up != 5 || len(pendingRestartStats) != 0 {
		t.Fatalf("restore stop contaminated restored database or kept old pending stats: up=%d pending=%+v", saved.Up, pendingRestartStats)
	}
}

func TestFailedConfigApplyDoesNotConsumePendingRestartStats(t *testing.T) {
	logger.InitLogger(logging.ERROR)
	if err := database.InitDB(filepath.Join(t.TempDir(), "pending.db")); err != nil {
		t.Fatal(err)
	}
	if _, err := (&SettingService{}).GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	if err := database.GetDB().Exec(`CREATE TRIGGER reject_config_audit
		BEFORE INSERT ON changes BEGIN SELECT RAISE(ABORT, 'reject config audit'); END`).Error; err != nil {
		t.Fatal(err)
	}
	box := core.NewCore()
	configService := NewConfigService(box)
	pendingRestartStats = []model.Stats{{Resource: "user", Tag: "pending", Traffic: 55}}
	t.Cleanup(func() {
		pendingRestartStats = nil
		_ = configService.StopCore()
		if sqlDB, err := database.GetDB().DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	current, err := configService.SettingService.GetConfig()
	if err != nil {
		t.Fatal(err)
	}

	if _, err := configService.Save("config", "edit", json.RawMessage(current), "", "test", "localhost"); err == nil {
		t.Fatal("config apply succeeded despite rejected audit insert")
	}
	if len(pendingRestartStats) != 1 || pendingRestartStats[0].Tag != "pending" || pendingRestartStats[0].Traffic != 55 {
		t.Fatalf("failed config apply consumed pending counters: %+v", pendingRestartStats)
	}
}
