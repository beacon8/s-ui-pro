package util

import (
	"crypto/tls"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/admin8800/s-ui/logger"
	"github.com/admin8800/s-ui/util/common"
)

func GetExternalLink(url string) string {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}

	client := &http.Client{Transport: tr}

	response, err := client.Get(url)
	if err != nil {
		logger.Warning("sub: Error making HTTP request:", err)
		return ""
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		logger.Warning("sub: Error reading response body:", err)
		return ""
	}

	data := StrOrBase64Encoded(string(body))
	return data
}

// parseSubContent parses already-decoded content (post base64 fallback) into outbounds.
func parseSubContent(data string) ([]map[string]interface{}, error) {
	var result []map[string]interface{}

	if strings.HasPrefix(data, "{") && strings.HasSuffix(data, "}") {
		var jsonData map[string]interface{}
		if err := json.Unmarshal([]byte(data), &jsonData); err != nil {
			logger.Warning("sub: Error unmarshalling JSON:", err)
			return nil, err
		}
		outbounds, ok := jsonData["outbounds"].([]any)
		if !ok {
			return nil, common.NewError("no outbounds in json")
		}
		for _, outbound := range outbounds {
			outboundMap, ok := outbound.(map[string]interface{})
			if ok && len(outboundMap) > 0 {
				oType, _ := outboundMap["type"].(string)
				switch oType {
				case "urltest", "direct", "selector", "block":
					continue
				default:
					result = append(result, outboundMap)
				}
			}
		}
		if len(result) == 0 {
			return nil, common.NewError("no result")
		}
		return result, nil
	}

	// multi-line: try URI first, then shorthand
	links := strings.Split(data, "\n")
	for idx, link := range links {
		out, _, err := GetOutboundLine(link, idx+1)
		if err == nil && out != nil {
			result = append(result, *out)
		}
	}
	if len(result) == 0 {
		return nil, common.NewError("no result")
	}
	return result, nil
}

// GetExternalSub fetches a remote URL and parses outbounds (original signature preserved).
func GetExternalSub(url string) ([]map[string]interface{}, error) {
	if len(url) == 0 {
		return nil, common.NewError("no url")
	}
	data := GetExternalLink(url)
	if len(data) == 0 {
		return nil, common.NewError("no result")
	}
	return parseSubContent(data)
}

// ParseLocalSub parses locally-pasted node text into outbounds (no outbound HTTP request).
func ParseLocalSub(content string) ([]map[string]interface{}, error) {
	if len(content) == 0 {
		return nil, common.NewError("no content")
	}
	data := StrOrBase64Encoded(content)
	return parseSubContent(data)
}
