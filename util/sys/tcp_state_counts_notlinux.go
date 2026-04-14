//go:build !linux

package sys

import (
	"strings"

	"github.com/shirou/gopsutil/v4/net"
)

// GetTCPCountByState returns counts of TCP sockets grouped by state (e.g. ESTABLISHED, TIME_WAIT).
func GetTCPCountByState() (map[string]int, error) {
	stats, err := net.Connections("tcp")
	if err != nil {
		return nil, err
	}
	out := make(map[string]int)
	for _, s := range stats {
		st := strings.TrimSpace(s.Status)
		if st == "" {
			st = "NONE"
		}
		out[st]++
	}
	return out, nil
}
