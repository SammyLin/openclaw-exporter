package collector

import (
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

const namespace = "openclaw"

// StateMap maps agent state strings to numeric values.
var StateMap = map[string]float64{
	"idle":     0,
	"working":  1,
	"thinking": 2,
	"error":    3,
}

// WorkspaceMap maps directory names to agent names.
var WorkspaceMap = map[string]string{
	"workspace":           "main",
	"workspace-kanbei":    "kanbei",
	"workspace-mitsunari": "mitsunari",
	"workspace-leyoyo":    "leyoyo",
}

// AgentNames is the list of known agent names.
var AgentNames = []string{"main", "kanbei", "mitsunari", "leyoyo"}

const sevenDaysMs = 7 * 24 * 3600 * 1000

// CachedCollector wraps a collect function with a TTL cache.
type CachedCollector struct {
	mu      sync.Mutex
	ttl     time.Duration
	lastAt  time.Time
	cached  []prometheus.Metric
	descs   []*prometheus.Desc
	collect func() []prometheus.Metric
}

// NewCachedCollector creates a CachedCollector with the given TTL, descriptors, and collect function.
func NewCachedCollector(ttl time.Duration, descs []*prometheus.Desc, fn func() []prometheus.Metric) *CachedCollector {
	return &CachedCollector{
		ttl:     ttl,
		descs:   descs,
		collect: fn,
	}
}

// Describe sends all descriptors.
func (c *CachedCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range c.descs {
		ch <- d
	}
}

// Collect returns cached metrics or refreshes if TTL expired.
func (c *CachedCollector) Collect(ch chan<- prometheus.Metric) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if time.Since(c.lastAt) > c.ttl {
		c.cached = c.collect()
		c.lastAt = time.Now()
	}
	for _, m := range c.cached {
		ch <- m
	}
}
