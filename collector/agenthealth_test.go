package collector

import (
	"database/sql"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/SammyLin/openclaw-exporter/internal/state"
	_ "modernc.org/sqlite"
)

func newHealthDB(t *testing.T, home, id string, stmts ...string) string {
	t.Helper()
	path := state.AgentDBPath(home, id)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, q := range stmts {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(q, err)
		}
	}
	return path
}

func TestAgentHealthMetrics(t *testing.T) {
	home := t.TempDir()
	now := time.Now()
	future := strconv.FormatInt(now.UnixMilli()+60_000, 10)
	past := strconv.FormatInt(now.UnixMilli()-60_000, 10)

	newHealthDB(t, home, "main",
		`CREATE TABLE schema_meta (meta_key TEXT NOT NULL PRIMARY KEY, role TEXT NOT NULL, schema_version INTEGER NOT NULL,
			agent_id TEXT, app_version TEXT, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL)`,
		`INSERT INTO schema_meta VALUES ('primary', 'agent', 42, 'main', NULL, 0, 0)`,
		`CREATE TABLE auth_profile_state (state_key TEXT NOT NULL PRIMARY KEY, state_json TEXT NOT NULL, updated_at INTEGER NOT NULL)`,
		`INSERT INTO auth_profile_state VALUES ('primary', '{"version":1,"usageStats":{
			"anthropic:a":{"cooldownUntil":`+future+`},
			"openai:b":{"cooldownUntil":`+past+`},
			"openai:c":{"disabledUntil":`+future+`,"cooldownUntil":`+future+`},
			"google:d":{"lastUsed":1}}}', 0)`,
	)
	// Older schema: no health tables at all.
	oldPath := newHealthDB(t, home, "old", `CREATE TABLE t (v TEXT)`)

	got := gather(t, NewAgentHealthCollector(home))
	want := map[string]float64{
		`openclaw_agent_auth_profiles_cooling_down{agent_name="main"}`: 2,
		`openclaw_agent_db_schema_version{agent_name="main"}`:          42,
	}
	// Both agents, including the old-schema one, report only a DB size beyond that.
	for _, id := range []string{"main", "old"} {
		key := `openclaw_agent_db_size_bytes{agent_name="` + id + `"}`
		if got[key] <= 0 {
			t.Fatalf("missing %s in %v", key, got)
		}
		delete(got, key)
	}
	if !maps.Equal(got, want) {
		t.Fatalf("got %v\nwant %v", got, want)
	}

	info, _ := os.Stat(oldPath)
	writeFile(t, oldPath+"-wal", "12345")
	if size, ok := dbSize(oldPath); !ok || size != info.Size()+5 {
		t.Fatalf("dbSize = %d, %v; want %d", size, ok, info.Size()+5)
	}
}
