package cronjob

import (
	"testing"
	"time"
)

func TestStartRegistersAllJobsBeforeReturning(t *testing.T) {
	job := NewCronJob()
	if err := job.Start(time.UTC, 0, 60, ""); err != nil {
		t.Fatal(err)
	}
	defer job.Stop()

	if got, want := len(job.cron.Entries()), 4; got != want {
		t.Fatalf("registered jobs = %d, want %d", got, want)
	}
}
