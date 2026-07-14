package util

import (
	"fmt"

	"github.com/admin8800/s-ui/database/model"
)

func GetHeaders(client *model.Client, updateInterval int) []string {
	var headers []string
	headers = append(headers, fmt.Sprintf("upload=%d; download=%d; total=%d; expire=%d", client.Up, client.Down, client.Volume, client.Expiry))
	headers = append(headers, fmt.Sprintf("%d", updateInterval))
	headers = append(headers, client.Name)
	return headers
}

func GetBatchHeaders(clients []*model.Client, updateInterval int, title string) []string {
	var sumUp, sumDown, sumTotal int64
	var minExpire int64 = 0
	for _, c := range clients {
		sumUp += c.Up
		sumDown += c.Down
		sumTotal += c.Volume
		if c.Expiry > 0 && (minExpire == 0 || c.Expiry < minExpire) {
			minExpire = c.Expiry
		}
	}
	return []string{
		fmt.Sprintf("upload=%d; download=%d; total=%d; expire=%d", sumUp, sumDown, sumTotal, minExpire),
		fmt.Sprintf("%d", updateInterval),
		title,
	}
}
