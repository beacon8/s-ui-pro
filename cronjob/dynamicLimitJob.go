package cronjob

import (
	"time"

	"github.com/admin8800/s-ui/service"
)

type DynamicLimitJob struct {
	service.StatsService
}

func NewDynamicLimitJob() *DynamicLimitJob {
	return &DynamicLimitJob{}
}

func (j *DynamicLimitJob) Run() {
	j.StatsService.SampleDynamicLimits(time.Now())
}
