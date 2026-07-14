package sub

import (
	"fmt"
	"strings"

	"github.com/admin8800/s-ui/logger"
	"github.com/admin8800/s-ui/service"

	"github.com/gin-gonic/gin"
)

type SubHandler struct {
	service.SettingService
	SubService
	JsonService
	ClashService
	BatchJsonService
}

func NewSubHandler(g *gin.RouterGroup) {
	a := &SubHandler{}
	a.initRouter(g)
}

func (s *SubHandler) initRouter(g *gin.RouterGroup) {
	g.GET("/:subid", s.subs)
	g.HEAD("/:subid", s.subHeaders)
}

// InitBatchRouter 挂载聚合订阅 API（独立于 /sub/:subid，路径可自定义）。
func (s *SubHandler) InitBatchRouter(g *gin.RouterGroup) {
	g.GET("", s.batchSearch)
}

func (s *SubHandler) batchSearch(c *gin.Context) {
	// 1. 鉴权：路径已是秘密，失败统一 404，避免暴露接口存在
	expectedKey, _ := s.SettingService.GetSubApiKey()
	if expectedKey != "" {
		if c.Query("key") != expectedKey {
			c.String(404, "404 page not found")
			return
		}
	}

	// 2. 校验 format
	format := c.Query("format")
	if format != "" && format != "json" {
		c.String(400, "unsupported format")
		return
	}

	// 3. 构造 filter
	filter := service.ClientFilter{
		Name:     c.Query("username"),
		NameLike: c.Query("name"),
		Group:    c.Query("group"),
	}

	clients, err := s.BatchJsonService.ClientService.SearchClients(filter)
	if err != nil {
		logger.Error(err)
		c.String(400, err.Error())
		return
	}
	if len(clients) == 0 {
		c.String(404, "no matching clients")
		return
	}

	title := buildBatchTitle(filter)
	result, headers, err := s.BatchJsonService.GetBatchJson(clients, title)
	if err != nil || result == nil {
		logger.Error(err)
		c.String(500, "Error!")
		return
	}

	s.addHeaders(c, headers)
	c.String(200, *result)
}

func buildBatchTitle(f service.ClientFilter) string {
	switch {
	case f.Name != "" && f.NameLike == "" && f.Group == "":
		return "user:" + f.Name
	case f.NameLike != "" && f.Name == "" && f.Group == "":
		return "search:" + f.NameLike
	case f.Group != "" && f.Name == "" && f.NameLike == "":
		return "group:" + f.Group
	case f.Name == "" && f.NameLike == "" && f.Group == "":
		return "all"
	}
	// 组合
	parts := []string{}
	if f.Name != "" {
		parts = append(parts, "username="+f.Name)
	}
	if f.NameLike != "" {
		parts = append(parts, "name="+f.NameLike)
	}
	if f.Group != "" {
		parts = append(parts, "group="+f.Group)
	}
	return "batch:" + strings.Join(parts, ",")
}

func (s *SubHandler) subs(c *gin.Context) {
	var headers []string
	var result *string
	var err error
	subId := c.Param("subid")
	format, isFormat := c.GetQuery("format")
	if isFormat {
		switch format {
		case "json":
			result, headers, err = s.JsonService.GetJson(subId, format)
		case "clash":
			result, headers, err = s.ClashService.GetClash(subId)
		}
		if err != nil || result == nil {
			logger.Error(err)
			c.String(400, "Error!")
			return
		}
	} else {
		result, headers, err = s.SubService.GetSubs(subId)
		if err != nil || result == nil {
			logger.Error(err)
			c.String(400, "Error!")
			return
		}
	}

	s.addHeaders(c, headers)

	c.String(200, *result)
}

func (s *SubHandler) subHeaders(c *gin.Context) {
	subId := c.Param("subid")
	client, err := s.SubService.getClientBySubId(subId)
	if err != nil {
		logger.Error(err)
		c.String(400, "Error!")
		return
	}

	headers := s.SubService.getClientHeaders(client)
	s.addHeaders(c, headers)

	c.Status(200)
}

func (s *SubHandler) addHeaders(c *gin.Context, headers []string) {
	c.Writer.Header().Set("Subscription-Userinfo", headers[0])
	c.Writer.Header().Set("Profile-Update-Interval", headers[1])
	c.Writer.Header().Set("Profile-Title", headers[2])
	c.Writer.Header().Set("Content-Disposition", contentDispositionHeader(headers[2]))
}

func contentDispositionHeader(name string) string {
	filename := strings.TrimSpace(name)
	if filename == "" {
		filename = "subscription"
	}

	return fmt.Sprintf("attachment; filename=\"%s\"; filename*=UTF-8''%s", asciiSafeFilename(filename), rfc5987Encode(filename))
}

func asciiSafeFilename(filename string) string {
	var builder strings.Builder
	for _, r := range filename {
		switch {
		case r == '"' || r == '\\':
			builder.WriteByte('_')
		case r >= 0x20 && r <= 0x7e:
			builder.WriteRune(r)
		}
	}

	fallback := strings.TrimSpace(builder.String())
	if fallback == "" {
		return "subscription"
	}

	return fallback
}

func rfc5987Encode(filename string) string {
	const hex = "0123456789ABCDEF"

	var builder strings.Builder
	for _, b := range []byte(filename) {
		if isRFC5987AttrChar(b) {
			builder.WriteByte(b)
			continue
		}

		builder.WriteByte('%')
		builder.WriteByte(hex[b>>4])
		builder.WriteByte(hex[b&0x0f])
	}

	return builder.String()
}

func isRFC5987AttrChar(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z':
		return true
	case b >= 'A' && b <= 'Z':
		return true
	case b >= '0' && b <= '9':
		return true
	}

	switch b {
	case '!', '#', '$', '&', '+', '-', '.', '^', '_', '`', '|', '~':
		return true
	default:
		return false
	}
}
