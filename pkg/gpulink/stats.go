package gpulink

import (
	"math"
	"time"
)

// StatsPath is the path the platform polls for a linked GPU's metrics. The
// agent answers it itself (it is never forwarded to the engine).
const StatsPath = "/sprout/stats"

// StatsVersion is the version of the Stats shape.
const StatsVersion = 1

// Stats is one sample of a linked GPU's state, all of it as its engine
// reports it: its load and work, and its GPUs' hardware. Counters and histograms are cumulative since the
// engine started; the platform derives rates and percentiles from the
// difference between samples. A field the engine does not report is absent
// (nil), never zero.
type Stats struct {
	Version     int          `json:"version"`
	CollectedAt time.Time    `json:"collected_at"`
	Engine      *EngineStats `json:"engine,omitempty"`
	GPUs        []GPUStats   `json:"gpus,omitempty"`
}

// EngineStats is what the inference engine reports about its load and work.
type EngineStats struct {
	// Kind is the engine family recognised from its metrics: "ninfer",
	// "vllm", "llama.cpp", "sglang", "sinter", or "" when only its model
	// list could be read.
	Kind  string `json:"kind"`
	Ready *bool  `json:"ready,omitempty"`
	Model string `json:"model,omitempty"`

	MaxConcurrency *float64 `json:"max_concurrency,omitempty"`
	MaxContext     *float64 `json:"max_context_tokens,omitempty"`
	Running        *float64 `json:"running,omitempty"`
	Waiting        *float64 `json:"waiting,omitempty"`
	Paused         *float64 `json:"paused,omitempty"`
	// KVCacheUsage is the fraction (0..1) of the KV cache in use, where the
	// engine reports one.
	KVCacheUsage *float64 `json:"kv_cache_usage,omitempty"`

	// Requests counts settled requests by outcome (completed, failed,
	// cancelled, rejected, ...), cumulative.
	Requests map[string]float64 `json:"requests,omitempty"`

	PromptTokens       *float64 `json:"prompt_tokens,omitempty"`
	CachedPromptTokens *float64 `json:"cached_prompt_tokens,omitempty"`
	GeneratedTokens    *float64 `json:"generated_tokens,omitempty"`
	SpecDraftTokens    *float64 `json:"spec_draft_tokens,omitempty"`
	SpecAcceptedTokens *float64 `json:"spec_accepted_tokens,omitempty"`
	Preemptions        *float64 `json:"preemptions,omitempty"`

	TimeToFirstToken *Histogram `json:"time_to_first_token_seconds,omitempty"`
	RequestDuration  *Histogram `json:"request_duration_seconds,omitempty"`
	QueueWait        *Histogram `json:"queue_wait_seconds,omitempty"`

	// MemoryBytes is the engine's own device memory by use (weights,
	// context, project folder, ...), where it reports one.
	MemoryBytes map[string]float64 `json:"memory_bytes,omitempty"`
}

// Histogram is a cumulative Prometheus histogram: Count observations summing
// to Sum, Buckets[i].Count of them at or below Buckets[i].LE. The +Inf bucket
// is Count and is not listed.
type Histogram struct {
	Buckets []Bucket `json:"buckets"`
	Sum     float64  `json:"sum"`
	Count   float64  `json:"count"`
}

// Bucket is one cumulative histogram bucket.
type Bucket struct {
	LE    float64 `json:"le"`
	Count float64 `json:"count"`
}

// GPUStats is one GPU's hardware state.
type GPUStats struct {
	Index            int      `json:"index"`
	Name             string   `json:"name,omitempty"`
	MemoryTotalBytes *float64 `json:"memory_total_bytes,omitempty"`
	MemoryUsedBytes  *float64 `json:"memory_used_bytes,omitempty"`
	MemoryFreeBytes  *float64 `json:"memory_free_bytes,omitempty"`
	UtilizationPct   *float64 `json:"utilization_pct,omitempty"`
	MemoryUtilPct    *float64 `json:"memory_utilization_pct,omitempty"`
	TemperatureC     *float64 `json:"temperature_c,omitempty"`
	PowerW           *float64 `json:"power_w,omitempty"`
	PowerLimitW      *float64 `json:"power_limit_w,omitempty"`
	SMClockMHz       *float64 `json:"sm_clock_mhz,omitempty"`
	FanPct           *float64 `json:"fan_pct,omitempty"`
	// Source is where these came from: "engine", the engine's own metrics
	// (the only source: the agent runs nothing on the machine to read GPUs).
	Source string `json:"source"`
}

func ptr(v float64) *float64 { return &v }

// finite drops the +Inf bucket (its count is Histogram.Count) and anything
// JSON cannot carry.
func (h *Histogram) finite() *Histogram {
	if h == nil {
		return nil
	}
	out := &Histogram{Sum: h.Sum, Count: h.Count}
	for _, b := range h.Buckets {
		if !math.IsInf(b.LE, 0) && !math.IsNaN(b.LE) {
			out.Buckets = append(out.Buckets, b)
		}
	}
	return out
}
