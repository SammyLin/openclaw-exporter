package collector

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/SammyLin/openclaw-exporter/internal/state"
)

// writeStateDB creates <home>/state/openclaw.sqlite and runs stmts against it.
func writeStateDB(t *testing.T, home string, stmts ...string) {
	t.Helper()
	path := state.StateDBPath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatal(s, err)
		}
	}
}

// gather returns metrics as `name{k="v",...}` -> value via a pedantic registry,
// which also rejects duplicate or inconsistent series.
func gather(t *testing.T, c prometheus.Collector) map[string]float64 {
	t.Helper()
	reg := prometheus.NewPedanticRegistry()
	reg.MustRegister(c)
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]float64{}
	for _, mf := range mfs {
		for _, m := range mf.GetMetric() {
			var labels []string
			for _, l := range m.GetLabel() {
				labels = append(labels, fmt.Sprintf("%s=%q", l.GetName(), l.GetValue()))
			}
			out[mf.GetName()+"{"+strings.Join(labels, ",")+"}"] = m.GetGauge().GetValue()
		}
	}
	return out
}

func expectMetrics(t *testing.T, got, want map[string]float64) {
	t.Helper()
	for k, v := range want {
		if g, ok := got[k]; !ok || g != v {
			t.Errorf("%s = %v (present %v), want %v", k, g, ok, v)
		}
	}
}

const cronJobsSchema = `CREATE TABLE cron_jobs (
	store_key TEXT NOT NULL, job_id TEXT NOT NULL, name TEXT NOT NULL, enabled INTEGER NOT NULL,
	job_json TEXT NOT NULL, state_json TEXT NOT NULL DEFAULT '{}', sort_order INTEGER NOT NULL DEFAULT 0,
	updated_at INTEGER NOT NULL, PRIMARY KEY (store_key, job_id)) STRICT`

func TestCronCollector_SQLite(t *testing.T) {
	home := t.TempDir()
	// jobs.json must be ignored when the state DB has cron_jobs.
	writeCronJSON(t, home, `{"jobs":[{"id":"legacy-job","name":"legacy","enabled":true}]}`)
	writeStateDB(t, home, cronJobsSchema,
		`INSERT INTO cron_jobs VALUES ('s', 'abcdef123456', 'daily', 1, '{"createdAtMs":1700000000000}',
			'{"lastRunAtMs":1700000100000,"nextRunAtMs":1700000200000,"lastDurationMs":2500,"consecutiveErrors":2,
			  "lastDelivered":true,"runningAtMs":1700000150000,"consecutiveSkipped":3,
			  "autoDisabled":{"reason":"consecutive-failures","atMs":1,"consecutiveErrors":5},
			  "lastRunStatus":"error","lastStatus":"ok","lastDeliveryStatus":"not-delivered"}', 0, 1)`,
		`INSERT INTO cron_jobs VALUES ('s', 'zz', 'old', 0, '{}', '{"lastStatus":"skipped"}', 1, 1)`,
	)

	got := gather(t, NewCronCollector(home))
	l := `{job_id="abcdef12",job_name="daily"}`
	expectMetrics(t, got, map[string]float64{
		"openclaw_cron_jobs_total{}":                                                                        2,
		"openclaw_cron_jobs_enabled{}":                                                                      1,
		"openclaw_cron_job_enabled" + l:                                                                     1,
		"openclaw_cron_job_last_run_at_seconds" + l:                                                         1700000100,
		"openclaw_cron_job_next_run_at_seconds" + l:                                                         1700000200,
		"openclaw_cron_job_last_duration_seconds" + l:                                                       2.5,
		"openclaw_cron_job_consecutive_errors" + l:                                                          2,
		"openclaw_cron_job_last_delivered" + l:                                                              1,
		"openclaw_cron_job_created_at_seconds" + l:                                                          1700000000,
		"openclaw_cron_job_running" + l:                                                                     1,
		"openclaw_cron_job_consecutive_skipped" + l:                                                         3,
		"openclaw_cron_job_auto_disabled" + l:                                                               1,
		`openclaw_cron_job_last_run_status{job_id="abcdef12",job_name="daily",status="error"}`:              1,
		`openclaw_cron_job_last_delivery_status{job_id="abcdef12",job_name="daily",status="not-delivered"}`: 1,
		// deprecated lastStatus fallback
		`openclaw_cron_job_last_run_status{job_id="zz",job_name="old",status="skipped"}`: 1,
		`openclaw_cron_job_running{job_id="zz",job_name="old"}`:                          0,
		`openclaw_cron_job_auto_disabled{job_id="zz",job_name="old"}`:                    0,
	})
	if _, ok := got[`openclaw_cron_job_created_at_seconds{job_id="zz",job_name="old"}`]; ok {
		t.Error("created_at emitted for job without createdAtMs")
	}
	if _, ok := got[`openclaw_cron_job_enabled{job_id="legacy-j",job_name="legacy"}`]; ok {
		t.Error("jobs.json read despite state DB")
	}
}

func writeCronJSON(t *testing.T, home, content string) {
	t.Helper()
	dir := filepath.Join(home, "cron")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "jobs.json"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCronCollector_JSONFallback(t *testing.T) {
	for name, withDB := range map[string]bool{"no db": false, "db without cron_jobs": true} {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			if withDB {
				writeStateDB(t, home, "CREATE TABLE other (x TEXT)")
			}
			writeCronJSON(t, home, `{"jobs":[{"id":"1234567890","name":"j","enabled":true,"createdAtMs":5000,
				"state":{"lastRunStatus":"ok","lastDeliveryStatus":"delivered","consecutiveSkipped":1}}]}`)
			l := `{job_id="12345678",job_name="j"}`
			expectMetrics(t, gather(t, NewCronCollector(home)), map[string]float64{
				"openclaw_cron_jobs_total{}":                                                                1,
				"openclaw_cron_job_enabled" + l:                                                             1,
				"openclaw_cron_job_created_at_seconds" + l:                                                  5,
				"openclaw_cron_job_consecutive_skipped" + l:                                                 1,
				`openclaw_cron_job_last_run_status{job_id="12345678",job_name="j",status="ok"}`:             1,
				`openclaw_cron_job_last_delivery_status{job_id="12345678",job_name="j",status="delivered"}`: 1,
			})
		})
	}
}

func TestCronCollector_NothingOnDisk(t *testing.T) {
	expectMetrics(t, gather(t, NewCronCollector(t.TempDir())), map[string]float64{
		"openclaw_cron_jobs_total{}": 0,
	})
}

func TestIsAutoDisabled(t *testing.T) {
	for raw, want := range map[string]bool{
		`{"reason":"errors","atMs":1}`: true,
		`true`:                         true,
		`{}`:                           false,
		`false`:                        false,
		`0`:                            false,
		`null`:                         false,
		``:                             false,
	} {
		if got := isAutoDisabled(json.RawMessage(raw)); got != want {
			t.Errorf("isAutoDisabled(%q) = %v, want %v", raw, got, want)
		}
	}
}
