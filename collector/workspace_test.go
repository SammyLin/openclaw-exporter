package collector

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestResolveWorkspaces(t *testing.T) {
	t.Setenv("OPENCLAW_CONFIG_PATH", "")
	userHome, _ := os.UserHomeDir()
	custom := t.TempDir()

	tests := []struct {
		name   string
		config string // empty = no openclaw.json
		disk   []string
		want   func(home string) map[string]string
	}{
		{
			name: "no config is implicit main",
			want: func(h string) map[string]string { return map[string]string{"main": filepath.Join(h, "workspace")} },
		},
		{
			name: "no config plus on-disk agents",
			disk: []string{"kanbei"},
			want: func(h string) map[string]string {
				return map[string]string{"main": filepath.Join(h, "workspace"), "kanbei": filepath.Join(h, "workspace-kanbei")}
			},
		},
		{
			name: "list roster with default marker, JSON5 comments and trailing commas",
			config: `{
				// comment
				"agents": {
					/* block */
					"list": [
						{"id": "Main", "default": true},
						{"id": "kanbei", "workspace": "~/ws/kanbei"},
						{"id": "leyoyo", "name": "a // not a comment",},
					],
				},
			}`,
			want: func(h string) map[string]string {
				return map[string]string{
					"main":   filepath.Join(h, "workspace"),
					"kanbei": filepath.Join(userHome, "ws", "kanbei"),
					"leyoyo": filepath.Join(h, "workspace-leyoyo"),
				}
			},
		},
		{
			name:   "entries roster with defaults.workspace",
			config: `{"agents": {"defaults": {"workspace": "` + custom + `"}, "entries": {"main": {"default": true}, "ops": {}}}}`,
			want: func(h string) map[string]string {
				return map[string]string{"main": custom, "ops": filepath.Join(custom, "ops")}
			},
		},
		{
			name:   "sole agent is data owner",
			config: `{"agents": {"entries": {"solo": {}}}}`,
			want:   func(h string) map[string]string { return map[string]string{"solo": filepath.Join(h, "workspace")} },
		},
		{
			name:   "no default marker in multi-agent roster; duplicate paths deduped",
			config: `{"agents": {"entries": {"solo": {}, "twin": {"workspace": "` + filepath.Join(custom, "x") + `"}, "alpha": {"workspace": "` + filepath.Join(custom, "x") + `"}}}}`,
			want: func(h string) map[string]string {
				return map[string]string{"alpha": filepath.Join(custom, "x"), "solo": filepath.Join(h, "workspace-solo")}
			},
		},
		{
			name:   "unparsable config falls back to defaults",
			config: `{agents: {list: [{id: 'x'}]}}`,
			want:   func(h string) map[string]string { return map[string]string{"main": filepath.Join(h, "workspace")} },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			if tt.config != "" {
				writeFile(t, filepath.Join(home, "openclaw.json"), tt.config)
			}
			for _, id := range tt.disk {
				if err := os.MkdirAll(filepath.Join(home, "agents", id, "sessions"), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			got := resolveWorkspaces(home)
			if want := tt.want(home); !maps.Equal(got, want) {
				t.Fatalf("got %v\nwant %v", got, want)
			}
		})
	}
}

func TestWorkspaceMetrics(t *testing.T) {
	home := t.TempDir()
	ws := filepath.Join(t.TempDir(), "custom")
	t.Setenv("OPENCLAW_CONFIG_PATH", filepath.Join(home, "elsewhere.json"))
	writeFile(t, filepath.Join(home, "elsewhere.json"), `{"agents":{"list":[{"id":"main","workspace":"`+ws+`"}]}}`)
	writeFile(t, filepath.Join(ws, "AGENTS.md"), strings.Repeat("a", 70))
	writeFile(t, filepath.Join(ws, "memory", "today.md"), strings.Repeat("b", 35))
	writeFile(t, filepath.Join(ws, "notes.txt"), "ignored")

	got := gather(t, NewWorkspaceCollector(home))
	want := map[string]float64{
		`openclaw_md_workspace_bytes{workspace="main"}`:                                  105,
		`openclaw_md_workspace_tokens_estimated{workspace="main"}`:                       30,
		`openclaw_md_file_bytes{filename="AGENTS.md",workspace="main"}`:                  70,
		`openclaw_md_file_tokens_estimated{filename="AGENTS.md",workspace="main"}`:       20,
		`openclaw_md_file_bytes{filename="memory/today.md",workspace="main"}`:            35,
		`openclaw_md_file_tokens_estimated{filename="memory/today.md",workspace="main"}`: 10,
	}
	if !maps.Equal(got, want) {
		t.Fatalf("got %v\nwant %v", got, want)
	}
}

// gather collects c into series keys like name{k="v",...} -> value.
func gather(t *testing.T, c prometheus.Collector) map[string]float64 {
	t.Helper()
	reg := prometheus.NewRegistry()
	reg.MustRegister(c)
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]float64{}
	for _, mf := range mfs {
		for _, m := range mf.GetMetric() {
			var labels []string
			for _, lp := range m.GetLabel() {
				labels = append(labels, fmt.Sprintf("%s=%q", lp.GetName(), lp.GetValue()))
			}
			out[mf.GetName()+"{"+strings.Join(labels, ",")+"}"] = m.GetGauge().GetValue()
		}
	}
	return out
}
