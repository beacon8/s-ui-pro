package service

import (
	"database/sql"
	"errors"
	"sort"
	"time"

	"github.com/admin8800/s-ui/core"
	"github.com/admin8800/s-ui/database"
	"github.com/admin8800/s-ui/database/model"
	"github.com/admin8800/s-ui/util/common"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type onlines struct {
	Inbound  []string `json:"inbound,omitempty"`
	User     []string `json:"user,omitempty"`
	Outbound []string `json:"outbound,omitempty"`
}

var onlineResources = &onlines{}

type StatsService struct {
}

func (s *StatsService) SaveStats(enableTraffic bool, bucketSeconds int64) error {
	coreLifecycleMu.Lock()
	defer coreLifecycleMu.Unlock()
	trafficAccountingMu.Lock()
	defer trafficAccountingMu.Unlock()
	return s.saveStatsLocked(enableTraffic, bucketSeconds)
}

func (s *StatsService) saveStatsLocked(enableTraffic bool, bucketSeconds int64) error {
	if corePtr == nil || !corePtr.IsRunning() {
		return nil
	}
	box := corePtr.GetInstance()
	if box == nil {
		return nil
	}
	st := box.StatsTracker()
	if st == nil {
		return nil
	}
	return s.saveTrackerStatsLocked(st, enableTraffic, bucketSeconds)
}

func (s *StatsService) saveTrackerStatsLocked(st *core.StatsTracker, enableTraffic bool, bucketSeconds int64) (resultErr error) {
	stats := st.GetStats()

	// Reset onlines
	onlineResources.Inbound = nil
	onlineResources.Outbound = nil
	onlineResources.User = nil

	if len(*stats) == 0 {
		return nil
	}

	var err error
	db := database.GetDB()
	tx := db.Begin()
	if tx.Error != nil {
		st.RestoreStats(*stats)
		return tx.Error
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		if rollbackErr := tx.Rollback().Error; rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			resultErr = common.NewErrorf("%v; rollback failed: %v", resultErr, rollbackErr)
		}
		st.RestoreStats(*stats)
	}()

	now := time.Now().Unix()

	// Aggregate per-resource so each active inbound/outbound/user is reported
	// online once (a tag may now appear in both directions), and each user's
	// up+down collapse into a single UPDATE.
	type traffic struct{ up, down int64 }
	userTraffic := map[string]*traffic{}
	seenInbound := map[string]bool{}
	seenOutbound := map[string]bool{}
	for _, stat := range *stats {
		switch stat.Resource {
		case "inbound":
			if !seenInbound[stat.Tag] {
				seenInbound[stat.Tag] = true
				onlineResources.Inbound = append(onlineResources.Inbound, stat.Tag)
			}
		case "outbound":
			if !seenOutbound[stat.Tag] {
				seenOutbound[stat.Tag] = true
				onlineResources.Outbound = append(onlineResources.Outbound, stat.Tag)
			}
		case "user":
			t, ok := userTraffic[stat.Tag]
			if !ok {
				t = &traffic{}
				userTraffic[stat.Tag] = t
				onlineResources.User = append(onlineResources.User, stat.Tag)
			}
			if stat.Direction {
				t.up += stat.Traffic
			} else {
				t.down += stat.Traffic
			}
		}
	}

	for name, t := range userTraffic {
		update := map[string]interface{}{"online_at": now}
		if t.up > 0 {
			update["up"] = gorm.Expr("up + ?", t.up)
		}
		if t.down > 0 {
			update["down"] = gorm.Expr("down + ?", t.down)
		}
		err = tx.Model(model.Client{}).Where("name = ?", name).Updates(update).Error
		if err != nil {
			return err
		}
	}

	if !enableTraffic {
		if err := tx.Commit().Error; err != nil {
			return err
		}
		committed = true
		return nil
	}

	// Round each sample down to its bucket and upsert, so all 10s cycles within
	// the same bucket accumulate into one row per (resource, tag, direction).
	if bucketSeconds < 1 {
		bucketSeconds = 1
	}
	bucket := now - (now % bucketSeconds)
	for i := range *stats {
		(*stats)[i].DateTime = bucket
	}
	err = tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "resource"}, {Name: "tag"}, {Name: "date_time"}, {Name: "direction"}},
		DoUpdates: clause.Assignments(map[string]interface{}{"traffic": gorm.Expr("stats.traffic + excluded.traffic")}),
	}).Create(&stats).Error
	if err != nil {
		return err
	}
	if err := tx.Commit().Error; err != nil {
		return err
	}
	committed = true
	return nil
}

func (s *StatsService) GetStats(resource string, tag string, limit int, start int64, end int64) (any, error) {
	var err error
	var result []model.Stats

	// Custom range when both start and end are provided, otherwise the last
	// `limit` hours up to now.
	var startTime, endTime int64
	if start > 0 && end > start {
		startTime, endTime = start, end
	} else {
		endTime = time.Now().Unix()
		startTime = endTime - (int64(limit) * 3600)
	}

	db := database.GetDB()
	resources := []string{resource}
	if resource == "endpoint" {
		resources = []string{"inbound", "outbound"}
	}
	err = db.Model(model.Stats{}).Where("resource in ? AND tag = ? AND date_time > ? AND date_time <= ?", resources, tag, startTime, endTime).Order("date_time ASC").Scan(&result).Error
	if err != nil {
		return nil, err
	}

	bucketSeconds, _ := (&SettingService{}).GetStatsBucketSeconds()
	if bucketSeconds < 1 {
		bucketSeconds = 1
	}
	numBuckets := 360
	if maxBuckets := (endTime - startTime) / bucketSeconds; maxBuckets < int64(numBuckets) {
		numBuckets = int(maxBuckets)
	}
	if numBuckets < 1 {
		numBuckets = 1
	}

	return s.downsampleStats(result, startTime, endTime, numBuckets), nil
}

func (s *StatsService) downsampleStats(stats []model.Stats, startTime, endTime int64, numBuckets int) any {
	result := make(map[int64][]int64)
	bucketSpan := (endTime - startTime) / int64(numBuckets)
	if bucketSpan == 0 {
		bucketSpan = 1
	}

	for _, r := range stats {
		bucket := (r.DateTime - startTime) / bucketSpan
		if bucket < 0 {
			bucket = 0
		}
		if bucket >= int64(numBuckets) {
			bucket = int64(numBuckets) - 1
		}
		if _, ok := result[bucket]; !ok {
			result[bucket] = []int64{0, 0}
		}
		if r.Direction {
			result[bucket][0] += r.Traffic
		} else {
			result[bucket][1] += r.Traffic
		}
	}

	return map[string]any{"stats": result, "startTime": startTime, "bucketSpan": bucketSpan, "numBuckets": numBuckets}
}

func (s *StatsService) GetOnlines() (onlines, error) {
	return *onlineResources, nil
}

// TopUser 流量排行单条记录
type TopUser struct {
	Name  string `json:"name"`
	Up    int64  `json:"up"`
	Down  int64  `json:"down"`
	Total int64  `json:"total"`
}

// GetTopUsers 按流量返回 Top N 客户端
//
//	period: total / 1h / 24h / 7d / 30d
//	direction: both / up / down（决定排序字段）
//	limit: 1..100，默认 10
//
// 不过滤 enable，停用客户端也参与排行。
func (s *StatsService) GetTopUsers(period string, limit int, direction string) ([]TopUser, error) {
	if limit <= 0 {
		limit = 10
	}
	if limit > 100 {
		limit = 100
	}

	sortBy := "total"
	switch direction {
	case "up":
		sortBy = "up"
	case "down":
		sortBy = "down"
	case "", "both":
		sortBy = "total"
	default:
		return nil, common.NewError("invalid direction: ", direction)
	}

	db := database.GetDB()

	// 累计：直接从 clients 表读
	if period == "" || period == "total" {
		var result []TopUser
		orderExpr := sortBy + " DESC"
		err := db.Model(&model.Client{}).
			Select("name, up, down, up+down AS total").
			Order(orderExpr).
			Limit(limit).
			Scan(&result).Error
		return result, err
	}

	// 时段：聚合 stats 表
	var since int64
	now := time.Now().Unix()
	switch period {
	case "1h":
		since = now - 3600
	case "24h":
		since = now - 86400
	case "7d":
		since = now - 7*86400
	case "30d":
		since = now - 30*86400
	default:
		return nil, common.NewError("invalid period: ", period)
	}

	type aggRow struct {
		Tag       string
		Direction bool
		Sum       int64
	}
	var rows []aggRow
	err := db.Model(&model.Stats{}).
		Select("tag, direction, SUM(traffic) AS sum").
		Where("resource = ? AND date_time > ?", "user", since).
		Group("tag").
		Group("direction").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	agg := make(map[string]*TopUser, len(rows))
	for _, r := range rows {
		u, ok := agg[r.Tag]
		if !ok {
			u = &TopUser{Name: r.Tag}
			agg[r.Tag] = u
		}
		if r.Direction {
			u.Up += r.Sum
		} else {
			u.Down += r.Sum
		}
	}

	result := make([]TopUser, 0, len(agg))
	for _, u := range agg {
		u.Total = u.Up + u.Down
		result = append(result, *u)
	}

	sort.Slice(result, func(i, j int) bool {
		switch sortBy {
		case "up":
			return result[i].Up > result[j].Up
		case "down":
			return result[i].Down > result[j].Down
		default:
			return result[i].Total > result[j].Total
		}
	})

	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}
func (s *StatsService) DelOldStats(days int) error {
	oldTime := time.Now().AddDate(0, 0, -(days)).Unix()
	db := database.GetDB()
	return db.Where("date_time < ?", oldTime).Delete(model.Stats{}).Error
}
