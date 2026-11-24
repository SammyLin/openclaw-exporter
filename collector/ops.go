package collector

import (
	"database/sql"
	"log/slog"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/SammyLin/openclaw-exporter/internal/state"
)

const cronRunsRecentWindow = 24 * time.Hour

var (
	approvalsPendingDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "approvals", "pending"),
		"Operator approvals waiting for a decision.",
		[]string{"kind"}, nil,
	)
	deliveryQueueEntriesDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "delivery_queue", "entries"),
		"Outbound delivery queue entries (completed/failed rows are retention-bounded).",
		[]string{"status"}, nil,
	)
	cronRunsRecentDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "cron", "runs_recent"),
		"Cron runs started in the last 24h, by status.",
		[]string{"status"}, nil,
	)
)

// opsQuery is a "SELECT label, COUNT(*) ... GROUP BY label" query against the state DB.
type opsQuery struct {
	desc  *prometheus.Desc
	query string
	args  func() []any
	// zero lists label values always emitted (as 0 when absent) so alerts
	// like `openclaw_approvals_pending > 0` have a series to evaluate.
	zero []string
}

var opsQueries = []opsQuery{
	{approvalsPendingDesc, `SELECT kind, COUNT(*) FROM operator_approvals WHERE status = 'pending' GROUP BY kind`, nil,
		[]string{"exec", "plugin", "system-agent"}}, // schema CHECK on kind
	{deliveryQueueEntriesDesc, `SELECT status, COUNT(*) FROM delivery_queue_entries GROUP BY status`, nil,
		[]string{"pending", "failed"}},
	{cronRunsRecentDesc, `SELECT status, COUNT(*) FROM task_runs WHERE runtime = 'cron' AND created_at >= ? GROUP BY status`,
		func() []any { return []any{time.Now().Add(-cronRunsRecentWindow).UnixMilli()} },
		[]string{"failed"}},
}

// NewOpsCollector exports operational counts from the shared state DB
// (OpenClaw >= 2026.6.1) with a 30s cache. Emits nothing if the DB is missing.
func NewOpsCollector(home string) prometheus.Collector {
	descs := make([]*prometheus.Desc, 0, len(opsQueries))
	for _, q := range opsQueries {
		descs = append(descs, q.desc)
	}
	return NewCachedCollector(30*time.Second, descs, func() []prometheus.Metric {
		return collectOps(home)
	})
}

func collectOps(home string) []prometheus.Metric {
	db, err := state.OpenRO(state.StateDBPath(home))
	if err != nil {
		slog.Debug("State DB unavailable for ops metrics", "err", err)
		return nil
	}
	defer db.Close()

	var metrics []prometheus.Metric
	for _, q := range opsQueries {
		var args []any
		if q.args != nil {
			args = q.args()
		}
		// Each query is independent: a table missing on an older schema only drops its own metric.
		counts, err := countBy(db, q.query, args...)
		if err != nil {
			slog.Debug("Ops query failed", "query", q.query, "err", err)
			continue
		}
		for _, label := range q.zero {
			if _, ok := counts[label]; !ok {
				counts[label] = 0
			}
		}
		for label, n := range counts {
			metrics = append(metrics, prometheus.MustNewConstMetric(q.desc, prometheus.GaugeValue, n, label))
		}
	}
	return metrics
}

func countBy(db *sql.DB, query string, args ...any) (map[string]float64, error) {
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := map[string]float64{}
	for rows.Next() {
		var (
			label string
			n     float64
		)
		if err := rows.Scan(&label, &n); err != nil {
			return nil, err
		}
		counts[label] = n
	}
	return counts, rows.Err()
}
