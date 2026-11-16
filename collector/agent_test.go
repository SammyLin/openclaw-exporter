package collector

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeSessionFile creates a .jsonl session file with the given lines and sets its mtime.
func writeSessionFile(t *testing.T, agentPath, name string, lines []string, mtime time.Time) {
	t.Helper()
	dir := filepath.Join(agentPath, "sessions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range lines {
		fmt.Fprintln(f, l)
	}
	f.Close()
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}

func TestGetAgentState_ToolCallRecent_ReturnsWorking(t *testing.T) {
	agentPath := t.TempDir()
	line := `{"message":{"role":"assistant","content":[{"type":"toolCall"}]}}`
	writeSessionFile(t, agentPath, "sess1.jsonl", []string{line}, time.Now().Add(-10*time.Second))

	state, ts := getAgentState(agentPath)
	if state != "working" {
		t.Errorf("expected state 'working', got %q", state)
	}
	if ts == 0 {
		t.Error("expected non-zero timestamp")
	}
}

func TestGetAgentState_ToolCallExpired_ReturnsIdle(t *testing.T) {
	// toolCall was 90s ago — past the 60s threshold.
	// Before the fix, this would return "working" due to fallthrough
	// to the generic assistant role check (300s timeout).
	agentPath := t.TempDir()
	line := `{"message":{"role":"assistant","content":[{"type":"toolCall"}]}}`
	writeSessionFile(t, agentPath, "sess1.jsonl", []string{line}, time.Now().Add(-90*time.Second))

	state, _ := getAgentState(agentPath)
	if state != "idle" {
		t.Errorf("expected state 'idle' (toolCall expired at 60s), got %q", state)
	}
}

func TestGetAgentState_ThinkingRecent_ReturnsThinking(t *testing.T) {
	agentPath := t.TempDir()
	line := `{"message":{"role":"assistant","content":[{"type":"thinking"}]}}`
	writeSessionFile(t, agentPath, "sess1.jsonl", []string{line}, time.Now().Add(-30*time.Second))

	state, _ := getAgentState(agentPath)
	if state != "thinking" {
		t.Errorf("expected state 'thinking', got %q", state)
	}
}

func TestGetAgentState_ThinkingExpired_ReturnsIdle(t *testing.T) {
	// thinking was 150s ago — past the 120s threshold.
	// Before the fix, this would return "working" due to fallthrough.
	agentPath := t.TempDir()
	line := `{"message":{"role":"assistant","content":[{"type":"thinking"}]}}`
	writeSessionFile(t, agentPath, "sess1.jsonl", []string{line}, time.Now().Add(-150*time.Second))

	state, _ := getAgentState(agentPath)
	if state != "idle" {
		t.Errorf("expected state 'idle' (thinking expired at 120s), got %q", state)
	}
}

func TestGetAgentState_AssistantTextRecent_ReturnsWorking(t *testing.T) {
	// Plain assistant message (content is a string, not an array) within 300s
	// should still return "working" via the role check.
	agentPath := t.TempDir()
	line := `{"message":{"role":"assistant","content":"Hello, how can I help?"}}`
	writeSessionFile(t, agentPath, "sess1.jsonl", []string{line}, time.Now().Add(-100*time.Second))

	state, _ := getAgentState(agentPath)
	if state != "working" {
		t.Errorf("expected state 'working' for recent assistant text, got %q", state)
	}
}

func TestGetAgentState_AssistantTextExpired_ReturnsIdle(t *testing.T) {
	// Plain assistant message beyond 300s should return idle.
	agentPath := t.TempDir()
	line := `{"message":{"role":"assistant","content":"Hello"}}`
	writeSessionFile(t, agentPath, "sess1.jsonl", []string{line}, time.Now().Add(-400*time.Second))

	state, _ := getAgentState(agentPath)
	if state != "idle" {
		t.Errorf("expected state 'idle' (assistant text expired at 300s), got %q", state)
	}
}

func TestGetAgentState_NoSessions_ReturnsIdle(t *testing.T) {
	agentPath := t.TempDir()
	state, ts := getAgentState(agentPath)
	if state != "idle" {
		t.Errorf("expected state 'idle', got %q", state)
	}
	if ts != 0 {
		t.Errorf("expected zero timestamp, got %d", ts)
	}
}

func TestCountSessions(t *testing.T) {
	agentPath := t.TempDir()
	sessDir := filepath.Join(agentPath, "sessions")
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.jsonl", "b.jsonl", "c.txt"} {
		os.WriteFile(filepath.Join(sessDir, name), []byte("{}"), 0o644)
	}

	count := countSessions(agentPath)
	if count != 2 {
		t.Errorf("expected 2 .jsonl sessions, got %d", count)
	}
}
