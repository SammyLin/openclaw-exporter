package collector

import (
	"fmt"
	"testing"
	"time"
)

func TestOpsCollector(t *testing.T) {
	home := t.TempDir()
	now := time.Now().UnixMilli()
	old := time.Now().Add(-48 * time.Hour).UnixMilli()
	writeStateDB(t, home,
		`CREATE TABLE operator_approvals (approval_id TEXT PRIMARY KEY, kind TEXT NOT NULL, status TEXT NOT NULL) STRICT`,
		`INSERT INTO operator_approvals VALUES ('a','exec','pending'),('b','exec','pending'),('c','plugin','pending'),('d','exec','allowed')`,
		`CREATE TABLE task_runs (task_id TEXT PRIMARY KEY, runtime TEXT NOT NULL, status TEXT NOT NULL, created_at INTEGER NOT NULL) STRICT`,
		fmt.Sprintf(`INSERT INTO task_runs VALUES ('1','cron','succeeded',%d),('2','cron','failed',%d),('3','cron','failed',%d),('4','subagent','running',%d)`,
			now, now, old, now),
		// delivery_queue_entries deliberately missing (older schema): other metrics must survive.
	)

	got := gather(t, NewOpsCollector(home))
	expectMetrics(t, got, map[string]float64{
		`openclaw_approvals_pending{kind="exec"}`:       2,
		`openclaw_approvals_pending{kind="plugin"}`:     1,
		`openclaw_cron_runs_recent{status="succeeded"}`: 1,
		`openclaw_cron_runs_recent{status="failed"}`:    1,
		// zero-filled so alerts on "> 0" have a series
		`openclaw_approvals_pending{kind="system-agent"}`: 0,
	})
	if len(got) != 5 {
		t.Errorf("got %d series, want 5: %v", len(got), got)
	}
}

func TestOpsCollector_DeliveryQueue(t *testing.T) {
	home := t.TempDir()
	writeStateDB(t, home,
		`CREATE TABLE delivery_queue_entries (queue_name TEXT NOT NULL, id TEXT NOT NULL, status TEXT NOT NULL, PRIMARY KEY (queue_name, id)) STRICT`,
		`INSERT INTO delivery_queue_entries VALUES ('q','1','pending'),('q','2','pending'),('q','3','failed')`,
	)
	expectMetrics(t, gather(t, NewOpsCollector(home)), map[string]float64{
		`openclaw_delivery_queue_entries{status="pending"}`: 2,
		`openclaw_delivery_queue_entries{status="failed"}`:  1,
	})
}

func TestOpsCollector_NoDB(t *testing.T) {
	if got := gather(t, NewOpsCollector(t.TempDir())); len(got) != 0 {
		t.Fatalf("want no metrics, got %v", got)
	}
}
