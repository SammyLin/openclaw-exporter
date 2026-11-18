package collector

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/SammyLin/openclaw-exporter/internal/filereader"
	"github.com/SammyLin/openclaw-exporter/internal/state"
)

var (
	activeSessionsDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "", "active_sessions"),
		"Total active sessions.",
		nil, nil,
	)
	agentSessionsDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "agent", "sessions"),
		"Sessions per agent.",
		[]string{"agent_name"}, nil,
	)
	agentSessionsByStatusDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "agent", "sessions_by_status"),
		"Non-archived sessions per agent by status (SQLite state only).",
		[]string{"agent_name", "status"}, nil,
	)
	agentStateDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "agent", "state"),
		"Agent state (0=idle, 1=working, 2=thinking, 3=error).",
		[]string{"agent_name"}, nil,
	)
	agentLastActivityTimestampDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "agent", "last_activity_timestamp_seconds"),
		"Unix timestamp of last activity.",
		[]string{"agent_name"}, nil,
	)
)

// AgentCollector collects agent metrics.
type AgentCollector struct {
	home string
}

// NewAgentCollector creates a new AgentCollector.
func NewAgentCollector(home string) *AgentCollector {
	return &AgentCollector{home: home}
}

// Describe sends metric descriptors.
func (c *AgentCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- activeSessionsDesc
	ch <- agentSessionsDesc
	ch <- agentSessionsByStatusDesc
	ch <- agentStateDesc
	ch <- agentLastActivityTimestampDesc
}

// Collect gathers agent metrics fresh on each scrape.
func (c *AgentCollector) Collect(ch chan<- prometheus.Metric) {
	var totalSessions float64
	for _, a := range state.ListAgents(c.home) {
		name := a.ID
		var sessions int
		var st string
		var timestamp int64
		snap, err := readAgentSQLite(a.DBPath)
		if err == nil {
			sessions, st, timestamp = snap.sessions, snap.state, snap.timestamp
			for status, n := range snap.byStatus {
				ch <- prometheus.MustNewConstMetric(agentSessionsByStatusDesc, prometheus.GaugeValue, float64(n), name, status)
			}
		} else {
			if a.DBPath != "" {
				slog.Debug("Failed to read agent DB", "agent", name, "err", err)
			}
			sessions = countSessions(a.Dir)
			st, timestamp = getAgentState(a.Dir)
		}

		ch <- prometheus.MustNewConstMetric(agentSessionsDesc, prometheus.GaugeValue, float64(sessions), name)
		ch <- prometheus.MustNewConstMetric(agentStateDesc, prometheus.GaugeValue, StateMap[st], name)
		ch <- prometheus.MustNewConstMetric(agentLastActivityTimestampDesc, prometheus.GaugeValue, float64(timestamp), name)
		totalSessions += float64(sessions)
	}

	ch <- prometheus.MustNewConstMetric(activeSessionsDesc, prometheus.GaugeValue, totalSessions)
}

func countSessions(agentPath string) int {
	sessionsDir := filepath.Join(agentPath, "sessions")
	matches, err := filepath.Glob(filepath.Join(sessionsDir, "*.jsonl"))
	if err != nil {
		return 0
	}
	return len(matches)
}

func getAgentState(agentPath string) (string, int64) {
	sessionsDir := filepath.Join(agentPath, "sessions")
	matches, err := filepath.Glob(filepath.Join(sessionsDir, "*.jsonl"))
	if err != nil || len(matches) == 0 {
		return "idle", 0
	}

	// Find the most recently modified file
	type fileInfo struct {
		path    string
		modTime time.Time
	}
	files := make([]fileInfo, 0, len(matches))
	for _, m := range matches {
		info, err := os.Stat(m)
		if err != nil {
			continue
		}
		files = append(files, fileInfo{path: m, modTime: info.ModTime()})
	}
	if len(files) == 0 {
		return "idle", 0
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].modTime.After(files[j].modTime)
	})
	latest := files[0]
	timestamp := latest.modTime.Unix()

	lines, err := filereader.TailLines(latest.path, 8192)
	if err != nil {
		slog.Debug("Failed to read session file", "path", latest.path, "err", err)
		return "idle", timestamp
	}

	// Check last 10 lines
	start := max(len(lines)-10, 0)
	return stateFromEvents(lines[start:], time.Now().Unix()-timestamp), timestamp
}

// stateFromEvents classifies transcript events (oldest first, newest last)
// whose latest write was secondsAgo seconds ago.
func stateFromEvents(tail []string, secondsAgo int64) string {
	for i := len(tail) - 1; i >= 0; i-- {
		var data struct {
			Message struct {
				Role    string `json:"role"`
				Content json.RawMessage
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(tail[i]), &data); err != nil {
			continue
		}

		// Try to parse content as array of objects
		var contents []struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(data.Message.Content, &contents); err == nil {
			for _, ct := range contents {
				if ct.Type == "toolCall" && secondsAgo < 60 {
					return "working"
				}
				if ct.Type == "thinking" && secondsAgo < 120 {
					return "thinking"
				}
			}
			// Content types were checked with their specific timeouts;
			// skip the generic role check to avoid bypassing those timeouts.
			continue
		}

		if data.Message.Role == "assistant" && secondsAgo < 300 {
			return "working"
		}
	}

	return "idle"
}
