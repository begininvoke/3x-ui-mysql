//go:build !linux

package sys

import (
	"strings"

	"github.com/shirou/gopsutil/v4/net"
)

// GetTCPConnectionStats returns the number of TCP connections and counts grouped by state
// using a single enumeration (avoid calling net.Connections twice per refresh).
func GetTCPConnectionStats() (int, map[string]int, error) {
	stats, err := net.Connections("tcp")
	if err != nil {
		return 0, nil, err
	}
	out := make(map[string]int)
	for _, s := range stats {
		st := strings.TrimSpace(s.Status)
		if st == "" {
			st = "NONE"
		}
		out[st]++
	}
	return len(stats), out, nil
}

// GetTCPCount returns the number of active TCP connections.
func GetTCPCount() (int, error) {
	n, _, err := GetTCPConnectionStats()
	return n, err
}

// GetTCPCountByState returns counts of TCP sockets grouped by state.
func GetTCPCountByState() (map[string]int, error) {
	_, m, err := GetTCPConnectionStats()
	return m, err
}
