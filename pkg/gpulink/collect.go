package gpulink

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	// statsFetchTimeout bounds each engine read while collecting.
	statsFetchTimeout = 5 * time.Second
	// statsMaxBody bounds an engine's /metrics or /slots body.
	statsMaxBody = 8 << 20
	// statsReuse is how long a collected sample answers repeat polls.
	statsReuse = 5 * time.Second
)

// statsCollector samples the engine and the GPUs for StatsPath, reusing a
// sample for statsReuse so overlapping polls do not scrape twice.
type statsCollector struct {
	mu     sync.Mutex
	last   *Stats
	lastAt time.Time
}

// collect returns the agent's current Stats.
func (a *Agent) collect(ctx context.Context) *Stats {
	a.stats.mu.Lock()
	defer a.stats.mu.Unlock()
	if a.stats.last != nil && time.Since(a.stats.lastAt) < statsReuse {
		return a.stats.last
	}
	s := &Stats{Version: StatsVersion, CollectedAt: time.Now().UTC()}
	if a.Local != nil {
		// sinter in-process: the model it serves; no engine metrics.
		s.Engine = &EngineStats{Kind: "sinter", Model: filepath.Base(a.State.Model)}
	} else {
		s.Engine, s.GPUs = a.engineStats(ctx)
	}
	s.GPUs = mergeGPUs(s.GPUs, gpusFromSMI(ctx))
	a.stats.last, a.stats.lastAt = s, time.Now()
	return s
}

// engineStats reads the engine's /metrics and /slots, falling back to its
// model list, and any GPU metrics it reports itself.
func (a *Agent) engineStats(ctx context.Context) (*EngineStats, []GPUStats) {
	var e *EngineStats
	var gpus []GPUStats
	if body, ok := a.engineGet(ctx, "/metrics"); ok {
		m := parseProm(strings.NewReader(string(body)))
		e = engineFromMetrics(m)
		gpus = gpusFromMetrics(m)
	}
	if body, ok := a.engineGet(ctx, "/slots"); ok {
		if e == nil {
			e = &EngineStats{}
		}
		applySlots(e, body)
	}
	if e == nil || e.Model == "" {
		if body, ok := a.engineGet(ctx, "/v1/models"); ok {
			var list struct {
				Data []struct {
					ID string `json:"id"`
				} `json:"data"`
			}
			if json.Unmarshal(body, &list) == nil && len(list.Data) > 0 {
				if e == nil {
					e = &EngineStats{}
				}
				e.Model = list.Data[0].ID
			}
		}
	}
	return e, gpus
}

// engineGet reads one engine path with the engine's key; ok is false for
// anything but a 200.
func (a *Agent) engineGet(ctx context.Context, path string) ([]byte, bool) {
	ctx, cancel := context.WithTimeout(ctx, statsFetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+a.State.Engine+path, nil)
	if err != nil {
		return nil, false
	}
	if a.EngineKey != "" {
		req.Header.Set("Authorization", "Bearer "+a.EngineKey)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, false
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, false
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, statsMaxBody))
	return body, err == nil
}

// serveStats answers StatsPath.
func (a *Agent) serveStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(a.collect(r.Context()))
}
