package job

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/mhsanaei/3x-ui/v2/database/model"
	"github.com/mhsanaei/3x-ui/v2/logger"
	"github.com/mhsanaei/3x-ui/v2/web/service"
	"github.com/mhsanaei/3x-ui/v2/xray"
)

// CaptureClientActivityJob appends Xray access-log rows to the database only for clients
// with activityCapture enabled (see InboundService.GetActivityCaptureEmailSet).
type CaptureClientActivityJob struct{}

// NewCaptureClientActivityJob creates a new activity capture job.
func NewCaptureClientActivityJob() *CaptureClientActivityJob {
	return &CaptureClientActivityJob{}
}

var (
	activityLogPath   string
	activityLogOffset int64
	activityPending   string
	activityMu        sync.Mutex
)

func (j *CaptureClientActivityJob) Run() {
	activityMu.Lock()
	defer activityMu.Unlock()

	var inboundService service.InboundService
	captureSet := inboundService.GetActivityCaptureEmailSet()

	path, err := xray.GetAccessLogPath()
	if err != nil || path == "" || path == "none" {
		return
	}

	st, err := os.Stat(path)
	if err != nil {
		return
	}

	if path != activityLogPath {
		activityLogPath = path
		activityLogOffset = 0
		activityPending = ""
	}
	if st.Size() < activityLogOffset {
		activityLogOffset = 0
		activityPending = ""
	}

	if len(captureSet) == 0 {
		activityLogOffset = st.Size()
		activityPending = ""
		return
	}

	// Skip existing log tail on cold start / restart so we do not import the whole file at once.
	if activityLogOffset == 0 && activityPending == "" && st.Size() > 0 {
		activityLogOffset = st.Size()
		return
	}

	if st.Size() == activityLogOffset && activityPending == "" {
		return
	}

	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	if _, err = f.Seek(activityLogOffset, io.SeekStart); err != nil {
		return
	}
	chunk, err := io.ReadAll(f)
	if err != nil {
		return
	}
	activityLogOffset = st.Size()
	activityPending += string(chunk)

	lastNL := strings.LastIndex(activityPending, "\n")
	if lastNL < 0 {
		return
	}
	toProcess := activityPending[:lastNL]
	activityPending = activityPending[lastNL+1:]

	var settingService service.SettingService
	tpl, err := settingService.GetXrayConfigTemplate()
	var cfgMap map[string]any
	if err == nil && tpl != "" {
		_ = json.Unmarshal([]byte(tpl), &cfgMap)
	}
	freedoms, blackholes := service.FreedomBlackholeTagsFromDefaultXrayConfig(cfgMap)

	var rows []model.ClientActivity
	for _, line := range strings.Split(toProcess, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.Contains(line, "api -> api") {
			continue
		}
		entry, ok := service.ParseXrayAccessLogLine(line)
		if !ok || entry.Email == "" {
			continue
		}
		if _, want := captureSet[entry.Email]; !want {
			continue
		}
		event := service.AccessLogEventKind(line, freedoms, blackholes)
		rows = append(rows, model.ClientActivity{
			ClientEmail: entry.Email,
			Ts:          entry.DateTime.Unix(),
			FromAddr:    entry.FromAddress,
			ToAddr:      entry.ToAddress,
			InboundTag:  entry.Inbound,
			OutboundTag: entry.Outbound,
			Event:       event,
		})
	}
	if len(rows) == 0 {
		return
	}
	if err := inboundService.AppendClientActivities(rows); err != nil {
		logger.Warning("append client activities:", err)
	}
}
