// Package config reads the parts of OpenClaw's openclaw.json the exporter needs:
// the agent roster and per-agent workspace directories.
//
// OpenClaw parses the file as JSON5 (its docs use unquoted keys, comments and
// trailing commas), so it is parsed with a JSON5 decoder here too.
package config

import (
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/titanous/json5"
)

// Agent is one configured roster entry. ID is normalized.
type Agent struct {
	ID        string `json:"id"`
	Default   bool   `json:"default"`
	Workspace string `json:"workspace"`
}

// Config is the subset of openclaw.json the exporter reads.
type Config struct {
	Agents struct {
		Ownership string `json:"ownership"`
		Defaults  struct {
			Workspace string `json:"workspace"`
		} `json:"defaults"`
		Entries map[string]Agent `json:"entries"`
		List    []Agent          `json:"list"`
	} `json:"agents"`

	home string
}

// Path returns the config path: $OPENCLAW_CONFIG_PATH or <home>/openclaw.json.
func Path(home string) string {
	if p := strings.TrimSpace(os.Getenv("OPENCLAW_CONFIG_PATH")); p != "" {
		return expandHome(p)
	}
	return filepath.Join(home, "openclaw.json")
}

// Load reads the config for an OpenClaw home (state dir). A missing or
// unparsable file yields an empty config, which OpenClaw treats as the
// implicit single "main" agent.
func Load(home string) *Config {
	cfg := &Config{home: home}
	path := Path(home)
	raw, err := os.ReadFile(path)
	if err != nil {
		slog.Debug("openclaw config not read", "path", path, "err", err)
		return cfg
	}
	if err := json5.Unmarshal(raw, cfg); err != nil {
		slog.Debug("openclaw config not parsed, using defaults", "path", path, "err", err)
		return &Config{home: home}
	}
	return cfg
}

// Roster returns the configured roster: agents.entries (keyed by id, sorted)
// if present, else agents.list.
func (c *Config) Roster() []Agent {
	var out []Agent
	if c.Agents.Entries != nil {
		for id, a := range c.Agents.Entries {
			a.ID = id
			out = append(out, a)
		}
		slices.SortFunc(out, func(x, y Agent) int { return strings.Compare(x.ID, y.ID) })
	} else {
		out = slices.Clone(c.Agents.List)
	}
	for i := range out {
		out[i].ID = NormalizeAgentID(out[i].ID)
	}
	// An absent roster, or an empty one without explicit ownership, implies
	// {main: {}} (applyImplicitAgentRosterDefaults / listAgentIds).
	hasRoster := c.Agents.Entries != nil || c.Agents.List != nil
	if len(out) == 0 && (!hasRoster || c.Agents.Ownership != "explicit") {
		out = []Agent{{ID: "main"}}
	}
	return out
}

// dataOwner mirrors tryResolveLegacyDataOwner: the single default:true entry
// (unless ownership is explicit), else the sole agent.
func (c *Config) dataOwner() string {
	agents := c.Roster()
	if c.Agents.Ownership != "explicit" {
		var marked []string
		for _, a := range agents {
			if a.Default {
				marked = append(marked, a.ID)
			}
		}
		if len(marked) == 1 {
			return marked[0]
		}
	}
	if len(agents) == 1 {
		return agents[0].ID
	}
	return ""
}

// WorkspaceDir mirrors resolveAgentWorkspaceDir:
//  1. the agent's own workspace
//  2. data-owner agent: agents.defaults.workspace, else <home>/workspace
//  3. others: <agents.defaults.workspace>/<id>, else <home>/workspace-<id>
func (c *Config) WorkspaceDir(agentID string) string {
	id := NormalizeAgentID(agentID)
	for _, a := range c.Roster() {
		if a.ID == id {
			if ws := strings.TrimSpace(a.Workspace); ws != "" {
				return expandHome(ws)
			}
			break
		}
	}
	fallback := strings.TrimSpace(c.Agents.Defaults.Workspace)
	if id == c.dataOwner() {
		if fallback != "" {
			return expandHome(fallback)
		}
		// ponytail: ignores OPENCLAW_WORKSPACE_DIR/OPENCLAW_PROFILE; add if someone runs those.
		return filepath.Join(c.home, "workspace")
	}
	if fallback != "" {
		return filepath.Join(expandHome(fallback), id)
	}
	return filepath.Join(c.home, "workspace-"+id)
}

var (
	validIDRe   = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)
	invalidIDRe = regexp.MustCompile(`[^a-z0-9_-]+`)
)

// NormalizeAgentID mirrors OpenClaw's normalizeAgentId.
func NormalizeAgentID(v string) string {
	id := strings.ToLower(strings.TrimSpace(v))
	if validIDRe.MatchString(id) {
		return id
	}
	id = strings.Trim(invalidIDRe.ReplaceAllString(id, "-"), "-")
	if len(id) > 64 {
		id = id[:64]
	}
	if id == "" {
		return "main"
	}
	return id
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, p[1:])
		}
	}
	return filepath.Clean(p)
}
