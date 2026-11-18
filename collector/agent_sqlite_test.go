package collector

import (
	"database/sql"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	_ "modernc.org/sqlite"

	"github.com/SammyLin/openclaw-exporter/internal/state"
)

const fixtureSchema = `
CREATE TABLE session_nodes (
  session_key TEXT NOT NULL PRIMARY KEY,
  current_session_id TEXT NOT NULL,
  updated_at INTEGER NOT NULL,
  status TEXT,
  created_via TEXT,
  label TEXT,
  display_name TEXT,
  archived_at INTEGER
);
CREATE TABLE session_windows (
  session_id TEXT NOT NULL PRIMARY KEY,
  session_key TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE TABLE transcript_events (
  session_id TEXT NOT NULL,
  seq INTEGER NOT NULL,
  event_json TEXT,
  created_at INTEGER NOT NULL,
  event_zstd BLOB,
  event_utf8_bytes INTEGER,
  message_role TEXT,
  PRIMARY KEY (session_id, seq)
);`

// newAgentDB creates <home>/agents/<id>/agent/openclaw-agent.sqlite with the fixture schema.
func newAgentDB(t *testing.T, home, id string) *sql.DB {
	t.Helper()
	path := state.AgentDBPath(home, id)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(fixtureSchema); err != nil {
		t.Fatal(err)
	}
	return db
}

func mustExec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatal(err)
	}
}

func addSession(t *testing.T, db *sql.DB, key, sid, label, via, status string, updated time.Time) {
	t.Helper()
	mustExec(t, db, `INSERT INTO session_nodes (session_key, current_session_id, updated_at, status, created_via, label)
		VALUES (?, ?, ?, nullif(?, ''), nullif(?, ''), nullif(?, ''))`, key, sid, updated.UnixMilli(), status, via, label)
	mustExec(t, db, `INSERT INTO session_windows (session_id, session_key, created_at, updated_at) VALUES (?, ?, ?, ?)`,
		sid, key, updated.UnixMilli(), updated.UnixMilli())
}

func addEvent(t *testing.T, db *sql.DB, sid string, seq int, role, body string, at time.Time, compress bool) {
	t.Helper()
	if compress {
		enc, err := zstd.NewWriter(nil)
		if err != nil {
			t.Fatal(err)
		}
		mustExec(t, db, `INSERT INTO transcript_events (session_id, seq, event_zstd, event_utf8_bytes, created_at, message_role)
			VALUES (?, ?, ?, ?, ?, ?)`, sid, seq, enc.EncodeAll([]byte(body), nil), len(body), at.UnixMilli(), role)
		enc.Close()
		return
	}
	mustExec(t, db, `INSERT INTO transcript_events (session_id, seq, event_json, created_at, message_role)
		VALUES (?, ?, ?, ?, ?)`, sid, seq, body, at.UnixMilli(), role)
}

func TestReadAgentSQLite_State(t *testing.T) {
	toolCall := `{"type":"message","message":{"role":"assistant","content":[{"type":"toolCall"}]}}`
	thinking := `{"type":"message","message":{"role":"assistant","content":[{"type":"thinking"}]}}`
	cases := []struct {
		name     string
		body     string
		ago      time.Duration
		compress bool
		want     string
	}{
		{"toolCall recent", toolCall, 10 * time.Second, false, "working"},
		{"toolCall expired no fallthrough", toolCall, 90 * time.Second, false, "idle"},
		{"thinking recent zstd", thinking, 30 * time.Second, true, "thinking"},
		{"assistant text recent", `{"message":{"role":"assistant","content":"hi"}}`, 200 * time.Second, false, "working"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			db := newAgentDB(t, home, "main")
			at := time.Now().Add(-tc.ago)
			// Older window must be ignored in favour of the most recently updated one.
			addSession(t, db, "old", "s-old", "", "", "done", at.Add(-time.Hour))
			addEvent(t, db, "s-old", 1, "assistant", toolCall, at.Add(-time.Hour), false)
			addSession(t, db, "new", "s-new", "", "", "running", at)
			addEvent(t, db, "s-new", 1, "user", `{"message":{"role":"user","content":"go"}}`, at.Add(-time.Second), false)
			addEvent(t, db, "s-new", 2, "assistant", tc.body, at, tc.compress)

			snap, err := readAgentSQLite(state.AgentDBPath(home, "main"))
			if err != nil {
				t.Fatal(err)
			}
			if snap.state != tc.want {
				t.Errorf("state = %q, want %q", snap.state, tc.want)
			}
			if snap.timestamp != at.UnixMilli()/1000 {
				t.Errorf("timestamp = %d, want %d", snap.timestamp, at.Unix())
			}
		})
	}
}

func TestAgentCollector_SQLiteSessionCounts(t *testing.T) {
	home := t.TempDir()
	db := newAgentDB(t, home, "ops")
	now := time.Now()
	addSession(t, db, "a", "s1", "", "", "running", now)
	addSession(t, db, "b", "s2", "", "", "done", now)
	addSession(t, db, "c", "s3", "", "", "done", now)
	addSession(t, db, "d", "s4", "", "", "done", now)
	mustExec(t, db, `UPDATE session_nodes SET archived_at = 1 WHERE session_key = 'd'`)

	ch := make(chan prometheus.Metric, 100)
	NewAgentCollector(home).Collect(ch)
	close(ch)
	got := map[string]float64{}
	for m := range ch {
		switch m.Desc() {
		case agentSessionsDesc:
			got["total"] = metricValue(t, m)
		case agentSessionsByStatusDesc:
			got[labelValue(t, m, "status")] = metricValue(t, m)
		}
	}
	if got["total"] != 3 || got["running"] != 1 || got["done"] != 2 || len(got) != 3 {
		t.Errorf("sessions = %v, want total=3 running=1 done=2", got)
	}
}

func metricValue(t *testing.T, m prometheus.Metric) float64 {
	t.Helper()
	var d dto.Metric
	if err := m.Write(&d); err != nil {
		t.Fatal(err)
	}
	return d.GetGauge().GetValue()
}

func labelValue(t *testing.T, m prometheus.Metric, name string) string {
	t.Helper()
	var d dto.Metric
	if err := m.Write(&d); err != nil {
		t.Fatal(err)
	}
	for _, l := range d.GetLabel() {
		if l.GetName() == name {
			return l.GetValue()
		}
	}
	return ""
}

func TestTokenCollectors_SQLiteCronVsRegular(t *testing.T) {
	home := t.TempDir()
	db := newAgentDB(t, home, "ops")
	now := time.Now()
	usage := func(in, out int, cost string) string {
		return `{"type":"message","message":{"role":"assistant","content":[],"usage":{"input":` +
			strconv.Itoa(in) + `,"output":` + strconv.Itoa(out) + `,"totalTokens":` + strconv.Itoa(in+out) + `,"cost":` + cost + `}}}`
	}
	addSession(t, db, "cron:1", "c1", "Cron: nightly", "cron", "done", now.Add(-time.Hour))
	addEvent(t, db, "c1", 1, "assistant", usage(100, 10, `{"total":0.5}`), now, true)
	addSession(t, db, "cron:2", "c2", "", "cron", "done", now.Add(-time.Hour))
	mustExec(t, db, `UPDATE session_nodes SET display_name = 'weekly' WHERE session_key = 'cron:2'`)
	addEvent(t, db, "c2", 1, "assistant", usage(7, 3, `0.1`), now, false)
	addSession(t, db, "chat", "r1", "", "channel", "running", now)
	addEvent(t, db, "r1", 1, "assistant", usage(40, 2, `{"total":0.2}`), now, false)
	addEvent(t, db, "r1", 2, "assistant", usage(10, 8, `{"total":0.1}`), now, false)
	addEvent(t, db, "r1", 3, "assistant", `{"message":{"role":"assistant","usage":{"input":999,"usageReport":{"state":"unavailable"}}}}`, now, false)
	addEvent(t, db, "r1", 4, "user", usage(1000, 1000, `1`), now, false) // not assistant: skipped by SQL

	cron := map[string]float64{}
	for _, m := range collectCronSessionTokens(home) {
		if m.Desc() == cronSessionTotalTokensDesc {
			cron[labelValue(t, m, "cron_name")] = metricValue(t, m)
		}
	}
	if cron["nightly"] != 110 || cron["weekly"] != 10 || len(cron) != 2 {
		t.Errorf("cron total tokens = %v, want nightly=110 weekly=10", cron)
	}

	var gotInput, gotCost float64
	for _, m := range collectAgentSessionTokens(home) {
		if m.Desc() == agentSessionLastTokensDesc && labelValue(t, m, "token_type") == "input" {
			gotInput = metricValue(t, m)
		}
		if m.Desc() == agentSessionLastCostDesc {
			gotCost = metricValue(t, m)
		}
	}
	if gotInput != 50 {
		t.Errorf("regular last input = %v, want 50", gotInput)
	}
	if gotCost < 0.2999 || gotCost > 0.3001 {
		t.Errorf("regular last cost = %v, want 0.3", gotCost)
	}
}

func TestTokenCollectors_FallbackToJSONWithoutDB(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "agents", "legacy", "sessions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sessions.json"),
		[]byte(`{"s1":{"label":"Cron: backup","updatedAt":`+strconv.Itoa(int(time.Now().UnixMilli()))+`}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "s1.jsonl"),
		[]byte(`{"message":{"role":"assistant","usage":{"input":5,"output":5,"totalTokens":10}}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, m := range collectCronSessionTokens(home) {
		if m.Desc() == cronSessionTotalTokensDesc && labelValue(t, m, "agent") == "legacy" && labelValue(t, m, "cron_name") == "backup" {
			found = metricValue(t, m) == 10
		}
	}
	if !found {
		t.Error("expected legacy JSON cron session with 10 total tokens")
	}
}
