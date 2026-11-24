package collector

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// setupSessionsJSON writes a sessions.json for the given agent and returns the home dir.
func setupSessionsJSON(t *testing.T, agentName, content string) string {
	t.Helper()
	home := t.TempDir()
	dir := filepath.Join(home, "agents", agentName, "sessions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sessions.json"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return home
}

func TestLoadSessions_MapFormat_UsesKeyAsSessionID(t *testing.T) {
	// Map format where session IDs are only in the keys, not in the values.
	// Before the fix, SessionID would be "" for all entries.
	jsonData := `{
		"abc123": {"label": "Session A", "updatedAt": 1712000000000},
		"def456": {"label": "Session B", "updatedAt": 1712100000000}
	}`
	home := setupSessionsJSON(t, "main", jsonData)

	sessions := loadSessions(home, "main")
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sessions))
	}

	// Sort for deterministic order (map iteration is random)
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].SessionID < sessions[j].SessionID
	})

	if sessions[0].SessionID != "abc123" {
		t.Errorf("expected SessionID 'abc123', got %q", sessions[0].SessionID)
	}
	if sessions[1].SessionID != "def456" {
		t.Errorf("expected SessionID 'def456', got %q", sessions[1].SessionID)
	}
}

func TestLoadSessions_MapFormat_PreservesExplicitSessionID(t *testing.T) {
	// When the value already has a sessionId, don't overwrite it with the key.
	jsonData := `{
		"key1": {"sessionId": "explicit-id", "label": "Has ID", "updatedAt": 1712000000000}
	}`
	home := setupSessionsJSON(t, "main", jsonData)

	sessions := loadSessions(home, "main")
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	if sessions[0].SessionID != "explicit-id" {
		t.Errorf("expected SessionID 'explicit-id', got %q", sessions[0].SessionID)
	}
}

func TestLoadSessions_ArrayFormat(t *testing.T) {
	jsonData := `[
		{"sessionId": "sess-1", "label": "First", "updatedAt": 1712000000000},
		{"sessionId": "sess-2", "label": "Second", "updatedAt": 1712100000000}
	]`
	home := setupSessionsJSON(t, "main", jsonData)

	sessions := loadSessions(home, "main")
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sessions))
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].SessionID < sessions[j].SessionID
	})

	if sessions[0].SessionID != "sess-1" {
		t.Errorf("expected SessionID 'sess-1', got %q", sessions[0].SessionID)
	}
	if sessions[1].SessionID != "sess-2" {
		t.Errorf("expected SessionID 'sess-2', got %q", sessions[1].SessionID)
	}
}

func TestLoadSessions_MissingFile_ReturnsNil(t *testing.T) {
	home := t.TempDir()
	sessions := loadSessions(home, "nonexistent")
	if sessions != nil {
		t.Errorf("expected nil for missing file, got %v", sessions)
	}
}

func TestLoadSessions_InvalidJSON_ReturnsNil(t *testing.T) {
	home := setupSessionsJSON(t, "main", `{not valid json}`)
	sessions := loadSessions(home, "main")
	if sessions != nil {
		t.Errorf("expected nil for invalid JSON, got %v", sessions)
	}
}

func TestCronName(t *testing.T) {
	for _, tt := range []struct {
		s    sessionEntry
		want string
	}{
		{sessionEntry{Label: "Automation: Daily digest"}, "Daily digest"},
		{sessionEntry{Label: "Cron: Nightly"}, "Nightly"},
		{sessionEntry{DisplayName: "Shown", SessionKey: "cron:abc"}, "Shown"},
		{sessionEntry{SessionKey: "agent:main:cron:job42:run:7"}, "job42"},
		{sessionEntry{SessionKey: "cron:job9"}, "job9"},
	} {
		if got := tt.s.cronName(); got != tt.want {
			t.Errorf("cronName(%+v) = %q, want %q", tt.s, got, tt.want)
		}
	}
	if !(sessionEntry{SessionKey: "agent:ops:cron:x"}).isCron() {
		t.Error("cron session key not detected as cron")
	}
	if (sessionEntry{SessionKey: "agent:main:chat"}).isCron() {
		t.Error("chat session detected as cron")
	}
}
