package collector

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	cronJobsTotalDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "cron", "jobs_total"),
		"Total cron jobs.",
		nil, nil,
	)
	cronJobsEnabledDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "cron", "jobs_enabled"),
		"Enabled cron jobs.",
		nil, nil,
	)
	cronJobEnabledDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "cron_job", "enabled"),
		"Job enabled (1) or disabled (0).",
		[]string{"job_name", "job_id"}, nil,
	)
	cronJobLastRunAtDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "cron_job", "last_run_at_seconds"),
		"Unix timestamp of last run.",
		[]string{"job_name", "job_id"}, nil,
	)
	cronJobNextRunAtDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "cron_job", "next_run_at_seconds"),
		"Unix timestamp of next run.",
		[]string{"job_name", "job_id"}, nil,
	)
	cronJobConsecutiveErrorsDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "cron_job", "consecutive_errors"),
		"Consecutive errors count.",
		[]string{"job_name", "job_id"}, nil,
	)
	cronJobLastDurationDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "cron_job", "last_duration_seconds"),
		"Last run duration in seconds.",
		[]string{"job_name", "job_id"}, nil,
	)
	cronJobLastDeliveredDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "cron_job", "last_delivered"),
		"Last message delivered (1/0).",
		[]string{"job_name", "job_id"}, nil,
	)
	cronJobCreatedAtDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "cron_job", "created_at_seconds"),
		"Job creation Unix timestamp.",
		[]string{"job_name", "job_id"}, nil,
	)
)

type cronJobsFile struct {
	Jobs []cronJob `json:"jobs"`
}

type cronJob struct {
	ID        string       `json:"id"`
	Name      string       `json:"name"`
	Enabled   bool         `json:"enabled"`
	State     cronJobState `json:"state"`
	CreatedAt float64      `json:"createdAtMs"`
}

type cronJobState struct {
	LastRunAtMs       *float64 `json:"lastRunAtMs"`
	NextRunAtMs       *float64 `json:"nextRunAtMs"`
	ConsecutiveErrors float64  `json:"consecutiveErrors"`
	LastDurationMs    *float64 `json:"lastDurationMs"`
	LastDelivered     bool     `json:"lastDelivered"`
}

// CronCollector collects cron job metrics.
type CronCollector struct {
	home string
}

// NewCronCollector creates a new CronCollector.
func NewCronCollector(home string) *CronCollector {
	return &CronCollector{home: home}
}

// Describe sends metric descriptors.
func (c *CronCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- cronJobsTotalDesc
	ch <- cronJobsEnabledDesc
	ch <- cronJobEnabledDesc
	ch <- cronJobLastRunAtDesc
	ch <- cronJobNextRunAtDesc
	ch <- cronJobConsecutiveErrorsDesc
	ch <- cronJobLastDurationDesc
	ch <- cronJobLastDeliveredDesc
	ch <- cronJobCreatedAtDesc
}

// Collect gathers cron job metrics fresh on each scrape.
func (c *CronCollector) Collect(ch chan<- prometheus.Metric) {
	jobs := getCronJobs(c.home)

	ch <- prometheus.MustNewConstMetric(cronJobsTotalDesc, prometheus.GaugeValue, float64(len(jobs)))

	enabledCount := 0
	for _, j := range jobs {
		if j.Enabled {
			enabledCount++
		}
	}
	ch <- prometheus.MustNewConstMetric(cronJobsEnabledDesc, prometheus.GaugeValue, float64(enabledCount))

	for _, job := range jobs {
		jobID := job.ID
		if len(jobID) > 8 {
			jobID = jobID[:8]
		}
		jobName := job.Name
		if jobName == "" {
			jobName = "Unknown"
		}

		enabled := 0.0
		if job.Enabled {
			enabled = 1.0
		}
		ch <- prometheus.MustNewConstMetric(cronJobEnabledDesc, prometheus.GaugeValue, enabled, jobName, jobID)

		if job.State.LastRunAtMs != nil {
			lastRunSec := *job.State.LastRunAtMs / 1000
			ch <- prometheus.MustNewConstMetric(cronJobLastRunAtDesc, prometheus.GaugeValue, lastRunSec, jobName, jobID)
		}

		if job.State.NextRunAtMs != nil {
			nextRunSec := *job.State.NextRunAtMs / 1000
			ch <- prometheus.MustNewConstMetric(cronJobNextRunAtDesc, prometheus.GaugeValue, nextRunSec, jobName, jobID)
		}

		ch <- prometheus.MustNewConstMetric(cronJobConsecutiveErrorsDesc, prometheus.GaugeValue, job.State.ConsecutiveErrors, jobName, jobID)

		if job.State.LastDurationMs != nil {
			ch <- prometheus.MustNewConstMetric(cronJobLastDurationDesc, prometheus.GaugeValue, *job.State.LastDurationMs/1000, jobName, jobID)
		}

		delivered := 0.0
		if job.State.LastDelivered {
			delivered = 1.0
		}
		ch <- prometheus.MustNewConstMetric(cronJobLastDeliveredDesc, prometheus.GaugeValue, delivered, jobName, jobID)

		if job.CreatedAt > 0 {
			ch <- prometheus.MustNewConstMetric(cronJobCreatedAtDesc, prometheus.GaugeValue, job.CreatedAt/1000, jobName, jobID)
		}
	}
}

func getCronJobs(home string) []cronJob {
	cronFile := filepath.Join(home, "cron", "jobs.json")
	data, err := os.ReadFile(cronFile)
	if err != nil {
		return nil
	}

	var file cronJobsFile
	if err := json.Unmarshal(data, &file); err != nil {
		slog.Warn("Failed to parse cron jobs", "err", err)
		return nil
	}
	return file.Jobs
}
