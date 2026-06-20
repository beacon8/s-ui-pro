package cronjob

import (
	"time"

	"github.com/admin8800/s-ui/service"
	"github.com/robfig/cron/v3"
)

type CronJob struct {
	cron *cron.Cron
}

func NewCronJob() *CronJob {
	return &CronJob{}
}

func (c *CronJob) Start(loc *time.Location, trafficAge int, certService *service.CertService) error {
	c.cron = cron.New(cron.WithLocation(loc), cron.WithSeconds())
	c.cron.Start()

	go func() {
		// Start stats job
		c.cron.AddJob("@every 10s", NewStatsJob(trafficAge > 0))
		// Start expiry job
		c.cron.AddJob("@every 1m", NewDepleteJob())
		// Start deleting old stats
		if trafficAge > 0 {
			c.cron.AddJob("@daily", NewDelStatsJob(trafficAge))
		}
		// Start core if it is not running
		c.cron.AddJob("@every 5s", NewCheckCoreJob())
		// database WAL checkpoint
		c.cron.AddJob("@every 10m", NewWALCheckpointJob())

		// SSL cert renewal job
		if certService != nil {
			certJob := NewCertRenewalJob(certService)
			c.cron.AddJob("@every 6h", certJob)
			// 启动 30s 后立即检查一次（防重启后漏签）
			go func() {
				time.Sleep(30 * time.Second)
				certJob.Run()
			}()
		}
	}()

	return nil
}

func (c *CronJob) Stop() {
	if c.cron != nil {
		c.cron.Stop()
	}
}
