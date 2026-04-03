package service

import (
	"net"
	"sort"
	"strings"
	"time"

	"github.com/mhsanaei/3x-ui/v2/database"
	"github.com/mhsanaei/3x-ui/v2/database/model"
	"github.com/mhsanaei/3x-ui/v2/xray"
)

// ActivityNamedCount is a label with an occurrence count.
type ActivityNamedCount struct {
	Name  string `json:"name"`
	Count int64  `json:"count"`
}

// ActivityUserRank ranks a client by stored activity rows in the time window.
type ActivityUserRank struct {
	Email         string `json:"email"`
	ActivityCount int64  `json:"activityCount"`
}

// ActivityTrafficRank ranks a client by recorded upload+download bytes.
type ActivityTrafficRank struct {
	Email string `json:"email"`
	Up    int64  `json:"up"`
	Down  int64  `json:"down"`
	Total int64  `json:"total"`
}

// ActivityStatusOverview aggregates captured access-log rows for the status dashboard.
type ActivityStatusOverview struct {
	WindowHours        int                   `json:"windowHours"`
	WindowLabel        string                `json:"windowLabel"`
	TotalActivityRows  int64                 `json:"totalActivityRows"`
	DistinctClients    int                   `json:"distinctClients"`
	TopDestHostnames   []ActivityNamedCount  `json:"topDestHostnames"`
	TopDestIPs         []ActivityNamedCount  `json:"topDestIps"`
	TopClientSourceIPs []ActivityNamedCount  `json:"topClientSourceIps"`
	TopUsersByActivity []ActivityUserRank    `json:"topUsersByActivity"`
	TopUsersByTraffic  []ActivityTrafficRank `json:"topUsersByTraffic"`
}

// hostFromActivityAddr extracts host or IP from Xray access "to" / "from" style strings.
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

// GetActivityStatusOverview builds dashboard aggregates from client_activities and client traffic.
// hours <= 0 means no time filter (all stored rows).
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
	}

	// Prefer live access-log tail so Network insights reflects all clients in the log, not only
	// rows stored for activity-capture users. Falls back to DB if the log is missing/unreadable.
	if !TryFillActivityOverviewFromAccessLog(out, since) {
		qBase := db.Model(&model.ClientActivity{})
		if since > 0 {
			qBase = qBase.Where("ts >= ?", since)
		}
		if err := qBase.Count(&out.TotalActivityRows).Error; err != nil {
			return nil, err
		}

		var emails []string
		qDistinct := db.Model(&model.ClientActivity{}).Distinct("client_email")
		if since > 0 {
			qDistinct = qDistinct.Where("ts >= ?", since)
		}
		if err := qDistinct.Pluck("client_email", &emails).Error; err != nil {
			return nil, err
		}
		out.DistinctClients = len(emails)

		var toRows []struct {
			ToAddr string `gorm:"column:to_addr"`
			Cnt    int64  `gorm:"column:cnt"`
		}
		qTo := db.Model(&model.ClientActivity{}).Select("to_addr, COUNT(*) as cnt")
		if since > 0 {
			qTo = qTo.Where("ts >= ?", since)
		}
		if err := qTo.Group("to_addr").Order("cnt DESC").Limit(500).Scan(&toRows).Error; err != nil {
			return nil, err
		}
		hostAgg := make(map[string]int64)
		ipAgg := make(map[string]int64)
		for _, r := range toRows {
			h := hostFromActivityAddr(r.ToAddr)
			if h == "" {
				continue
			}
			if net.ParseIP(h) != nil {
				ipAgg[h] += r.Cnt
			} else {
				hostAgg[h] += r.Cnt
			}
		}
		out.TopDestHostnames = topNamedCounts(hostAgg, 30)
		out.TopDestIPs = topNamedCounts(ipAgg, 30)

		var fromRows []struct {
			FromAddr string `gorm:"column:from_addr"`
			Cnt      int64  `gorm:"column:cnt"`
		}
		qFrom := db.Model(&model.ClientActivity{}).Select("from_addr, COUNT(*) as cnt")
		if since > 0 {
			qFrom = qFrom.Where("ts >= ?", since)
		}
		if err := qFrom.Group("from_addr").Order("cnt DESC").Limit(400).Scan(&fromRows).Error; err != nil {
			return nil, err
		}
		fromAgg := make(map[string]int64)
		for _, r := range fromRows {
			h := hostFromActivityAddr(r.FromAddr)
			if h == "" {
				continue
			}
			fromAgg[h] += r.Cnt
		}
		out.TopClientSourceIPs = topNamedCounts(fromAgg, 30)

		var userRows []struct {
			Email string `gorm:"column:client_email"`
			Cnt   int64  `gorm:"column:cnt"`
		}
		qUser := db.Model(&model.ClientActivity{}).Select("client_email, COUNT(*) as cnt")
		if since > 0 {
			qUser = qUser.Where("ts >= ?", since)
		}
		if err := qUser.Group("client_email").Order("cnt DESC").Limit(25).Scan(&userRows).Error; err != nil {
			return nil, err
		}
		for _, r := range userRows {
			if r.Email == "" {
				continue
			}
			out.TopUsersByActivity = append(out.TopUsersByActivity, ActivityUserRank{
				Email:         r.Email,
				ActivityCount: r.Cnt,
			})
		}
	}

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

	return out, nil
}
