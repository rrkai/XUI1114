package job

import (
	"time"

	"github.com/alireza0/x-ui/database"
	"github.com/alireza0/x-ui/logger"
)

const dailyTrafficRetentionDays = 90

type ClientTrafficDailyCleanupJob struct{}

func NewClientTrafficDailyCleanupJob() *ClientTrafficDailyCleanupJob {
	return new(ClientTrafficDailyCleanupJob)
}

// Run removes daily traffic rows older than the retention window. Scheduled
// monthly, so rows may live up to ~120 days before being pruned.
func (j *ClientTrafficDailyCleanupJob) Run() {
	cutoff := time.Now().AddDate(0, 0, -dailyTrafficRetentionDays).Format("2006-01-02")
	result := database.GetDB().Exec("DELETE FROM client_traffic_dailies WHERE day < ?", cutoff)
	if result.Error != nil {
		logger.Warning("clean client daily traffic failed:", result.Error)
		return
	}
	logger.Infof("cleaned client daily traffic: %d rows before %s removed", result.RowsAffected, cutoff)
}