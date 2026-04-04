package job

import (
	"encoding/json"
	"html"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mhsanaei/3x-ui/v2/logger"
	"github.com/mhsanaei/3x-ui/v2/web/service"
)

// pingOutcome is one outbound's last result on this process (each node keeps its own memory).
type pingOutcome struct {
	ok      bool
	delayMs int64
}

// outboundPingState holds the previous run per tag in RAM only (not DB); safe for multi-node — each server is separate.
var outboundPingState = struct {
	mu   sync.Mutex
	last map[string]pingOutcome
}{
	last: make(map[string]pingOutcome),
}

// OutboundPingNotifyJob checks all Xray outbounds on a schedule, keeps last results in memory on this host, and notifies Telegram on up/down changes.
type OutboundPingNotifyJob struct {
	settingService  service.SettingService
	outboundService service.OutboundService
	tgbotService    service.Tgbot
}

// NewOutboundPingNotifyJob creates a new outbound ping notification job.
func NewOutboundPingNotifyJob() *OutboundPingNotifyJob {
	return new(OutboundPingNotifyJob)
}

func formatPingValue(ok bool, delayMs int64) string {
	if !ok {
		return "NO"
	}
	if delayMs < 0 {
		delayMs = 0
	}
	return strconv.FormatInt(delayMs, 10) + " ms"
}

func (j *OutboundPingNotifyJob) resolveHostLabel() string {
	host, err := os.Hostname()
	if err != nil || strings.TrimSpace(host) == "" {
		host = ""
	}
	if host != "" {
		return host
	}
	if dom, err := j.settingService.GetWebDomain(); err == nil && strings.TrimSpace(dom) != "" {
		return strings.TrimSpace(dom)
	}
	return "unknown"
}

// Run tests each testable outbound (two attempts when the first fails), compares to in-memory state, and alerts on reachability changes.
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

	next := make(map[string]pingOutcome, len(rawOutbounds))
	var changeTags []string
	var changeOld []string
	var changeNew []string

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

		okPing, delayMs, _ := j.outboundService.CheckOutboundPingWithRetry(string(oneJSON), testURL, allStr)
		next[tag] = pingOutcome{ok: okPing, delayMs: delayMs}
	}

	// First pass: identify tags whose status changed from the previous run.
	outboundPingState.mu.Lock()
	prev := outboundPingState.last
	type candidate struct {
		tag         string
		outboundJSON string
		prevOk      bool
		prevDelay   int64
		firstOk     bool
		firstDelay  int64
	}
	var candidates []candidate
	for tag, cur := range next {
		p, had := prev[tag]
		if !had {
			continue
		}
		if p.ok == cur.ok {
			continue
		}
		candidates = append(candidates, candidate{
			tag: tag, prevOk: p.ok, prevDelay: p.delayMs,
			firstOk: cur.ok, firstDelay: cur.delayMs,
		})
	}
	outboundPingState.mu.Unlock()

	if len(candidates) == 0 {
		outboundPingState.mu.Lock()
		outboundPingState.last = next
		outboundPingState.mu.Unlock()
		return
	}

	// Populate outbound JSON for each candidate so the confirmation pass can re-test them.
	for _, ob := range rawOutbounds {
		obMap, ok := ob.(map[string]any)
		if !ok {
			continue
		}
		tag, _ := obMap["tag"].(string)
		if tag == "" {
			continue
		}
		for i := range candidates {
			if candidates[i].tag == tag {
				oneJSON, err := json.Marshal(obMap)
				if err == nil {
					candidates[i].outboundJSON = string(oneJSON)
				}
				break
			}
		}
	}

	// Second pass (confirmation): re-check only the changed tags after a delay.
	logger.Info("outbound ping job: confirming ", len(candidates), " changed tag(s)…")
	time.Sleep(5 * time.Second)

	for i := range candidates {
		c := &candidates[i]
		if c.outboundJSON == "" {
			continue
		}
		okPing, delayMs, _ := j.outboundService.CheckOutboundPingWithRetry(c.outboundJSON, testURL, allStr)
		// Update next with the confirmation result so stored state reflects the latest.
		next[c.tag] = pingOutcome{ok: okPing, delayMs: delayMs}
		// If the confirmation disagrees with the first check (transient blip), drop this candidate.
		if okPing != c.firstOk {
			logger.Info("outbound ping job: tag ", c.tag, " reverted on confirmation, skipping alert")
			c.tag = "" // mark as dropped
		}
	}

	// Build final change lists from confirmed candidates only.
	for _, c := range candidates {
		if c.tag == "" {
			continue
		}
		confirmed := next[c.tag]
		changeTags = append(changeTags, c.tag)
		changeOld = append(changeOld, formatPingValue(c.prevOk, c.prevDelay))
		changeNew = append(changeNew, formatPingValue(confirmed.ok, confirmed.delayMs))
	}

	outboundPingState.mu.Lock()
	outboundPingState.last = next
	outboundPingState.mu.Unlock()

	if len(changeTags) == 0 {
		logger.Info("outbound ping job: all changes reverted on confirmation, no alert sent")
		return
	}

	loc, _ := j.settingService.GetTimeLocation()
	if loc == nil {
		loc = time.Local
	}
	timeStr := time.Now().In(loc).Format("2006-01-02 15:04:05")
	hostEsc := html.EscapeString(j.resolveHostLabel())

	var b strings.Builder
	b.WriteString(j.tgbotService.I18nBot("tgbot.messages.outboundPingHeader",
		"Hostname=="+hostEsc,
		"Time=="+timeStr,
	))
	for i := range changeTags {
		tagEsc := html.EscapeString(changeTags[i])
		line := j.tgbotService.I18nBot("tgbot.messages.outboundPingLine",
			"Tag=="+tagEsc,
			"Old=="+changeOld[i],
			"New=="+changeNew[i],
		)
		b.WriteString(line)
	}
	b.WriteString(j.tgbotService.I18nBot("tgbot.messages.outboundPingFooter"))
	j.tgbotService.SendMsgToTgbotAdmins(b.String())
}
