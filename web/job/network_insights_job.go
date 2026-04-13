package job

import (
	"github.com/mhsanaei/3x-ui/v2/logger"
	"github.com/mhsanaei/3x-ui/v2/web/service"
)

// NetworkInsightsJob periodically persists rolling 24h destination hostname and IP counts to the panel database.
type NetworkInsightsJob struct{}

// NewNetworkInsightsJob creates a NetworkInsightsJob instance.
func NewNetworkInsightsJob() *NetworkInsightsJob {
	return &NetworkInsightsJob{}
}

// Run scans the Xray access log and updates the stored 24h snapshot row.
func (j *NetworkInsightsJob) Run() {
	var inboundService service.InboundService
	if err := inboundService.RefreshNetworkInsightsPanel24h(); err != nil {
		logger.Warning("network insights 24h snapshot:", err)
	}
}
