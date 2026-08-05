package service

import (
	"sort"
	"time"

	"github.com/admin8800/s-ui/database"
	"github.com/admin8800/s-ui/database/model"
	"github.com/admin8800/s-ui/util/common"

	"gorm.io/gorm"
)

type onlines struct {
	Inbound  []string `json:"inbound,omitempty"`
	User     []string `json:"user,omitempty"`
	Outbound []string `json:"outbound,omitempty"`
}

var onlineResources = &onlines{}

type StatsService struct {
}

type ClientRate struct {
	Id                      uint   `json:"id"`
	Up                      int64  `json:"up"`
	Down                    int64  `json:"down"`
	DynamicState            string `json:"dynamicState"`
	DynamicObservedSeconds  int64  `json:"dynamicObservedSeconds"`
	DynamicRemainingSeconds int64  `json:"dynamicRemainingSeconds"`
}

type ClientRates struct {
	At    int64        `json:"at"`
	Rates []ClientRate `json:"rates"`
}

func (s *StatsService) SaveStats(enableTraffic bool) error {
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
	defer func() {
		if err == nil {
			tx.Commit()
		} else {
			tx.Rollback()
		}
	}()

	for _, stat := range *stats {
		if stat.Resource == "user" {
			if stat.Direction {
				err = tx.Model(model.Client{}).Where("name = ?", stat.Tag).
					UpdateColumn("up", gorm.Expr("up + ?", stat.Traffic)).Error
			} else {
				err = tx.Model(model.Client{}).Where("name = ?", stat.Tag).
					UpdateColumn("down", gorm.Expr("down + ?", stat.Traffic)).Error
			}
			if err != nil {
				return err
			}
		}
		if stat.Direction {
			switch stat.Resource {
			case "inbound":
				onlineResources.Inbound = append(onlineResources.Inbound, stat.Tag)
			case "outbound":
				onlineResources.Outbound = append(onlineResources.Outbound, stat.Tag)
			case "user":
				onlineResources.User = append(onlineResources.User, stat.Tag)
			}
		}
	}

	if !enableTraffic {
		return nil
	}
	return tx.Create(&stats).Error
}

func (s *StatsService) GetStats(resource string, tag string, limit int) ([]model.Stats, error) {
	var err error
	var result []model.Stats

	currentTime := time.Now().Unix()
	timeDiff := currentTime - (int64(limit) * 3600)

	db := database.GetDB()
	resources := []string{resource}
	if resource == "endpoint" {
		resources = []string{"inbound", "outbound"}
	}
	err = db.Model(model.Stats{}).Where("resource in ? AND tag = ? AND date_time > ?", resources, tag, timeDiff).Scan(&result).Error
	if err != nil {
		return nil, err
	}

	result = s.downsampleStats(result, 60) // 60 rows for 30 buckets
	return result, nil
}

// downsampleStats reduces stats to maxRows rows.
// Each bucket outputs two rows (direction false and true) with average Traffic.
func (s *StatsService) downsampleStats(stats []model.Stats, maxRows int) []model.Stats {
	if len(stats) <= maxRows {
		return stats
	}
	numBuckets := int(maxRows / 2)
	sort.Slice(stats, func(i, j int) bool { return stats[i].DateTime < stats[j].DateTime })
	timeMin, timeMax := stats[0].DateTime, stats[len(stats)-1].DateTime
	bucketSpan := (timeMax - timeMin) / int64(numBuckets)
	if bucketSpan == 0 {
		bucketSpan = 1
	}
	downsampled := make([]model.Stats, 0, maxRows)
	for i := 0; i < numBuckets; i++ {
		bucketStart := timeMin + int64(i)*bucketSpan
		bucketEnd := timeMin + int64(i+1)*bucketSpan
		if i == numBuckets-1 {
			bucketEnd = timeMax + 1
		}
		for _, dir := range []bool{false, true} {
			var sum int64
			var count int
			for _, r := range stats {
				if r.DateTime >= bucketStart && r.DateTime < bucketEnd && r.Direction == dir {
					sum += r.Traffic
					count++
				}
			}
			avg := int64(0)
			if count > 0 {
				avg = sum / int64(count)
			}
			downsampled = append(downsampled, model.Stats{
				DateTime:  bucketStart,
				Resource:  stats[0].Resource,
				Tag:       stats[0].Tag,
				Direction: dir,
				Traffic:   avg,
			})
		}
	}
	return downsampled
}

func (s *StatsService) GetOnlines() (onlines, error) {
	return *onlineResources, nil
}

func (s *StatsService) GetClientRates(ids []uint) (*ClientRates, error) {
	result := &ClientRates{At: time.Now().UnixMilli(), Rates: []ClientRate{}}
	if len(ids) == 0 || corePtr == nil || !corePtr.IsRunning() {
		return result, nil
	}
	box := corePtr.GetInstance()
	if box == nil || box.StatsTracker() == nil {
		return result, nil
	}

	type clientName struct {
		Id   uint
		Name string
	}
	var clients []clientName
	err := database.GetDB().Model(model.Client{}).Select("id", "name").Where("id IN ?", ids).Find(&clients).Error
	if err != nil {
		return nil, err
	}

	names := make([]string, 0, len(clients))
	for _, client := range clients {
		names = append(names, client.Name)
	}
	trafficByName := make(map[string][2]int64, len(clients))
	for _, traffic := range box.StatsTracker().UserTrafficSnapshot(names) {
		trafficByName[traffic.Name] = [2]int64{traffic.Up, traffic.Down}
	}
	now := time.Now()
	for _, client := range clients {
		traffic := trafficByName[client.Name]
		status := box.LimiterTracker().DynamicStatus(client.Name, now)
		result.Rates = append(result.Rates, ClientRate{
			Id: client.Id, Up: traffic[0], Down: traffic[1],
			DynamicState:            status.State,
			DynamicObservedSeconds:  status.ObservedSeconds,
			DynamicRemainingSeconds: status.RemainingSeconds,
		})
	}
	return result, nil
}

func (s *StatsService) SampleDynamicLimits(now time.Time) {
	if corePtr == nil || !corePtr.IsRunning() {
		return
	}
	box := corePtr.GetInstance()
	if box == nil || box.StatsTracker() == nil || box.LimiterTracker() == nil {
		return
	}
	users := box.LimiterTracker().DynamicUsers()
	trafficByName := make(map[string]int64, len(users))
	for _, traffic := range box.StatsTracker().UserTrafficSnapshot(users) {
		trafficByName[traffic.Name] = traffic.Down
	}
	for _, user := range users {
		box.LimiterTracker().ObserveDownload(user, trafficByName[user], now)
	}
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
