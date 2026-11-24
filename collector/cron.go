package collector

import (
	"cmp"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/SammyLin/openclaw-exporter/internal/state"
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
	cronJobRunningDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "cron_job", "running"),
		"Job is currently running (1/0).",
		[]string{"job_name", "job_id"}, nil,
	)
	cronJobConsecutiveSkippedDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "cron_job", "consecutive_skipped"),
		"Consecutive skipped runs count.",
		[]string{"job_name", "job_id"}, nil,
	)
	cronJobAutoDisabledDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "cron_job", "auto_disabled"),
		"Job was auto-disabled by the scheduler (1/0).",
		[]string{"job_name", "job_id"}, nil,
	)
	cronJobLastRunStatusDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "cron_job", "last_run_status"),
		"Last run status (ok/error/skipped); 1 for the current status.",
		[]string{"job_name", "job_id", "status"}, nil,
	)
	cronJobLastDeliveryStatusDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "cron_job", "last_delivery_status"),
		"Last delivery status (delivered/not-delivered/unknown/not-requested); 1 for the current status.",
		[]string{"job_name", "job_id", "status"}, nil,
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
	LastRunAtMs        *float64        `json:"lastRunAtMs"`
	NextRunAtMs        *float64        `json:"nextRunAtMs"`
	ConsecutiveErrors  float64         `json:"consecutiveErrors"`
	LastDurationMs     *float64        `json:"lastDurationMs"`
	LastDelivered      bool            `json:"lastDelivered"`
	RunningAtMs        *float64        `json:"runningAtMs"`
	ConsecutiveSkipped float64         `json:"consecutiveSkipped"`
	AutoDisabled       json.RawMessage `json:"autoDisabled"` // object {reason, atMs, ...} when set
	LastRunStatus      string          `json:"lastRunStatus"`
	LastStatus         string          `json:"lastStatus"` // deprecated alias of lastRunStatus
	LastDeliveryStatus string          `json:"lastDeliveryStatus"`
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
	ch <- cronJobRunningDesc
	ch <- cronJobConsecutiveSkippedDesc
	ch <- cronJobAutoDisabledDesc
	ch <- cronJobLastRunStatusDesc
	ch <- cronJobLastDeliveryStatusDesc
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

	seen := map[[2]string]bool{}
	for _, job := range jobs {
		jobID := job.ID
		if len(jobID) > 8 {
			jobID = jobID[:8]
		}
		jobName := job.Name
		if jobName == "" {
			jobName = "Unknown"
		}
		// Duplicate label sets (truncated IDs, same job in two stores) fail the whole scrape.
		key := [2]string{jobName, jobID}
		if seen[key] {
			continue
		}
		seen[key] = true

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

		ch <- prometheus.MustNewConstMetric(cronJobRunningDesc, prometheus.GaugeValue, boolGauge(job.State.RunningAtMs != nil), jobName, jobID)
		ch <- prometheus.MustNewConstMetric(cronJobConsecutiveSkippedDesc, prometheus.GaugeValue, job.State.ConsecutiveSkipped, jobName, jobID)
		ch <- prometheus.MustNewConstMetric(cronJobAutoDisabledDesc, prometheus.GaugeValue, boolGauge(isAutoDisabled(job.State.AutoDisabled)), jobName, jobID)
		if status := cmp.Or(job.State.LastRunStatus, job.State.LastStatus); status != "" {
			ch <- prometheus.MustNewConstMetric(cronJobLastRunStatusDesc, prometheus.GaugeValue, 1, jobName, jobID, status)
		}
		if status := job.State.LastDeliveryStatus; status != "" {
			ch <- prometheus.MustNewConstMetric(cronJobLastDeliveryStatusDesc, prometheus.GaugeValue, 1, jobName, jobID, status)
		}
	}
}

// isAutoDisabled reports whether state.autoDisabled is set. OpenClaw writes an
// object ({reason, atMs, ...}); treat true or a non-empty object as set and
// null, false, 0 or {} as not set.
func isAutoDisabled(raw json.RawMessage) bool {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return false
	}
	switch v := v.(type) {
	case bool:
		return v
	case map[string]any:
		return len(v) > 0
	}
	return false
}

func boolGauge(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// getCronJobs reads cron jobs from the SQLite state DB (OpenClaw >= 2026.6.1),
// falling back to legacy cron/jobs.json when the DB or its cron_jobs table is missing.
func getCronJobs(home string) []cronJob {
	jobs, err := getCronJobsSQLite(home)
	switch {
	case err == nil:
		return jobs
	case errors.Is(err, os.ErrNotExist) || strings.Contains(err.Error(), "no such table"):
		slog.Debug("Cron jobs not in state DB, trying jobs.json", "err", err)
		return getCronJobsJSON(home)
	default:
		// The DB exists but is unreadable right now (e.g. locked); a stale
		// jobs.json would report wrong state, so skip this scrape.
		slog.Warn("Failed to read cron jobs from state DB", "err", err)
		return nil
	}
}

func getCronJobsSQLite(home string) ([]cronJob, error) {
	db, err := state.OpenRO(state.StateDBPath(home))
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query(`SELECT job_id, name, enabled, json_extract(job_json, '$.createdAtMs'), state_json
		FROM cron_jobs ORDER BY store_key, sort_order, job_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	jobs := []cronJob{}
	for rows.Next() {
		var (
			j         cronJob
			createdAt sql.NullFloat64
			stateJSON string
		)
		if err := rows.Scan(&j.ID, &j.Name, &j.Enabled, &createdAt, &stateJSON); err != nil {
			return nil, err
		}
		j.CreatedAt = createdAt.Float64
		if err := json.Unmarshal([]byte(stateJSON), &j.State); err != nil {
			slog.Debug("Failed to parse cron state_json", "job_id", j.ID, "err", err)
		}
		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}

func getCronJobsJSON(home string) []cronJob {
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
