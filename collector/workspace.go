package collector

import (
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/SammyLin/openclaw-exporter/internal/config"
	"github.com/SammyLin/openclaw-exporter/internal/state"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	mdFileBytesDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "md", "file_bytes"),
		"MD file size in bytes.",
		[]string{"workspace", "filename"}, nil,
	)
	mdFileTokensEstDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "md", "file_tokens_estimated"),
		"Estimated token count.",
		[]string{"workspace", "filename"}, nil,
	)
	mdWorkspaceBytesDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "md", "workspace_bytes"),
		"Total MD bytes in workspace.",
		[]string{"workspace"}, nil,
	)
	mdWorkspaceTokensEstDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "md", "workspace_tokens_estimated"),
		"Total estimated tokens.",
		[]string{"workspace"}, nil,
	)
)

// NewWorkspaceCollector creates a workspace collector with 60s cache.
func NewWorkspaceCollector(home string) prometheus.Collector {
	descs := []*prometheus.Desc{
		mdFileBytesDesc,
		mdFileTokensEstDesc,
		mdWorkspaceBytesDesc,
		mdWorkspaceTokensEstDesc,
	}
	return NewCachedCollector(60*time.Second, descs, func() []prometheus.Metric {
		return collectWorkspaceMetrics(home)
	})
}

func collectWorkspaceMetrics(home string) []prometheus.Metric {
	var metrics []prometheus.Metric

	for agent, wsPath := range resolveWorkspaces(home) {
		if _, err := os.Stat(wsPath); err != nil {
			continue
		}

		var totalBytes int64
		var totalTokens float64

		// Collect *.md from root and memory/
		patterns := []string{
			filepath.Join(wsPath, "*.md"),
			filepath.Join(wsPath, "memory", "*.md"),
		}

		for _, pattern := range patterns {
			matches, err := filepath.Glob(pattern)
			if err != nil {
				continue
			}
			for _, mdFile := range matches {
				info, err := os.Stat(mdFile)
				if err != nil {
					slog.Debug("Failed to stat md file", "path", mdFile, "err", err)
					continue
				}
				size := info.Size()
				tokens := math.Round(float64(size) / 3.5)

				relPath, _ := filepath.Rel(wsPath, mdFile)

				metrics = append(metrics,
					prometheus.MustNewConstMetric(mdFileBytesDesc, prometheus.GaugeValue, float64(size), agent, relPath),
					prometheus.MustNewConstMetric(mdFileTokensEstDesc, prometheus.GaugeValue, tokens, agent, relPath),
				)

				totalBytes += size
				totalTokens += tokens
			}
		}

		metrics = append(metrics,
			prometheus.MustNewConstMetric(mdWorkspaceBytesDesc, prometheus.GaugeValue, float64(totalBytes), agent),
			prometheus.MustNewConstMetric(mdWorkspaceTokensEstDesc, prometheus.GaugeValue, totalTokens, agent),
		)
	}

	return metrics
}

// resolveWorkspaces maps agent id to workspace dir for the union of the config
// roster and agents found on disk. When two agents share a dir, the first id
// in sorted order keeps it.
func resolveWorkspaces(home string) map[string]string {
	cfg := config.Load(home)
	var ids []string
	for _, a := range cfg.Roster() {
		ids = append(ids, a.ID)
	}
	for _, a := range state.ListAgents(home) {
		ids = append(ids, config.NormalizeAgentID(a.ID))
	}
	slices.Sort(ids)
	ids = slices.Compact(ids)

	out := make(map[string]string, len(ids))
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		dir := cfg.WorkspaceDir(id)
		if seen[dir] {
			slog.Debug("Workspace shared with another agent, skipping", "agent", id, "path", dir)
			continue
		}
		seen[dir] = true
		out[id] = dir
	}
	return out
}
