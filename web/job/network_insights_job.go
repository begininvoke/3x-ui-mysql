package job

import (
	"github.com/mhsanaei/3x-ui/v2/logger"
	"github.com/mhsanaei/3x-ui/v2/web/service"
)

// NetworkInsightsJob periodically merges access-log-derived destination counts into the panel database snapshot.
type NetworkInsightsJob struct{}

// NewNetworkInsightsJob creates a NetworkInsightsJob instance.
func NewNetworkInsightsJob() *NetworkInsightsJob {
	return &NetworkInsightsJob{}
}

// Run scans the Xray access log and updates the stored snapshot row (counts never shrink until cleared in the UI).
func (j *NetworkInsightsJob) Run() {
	var inboundService service.InboundService
	if err := inboundService.RefreshNetworkInsightsPanel24h(); err != nil {
		logger.Warning("network insights 24h snapshot:", err)
	}
}
