package service

import (
	"bufio"
	"encoding/json"
	"io"
	"net"
	"os"
	"sort"
	"strings"

	"github.com/mhsanaei/3x-ui/v2/database/model"
	"github.com/mhsanaei/3x-ui/v2/xray"
)

const (
	accessLogOverviewMaxTail = 48 << 20 // tail scan for dashboard aggregates
	accessLogClientMaxTail   = 64 << 20 // tail scan when loading one client's log lines
)

// TryFillActivityOverviewFromAccessLog fills overview activity fields from the Xray access log
// tail, including all clients that appear in the log (not limited to activity capture).
// Returns true if the access log was read successfully; false to fall back to DB aggregates.
func TryFillActivityOverviewFromAccessLog(out *ActivityStatusOverview, sinceUnix int64) bool {
	path, err := xray.GetAccessLogPath()
	if err != nil || path == "" || path == "none" {
		return false
	}
	if _, err := os.Stat(path); err != nil {
		return false
	}
	total, distinct, hostAgg, ipAgg, fromAgg, userAgg, err := aggregateAccessLogOverview(path, accessLogOverviewMaxTail, sinceUnix)
	if err != nil {
		return false
	}
	out.TotalActivityRows = total
	out.DistinctClients = distinct
	out.TopDestHostnames = topNamedCounts(hostAgg, 30)
	out.TopDestIPs = topNamedCounts(ipAgg, 30)
	out.TopClientSourceIPs = topNamedCounts(fromAgg, 30)

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
	return true
}

func aggregateAccessLogOverview(path string, maxTail int64, sinceUnix int64) (
	total int64,
	distinct int,
	hostAgg map[string]int64,
	ipAgg map[string]int64,
	fromAgg map[string]int64,
	userAgg map[string]int64,
	err error,
) {
	hostAgg = make(map[string]int64)
	ipAgg = make(map[string]int64)
	fromAgg = make(map[string]int64)
	userAgg = make(map[string]int64)
	uniqueEmails := make(map[string]struct{})

	f, err := os.Open(path)
	if err != nil {
		return 0, 0, nil, nil, nil, nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return 0, 0, nil, nil, nil, nil, err
	}
	size := st.Size()
	start := int64(0)
	if size > maxTail {
		start = size - maxTail
	}
	if _, err = f.Seek(start, io.SeekStart); err != nil {
		return 0, 0, nil, nil, nil, nil, err
	}
	br := bufio.NewReader(f)
	if start > 0 {
		_, _ = br.ReadString('\n')
	}
	for {
		line, rerr := br.ReadString('\n')
		line = strings.TrimSpace(strings.TrimSuffix(line, "\n"))
		if line != "" && !strings.Contains(line, "api -> api") {
			entry, ok := ParseXrayAccessLogLine(line)
			if ok {
				ts := entry.DateTime.Unix()
				if sinceUnix <= 0 || ts >= sinceUnix {
					total++
					if h := hostFromActivityAddr(entry.ToAddress); h != "" {
						if net.ParseIP(h) != nil {
							ipAgg[h]++
						} else {
							hostAgg[h]++
						}
					}
					if fh := hostFromActivityAddr(entry.FromAddress); fh != "" {
						fromAgg[fh]++
					}
					if entry.Email != "" {
						userAgg[entry.Email]++
						uniqueEmails[entry.Email] = struct{}{}
					}
				}
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return total, len(uniqueEmails), hostAgg, ipAgg, fromAgg, userAgg, rerr
		}
	}
	return total, len(uniqueEmails), hostAgg, ipAgg, fromAgg, userAgg, nil
}

// ClientActivitiesFromAccessLogTail returns recent access-log rows for one email by scanning
// the log tail (used when the DB has no rows yet but activity capture is enabled).
func ClientActivitiesFromAccessLogTail(email string, limit int) ([]model.ClientActivity, error) {
	if email == "" || limit <= 0 {
		return nil, nil
	}
	if limit > 500 {
		limit = 500
	}
	path, err := xray.GetAccessLogPath()
	if err != nil || path == "" || path == "none" {
		return nil, err
	}
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}

	var settingService SettingService
	tpl, terr := settingService.GetXrayConfigTemplate()
	var cfgMap map[string]any
	if terr == nil && tpl != "" {
		_ = json.Unmarshal([]byte(tpl), &cfgMap)
	}
	freedoms, blackholes := FreedomBlackholeTagsFromDefaultXrayConfig(cfgMap)

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	size := st.Size()
	start := int64(0)
	maxTail := int64(accessLogClientMaxTail)
	if size > maxTail {
		start = size - maxTail
	}
	if _, err = f.Seek(start, io.SeekStart); err != nil {
		return nil, err
	}
	br := bufio.NewReader(f)
	if start > 0 {
		_, _ = br.ReadString('\n')
	}

	var rows []model.ClientActivity
	for {
		line, rerr := br.ReadString('\n')
		line = strings.TrimSpace(strings.TrimSuffix(line, "\n"))
		if line != "" && !strings.Contains(line, "api -> api") {
			entry, ok := ParseXrayAccessLogLine(line)
			if ok && entry.Email == email {
				rows = append(rows, model.ClientActivity{
					ClientEmail: entry.Email,
					Ts:          entry.DateTime.Unix(),
					FromAddr:    entry.FromAddress,
					ToAddr:      entry.ToAddress,
					InboundTag:  entry.Inbound,
					OutboundTag: entry.Outbound,
					Event:       AccessLogEventKind(line, freedoms, blackholes),
				})
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return nil, rerr
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Ts > rows[j].Ts })
	if len(rows) > limit {
		rows = rows[:limit]
	}
	return rows, nil
}
