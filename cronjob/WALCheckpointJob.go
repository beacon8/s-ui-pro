package cronjob

import (
	"github.com/admin8800/s-ui/database"
	"github.com/admin8800/s-ui/logger"
)

type WALCheckpointJob struct{}

func NewWALCheckpointJob() *WALCheckpointJob {
	return &WALCheckpointJob{}
}

func (s *WALCheckpointJob) Run() {
	db := database.GetDB()
	// PASSIVE never waits for active readers or writers; SQLite will checkpoint
	// as many frames as it safely can and leave the remainder for the next run.
	if err := db.Exec("PRAGMA wal_checkpoint(PASSIVE)").Error; err != nil {
		logger.Error("Error checkpointing WAL: ", err.Error())
	}
}
