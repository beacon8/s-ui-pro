package service

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/admin8800/s-ui/database"
	"github.com/admin8800/s-ui/database/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestClientEditBulkPreservesServerManagedFields(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "clients.db")))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Tls{}, &model.Inbound{}, &model.Client{}); err != nil {
		t.Fatal(err)
	}

	original := model.Client{
		Enable:     true,
		Name:       "client-one",
		Config:     json.RawMessage(`{"vless":{"name":"client-one"}}`),
		Inbounds:   json.RawMessage(`[1]`),
		Links:      json.RawMessage(`[{"remark":"remote","type":"external","uri":"https://example.com/sub"}]`),
		Volume:     100,
		Expiry:     200,
		Down:       300,
		Up:         400,
		CreatedAt:  1_700_000_001,
		OnlineAt:   1_700_000_002,
		DelayStart: true,
		AutoReset:  true,
		ResetDays:  30,
		NextReset:  1_700_000_003,
		TotalUp:    500,
		TotalDown:  600,
		UpLimit:    10,
		DownLimit:  20,
		LimitUnit:  "mbps",
	}
	if err := db.Create(&original).Error; err != nil {
		t.Fatal(err)
	}

	payload, err := json.Marshal([]map[string]any{{
		"id":        original.Id,
		"enable":    false,
		"name":      original.Name,
		"inbounds":  []uint{2},
		"volume":    int64(101),
		"expiry":    int64(201),
		"down":      original.Down,
		"up":        original.Up,
		"upLimit":   int64(11),
		"downLimit": int64(21),
		"limitUnit": "kbps",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (&ClientService{}).Save(db, "editbulk", payload, "example.com"); err != nil {
		t.Fatal(err)
	}

	var saved model.Client
	if err := db.First(&saved, original.Id).Error; err != nil {
		t.Fatal(err)
	}
	if !jsonEqual(saved.Config, original.Config) || !jsonEqual(saved.Links, original.Links) {
		t.Fatalf("config or links were not preserved: config=%s links=%s", saved.Config, saved.Links)
	}
	if saved.CreatedAt != original.CreatedAt || saved.OnlineAt != original.OnlineAt ||
		saved.DelayStart != original.DelayStart || saved.AutoReset != original.AutoReset ||
		saved.ResetDays != original.ResetDays || saved.NextReset != original.NextReset ||
		saved.TotalUp != original.TotalUp || saved.TotalDown != original.TotalDown {
		t.Fatalf("server-managed fields were not preserved: %+v", saved)
	}
	if saved.Enable || saved.Volume != 101 || saved.Expiry != 201 ||
		!jsonEqual(saved.Inbounds, json.RawMessage(`[2]`)) ||
		saved.UpLimit != 11 || saved.DownLimit != 21 || saved.LimitUnit != "kbps" {
		t.Fatalf("bulk-editable fields were not updated: %+v", saved)
	}
}

func TestResetAllClientsTrafficRollsBackWhenAuditInsertFails(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "reset.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if sqlDB, err := database.GetDB().DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	client := model.Client{Name: "rollback-client", Enable: false, Up: 10, Down: 20, TotalUp: 30, TotalDown: 40}
	if err := database.GetDB().Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.GetDB().Exec(`CREATE TRIGGER reject_reset_audit
		BEFORE INSERT ON changes BEGIN SELECT RAISE(ABORT, 'reject reset audit'); END`).Error; err != nil {
		t.Fatal(err)
	}

	if err := (&ClientService{}).ResetAllClientsTraffic(); err == nil {
		t.Fatal("reset succeeded even though its audit record was rejected")
	}
	var saved model.Client
	if err := database.GetDB().First(&saved, client.Id).Error; err != nil {
		t.Fatal(err)
	}
	if saved.Enable != client.Enable || saved.Up != client.Up || saved.Down != client.Down ||
		saved.TotalUp != client.TotalUp || saved.TotalDown != client.TotalDown {
		t.Fatalf("client reset was only partially rolled back: got %+v want %+v", saved, client)
	}
}

func jsonEqual(left, right []byte) bool {
	var l, r any
	return json.Unmarshal(left, &l) == nil && json.Unmarshal(right, &r) == nil &&
		reflect.DeepEqual(l, r)
}
