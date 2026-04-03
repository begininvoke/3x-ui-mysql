package database

import (
	"context"
	"sync"
	"time"

	"gorm.io/gorm/logger"
)

// QueryLogEntry is one executed SQL statement recorded for the admin panel (MySQL only).
type QueryLogEntry struct {
	At         string  `json:"at"`
	DurationMs float64 `json:"durationMs"`
	Rows       int64   `json:"rows"`
	SQL        string  `json:"sql"`
	Err        string  `json:"err,omitempty"`
}

const defaultQueryLogCap = 100
const maxQueryLogCap = 500
const maxSQLChars = 4000

var globalQueryLog = &queryLogRing{}

type queryLogRing struct {
	mu   sync.RWMutex
	cap  int
	buf  []QueryLogEntry
	head int
	n    int
}

func (r *queryLogRing) configure(cap int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if cap < 1 {
		cap = defaultQueryLogCap
	}
	if cap > maxQueryLogCap {
		cap = maxQueryLogCap
	}
	r.cap = cap
	r.buf = nil
	r.head = 0
	r.n = 0
}

func (r *queryLogRing) append(e QueryLogEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cap <= 0 {
		return
	}
	if len(r.buf) != r.cap {
		r.buf = make([]QueryLogEntry, r.cap)
		r.head = 0
		r.n = 0
	}
	if r.n < r.cap {
		r.buf[(r.head+r.n)%r.cap] = e
		r.n++
		return
	}
	r.buf[r.head] = e
	r.head = (r.head + 1) % r.cap
}

func (r *queryLogRing) snapshotNewestFirst(limit int) []QueryLogEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.n == 0 || limit <= 0 {
		return nil
	}
	if limit > r.n {
		limit = r.n
	}
	out := make([]QueryLogEntry, 0, limit)
	for i := 0; i < limit; i++ {
		idx := (r.head + r.n - 1 - i + r.cap) % r.cap
		out = append(out, r.buf[idx])
	}
	return out
}

func truncateSQLString(s string) string {
	runes := []rune(s)
	if len(runes) <= maxSQLChars {
		return s
	}
	return string(runes[:maxSQLChars]) + "…"
}

// InitMySQLQueryLog configures the in-memory query ring used when backend is MySQL.
func InitMySQLQueryLog(maxEntries int) {
	globalQueryLog.configure(maxEntries)
}

func recordGORMQuery(sql string, rows int64, elapsed time.Duration, err error) {
	var errStr string
	if err != nil {
		errStr = err.Error()
	}
	globalQueryLog.append(QueryLogEntry{
		At:         time.Now().Format(time.RFC3339Nano),
		DurationMs: float64(elapsed.Microseconds()) / 1000.0,
		Rows:       rows,
		SQL:        truncateSQLString(sql),
		Err:        errStr,
	})
}

// QueryLogSnapshot returns the newest queries (up to limit) for the admin API.
func QueryLogSnapshot(limit int) []QueryLogEntry {
	if limit <= 0 {
		limit = 50
	}
	if limit > maxQueryLogCap {
		limit = maxQueryLogCap
	}
	return globalQueryLog.snapshotNewestFirst(limit)
}

// queryRecordingLogger wraps a GORM logger and records Trace data for the panel.
type queryRecordingLogger struct {
	level logger.LogLevel
	inner logger.Interface
}

func newQueryRecordingLogger(inner logger.Interface) *queryRecordingLogger {
	return &queryRecordingLogger{
		level: logger.Silent,
		inner: inner,
	}
}

func (l *queryRecordingLogger) LogMode(level logger.LogLevel) logger.Interface {
	nl := *l
	nl.level = level
	if nl.inner != nil {
		nl.inner = nl.inner.LogMode(level)
	}
	return &nl
}

func (l *queryRecordingLogger) Info(ctx context.Context, msg string, data ...interface{}) {
	if l.inner != nil {
		l.inner.Info(ctx, msg, data...)
	}
}

func (l *queryRecordingLogger) Warn(ctx context.Context, msg string, data ...interface{}) {
	if l.inner != nil {
		l.inner.Warn(ctx, msg, data...)
	}
}

func (l *queryRecordingLogger) Error(ctx context.Context, msg string, data ...interface{}) {
	if l.inner != nil {
		l.inner.Error(ctx, msg, data...)
	}
}

func (l *queryRecordingLogger) Trace(ctx context.Context, begin time.Time, fc func() (sql string, rowsAffected int64), err error) {
	sqlStr, rows := fc()
	elapsed := time.Since(begin)
	recordGORMQuery(sqlStr, rows, elapsed, err)
	if l.inner != nil && l.level >= logger.Info {
		l.inner.Trace(ctx, begin, func() (string, int64) { return sqlStr, rows }, err)
	}
}
