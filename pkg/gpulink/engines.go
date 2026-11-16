package gpulink

import (
	"encoding/json"
	"sort"
	"strings"
)

// engineFromMetrics maps an engine's /metrics onto EngineStats. The family
// is recognised by its metric prefix. ninfer is mapped from its own scrape;
// vLLM, llama.cpp and SGLang from the metric names they document, the common
// ones only (load, tokens, latency, cache).
func engineFromMetrics(m promMetrics) *EngineStats {
	switch {
	case hasPrefix(m, "ninfer_"):
		return ninferStats(m)
	case hasPrefix(m, "vllm:"):
		return vllmStats(m)
	case hasPrefix(m, "llamacpp:"):
		return llamacppStats(m)
	case hasPrefix(m, "sglang:"):
		return sglangStats(m)
	}
	return nil
}

func hasPrefix(m promMetrics, prefix string) bool {
	for name := range m {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// opt is m.sum as an optional value.
func opt(m promMetrics, names ...string) *float64 {
	if v, ok := m.sum(names...); ok {
		return ptr(v)
	}
	return nil
}

func ninferStats(m promMetrics) *EngineStats {
	e := &EngineStats{Kind: "ninfer", Model: m.label("model_name", "ninfer_model_info")}
	if v, ok := m.sum("ninfer_engine_ready"); ok {
		ready := v > 0
		e.Ready = &ready
	}
	e.MaxConcurrency = opt(m, "ninfer_max_concurrency")
	e.MaxContext = opt(m, "ninfer_max_context_tokens")
	e.Running = opt(m, "ninfer_requests_running")
	e.Waiting = opt(m, "ninfer_requests_waiting")
	e.Paused = opt(m, "ninfer_requests_paused")
	e.Requests = m.byLabel("outcome", "ninfer_requests_total")
	e.PromptTokens = opt(m, "ninfer_prompt_tokens_total")
	e.CachedPromptTokens = opt(m, "ninfer_prompt_tokens_cached_total")
	e.GeneratedTokens = opt(m, "ninfer_generation_tokens_total")
	e.SpecDraftTokens = opt(m, "ninfer_spec_decode_draft_tokens_total")
	e.SpecAcceptedTokens = opt(m, "ninfer_spec_decode_accepted_tokens_total")
	e.Preemptions = opt(m, "ninfer_preemptions_total")
	e.TimeToFirstToken = m.histogram("ninfer_time_to_first_token_seconds").finite()
	e.RequestDuration = m.histogram("ninfer_request_duration_seconds").finite()
	e.QueueWait = m.histogram("ninfer_request_queue_seconds").finite()
	mem := map[string]float64{}
	for _, s := range m["ninfer_memory_allocation_bytes"] {
		if s.labels["location"] == "device" {
			mem[s.labels["resource"]] += s.value
		}
	}
	if len(mem) > 0 {
		e.MemoryBytes = mem
	}
	return e
}

func vllmStats(m promMetrics) *EngineStats {
	e := &EngineStats{Kind: "vllm", Model: m.label("model_name", "vllm:num_requests_running", "vllm:prompt_tokens_total")}
	e.Running = opt(m, "vllm:num_requests_running")
	e.Waiting = opt(m, "vllm:num_requests_waiting")
	e.KVCacheUsage = opt(m, "vllm:kv_cache_usage_perc", "vllm:gpu_cache_usage_perc")
	e.Requests = m.byLabel("finished_reason", "vllm:request_success_total")
	e.PromptTokens = opt(m, "vllm:prompt_tokens_total")
	e.GeneratedTokens = opt(m, "vllm:generation_tokens_total")
	e.CachedPromptTokens = opt(m, "vllm:prefix_cache_hits_total", "vllm:gpu_prefix_cache_hits_total")
	e.SpecDraftTokens = opt(m, "vllm:spec_decode_num_draft_tokens_total")
	e.SpecAcceptedTokens = opt(m, "vllm:spec_decode_num_accepted_tokens_total")
	e.Preemptions = opt(m, "vllm:num_preemptions_total")
	e.TimeToFirstToken = m.histogram("vllm:time_to_first_token_seconds").finite()
	e.RequestDuration = m.histogram("vllm:e2e_request_latency_seconds").finite()
	e.QueueWait = m.histogram("vllm:request_queue_time_seconds").finite()
	return e
}

func llamacppStats(m promMetrics) *EngineStats {
	e := &EngineStats{Kind: "llama.cpp"}
	e.Running = opt(m, "llamacpp:requests_processing")
	e.Waiting = opt(m, "llamacpp:requests_deferred")
	e.KVCacheUsage = opt(m, "llamacpp:kv_cache_usage_ratio")
	e.PromptTokens = opt(m, "llamacpp:prompt_tokens_total")
	e.GeneratedTokens = opt(m, "llamacpp:tokens_predicted_total")
	return e
}

func sglangStats(m promMetrics) *EngineStats {
	e := &EngineStats{Kind: "sglang", Model: m.label("model_name", "sglang:num_running_reqs", "sglang:prompt_tokens_total")}
	e.Running = opt(m, "sglang:num_running_reqs")
	e.Waiting = opt(m, "sglang:num_queue_reqs")
	e.KVCacheUsage = opt(m, "sglang:token_usage")
	e.PromptTokens = opt(m, "sglang:prompt_tokens_total")
	e.GeneratedTokens = opt(m, "sglang:generation_tokens_total")
	e.TimeToFirstToken = m.histogram("sglang:time_to_first_token_seconds").finite()
	e.RequestDuration = m.histogram("sglang:e2e_request_latency_seconds").finite()
	return e
}

// applySlots fills what an engine's /slots tells and its metrics did not:
// ninfer's object (max_concurrency, requests_processing, requests_waiting,
// slots) or llama.cpp's array of slots with is_processing.
func applySlots(e *EngineStats, body []byte) {
	var obj struct {
		Model          string            `json:"model"`
		MaxConcurrency *float64          `json:"max_concurrency"`
		Processing     *float64          `json:"requests_processing"`
		Waiting        *float64          `json:"requests_waiting"`
		Slots          []json.RawMessage `json:"slots"`
	}
	if json.Unmarshal(body, &obj) == nil && (obj.MaxConcurrency != nil || obj.Slots != nil) {
		if e.Model == "" {
			e.Model = obj.Model
		}
		if e.MaxConcurrency == nil {
			if obj.MaxConcurrency != nil {
				e.MaxConcurrency = obj.MaxConcurrency
			} else if len(obj.Slots) > 0 {
				e.MaxConcurrency = ptr(float64(len(obj.Slots)))
			}
		}
		if e.Running == nil {
			e.Running = obj.Processing
		}
		if e.Waiting == nil {
			e.Waiting = obj.Waiting
		}
		return
	}
	var arr []struct {
		IsProcessing bool `json:"is_processing"`
	}
	if json.Unmarshal(body, &arr) == nil && len(arr) > 0 {
		busy := 0
		for _, s := range arr {
			if s.IsProcessing {
				busy++
			}
		}
		if e.MaxConcurrency == nil {
			e.MaxConcurrency = ptr(float64(len(arr)))
		}
		if e.Running == nil {
			e.Running = ptr(float64(busy))
		}
	}
}

// gpusFromMetrics reads GPU hardware metrics an engine (or a DCGM exporter
// it embeds) reports under the DCGM field names, one GPU per "gpu" label.
func gpusFromMetrics(m promMetrics) []GPUStats {
	fields := map[string]func(*GPUStats, float64){
		"DCGM_FI_DEV_GPU_UTIL":         func(g *GPUStats, v float64) { g.UtilizationPct = ptr(v) },
		"DCGM_FI_DEV_MEM_COPY_UTIL":    func(g *GPUStats, v float64) { g.MemoryUtilPct = ptr(v) },
		"DCGM_FI_DEV_GPU_TEMP":         func(g *GPUStats, v float64) { g.TemperatureC = ptr(v) },
		"DCGM_FI_DEV_POWER_USAGE":      func(g *GPUStats, v float64) { g.PowerW = ptr(v) },
		"DCGM_FI_DEV_POWER_MGMT_LIMIT": func(g *GPUStats, v float64) { g.PowerLimitW = ptr(v) },
		"DCGM_FI_DEV_SM_CLOCK":         func(g *GPUStats, v float64) { g.SMClockMHz = ptr(v) },
		"DCGM_FI_DEV_FAN_SPEED":        func(g *GPUStats, v float64) { g.FanPct = ptr(v) },
		"DCGM_FI_DEV_FB_USED":          func(g *GPUStats, v float64) { g.MemoryUsedBytes = ptr(v * 1024 * 1024) },
	}
	byIndex := map[string]*GPUStats{}
	order := []string{}
	for name, set := range fields {
		for _, s := range m[name] {
			idx := s.labels["gpu"]
			g, ok := byIndex[idx]
			if !ok {
				g = &GPUStats{Source: "engine", Name: s.labels["modelName"]}
				if n, err := parseIndex(idx); err == nil {
					g.Index = n
				}
				byIndex[idx] = g
				order = append(order, idx)
			}
			set(g, s.value)
		}
	}
	for _, s := range m["DCGM_FI_DEV_FB_FREE"] {
		if g, ok := byIndex[s.labels["gpu"]]; ok && g.MemoryUsedBytes != nil {
			g.MemoryTotalBytes = ptr(*g.MemoryUsedBytes + s.value*1024*1024)
		}
	}
	out := make([]GPUStats, 0, len(order))
	for _, idx := range order {
		out = append(out, *byIndex[idx])
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Index < out[j].Index })
	return out
}
