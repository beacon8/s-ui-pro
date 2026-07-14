package cronjob

import (
	"fmt"
	"time"

	"github.com/admin8800/s-ui/logger"

	"github.com/robfig/cron/v3"
)

// cronParser accepts standard 5-field cron, optional leading seconds (6-field)
// and descriptors (@daily, @weekly, @every 10s, ...). Used both for the cron
// engine and for parsing the user-provided globalReset spec.
var cronParser = cron.NewParser(
	cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
)

type CronJob struct {
	cron *cron.Cron
}

func NewCronJob() *CronJob {
	return &CronJob{}
}

func (c *CronJob) Start(loc *time.Location, trafficAge int, statsBucketSeconds int64, globalReset string) error {
	c.cron = cron.New(
		cron.WithLocation(loc),
		cron.WithParser(cronParser),
		cron.WithChain(cron.SkipIfStillRunning(cron.DefaultLogger)),
	)
	addJob := func(spec string, job cron.Job) error {
		if _, err := c.cron.AddJob(spec, job); err != nil {
			return fmt.Errorf("schedule %s: %w", spec, err)
		}
		return nil
	}

	if err := addJob("@every 10s", NewStatsJob(trafficAge > 0, statsBucketSeconds)); err != nil {
		return err
	}
	if err := addJob("@every 1m", NewDepleteJob()); err != nil {
		return err
	}
	if globalReset != "" && globalReset != "off" {
		schedule, err := cronParser.Parse(globalReset)
		if err != nil {
			logger.Warning("invalid globalReset cron spec <", globalReset, ">: ", err)
		} else {
			c.cron.Schedule(schedule, NewResetTrafficJob(schedule))
		}
	}
	if trafficAge > 0 {
		if err := addJob("@daily", NewDelStatsJob(trafficAge)); err != nil {
			return err
		}
	}
	if err := addJob("@every 5s", NewCheckCoreJob()); err != nil {
		return err
	}
	if err := addJob("@every 10m", NewWALCheckpointJob()); err != nil {
		return err
	}
	c.cron.Start()

	return nil
}

func (c *CronJob) Stop() {
	if c.cron != nil {
		<-c.cron.Stop().Done()
		c.cron = nil
	}
}
