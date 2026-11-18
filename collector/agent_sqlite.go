package collector

import (
	"database/sql"
	"errors"
	"log/slog"
	"os"
	"slices"
	"time"

	"github.com/SammyLin/openclaw-exporter/internal/state"
)

// OpenClaw >= 2026.8.1 keeps sessions and transcripts in the per-agent SQLite
// DB (agents/<id>/agent/openclaw-agent.sqlite) instead of sessions/*.json(l).

// ponytail: usage is summed over the newest maxUsageEvents assistant events per
// session; very long sessions are undercounted. Raise it or page by seq if that matters.
const maxUsageEvents = 2000

type agentSnapshot struct {
	sessions  int
	byStatus  map[string]int
	state     string
	timestamp int64
}

// readAgentSQLite reads session counts and the current state from an agent DB.
// Returns os.ErrNotExist when dbPath is empty or missing.
func readAgentSQLite(dbPath string) (agentSnapshot, error) {
	snap := agentSnapshot{state: "idle", byStatus: map[string]int{}}
	if dbPath == "" {
		return snap, os.ErrNotExist
	}
	db, err := state.OpenRO(dbPath)
	if err != nil {
		return snap, err
	}
	defer db.Close()

	rows, err := db.Query(`SELECT coalesce(status, 'unknown'), count(*) FROM session_nodes
		WHERE archived_at IS NULL GROUP BY 1`)
	if err != nil {
		return snap, err
	}
	for rows.Next() {
		var status string
		var n int
		if err := rows.Scan(&status, &n); err != nil {
			rows.Close()
			return snap, err
		}
		snap.byStatus[status] = n
		snap.sessions += n
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return snap, err
	}

	// Last 10 events of the most recently updated session window.
	rows, err = db.Query(`SELECT event_json, event_zstd, created_at FROM transcript_events
		WHERE session_id = (SELECT session_id FROM session_windows ORDER BY updated_at DESC LIMIT 1)
		ORDER BY seq DESC LIMIT 10`)
	if err != nil {
		return snap, err
	}
	defer rows.Close()
	var events []string // newest first
	for rows.Next() {
		var eventJSON sql.NullString
		var eventZstd []byte
		var createdAt int64
		if err := rows.Scan(&eventJSON, &eventZstd, &createdAt); err != nil {
			return snap, err
		}
		snap.timestamp = max(snap.timestamp, createdAt/1000)
		body, err := state.DecodeEvent(eventJSON, eventZstd)
		if err != nil {
			if !errors.Is(err, state.ErrColdEvent) {
				slog.Debug("Failed to decode transcript event", "db", dbPath, "err", err)
			}
			continue
		}
		events = append(events, string(body))
	}
	if err := rows.Err(); err != nil {
		return snap, err
	}
	slices.Reverse(events)
	if snap.timestamp > 0 {
		snap.state = stateFromEvents(events, time.Now().Unix()-snap.timestamp)
	}
	return snap, nil
}

// loadSQLiteSessions lists non-archived sessions from session_nodes.
func loadSQLiteSessions(db *sql.DB) ([]sessionEntry, error) {
	rows, err := db.Query(`SELECT session_key, current_session_id, coalesce(label, ''),
		coalesce(display_name, ''), coalesce(created_via, ''), updated_at
		FROM session_nodes WHERE archived_at IS NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var sessions []sessionEntry
	for rows.Next() {
		var s sessionEntry
		if err := rows.Scan(&s.SessionKey, &s.SessionID, &s.Label, &s.DisplayName, &s.CreatedVia, &s.UpdatedAt); err != nil {
			return nil, err
		}
		sessions = append(sessions, s)
	}
	return sessions, rows.Err()
}

// readSQLiteUsage sums usage over a session's assistant transcript events.
// Returns nil when no event carries usage.
func readSQLiteUsage(db *sql.DB, sessionID string) *usageTotals {
	// message_role may be NULL on rows written before the column was
	// materialized, so those are read and filtered by JSON parsing.
	rows, err := db.Query(`SELECT event_json, event_zstd FROM transcript_events
		WHERE session_id = ? AND (message_role = 'assistant' OR message_role IS NULL)
		ORDER BY seq DESC LIMIT ?`, sessionID, maxUsageEvents)
	if err != nil {
		slog.Debug("Failed to query transcript events", "session", sessionID, "err", err)
		return nil
	}
	defer rows.Close()
	totals := &usageTotals{}
	count := 0
	for rows.Next() {
		var eventJSON sql.NullString
		var eventZstd []byte
		if err := rows.Scan(&eventJSON, &eventZstd); err != nil {
			slog.Debug("Failed to scan transcript event", "session", sessionID, "err", err)
			return nil
		}
		body, err := state.DecodeEvent(eventJSON, eventZstd)
		if err != nil {
			continue // cold-archived or corrupt
		}
		if totals.add(body) {
			count++
		}
	}
	if count == 0 {
		return nil
	}
	return totals
}
