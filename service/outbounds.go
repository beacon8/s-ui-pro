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

// pendingHotSwaps 暂存待执行的热替换任务（事务提交后由 ConfigService.Save 执行）
var pendingHotSwaps []hotSwap

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
		// 事务内只做 DB 写入（tx.Save），不碰内核，避免长时间持锁
		// 热替换任务存到 pendingHotSwaps，由 ConfigService.Save 在事务提交后执行
		pendingHotSwaps = nil
		for _, item := range outboundList {
			var outbound model.Outbound
			err = outbound.UnmarshalJSON(item)
			if err != nil {
				return err
			}
			if corePtr.IsRunning() {
				var oldTag string
				err = tx.Model(model.Outbound{}).Select("tag").Where("id = ?", outbound.Id).Find(&oldTag).Error
				if err != nil {
					return err
				}
				configData, err := outbound.MarshalJSON()
				if err != nil {
					return err
				}
				pendingHotSwaps = append(pendingHotSwaps, hotSwap{oldTag: oldTag, config: configData})
			}
			err = tx.Save(&outbound).Error
			if err != nil {
				return err
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
