package job

import (
	"encoding/json"
	"html"
	"strings"
	"time"

	"github.com/mhsanaei/3x-ui/v2/logger"
	"github.com/mhsanaei/3x-ui/v2/web/service"
)

// OutboundPingNotifyJob checks all Xray outbounds on a schedule, persists up/down state, and notifies Telegram on changes.
type OutboundPingNotifyJob struct {
	settingService  service.SettingService
	outboundService service.OutboundService
	tgbotService    service.Tgbot
}

// NewOutboundPingNotifyJob creates a new outbound ping notification job.
func NewOutboundPingNotifyJob() *OutboundPingNotifyJob {
	return new(OutboundPingNotifyJob)
}

// Run tests each testable outbound (two attempts per tag when the first fails), compares to saved state, and alerts on changes.
func (j *OutboundPingNotifyJob) Run() {
	enabled, err := j.settingService.GetTgOutboundPingNotify()
	if err != nil || !enabled {
		return
	}
	tgOn, err := j.settingService.GetTgbotEnabled()
	if err != nil || !tgOn {
		return
	}
	if !j.tgbotService.IsRunning() {
		return
	}

	testURL, _ := j.settingService.GetXrayOutboundTestUrl()
	if testURL == "" {
		testURL = "https://www.google.com/generate_204"
	}

	cfgStr, err := j.settingService.GetXrayConfigTemplate()
	if err != nil || strings.TrimSpace(cfgStr) == "" {
		logger.Warning("outbound ping job: no xray config template")
		return
	}

	var root map[string]any
	if err := json.Unmarshal([]byte(cfgStr), &root); err != nil {
		logger.Warning("outbound ping job: parse config:", err)
		return
	}
	rawOutbounds, _ := root["outbounds"].([]any)
	if len(rawOutbounds) == 0 {
		return
	}

	allOutboundsJSON, err := json.Marshal(rawOutbounds)
	if err != nil {
		return
	}
	allStr := string(allOutboundsJSON)

	prev, err := j.settingService.GetOutboundPingLastStatus()
	if err != nil {
		prev = map[string]bool{}
	}

	next := make(map[string]bool, len(rawOutbounds))
	var changes []string

	for _, ob := range rawOutbounds {
		obMap, ok := ob.(map[string]any)
		if !ok {
			continue
		}
		tag, _ := obMap["tag"].(string)
		if tag == "" {
			continue
		}
		protocol, _ := obMap["protocol"].(string)
		if protocol == "blackhole" || tag == "blocked" {
			continue
		}

		oneJSON, err := json.Marshal(obMap)
		if err != nil {
			continue
		}

		okPing, _ := j.outboundService.CheckOutboundPingWithRetry(string(oneJSON), testURL, allStr)
		next[tag] = okPing

		if _, had := prev[tag]; !had {
			continue
		}
		if prev[tag] == okPing {
			continue
		}
		oldLabel := j.tgbotService.I18nBot("tgbot.messages.yes")
		newLabel := j.tgbotService.I18nBot("tgbot.messages.no")
		if !prev[tag] {
			oldLabel = j.tgbotService.I18nBot("tgbot.messages.no")
		}
		if okPing {
			newLabel = j.tgbotService.I18nBot("tgbot.messages.yes")
		}
		line := j.tgbotService.I18nBot("tgbot.messages.outboundPingLine",
			"Tag=="+html.EscapeString(tag),
			"Old=="+oldLabel,
			"New=="+newLabel,
		)
		changes = append(changes, line)
	}

	if err := j.settingService.SetOutboundPingLastStatus(next); err != nil {
		logger.Warning("outbound ping job: save state:", err)
	}

	if len(changes) == 0 {
		return
	}

	loc, _ := j.settingService.GetTimeLocation()
	if loc == nil {
		loc = time.Local
	}
	timeStr := time.Now().In(loc).Format("2006-01-02 15:04:05")
	header := j.tgbotService.I18nBot("tgbot.messages.outboundPingHeader",
		"Time=="+timeStr,
	)
	msg := header + strings.Join(changes, "")
	j.tgbotService.SendMsgToTgbotAdmins(msg)
}
