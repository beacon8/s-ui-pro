package service

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/admin8800/s-ui/logger"
)

const (
	selfCertDir  = "/usr/local/s-ui/cert/self"
	selfCertFile = selfCertDir + "/self.crt"
	selfKeyFile  = selfCertDir + "/self.key"
)

// generateSelfSignedCert 生成自签证书（PEM 格式）
// 返回 certPEM, keyPEM, error
func generateSelfSignedCert() (certPEM, keyPEM []byte, err error) {
	// 生成 ECDSA P-256 私钥
	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("生成 ECDSA 密钥失败：%s", err)
	}

	// 生成 128-bit 随机序列号
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, fmt.Errorf("生成序列号失败：%s", err)
	}

	notBefore := time.Now().Add(-1 * time.Hour)   // 防时钟漂移
	notAfter := time.Now().Add(10 * 365 * 24 * time.Hour) // 10 年

	// 收集 SAN
	var dnsNames []string
	var ipAddresses []net.IP

	dnsNames = append(dnsNames, "localhost", "*.localhost")
	ipAddresses = append(ipAddresses, net.IPv4(127, 0, 0, 1), net.IPv6loopback)

	// 枚举本机所有非 loopback 地址
	addrs, _ := net.InterfaceAddrs()
	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok {
			continue
		}
		ip := ipNet.IP
		if ip == nil {
			continue
		}
		if ip.IsLoopback() {
			continue
		}
		if ip.To4() != nil {
			ipAddresses = append(ipAddresses, ip.To4())
		} else if !ip.IsLinkLocalUnicast() {
			ipAddresses = append(ipAddresses, ip)
		}
	}

	// 公网 IP (best-effort)
	publicIP := getPublicIP()
	if publicIP != "" {
		if pip := net.ParseIP(publicIP); pip != nil {
			ipAddresses = append(ipAddresses, pip)
		}
	}

	// 去重 IP 地址
	seen := make(map[string]bool)
	var uniqueIPs []net.IP
	for _, ip := range ipAddresses {
		key := ip.String()
		if !seen[key] {
			seen[key] = true
			uniqueIPs = append(uniqueIPs, ip)
		}
	}

	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   "s-ui-panel",
			Organization: []string{"s-ui"},
		},
		NotBefore: notBefore,
		NotAfter:  notAfter,

		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            0,

		DNSNames:    dnsNames,
		IPAddresses: uniqueIPs,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &privKey.PublicKey, privKey)
	if err != nil {
		return nil, nil, fmt.Errorf("创建证书失败：%s", err)
	}

	// PEM 编码证书
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	// PEM 编码私钥 (PKCS#8)
	privDER, err := x509.MarshalPKCS8PrivateKey(privKey)
	if err != nil {
		return nil, nil, fmt.Errorf("编码私钥失败：%s", err)
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privDER})

	return certPEM, keyPEM, nil
}

// issueSelfSignedCert 生成自签证书并写入磁盘
func (s *CertService) issueSelfSignedCert() (*CertStatus, error) {
	// 创建目录
	if err := os.MkdirAll(selfCertDir, 0755); err != nil {
		return nil, fmt.Errorf("创建证书目录失败：%s", err)
	}

	// 备份旧证书
	if _, err := os.Stat(selfCertFile); err == nil {
		bakFile := fmt.Sprintf("%s.bak.%d", selfCertFile, time.Now().Unix())
		if err := os.Rename(selfCertFile, bakFile); err != nil {
			logger.Warning("备份旧证书失败：", err)
		}
	}
	if _, err := os.Stat(selfKeyFile); err == nil {
		bakFile := fmt.Sprintf("%s.bak.%d", selfKeyFile, time.Now().Unix())
		if err := os.Rename(selfKeyFile, bakFile); err != nil {
			logger.Warning("备份旧私钥失败：", err)
		}
	}

	certPEM, keyPEM, err := generateSelfSignedCert()
	if err != nil {
		// 恢复备份
		restoreBackup(selfCertFile, selfKeyFile)
		return nil, err
	}

	// 写证书文件 (0644)
	if err := os.WriteFile(selfCertFile, certPEM, 0644); err != nil {
		restoreBackup(selfCertFile, selfKeyFile)
		return nil, fmt.Errorf("写入证书文件失败：%s", err)
	}

	// 写私钥文件 (0600)
	if err := os.WriteFile(selfKeyFile, keyPEM, 0600); err != nil {
		restoreBackup(selfCertFile, selfKeyFile)
		return nil, fmt.Errorf("写入私钥文件失败：%s", err)
	}

	// Sanity check: 用 tls.LoadX509KeyPair 加载验证
	if _, err := tls.LoadX509KeyPair(selfCertFile, selfKeyFile); err != nil {
		// 删除坏文件，恢复备份
		os.Remove(selfCertFile)
		os.Remove(selfKeyFile)
		restoreBackup(selfCertFile, selfKeyFile)
		return nil, fmt.Errorf("自签证书自检失败，已回滚：%s", err)
	}

	logger.Info("自签证书生成成功：", selfCertFile)
	return s.buildCertStatus(selfCertFile, selfKeyFile, CertModeSelf, "")
}

// restoreBackup 恢复备份文件
func restoreBackup(certFile, keyFile string) {
	bakCert := certFile + ".bak.*"
	bakKey := keyFile + ".bak.*"
	// 简单处理：找最新的 bak 文件恢复
	entries, err := os.ReadDir(selfCertDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if strings.Contains(name, ".bak.") {
			origName := strings.Split(name, ".bak.")[0]
			os.Rename(filepath.Join(selfCertDir, name), filepath.Join(selfCertDir, origName))
		}
	}
	_ = bakCert
	_ = bakKey
}

// getPublicIP 获取公网 IP (best-effort, 2 秒超时)
func getPublicIP() string {
	urls := []string{
		"https://api.ipify.org",
		"https://ifconfig.me",
		"https://icanhazip.com",
		"https://ip.sb",
	}
	client := &http.Client{Timeout: 3 * time.Second}
	for _, url := range urls {
		resp, err := client.Get(url)
		if err != nil {
			continue
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 128))
		resp.Body.Close()
		if err != nil {
			continue
		}
		ip := strings.TrimSpace(string(body))
		if net.ParseIP(ip) != nil {
			return ip
		}
	}
	return ""
}
