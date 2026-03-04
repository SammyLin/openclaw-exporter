package collector

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	cronSessionTokensDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "cron_session", "tokens_last"),
		"Token usage in last cron session.",
		[]string{"agent", "cron_name", "token_type"}, nil,
	)
	cronSessionCostDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "cron_session", "cost_last_usd"),
		"Cost of last cron session.",
		[]string{"agent", "cron_name"}, nil,
	)
	cronSessionTotalTokensDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "cron_session", "total_tokens_last"),
		"Total tokens in last session.",
		[]string{"agent", "cron_name"}, nil,
	)
	agentSessionAvgTokensDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "agent_session", "avg_tokens"),
		"Avg tokens per session (last 5).",
		[]string{"agent", "token_type"}, nil,
	)
	agentSessionAvgCostDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "agent_session", "avg_cost_usd"),
		"Avg cost per session (last 5).",
		[]string{"agent"}, nil,
	)
	agentSessionLastTokensDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "agent_session", "last_tokens"),
		"Tokens in latest session.",
		[]string{"agent", "token_type"}, nil,
	)
	agentSessionLastCostDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "agent_session", "last_cost_usd"),
		"Cost of latest session.",
		[]string{"agent"}, nil,
	)
)

// sessionEntry represents a session from sessions.json.
type sessionEntry struct {
	SessionID string  `json:"sessionId"`
	Label     string  `json:"label"`
	UpdatedAt float64 `json:"updatedAt"`
}

// usageTotals holds aggregated token usage.
type usageTotals struct {
	Input       float64
	Output      float64
	CacheRead   float64
	CacheWrite  float64
	TotalTokens float64
	Cost        float64
}

// NewTokenCollector creates a token collector with 60s cache.
func NewTokenCollector(home string) prometheus.Collector {
	descs := []*prometheus.Desc{
		cronSessionTokensDesc,
		cronSessionCostDesc,
		cronSessionTotalTokensDesc,
		agentSessionAvgTokensDesc,
		agentSessionAvgCostDesc,
		agentSessionLastTokensDesc,
		agentSessionLastCostDesc,
	}
	return NewCachedCollector(60*time.Second, descs, func() []prometheus.Metric {
		var metrics []prometheus.Metric
		metrics = append(metrics, collectCronSessionTokens(home)...)
		metrics = append(metrics, collectAgentSessionTokens(home)...)
		return metrics
	})
}

func loadSessions(home, agentName string) []sessionEntry {
	path := filepath.Join(home, "agents", agentName, "sessions", "sessions.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	// Try as map first (dict), then as array
	var asMap map[string]sessionEntry
	if err := json.Unmarshal(data, &asMap); err == nil {
		entries := make([]sessionEntry, 0, len(asMap))
		for _, v := range asMap {
			entries = append(entries, v)
		}
		return entries
	}

	var asArray []sessionEntry
	if err := json.Unmarshal(data, &asArray); err == nil {
		return asArray
	}

	slog.Debug("Failed to parse sessions.json", "agent", agentName)
	return nil
}

func readSessionUsage(home, agentName, sessionID string) *usageTotals {
	path := filepath.Join(home, "agents", agentName, "sessions", sessionID+".jsonl")
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil
	}

	// Read last 64KB for large files
	const tailSize = 65536
	size := info.Size()
	if size > tailSize {
		if _, err := f.Seek(-tailSize, io.SeekEnd); err != nil {
			return nil
		}
		// Skip partial line
		buf := make([]byte, 1)
		for {
			_, err := f.Read(buf)
			if err != nil {
				break
			}
			if buf[0] == '\n' {
				break
			}
		}
	}

	rawData, err := io.ReadAll(f)
	if err != nil {
		return nil
	}

	totals := &usageTotals{}
	count := 0

	for _, line := range bytes.Split(bytes.TrimSpace(rawData), []byte("\n")) {
		if len(line) == 0 {
			continue
		}

		var rec map[string]json.RawMessage
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}

		// Find usage: either top-level or inside message
		usageRaw, ok := rec["usage"]
		if !ok {
			var msg map[string]json.RawMessage
			if msgRaw, exists := rec["message"]; exists {
				if err := json.Unmarshal(msgRaw, &msg); err == nil {
					usageRaw, ok = msg["usage"]
				}
			}
		}
		if !ok || usageRaw == nil {
			continue
		}

		var usage struct {
			Input       float64         `json:"input"`
			Output      float64         `json:"output"`
			CacheRead   float64         `json:"cacheRead"`
			CacheWrite  float64         `json:"cacheWrite"`
			TotalTokens float64         `json:"totalTokens"`
			Cost        json.RawMessage `json:"cost"`
		}
		if err := json.Unmarshal(usageRaw, &usage); err != nil {
			continue
		}

		totals.Input += usage.Input
		totals.Output += usage.Output
		totals.CacheRead += usage.CacheRead
		totals.CacheWrite += usage.CacheWrite
		totals.TotalTokens += usage.TotalTokens

		// Handle cost polymorphism: can be {"total": 0.5} or 0.5
		if len(usage.Cost) > 0 {
			var costObj struct {
				Total float64 `json:"total"`
			}
			if err := json.Unmarshal(usage.Cost, &costObj); err == nil {
				totals.Cost += costObj.Total
			} else {
				var costNum float64
				if err := json.Unmarshal(usage.Cost, &costNum); err == nil {
					totals.Cost += costNum
				}
			}
		}
		count++
	}

	if count == 0 {
		return nil
	}
	return totals
}

func collectCronSessionTokens(home string) []prometheus.Metric {
	var metrics []prometheus.Metric
	nowMs := float64(time.Now().UnixMilli())
	cutoffMs := nowMs - sevenDaysMs

	for _, agent := range AgentNames {
		sessions := loadSessions(home, agent)
		// Find latest cron session per cron_name
		cronSessions := make(map[string]sessionEntry)
		for _, s := range sessions {
			if !strings.Contains(s.Label, "Cron:") {
				continue
			}
			if s.UpdatedAt < cutoffMs {
				continue
			}
			cronName := strings.TrimSpace(strings.Replace(strings.Replace(s.Label, "Cron: ", "", 1), "Cron:", "", 1))
			if existing, ok := cronSessions[cronName]; !ok || s.UpdatedAt > existing.UpdatedAt {
				cronSessions[cronName] = s
			}
		}

		for cronName, s := range cronSessions {
			usage := readSessionUsage(home, agent, s.SessionID)
			if usage == nil {
				continue
			}
			for _, tt := range []struct {
				name string
				val  float64
			}{
				{"input", usage.Input},
				{"output", usage.Output},
				{"cacheRead", usage.CacheRead},
				{"cacheWrite", usage.CacheWrite},
			} {
				metrics = append(metrics, prometheus.MustNewConstMetric(
					cronSessionTokensDesc, prometheus.GaugeValue, tt.val, agent, cronName, tt.name,
				))
			}
			metrics = append(metrics, prometheus.MustNewConstMetric(
				cronSessionCostDesc, prometheus.GaugeValue, usage.Cost, agent, cronName,
			))
			metrics = append(metrics, prometheus.MustNewConstMetric(
				cronSessionTotalTokensDesc, prometheus.GaugeValue, usage.TotalTokens, agent, cronName,
			))
		}
	}
	return metrics
}

func collectAgentSessionTokens(home string) []prometheus.Metric {
	var metrics []prometheus.Metric

	for _, agent := range AgentNames {
		sessions := loadSessions(home, agent)
		// Filter non-cron sessions
		var regular []sessionEntry
		for _, s := range sessions {
			if !strings.HasPrefix(s.Label, "Cron") {
				regular = append(regular, s)
			}
		}
		// Sort by updatedAt descending
		sort.Slice(regular, func(i, j int) bool {
			return regular[i].UpdatedAt > regular[j].UpdatedAt
		})

		// Take last 5
		if len(regular) > 5 {
			regular = regular[:5]
		}
		if len(regular) == 0 {
			continue
		}

		var allUsage []*usageTotals
		for _, s := range regular {
			usage := readSessionUsage(home, agent, s.SessionID)
			if usage != nil {
				allUsage = append(allUsage, usage)
			}
		}
		if len(allUsage) == 0 {
			continue
		}

		// Latest session
		latest := allUsage[0]
		for _, tt := range []struct {
			name string
			val  float64
		}{
			{"input", latest.Input},
			{"output", latest.Output},
			{"cacheRead", latest.CacheRead},
			{"cacheWrite", latest.CacheWrite},
		} {
			metrics = append(metrics, prometheus.MustNewConstMetric(
				agentSessionLastTokensDesc, prometheus.GaugeValue, tt.val, agent, tt.name,
			))
		}
		metrics = append(metrics, prometheus.MustNewConstMetric(
			agentSessionLastCostDesc, prometheus.GaugeValue, latest.Cost, agent,
		))

		// Averages over all collected
		n := float64(len(allUsage))
		for _, tt := range []struct {
			name string
			sum  float64
		}{
			{"input", sumField(allUsage, func(u *usageTotals) float64 { return u.Input })},
			{"output", sumField(allUsage, func(u *usageTotals) float64 { return u.Output })},
			{"cacheRead", sumField(allUsage, func(u *usageTotals) float64 { return u.CacheRead })},
			{"cacheWrite", sumField(allUsage, func(u *usageTotals) float64 { return u.CacheWrite })},
		} {
			metrics = append(metrics, prometheus.MustNewConstMetric(
				agentSessionAvgTokensDesc, prometheus.GaugeValue, math.Round(tt.sum/n), agent, tt.name,
			))
		}
		avgCost := sumField(allUsage, func(u *usageTotals) float64 { return u.Cost }) / n
		metrics = append(metrics, prometheus.MustNewConstMetric(
			agentSessionAvgCostDesc, prometheus.GaugeValue, avgCost, agent,
		))
	}

	return metrics
}

func sumField(usages []*usageTotals, fn func(*usageTotals) float64) float64 {
	var total float64
	for _, u := range usages {
		total += fn(u)
	}
	return total
}
