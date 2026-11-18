package collector

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/SammyLin/openclaw-exporter/internal/state"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	agentHeartbeatLastOutcomeDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "agent", "heartbeat_last_outcome"),
		"Latest heartbeat outcome (1 for the current outcome).",
		[]string{"agent_name", "outcome"}, nil,
	)
	agentHeartbeatLastTimestampDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "agent", "heartbeat_last_timestamp_seconds"),
		"Unix time of the latest heartbeat outcome.",
		[]string{"agent_name"}, nil,
	)
	agentHeartbeatNextCheckDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "agent", "heartbeat_next_check_timestamp_seconds"),
		"Unix time of the next heartbeat check requested by the latest outcome.",
		[]string{"agent_name"}, nil,
	)
	agentAuthProfilesCoolingDownDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "agent", "auth_profiles_cooling_down"),
		"Auth profiles currently in cooldown, blocked or disabled.",
		[]string{"agent_name"}, nil,
	)
	agentDBSizeDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "agent", "db_size_bytes"),
		"Per-agent SQLite database size including the WAL file.",
		[]string{"agent_name"}, nil,
	)
	agentDBSchemaVersionDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "agent", "db_schema_version"),
		"Per-agent SQLite schema version.",
		[]string{"agent_name"}, nil,
	)
)

// NewAgentHealthCollector exports health signals from each agent's SQLite DB, cached 30s.
func NewAgentHealthCollector(home string) prometheus.Collector {
	descs := []*prometheus.Desc{
		agentHeartbeatLastOutcomeDesc,
		agentHeartbeatLastTimestampDesc,
		agentHeartbeatNextCheckDesc,
		agentAuthProfilesCoolingDownDesc,
		agentDBSizeDesc,
		agentDBSchemaVersionDesc,
	}
	return NewCachedCollector(30*time.Second, descs, func() []prometheus.Metric {
		return collectAgentHealthMetrics(home, time.Now())
	})
}

func collectAgentHealthMetrics(home string, now time.Time) []prometheus.Metric {
	var metrics []prometheus.Metric
	gauge := func(d *prometheus.Desc, v float64, labels ...string) {
		metrics = append(metrics, prometheus.MustNewConstMetric(d, prometheus.GaugeValue, v, labels...))
	}

	for _, a := range state.ListAgents(home) {
		if a.DBPath == "" {
			continue
		}
		if size, ok := dbSize(a.DBPath); ok {
			gauge(agentDBSizeDesc, float64(size), a.ID)
		}

		db, err := state.OpenRO(a.DBPath)
		if err != nil {
			slog.Debug("Failed to open agent DB", "agent", a.ID, "err", err)
			continue
		}
		// Each query is independent: a table missing from an older schema skips only its metrics.
		skip := func(what string, err error) bool {
			if err == nil {
				return false
			}
			if !errors.Is(err, sql.ErrNoRows) {
				slog.Debug("Agent health query failed", "agent", a.ID, "query", what, "err", err)
			}
			return true
		}

		var version int64
		err = db.QueryRow(`SELECT schema_version FROM schema_meta WHERE meta_key = 'primary'`).Scan(&version)
		if !skip("schema_meta", err) {
			gauge(agentDBSchemaVersionDesc, float64(version), a.ID)
		}

		var (
			outcome    string
			occurredAt int64
			nextCheck  sql.NullString
		)
		err = db.QueryRow(`SELECT outcome, occurred_at, next_check FROM heartbeat_outcomes
			ORDER BY occurred_at DESC LIMIT 1`).Scan(&outcome, &occurredAt, &nextCheck)
		if !skip("heartbeat_outcomes", err) {
			gauge(agentHeartbeatLastOutcomeDesc, 1, a.ID, outcome)
			gauge(agentHeartbeatLastTimestampDesc, float64(occurredAt)/1000, a.ID)
			if ts, ok := parseTimestamp(nextCheck.String); ok {
				gauge(agentHeartbeatNextCheckDesc, float64(ts.UnixMilli())/1000, a.ID)
			}
		}

		var stateJSON string
		err = db.QueryRow(`SELECT state_json FROM auth_profile_state WHERE state_key = 'primary'`).Scan(&stateJSON)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			gauge(agentAuthProfilesCoolingDownDesc, 0, a.ID)
		case !skip("auth_profile_state", err):
			if n, err := countCoolingDown(stateJSON, now); err != nil {
				slog.Debug("Failed to parse auth profile state", "agent", a.ID, "err", err)
			} else {
				gauge(agentAuthProfilesCoolingDownDesc, float64(n), a.ID)
			}
		}

		db.Close()
	}
	return metrics
}

// dbSize returns the size of the DB file plus its -wal sidecar.
func dbSize(path string) (int64, bool) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, false
	}
	size := info.Size()
	if wal, err := os.Stat(path + "-wal"); err == nil {
		size += wal.Size()
	}
	return size, true
}

// countCoolingDown counts profiles whose usageStats cooldownUntil, blockedUntil
// or disabledUntil (epoch ms) is in the future, like OpenClaw's
// resolveProfileUnusableUntil. Model-scoped cooldowns count too.
func countCoolingDown(stateJSON string, now time.Time) (int, error) {
	var st struct {
		UsageStats map[string]map[string]any `json:"usageStats"`
	}
	if err := json.Unmarshal([]byte(stateJSON), &st); err != nil {
		return 0, err
	}
	nowMs := float64(now.UnixMilli())
	n := 0
	for _, stats := range st.UsageStats {
		for _, k := range []string{"cooldownUntil", "blockedUntil", "disabledUntil"} {
			if v, ok := stats[k].(float64); ok && v > nowMs {
				n++
				break
			}
		}
	}
	return n, nil
}

// parseTimestamp accepts RFC 3339 or epoch milliseconds. next_check is a
// free-form TEXT column, so anything else is ignored.
func parseTimestamp(s string) (time.Time, bool) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, true
	}
	if ms, err := strconv.ParseInt(s, 10, 64); err == nil && ms > 0 {
		return time.UnixMilli(ms), true
	}
	return time.Time{}, false
}
