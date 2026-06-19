package sub

import (
	"encoding/json"
	"fmt"

	"github.com/admin8800/s-ui/database"
	"github.com/admin8800/s-ui/database/model"
	"github.com/admin8800/s-ui/service"
	"github.com/admin8800/s-ui/util"
)

type BatchJsonService struct {
	service.SettingService
	service.ClientService
	JsonService // 复用 getOutbounds / addDefaultOutbounds / addOthers / pushMixed
	LinkService
}

// GetBatchJson 渲染聚合 sing-box JSON 订阅。
// clients 由调用方先用 ClientService.SearchClients 过滤好。
func (b *BatchJsonService) GetBatchJson(clients []*model.Client, title string) (*string, []string, error) {
	if len(clients) == 0 {
		return nil, nil, nil // 调用方负责翻译为 404
	}

	// 一次性预加载所有 client 引用到的 inbound（避免 N+1）
	inboundMap, err := b.preloadInbounds(clients)
	if err != nil {
		return nil, nil, err
	}

	var mergedOutbounds []map[string]interface{}
	var mergedTags []string

	for _, c := range clients {
		outs, tags, err := b.renderClient(c, inboundMap)
		if err != nil {
			return nil, nil, err
		}
		mergedOutbounds = append(mergedOutbounds, outs...)
		mergedTags = append(mergedTags, tags...)
	}

	// 复用默认 selector/urltest/direct + 模板
	b.JsonService.addDefaultOutbounds(&mergedOutbounds, &mergedTags)

	var jsonConfig map[string]interface{}
	if err := json.Unmarshal([]byte(defaultJson), &jsonConfig); err != nil {
		return nil, nil, err
	}
	jsonConfig["outbounds"] = mergedOutbounds
	if err := b.JsonService.addOthers(&jsonConfig); err != nil {
		return nil, nil, err
	}

	result, err := json.MarshalIndent(jsonConfig, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	resultStr := string(result)

	updateInterval, _ := b.SettingService.GetSubUpdates()
	headers := util.GetBatchHeaders(clients, updateInterval, title)

	return &resultStr, headers, nil
}

// renderClient 渲染单个 client 的 outbounds，并给所有 tag 加 [name] 前缀防冲突。
func (b *BatchJsonService) renderClient(c *model.Client, inboundMap map[uint]*model.Inbound) ([]map[string]interface{}, []string, error) {
	var clientInbounds []uint
	if err := json.Unmarshal(c.Inbounds, &clientInbounds); err != nil {
		return nil, nil, err
	}
	var inbounds []*model.Inbound
	for _, id := range clientInbounds {
		if in, ok := inboundMap[id]; ok {
			inbounds = append(inbounds, in)
		}
	}

	outs, tags, err := b.JsonService.getOutbounds(c.Config, inbounds)
	if err != nil {
		return nil, nil, err
	}

	prefix := fmt.Sprintf("[%s] ", c.Name)
	for i := range *outs {
		origTag, _ := (*outs)[i]["tag"].(string)
		(*outs)[i]["tag"] = prefix + origTag
	}
	prefixedTags := make([]string, len(*tags))
	for i, t := range *tags {
		prefixedTags[i] = prefix + t
	}

	// 处理 client.Links 的 external link（与 JsonService.GetJson 同逻辑）
	links := b.LinkService.GetLinks(&c.Links, "external", "")
	tagNumEnable := 0
	if len(links) > 1 {
		tagNumEnable = 1
	}
	for index, link := range links {
		jOut, tag, err := util.GetOutbound(link, (index+1)*tagNumEnable)
		if err == nil && len(tag) > 0 {
			prefTag := prefix + tag
			(*jOut)["tag"] = prefTag
			*outs = append(*outs, *jOut)
			prefixedTags = append(prefixedTags, prefTag)
		}
	}

	return *outs, prefixedTags, nil
}

func (b *BatchJsonService) preloadInbounds(clients []*model.Client) (map[uint]*model.Inbound, error) {
	idSet := make(map[uint]struct{})
	for _, c := range clients {
		var ids []uint
		if err := json.Unmarshal(c.Inbounds, &ids); err != nil {
			return nil, err
		}
		for _, id := range ids {
			idSet[id] = struct{}{}
		}
	}
	if len(idSet) == 0 {
		return map[uint]*model.Inbound{}, nil
	}
	allIds := make([]uint, 0, len(idSet))
	for id := range idSet {
		allIds = append(allIds, id)
	}

	var inbounds []*model.Inbound
	db := database.GetDB()
	if err := db.Model(model.Inbound{}).Preload("Tls").Where("id IN ?", allIds).Find(&inbounds).Error; err != nil {
		return nil, err
	}
	out := make(map[uint]*model.Inbound, len(inbounds))
	for _, in := range inbounds {
		out[in.Id] = in
	}
	return out, nil
}
