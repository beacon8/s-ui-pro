package service

import (
	"encoding/json"
	"os"

	"github.com/admin8800/s-ui/database"
	"github.com/admin8800/s-ui/database/model"
	"github.com/admin8800/s-ui/util/common"

	"gorm.io/gorm"
)

type OutboundService struct{}

// hotSwap 记录热替换任务，用于事务提交后执行
type hotSwap struct {
	oldTag string
	config json.RawMessage
}

func (o *OutboundService) GetAll() (*[]map[string]interface{}, error) {
	db := database.GetDB()
	outbounds := []*model.Outbound{}
	err := db.Model(model.Outbound{}).Scan(&outbounds).Error
	if err != nil {
		return nil, err
	}
	var data []map[string]interface{}
	for _, outbound := range outbounds {
		outData := map[string]interface{}{
			"id":   outbound.Id,
			"type": outbound.Type,
			"tag":  outbound.Tag,
		}
		if outbound.Options != nil {
			var restFields map[string]json.RawMessage
			if err := json.Unmarshal(outbound.Options, &restFields); err != nil {
				return nil, err
			}
			for k, v := range restFields {
				outData[k] = v
			}
		}
		data = append(data, outData)
	}
	return &data, nil
}

func (o *OutboundService) GetAllConfig(db *gorm.DB) ([]json.RawMessage, error) {
	var outboundsJson []json.RawMessage
	var outbounds []*model.Outbound
	err := db.Model(model.Outbound{}).Scan(&outbounds).Error
	if err != nil {
		return nil, err
	}
	for _, outbound := range outbounds {
		outboundJson, err := outbound.MarshalJSON()
		if err != nil {
			return nil, err
		}
		outboundsJson = append(outboundsJson, outboundJson)
	}
	return outboundsJson, nil
}

func (s *OutboundService) Save(tx *gorm.DB, act string, data json.RawMessage) error {
	var err error

	switch act {
	case "newbulk":
		var outboundList []json.RawMessage
		err = json.Unmarshal(data, &outboundList)
		if err != nil {
			return err
		}
		for _, item := range outboundList {
			var outbound model.Outbound
			err = outbound.UnmarshalJSON(item)
			if err != nil {
				return err
			}
			if corePtr.IsRunning() {
				configData, err := outbound.MarshalJSON()
				if err != nil {
					return err
				}
				err = corePtr.AddOutbound(configData)
				if err != nil {
					return err
				}
			}
			err = tx.Save(&outbound).Error
			if err != nil {
				return err
			}
		}
	case "new", "edit":
		var outbound model.Outbound
		err = outbound.UnmarshalJSON(data)
		if err != nil {
			return err
		}

		if corePtr.IsRunning() {
			configData, err := outbound.MarshalJSON()
			if err != nil {
				return err
			}
			if act == "edit" {
				var oldTag string
				err = tx.Model(model.Outbound{}).Select("tag").Where("id = ?", outbound.Id).Find(&oldTag).Error
				if err != nil {
					return err
				}
				err = corePtr.RemoveOutbound(oldTag)
				if err != nil && err != os.ErrInvalid {
					return err
				}
			}
			err = corePtr.AddOutbound(configData)
			if err != nil {
				return err
			}
		}

		err = tx.Save(&outbound).Error
		if err != nil {
			return err
		}
	case "editbulk":
		var outboundList []json.RawMessage
		err = json.Unmarshal(data, &outboundList)
		if err != nil {
			return err
		}
		// 不使用传入的 tx 事务，改用独立连接逐条保存，避免长时间持锁导致 database is locked
		// （RemoveOutbound/AddOutbound 操作内核期间，StatsJob 等定时任务无法写入）
		db := database.GetDB()
		var hotSwaps []hotSwap
		for _, item := range outboundList {
			var outbound model.Outbound
			err = outbound.UnmarshalJSON(item)
			if err != nil {
				return err
			}
			if corePtr.IsRunning() {
				var oldTag string
				err = db.Model(model.Outbound{}).Select("tag").Where("id = ?", outbound.Id).Find(&oldTag).Error
				if err != nil {
					return err
				}
				configData, err := outbound.MarshalJSON()
				if err != nil {
					return err
				}
				hotSwaps = append(hotSwaps, hotSwap{oldTag: oldTag, config: configData})
			}
			err = db.Save(&outbound).Error
			if err != nil {
				return err
			}
		}
		// DB 写入完成后再统一热替换
		for _, hs := range hotSwaps {
			if e := corePtr.RemoveOutbound(hs.oldTag); e != nil && e != os.ErrInvalid {
				return e
			}
			if e := corePtr.AddOutbound(hs.config); e != nil {
				return e
			}
		}
	case "del":
		var tag string
		err = json.Unmarshal(data, &tag)
		if err != nil {
			return err
		}
		if corePtr.IsRunning() {
			err = corePtr.RemoveOutbound(tag)
			if err != nil && err != os.ErrInvalid {
				return err
			}
		}
		err = tx.Where("tag = ?", tag).Delete(model.Outbound{}).Error
		if err != nil {
			return err
		}
	default:
		return common.NewErrorf("unknown action: %s", act)
	}
	return nil
}
