package service

import (
	"bufio"
	"encoding/json"
	"io"
	"net"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/mhsanaei/3x-ui/v2/database"
	"github.com/mhsanaei/3x-ui/v2/database/model"
	"github.com/mhsanaei/3x-ui/v2/xray"
)

type ActivityNamedCount struct {
	Name  string `json:"name"`
	Count int64  `json:"count"`
}

type ActivityUserRank struct {
	Email         string `json:"email"`
	ActivityCount int64  `json:"activityCount"`
}

type ActivityTrafficRank struct {
	Email string `json:"email"`
	Up    int64  `json:"up"`
	Down  int64  `json:"down"`
	Total int64  `json:"total"`
}

type ActivityAccessLogEntry struct {
	Ts       int64  `json:"ts"`
	From     string `json:"from"`
	To       string `json:"to"`
	Email    string `json:"email"`
	Inbound  string `json:"inbound"`
	Outbound string `json:"outbound"`
	Event    string `json:"event"`
}

type ActivityStatusOverview struct {
	WindowHours        int                      `json:"windowHours"`
	WindowLabel        string                   `json:"windowLabel"`
	TotalActivityRows  int64                    `json:"totalActivityRows"`
	DistinctClients    int                      `json:"distinctClients"`
	TopDestHostnames   []ActivityNamedCount     `json:"topDestHostnames"`
	TopDestIPs         []ActivityNamedCount     `json:"topDestIps"`
	TopClientSourceIPs []ActivityNamedCount     `json:"topClientSourceIps"`
	TopUsersByActivity []ActivityUserRank       `json:"topUsersByActivity"`
	TopUsersByTraffic  []ActivityTrafficRank    `json:"topUsersByTraffic"`
	RecentAccessLog    []ActivityAccessLogEntry `json:"recentAccessLog"`
	// PanelStored24h is read from the panel database (rolling 24h snapshot refreshed by a background job).
	PanelStored24h *PanelStored24hInsight `json:"panelStored24h"`
}

// PanelStored24hInsight is the API shape for DB-backed 24h request totals and hostname rankings.
type PanelStored24hInsight struct {
	UpdatedAt        int64                `json:"updatedAt"`
	TotalRequests24h int64                `json:"totalRequests24h"`
	TopDestHostnames []ActivityNamedCount `json:"topDestHostnames"`
	TopDestIPs       []ActivityNamedCount `json:"topDestIps"`
}

func activityLogFreedomBlackholes() (freedoms, blackholes []string) {
	var settingService SettingService
	tpl, terr := settingService.GetXrayConfigTemplate()
	if terr != nil || tpl == "" {
		return nil, nil
	}
	var cfgMap map[string]any
	if err := json.Unmarshal([]byte(tpl), &cfgMap); err != nil {
		return nil, nil
	}
	return FreedomBlackholeTagsFromDefaultXrayConfig(cfgMap)
}

func hostFromActivityAddr(s string) string {
	s = strings.TrimSpace(strings.TrimPrefix(s, "/"))
	for _, p := range []string{"tcp:", "udp:"} {
		if strings.HasPrefix(s, p) {
			s = s[len(p):]
			break
		}
	}
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, "[") {
		if i := strings.IndexByte(s, ']'); i > 0 {
			return s[1:i]
		}
	}
	i := strings.LastIndex(s, ":")
	if i <= 0 {
		return s
	}
	port := s[i+1:]
	isDigits := len(port) > 0 && len(port) <= 5
	for _, c := range port {
		if c < '0' || c > '9' {
			isDigits = false
			break
		}
	}
	if isDigits {
		return s[:i]
	}
	return s
}

func topNamedCounts(m map[string]int64, limit int) []ActivityNamedCount {
	if len(m) == 0 {
		return []ActivityNamedCount{}
	}
	type kv struct {
		k string
		v int64
	}
	ss := make([]kv, 0, len(m))
	for k, v := range m {
		if k == "" {
			continue
		}
		ss = append(ss, kv{k, v})
	}
	sort.Slice(ss, func(i, j int) bool {
		if ss[i].v == ss[j].v {
			return ss[i].k < ss[j].k
		}
		return ss[i].v > ss[j].v
	})
	if limit > 0 && len(ss) > limit {
		ss = ss[:limit]
	}
	out := make([]ActivityNamedCount, len(ss))
	for i := range ss {
		out[i] = ActivityNamedCount{Name: ss[i].k, Count: ss[i].v}
	}
	return out
}

const (
	accessLogOverviewMaxTail   = 48 << 20
	accessLogSnapshotMaxTail   = 256 << 20
	recentAccessLogLimit       = 500
	topDestAggLimit            = 40
	panelHostnameSnapshotLimit = 120
)

func (s *InboundService) GetActivityStatusOverview(hours int) (*ActivityStatusOverview, error) {
	db := database.GetDB()
	var since int64
	var label string
	switch {
	case hours <= 0:
		hours = 0
		label = "all time"
	case hours <= 24:
		since = time.Now().Add(-time.Duration(hours) * time.Hour).Unix()
		label = "last 24 hours"
	case hours <= 24*7:
		since = time.Now().Add(-time.Duration(hours) * time.Hour).Unix()
		label = "last 7 days"
	case hours <= 24*31:
		since = time.Now().Add(-time.Duration(hours) * time.Hour).Unix()
		label = "last 30 days"
	default:
		since = time.Now().Add(-time.Duration(hours) * time.Hour).Unix()
		label = "selected window"
	}

	out := &ActivityStatusOverview{
		WindowHours:        hours,
		WindowLabel:        label,
		TopDestHostnames:   []ActivityNamedCount{},
		TopDestIPs:         []ActivityNamedCount{},
		TopClientSourceIPs: []ActivityNamedCount{},
		TopUsersByActivity: []ActivityUserRank{},
		TopUsersByTraffic:  []ActivityTrafficRank{},
		RecentAccessLog:    []ActivityAccessLogEntry{},
	}

	fillActivityOverviewFromAccessLog(out, since)

	var traffics []xray.ClientTraffic
	if err := db.Model(&xray.ClientTraffic{}).
		Where("email != ? AND email IS NOT NULL", "").
		Order("up + down DESC").
		Limit(25).
		Find(&traffics).Error; err != nil {
		return nil, err
	}
	for _, t := range traffics {
		if t.Email == "" {
			continue
		}
		out.TopUsersByTraffic = append(out.TopUsersByTraffic, ActivityTrafficRank{
			Email: t.Email,
			Up:    t.Up,
			Down:  t.Down,
			Total: t.Up + t.Down,
		})
	}

	out.PanelStored24h = s.panelStored24hFromDB()
	return out, nil
}

// aggregateAccessLogLast24hDestinations scans the access log tail and counts rows in the last 24h.
// Blocked (blackhole) lines are excluded from totals and destination aggregates.
// If there is no access log path or file, ok is false and err is nil.
func aggregateAccessLogLast24hDestinations(maxTail int64) (total int64, hostAgg map[string]int64, ipAgg map[string]int64, ok bool, err error) {
	sinceUnix := time.Now().Add(-24 * time.Hour).Unix()
	path, perr := xray.GetAccessLogPath()
	if perr != nil || path == "" || path == "none" {
		return 0, nil, nil, false, nil
	}
	if _, statErr := os.Stat(path); statErr != nil {
		return 0, nil, nil, false, nil
	}

	freedoms, blackholes := activityLogFreedomBlackholes()
	hostAgg = make(map[string]int64)
	ipAgg = make(map[string]int64)
	f, err := os.Open(path)
	if err != nil {
		return 0, nil, nil, false, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return 0, nil, nil, false, err
	}
	size := st.Size()
	start := int64(0)
	if size > maxTail {
		start = size - maxTail
	}
	if _, err = f.Seek(start, io.SeekStart); err != nil {
		return 0, nil, nil, false, err
	}
	br := bufio.NewReader(f)
	if start > 0 {
		_, _ = br.ReadString('\n')
	}

	for {
		line, rerr := br.ReadString('\n')
		line = strings.TrimSpace(strings.TrimSuffix(line, "\n"))
		if line != "" && !strings.Contains(line, "api -> api") {
			entry, parsed := ParseXrayAccessLogLine(line)
			if parsed {
				ts := entry.DateTime.Unix()
				if ts >= sinceUnix && AccessLogEventKind(line, freedoms, blackholes) != "blocked" {
					total++
					if h := hostFromActivityAddr(entry.ToAddress); h != "" {
						if net.ParseIP(h) != nil {
							ipAgg[h]++
						} else {
							hostAgg[h]++
						}
					}
				}
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return 0, nil, nil, false, rerr
		}
	}
	return total, hostAgg, ipAgg, true, nil
}

// RefreshNetworkInsightsPanel24h recomputes 24h totals and destination ranks from the access log and stores them in the database.
func (s *InboundService) RefreshNetworkInsightsPanel24h() error {
	total, hostAgg, ipAgg, ok, err := aggregateAccessLogLast24hDestinations(accessLogSnapshotMaxTail)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	topHosts := topNamedCounts(hostAgg, panelHostnameSnapshotLimit)
	topIPs := topNamedCounts(ipAgg, panelHostnameSnapshotLimit)
	rawHosts, err := json.Marshal(topHosts)
	if err != nil {
		return err
	}
	rawIPs, err := json.Marshal(topIPs)
	if err != nil {
		return err
	}
	db := database.GetDB()
	row := model.NetworkInsightsSnapshot{Id: 1}
	if err := db.FirstOrCreate(&row, model.NetworkInsightsSnapshot{Id: 1}).Error; err != nil {
		return err
	}
	return db.Model(&row).Updates(map[string]any{
		"updated_at_unix":    time.Now().Unix(),
		"total_requests_24h": total,
		"top_hostnames_json": string(rawHosts),
		"top_ips_json":       string(rawIPs),
	}).Error
}

func (s *InboundService) panelStored24hFromDB() *PanelStored24hInsight {
	db := database.GetDB()
	var row model.NetworkInsightsSnapshot
	if err := db.First(&row, 1).Error; err != nil {
		return nil
	}
	var hosts []ActivityNamedCount
	if row.TopHostnamesJSON != "" {
		_ = json.Unmarshal([]byte(row.TopHostnamesJSON), &hosts)
	}
	var ips []ActivityNamedCount
	if row.TopIpsJSON != "" {
		_ = json.Unmarshal([]byte(row.TopIpsJSON), &ips)
	}
	return &PanelStored24hInsight{
		UpdatedAt:        row.UpdatedAtUnix,
		TotalRequests24h: row.TotalRequests24h,
		TopDestHostnames: hosts,
		TopDestIPs:       ips,
	}
}

func fillActivityOverviewFromAccessLog(out *ActivityStatusOverview, sinceUnix int64) {
	path, err := xray.GetAccessLogPath()
	if err != nil || path == "" || path == "none" {
		return
	}
	if _, err := os.Stat(path); err != nil {
		return
	}

	freedoms, blackholes := activityLogFreedomBlackholes()

	hostAgg := make(map[string]int64)
	ipAgg := make(map[string]int64)
	fromAgg := make(map[string]int64)
	userAgg := make(map[string]int64)
	uniqueEmails := make(map[string]struct{})
	recentEntries := make([]ActivityAccessLogEntry, 0, recentAccessLogLimit)

	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return
	}
	size := st.Size()
	start := int64(0)
	if size > accessLogOverviewMaxTail {
		start = size - accessLogOverviewMaxTail
	}
	if _, err = f.Seek(start, io.SeekStart); err != nil {
		return
	}
	br := bufio.NewReader(f)
	if start > 0 {
		_, _ = br.ReadString('\n')
	}

	var total int64
	for {
		line, rerr := br.ReadString('\n')
		line = strings.TrimSpace(strings.TrimSuffix(line, "\n"))
		if line != "" && !strings.Contains(line, "api -> api") {
			entry, ok := ParseXrayAccessLogLine(line)
			if ok {
				ts := entry.DateTime.Unix()
				if sinceUnix <= 0 || ts >= sinceUnix {
					total++
					if AccessLogEventKind(line, freedoms, blackholes) != "blocked" {
						if h := hostFromActivityAddr(entry.ToAddress); h != "" {
							if net.ParseIP(h) != nil {
								ipAgg[h]++
							} else {
								hostAgg[h]++
							}
						}
					}
					if fh := hostFromActivityAddr(entry.FromAddress); fh != "" {
						fromAgg[fh]++
					}
					if entry.Email != "" {
						userAgg[entry.Email]++
						uniqueEmails[entry.Email] = struct{}{}
					}
					recentEntries = append(recentEntries, ActivityAccessLogEntry{
						Ts:       ts,
						From:     entry.FromAddress,
						To:       entry.ToAddress,
						Email:    entry.Email,
						Inbound:  entry.Inbound,
						Outbound: entry.Outbound,
						Event:    AccessLogEventKind(line, freedoms, blackholes),
					})
				}
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			break
		}
	}

	if len(recentEntries) > recentAccessLogLimit {
		recentEntries = recentEntries[len(recentEntries)-recentAccessLogLimit:]
	}
	for i, j := 0, len(recentEntries)-1; i < j; i, j = i+1, j-1 {
		recentEntries[i], recentEntries[j] = recentEntries[j], recentEntries[i]
	}

	out.TotalActivityRows = total
	out.DistinctClients = len(uniqueEmails)
	out.TopDestHostnames = topNamedCounts(hostAgg, topDestAggLimit)
	out.TopDestIPs = topNamedCounts(ipAgg, topDestAggLimit)
	out.TopClientSourceIPs = topNamedCounts(fromAgg, topDestAggLimit)
	out.RecentAccessLog = recentEntries

	type rank struct {
		email string
		cnt   int64
	}
	ranks := make([]rank, 0, len(userAgg))
	for e, c := range userAgg {
		if e == "" {
			continue
		}
		ranks = append(ranks, rank{e, c})
	}
	sort.Slice(ranks, func(i, j int) bool {
		if ranks[i].cnt == ranks[j].cnt {
			return ranks[i].email < ranks[j].email
		}
		return ranks[i].cnt > ranks[j].cnt
	})
	out.TopUsersByActivity = make([]ActivityUserRank, 0, 25)
	for i := range ranks {
		if i >= 25 {
			break
		}
		out.TopUsersByActivity = append(out.TopUsersByActivity, ActivityUserRank{
			Email:         ranks[i].email,
			ActivityCount: ranks[i].cnt,
		})
	}
}
