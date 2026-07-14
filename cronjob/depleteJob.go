package cronjob

import (
	"github.com/admin8800/s-ui/logger"
	"github.com/admin8800/s-ui/service"
)

type DepleteJob struct {
	service.ConfigService
}

func NewDepleteJob() *DepleteJob {
	return new(DepleteJob)
}

func (s *DepleteJob) Run() {
	if err := s.ConfigService.DepleteClients(); err != nil {
		logger.Warning("Disable depleted users failed: ", err)
	}
}
