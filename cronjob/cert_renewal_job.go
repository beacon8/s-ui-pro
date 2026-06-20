package cronjob

import (
	"github.com/admin8800/s-ui/logger"
	"github.com/admin8800/s-ui/service"
)

// CertRenewalJob 证书续签守护任务
// 每 6 小时检查一次，剩余有效期 < 3 天则触发续签
type CertRenewalJob struct {
	certService *service.CertService
}

func NewCertRenewalJob(s *service.CertService) *CertRenewalJob {
	return &CertRenewalJob{certService: s}
}

func (j *CertRenewalJob) Run() {
	status, err := j.certService.GetCertStatus()
	if err != nil {
		logger.Warning("cert renewal: 读取状态失败：", err)
		return
	}
	if status.Mode != service.CertModeLeIP {
		// 非 LE IP 证书，无需续签
		return
	}
	if status.DaysLeft > 3 {
		logger.Debug("cert renewal: 剩余", status.DaysLeft, "天，无需续签")
		return
	}
	logger.Info("cert renewal: 剩余", status.DaysLeft, "天，开始续签")
	if err := j.certService.RenewIpCert(); err != nil {
		logger.Error("cert renewal: 续签失败：", err)
		return
	}
	logger.Info("cert renewal: 续签成功")
}
