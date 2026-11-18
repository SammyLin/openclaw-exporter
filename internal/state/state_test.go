package state

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/klauspost/compress/zstd"
)

func TestOpenROReadsWALDatabase(t *testing.T) {
	home := t.TempDir()
	path := AgentDBPath(home, "main")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	w, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	for _, q := range []string{
		"PRAGMA journal_mode=WAL",
		"CREATE TABLE t (v TEXT)",
		"INSERT INTO t VALUES ('ok')",
	} {
		if _, err := w.Exec(q); err != nil {
			t.Fatal(q, err)
		}
	}

	r, err := OpenRO(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	var v string
	if err := r.QueryRow("SELECT v FROM t").Scan(&v); err != nil || v != "ok" {
		t.Fatalf("got %q, %v", v, err)
	}
	if _, err := r.Exec("INSERT INTO t VALUES ('no')"); err == nil {
		t.Fatal("expected write to fail on read-only connection")
	}

	agents := ListAgents(home)
	if len(agents) != 1 || agents[0].ID != "main" || agents[0].DBPath != path {
		t.Fatalf("ListAgents = %+v", agents)
	}
}

func TestOpenROMissing(t *testing.T) {
	if _, err := OpenRO(filepath.Join(t.TempDir(), "nope.sqlite")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("want ErrNotExist, got %v", err)
	}
}

func TestDecodeEvent(t *testing.T) {
	body := `{"message":{"role":"assistant"}}`
	got, err := DecodeEvent(sql.NullString{String: body, Valid: true}, nil)
	if err != nil || string(got) != body {
		t.Fatalf("plain: %q %v", got, err)
	}

	enc, _ := zstd.NewWriter(nil)
	got, err = DecodeEvent(sql.NullString{}, enc.EncodeAll([]byte(body), nil))
	if err != nil || string(got) != body {
		t.Fatalf("zstd: %q %v", got, err)
	}

	if _, err := DecodeEvent(sql.NullString{}, nil); !errors.Is(err, ErrColdEvent) {
		t.Fatalf("cold: %v", err)
	}
}
