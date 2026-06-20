package service

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"github.com/admin8800/s-ui/database"
	"github.com/admin8800/s-ui/database/model"
	"github.com/admin8800/s-ui/logger"
	"github.com/admin8800/s-ui/util/common"
)

// 证书模式常量
const (
	CertModeNone   = "none"   // 未配置
	CertModeSelf   = "self"   // 自签
	CertModeLeIP   = "le-ip"  // Let's Encrypt IP
	CertModeManual = "manual" // 用户手动填写
)

// CertStatus 证书状态（对外返回）
type CertStatus struct {
	Mode      string    `json:"mode"`
	HasCert   bool      `json:"hasCert"`
	CertFile  string    `json:"certFile"`
	KeyFile   string    `json:"keyFile"`
	Subject   string    `json:"subject"`
	Issuer    string    `json:"issuer"`
	IP        string    `json:"ip"`
	NotBefore time.Time `json:"notBefore"`
	NotAfter  time.Time `json:"notAfter"`
	DaysLeft  int       `json:"daysLeft"`
}

// CertPrecheckResult 预检结果
type CertPrecheckResult struct {
	PublicIP   string `json:"publicIp"`
	Port80Free bool   `json:"port80Free"`
	AcmeReady  bool   `json:"acmeReady"`
	OK         bool   `json:"ok"`
	Message    string `json:"message"`
}

// WebReloader 接口，由 web.Server 实现
type WebReloader interface {
	ReloadCert() error
}

// CertService 证书管理服务
type CertService struct {
	settingService *SettingService
	panelService   *PanelService
	webReloader    WebReloader
	mu             sync.Mutex
}

func NewCertService(s *SettingService, p *PanelService, w WebReloader) *CertService {
	return &CertService{
		settingService: s,
		panelService:   p,
		webReloader:    w,
	}
}

// Precheck 预检：检测公网 IP、80 端口、acme.sh 是否就绪
func (s *CertService) Precheck() (*CertPrecheckResult, error) {
	result := &CertPrecheckResult{}

	// 公网 IP 检测
	publicIP := getPublicIP()
	result.PublicIP = publicIP

	// 80 端口检测
	ln, err := net.Listen("tcp", ":80")
	if err == nil {
		ln.Close()
		result.Port80Free = true
	}

	// acme.sh 就绪检测
	acme := NewAcmeClient()
	result.AcmeReady = acme.Installed()

	// 汇总
	result.OK = publicIP != "" && result.Port80Free
	if !result.OK {
		if publicIP == "" {
			result.Message = "公网 IP 检测失败，请确认服务器可访问公网"
		} else if !result.Port80Free {
			result.Message = "80 端口被占用，请先停止占用 80 端口的服务（如 nginx）再申请"
		}
	}

	return result, nil
}

// IssueLeIPCert 申请 LE IP 证书
func (s *CertService) IssueLeIPCert(force bool) (*CertStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 1. 预检
	precheck, err := s.Precheck()
	if err != nil {
		return nil, err
	}
	if !precheck.OK {
		return nil, common.NewError(precheck.Message)
	}

	// 2. force=false 时检查是否已有有效 LE 证书
	if !force {
		status, err := s.GetCertStatus()
		if err == nil && status.Mode == CertModeLeIP && status.DaysLeft > 3 {
			logger.Info("证书仍在有效期内（剩余", status.DaysLeft, "天），跳过申请")
			return status, nil
		}
	}

	ip := precheck.PublicIP

	// 3. 确保 acme.sh 已安装
	acme := NewAcmeClient()
	if !acme.Installed() {
		logger.Info("安装 acme.sh...")
		if err := acme.Install("admin@s-ui.local"); err != nil {
			return nil, fmt.Errorf("acme.sh 安装失败：%s，请检查 curl 和网络", err)
		}
	}

	// 4. 设默认 CA
	if err := acme.SetDefaultCALetsEncrypt(); err != nil {
		return nil, fmt.Errorf("设置默认 CA 失败：%s", err)
	}

	// 5. 申请 IP 证书
	logger.Info("开始申请 Let's Encrypt IP 证书：", ip)
	if err := acme.IssueIPCert(ip); err != nil {
		return nil, fmt.Errorf("证书申请失败，acme.sh 输出：\n%s", err)
	}

	// 6. 创建证书目录
	ipCertDir := "/usr/local/s-ui/cert/ip"
	if err := os.MkdirAll(ipCertDir, 0755); err != nil {
		return nil, fmt.Errorf("创建证书目录失败：%s", err)
	}

	ipKeyFile := ipCertDir + "/privkey.pem"
	ipCertFile := ipCertDir + "/fullchain.pem"
	reloadCmd := "/usr/local/s-ui/sui cert -reload"

	// 7. installcert
	if err := acme.InstallCert(ip, ipKeyFile, ipCertFile, reloadCmd); err != nil {
		return nil, fmt.Errorf("安装证书失败：%s", err)
	}

	// 8. 文件权限
	os.Chmod(ipKeyFile, 0600)
	os.Chmod(ipCertFile, 0644)

	// 9. 写入 setting
	s.settingService.SetCertFile(ipCertFile)
	s.settingService.SetKeyFile(ipKeyFile)
	s.settingService.SetCertMode(CertModeLeIP)
	s.settingService.SetCertDomain(ip)

	// 10. 热加载
	if err := s.webReloader.ReloadCert(); err != nil {
		logger.Warning("热加载证书失败（新连接可能暂时用旧证书）：", err)
	}

	// 11. 写 changes 审计
	s.writeChange("cert-system", "cert", "issue-ip")

	logger.Info("LE IP 证书申请成功：", ip)
	return s.GetCertStatus()
}

// IssueSelfSignedCert 生成自签证书
func (s *CertService) IssueSelfSignedCert() (*CertStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	status, err := s.issueSelfSignedCert()
	if err != nil {
		return nil, err
	}

	// 写入 setting
	s.settingService.SetCertFile(selfCertFile)
	s.settingService.SetKeyFile(selfKeyFile)
	s.settingService.SetCertMode(CertModeSelf)
	s.settingService.SetCertDomain("")

	// 热加载
	if err := s.webReloader.ReloadCert(); err != nil {
		logger.Warning("热加载证书失败：", err)
	}

	// 写 changes
	s.writeChange("cert-system", "cert", "issue-self")

	return status, nil
}

// RenewIpCert 手动续签
func (s *CertService) RenewIpCert() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	mode, _ := s.settingService.GetCertMode()
	if mode != CertModeLeIP {
		logger.Info("当前不是 LE IP 证书，无需续签")
		return nil
	}

	ip, _ := s.settingService.GetCertDomain()
	if ip == "" {
		return common.NewError("未找到证书 IP，无法续签")
	}

	logger.Info("开始续签 LE IP 证书：", ip)
	acme := NewAcmeClient()
	if err := acme.Renew(ip); err != nil {
		logger.Error("证书续签失败：", err)
		// 兜底：再试一次热加载
		if reloadErr := s.webReloader.ReloadCert(); reloadErr != nil {
			logger.Error("热加载也失败：", reloadErr)
		}
		return err
	}

	// acme.sh renew 后会自动调用 reloadcmd，但兜底再调一次
	time.Sleep(2 * time.Second)
	if err := s.webReloader.ReloadCert(); err != nil {
		logger.Warning("续签后热加载失败：", err)
	}

	// 写 changes
	s.writeChange("cert-cron", "cert", "renew")

	logger.Info("证书续签成功：", ip)
	return nil
}

// RemoveCert 移除证书（清空 setting，触发热加载切回 HTTP）
func (s *CertService) RemoveCert() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.settingService.SetCertFile("")
	s.settingService.SetKeyFile("")
	s.settingService.SetCertMode(CertModeNone)
	s.settingService.SetCertDomain("")

	// 不删磁盘上的证书文件

	// 热加载（清除证书）
	if err := s.webReloader.ReloadCert(); err != nil {
		logger.Warning("清除证书热加载失败：", err)
	}

	// 从 HTTPS 切 HTTP 的唯一安全路径：重启
	s.panelService.RestartPanel(3 * time.Second)

	// 写 changes
	s.writeChange("cert-system", "cert", "remove")

	logger.Info("证书已移除，面板将恢复 HTTP")
	return nil
}

// GetCertStatus 读取当前证书状态
func (s *CertService) GetCertStatus() (*CertStatus, error) {
	certFile, _ := s.settingService.GetCertFile()
	keyFile, _ := s.settingService.GetKeyFile()
	certMode, _ := s.settingService.GetCertMode()
	certDomain, _ := s.settingService.GetCertDomain()

	return s.buildCertStatus(certFile, keyFile, certMode, certDomain)
}

// buildCertStatus 根据路径和模式构建证书状态
func (s *CertService) buildCertStatus(certFile, keyFile, certMode, certDomain string) (*CertStatus, error) {
	status := &CertStatus{
		Mode:     CertModeNone,
		HasCert:  false,
		CertFile: certFile,
		KeyFile:  keyFile,
	}

	if certFile == "" || keyFile == "" {
		return status, nil
	}

	// 尝试解析证书
	certData, err := os.ReadFile(certFile)
	if err != nil {
		status.Mode = certMode
		if status.Mode == "" {
			status.Mode = CertModeManual
		}
		return status, nil
	}

	block, _ := pem.Decode(certData)
	if block == nil || block.Type != "CERTIFICATE" {
		status.Mode = certMode
		if status.Mode == "" {
			status.Mode = CertModeManual
		}
		return status, nil
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		status.Mode = certMode
		if status.Mode == "" {
			status.Mode = CertModeManual
		}
		return status, nil
	}

	// 模式判断
	if certMode == "" {
		// 磁盘上有证书但 setting 中无 mode，标记为 manual
		status.Mode = CertModeManual
	} else {
		status.Mode = certMode
	}

	status.HasCert = true
	status.Subject = cert.Subject.CommonName
	status.Issuer = cert.Issuer.CommonName
	if cert.Issuer.Organization != nil && len(cert.Issuer.Organization) > 0 {
		status.Issuer = cert.Issuer.Organization[0]
	}
	status.NotBefore = cert.NotBefore
	status.NotAfter = cert.NotAfter
	status.DaysLeft = int(cert.NotAfter.Sub(time.Now()).Hours() / 24)
	if status.DaysLeft < 0 {
		status.DaysLeft = 0
	}

	// IP 信息
	if certDomain != "" {
		status.IP = certDomain
	} else if len(cert.IPAddresses) > 0 {
		status.IP = cert.IPAddresses[0].String()
	}

	// 用 tls 验证
	if _, err := tls.LoadX509KeyPair(certFile, keyFile); err != nil {
		status.HasCert = false
	}

	return status, nil
}

// writeChange 写 changes 审计记录
func (s *CertService) writeChange(actor, key, action string) {
	db := database.GetDB()
	if db == nil {
		return
	}
	status, _ := s.GetCertStatus()
	objJSON, _ := json.Marshal(status)
	changeTime := time.Now().Unix()
	_ = db.Create(&model.Changes{
		DateTime: changeTime,
		Actor:    actor,
		Key:      key,
		Action:   action,
		Obj:      objJSON,
	}).Error
}
