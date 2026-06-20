package service

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/admin8800/s-ui/logger"
)

const (
	acmeHome = "/root/.acme.sh"
	acmeBin  = acmeHome + "/acme.sh"
)

// AcmeClient 封装 acme.sh 命令调用
type AcmeClient struct {
	Home string // /root/.acme.sh
}

func NewAcmeClient() *AcmeClient {
	return &AcmeClient{Home: acmeHome}
}

// Installed 是否已安装
func (c *AcmeClient) Installed() bool {
	_, err := os.Stat(acmeBin)
	return err == nil
}

// Install 安装 acme.sh（curl | sh）
func (c *AcmeClient) Install(email string) error {
	cmd := exec.Command("bash", "-c",
		fmt.Sprintf("curl -s https://get.acme.sh | sh -s email=%s", email))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("acme.sh 安装失败：%s，输出：%s", err, tailOutput(output))
	}
	logger.Info("acme.sh 安装成功")
	return nil
}

// SetDefaultCALetsEncrypt 设为 letsencrypt CA
func (c *AcmeClient) SetDefaultCALetsEncrypt() error {
	return c.exec([]string{"--set-default-ca", "--server", "letsencrypt", "--force"})
}

// IssueIPCert 申请 IP 证书（shortlived）
func (c *AcmeClient) IssueIPCert(ip string) error {
	return c.exec([]string{
		"--issue", "-d", ip,
		"--standalone",
		"--server", "letsencrypt",
		"--certificate-profile", "shortlived",
		"--days", "6",
		"--httpport", "80",
		"--force",
	})
}

// InstallCert installcert 并注册 reloadcmd
func (c *AcmeClient) InstallCert(ip, keyFile, certFile, reloadCmd string) error {
	return c.exec([]string{
		"--installcert", "-d", ip,
		"--key-file", keyFile,
		"--fullchain-file", certFile,
		"--reloadcmd", reloadCmd,
	})
}

// Renew 续签
func (c *AcmeClient) Renew(ip string) error {
	return c.exec([]string{"--renew", "-d", ip, "--force"})
}

// exec 执行 acme.sh 命令，5 分钟超时
func (c *AcmeClient) exec(args []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, acmeBin, args...)
	cmd.Env = append(os.Environ(), "LE_WORKING_DIR="+c.Home)
	output, err := cmd.CombinedOutput()

	if err != nil {
		return fmt.Errorf("acme.sh 命令失败：%s\n输出：%s", err, tailOutput(output))
	}
	logger.Info("acme.sh 执行成功")
	return nil
}

// tailOutput 取输出末尾 4KB
func tailOutput(output []byte) string {
	if len(output) <= 4096 {
		return string(output)
	}
	return "...(截断)\n" + string(output[len(output)-4096:])
}
