// Package service contains Xray access log parsing shared by log viewers and activity capture.
package service

import (
	"strings"
	"time"
)

// ParseXrayAccessLogLine parses one non-empty Xray access.log line into LogEntry fields.
// It returns ok=false for lines that cannot be parsed (including empty and internal api lines).
func ParseXrayAccessLogLine(line string) (LogEntry, bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.Contains(line, "api -> api") {
		return LogEntry{}, false
	}
	parts := strings.Fields(line)
	if len(parts) < 3 {
		return LogEntry{}, false
	}
	dateTime, err := time.ParseInLocation("2006/01/02 15:04:05.999999", parts[0]+" "+parts[1], time.Local)
	if err != nil {
		dateTime, err = time.ParseInLocation("2006/01/02 15:04:05", parts[0]+" "+parts[1], time.Local)
		if err != nil {
			return LogEntry{}, false
		}
	}
	var entry LogEntry
	entry.DateTime = dateTime.UTC()
	for i, part := range parts {
		switch {
		case part == "from" && i+1 < len(parts):
			entry.FromAddress = strings.TrimLeft(parts[i+1], "/")
		case part == "accepted" && i+1 < len(parts):
			entry.ToAddress = strings.TrimLeft(parts[i+1], "/")
		case strings.HasPrefix(part, "["):
			entry.Inbound = part[1:]
		case strings.HasSuffix(part, "]"):
			entry.Outbound = part[:len(part)-1]
		case part == "email:" && i+1 < len(parts):
			entry.Email = parts[i+1]
		}
	}
	return entry, true
}

// AccessLogEventKind classifies a raw access log line as direct, blocked, or proxied using outbound tags.
func AccessLogEventKind(line string, freedoms, blackholes []string) string {
	if logEntryContains(line, freedoms) {
		return "direct"
	}
	if logEntryContains(line, blackholes) {
		return "blocked"
	}
	return "proxied"
}

// FreedomBlackholeTagsFromDefaultXrayConfig extracts freedom and blackhole outbound tags from panel default Xray JSON.
func FreedomBlackholeTagsFromDefaultXrayConfig(cfg any) (freedoms, blackholes []string) {
	if cfg == nil {
		return nil, nil
	}
	cfgMap, ok := cfg.(map[string]any)
	if !ok {
		return nil, nil
	}
	outbounds, ok := cfgMap["outbounds"].([]any)
	if !ok {
		return nil, nil
	}
	for _, outbound := range outbounds {
		obMap, ok := outbound.(map[string]any)
		if !ok {
			continue
		}
		switch obMap["protocol"] {
		case "freedom":
			if tag, ok := obMap["tag"].(string); ok {
				freedoms = append(freedoms, tag)
			}
		case "blackhole":
			if tag, ok := obMap["tag"].(string); ok {
				blackholes = append(blackholes, tag)
			}
		}
	}
	return freedoms, blackholes
}
