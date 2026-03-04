package collector

import (
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"time"

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
	mdWorkspaceTotalBytesDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "md", "workspace_total_bytes"),
		"Total MD bytes in workspace.",
		[]string{"workspace"}, nil,
	)
	mdWorkspaceTotalTokensEstDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "md", "workspace_total_tokens_estimated"),
		"Total estimated tokens.",
		[]string{"workspace"}, nil,
	)
)

// NewWorkspaceCollector creates a workspace collector with 60s cache.
func NewWorkspaceCollector(home string) prometheus.Collector {
	descs := []*prometheus.Desc{
		mdFileBytesDesc,
		mdFileTokensEstDesc,
		mdWorkspaceTotalBytesDesc,
		mdWorkspaceTotalTokensEstDesc,
	}
	return NewCachedCollector(60*time.Second, descs, func() []prometheus.Metric {
		return collectWorkspaceMetrics(home)
	})
}

func collectWorkspaceMetrics(home string) []prometheus.Metric {
	var metrics []prometheus.Metric

	for wsDir, agent := range WorkspaceMap {
		wsPath := filepath.Join(home, wsDir)
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
			prometheus.MustNewConstMetric(mdWorkspaceTotalBytesDesc, prometheus.GaugeValue, float64(totalBytes), agent),
			prometheus.MustNewConstMetric(mdWorkspaceTotalTokensEstDesc, prometheus.GaugeValue, totalTokens, agent),
		)
	}

	return metrics
}
