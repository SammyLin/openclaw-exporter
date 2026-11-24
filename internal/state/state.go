// Package state opens OpenClaw's SQLite state databases read-only.
//
// Since OpenClaw 2026.6.1 (cron) and 2026.8.1 (sessions/transcripts), runtime
// state lives in SQLite instead of JSON files:
//
//	<home>/state/openclaw.sqlite                       shared gateway state
//	<home>/agents/<id>/agent/openclaw-agent.sqlite     per-agent state
package state

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/klauspost/compress/zstd"
	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

// Agent is one OpenClaw agent discovered on disk.
type Agent struct {
	ID     string
	Dir    string // <home>/agents/<id>
	DBPath string // per-agent SQLite path; empty if the agent has no DB yet
}

// StateDBPath returns the shared state database path.
func StateDBPath(home string) string {
	return filepath.Join(home, "state", "openclaw.sqlite")
}

// AgentDBPath returns the per-agent database path.
func AgentDBPath(home, agentID string) string {
	return filepath.Join(home, "agents", agentID, "agent", "openclaw-agent.sqlite")
}

// Exists reports whether path is an existing regular file.
func Exists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// OpenRO opens a SQLite database read-only. The gateway writes continuously
// with WAL enabled, so this uses mode=ro (not immutable) and a busy timeout.
// Returns os.ErrNotExist if the file is missing.
func OpenRO(path string) (*sql.DB, error) {
	if !Exists(path) {
		return nil, fmt.Errorf("open %s: %w", path, os.ErrNotExist)
	}
	query := "mode=ro&_pragma=busy_timeout(2000)&_pragma=query_only(1)"
	if !sidecarsExist(path) {
		// No -wal/-shm means no live writer (the gateway keeps -shm while it
		// has the DB open). In that state SQLite would try to create -shm for
		// a WAL database and fail on a read-only mount (SQLITE_READONLY_DIRECTORY),
		// so open it immutable instead.
		query += "&immutable=1"
	}
	dsn := (&url.URL{
		Scheme:   "file",
		Path:     path,
		RawQuery: query,
	}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

func sidecarsExist(path string) bool {
	_, errWAL := os.Stat(path + "-wal")
	_, errSHM := os.Stat(path + "-shm")
	return errWAL == nil || errSHM == nil
}

// ListAgents returns agents found under <home>/agents, sorted by ID.
// A directory counts as an agent if it has an agent DB or a legacy sessions/ dir.
func ListAgents(home string) []Agent {
	entries, err := os.ReadDir(filepath.Join(home, "agents"))
	if err != nil {
		return nil
	}
	var agents []Agent
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		id := e.Name()
		a := Agent{ID: id, Dir: filepath.Join(home, "agents", id)}
		if p := AgentDBPath(home, id); Exists(p) {
			a.DBPath = p
		} else if _, err := os.Stat(filepath.Join(a.Dir, "sessions")); err != nil {
			continue
		}
		agents = append(agents, a)
	}
	slices.SortFunc(agents, func(x, y Agent) int { return strings.Compare(x.ID, y.ID) })
	return agents
}

// ErrColdEvent means the transcript body was moved to cold storage.
var ErrColdEvent = errors.New("transcript event is in cold storage")

var zstdDecoder = sync.OnceValue(func() *zstd.Decoder {
	d, err := zstd.NewReader(nil, zstd.WithDecoderConcurrency(1), zstd.WithDecoderMaxMemory(64<<20))
	if err != nil {
		panic(err) // only fails on invalid options
	}
	return d
})

// maxCompressedEventBytes mirrors OpenClaw's MAX_COMPRESSED_EVENT_BYTES.
const maxCompressedEventBytes = 4 << 20

// DecodeEvent returns the JSON body of a transcript_events row, which is
// stored either as event_json text or as a zstd frame in event_zstd.
func DecodeEvent(eventJSON sql.NullString, eventZstd []byte) ([]byte, error) {
	if eventJSON.Valid {
		return []byte(eventJSON.String), nil
	}
	if len(eventZstd) == 0 {
		return nil, ErrColdEvent
	}
	if len(eventZstd) > maxCompressedEventBytes {
		return nil, fmt.Errorf("compressed event too large: %d bytes", len(eventZstd))
	}
	return zstdDecoder().DecodeAll(eventZstd, nil)
}
