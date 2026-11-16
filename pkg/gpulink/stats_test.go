package gpulink

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
)

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestNinferMetricsMapToEngineStats(t *testing.T) {
	m := parseProm(strings.NewReader(string(readFixture(t, "ninfer_metrics.txt"))))
	e := engineFromMetrics(m)
	if e == nil || e.Kind != "ninfer" || e.Model != "example-model" {
		t.Fatalf("engine = %+v", e)
	}
	if e.Ready == nil || !*e.Ready || *e.MaxConcurrency != 16 || *e.MaxContext != 262144 || *e.Running != 0 || *e.Waiting != 0 {
		t.Fatalf("load fields = %+v", e)
	}
	if e.Requests["completed"] != 64 || *e.PromptTokens != 531233 || *e.GeneratedTokens != 15550 {
		t.Fatalf("counters = %+v %v %v", e.Requests, *e.PromptTokens, *e.GeneratedTokens)
	}
	if *e.SpecDraftTokens != 19257 || *e.SpecAcceptedTokens != 9055 {
		t.Fatalf("spec decode = %v/%v", *e.SpecAcceptedTokens, *e.SpecDraftTokens)
	}
	h := e.TimeToFirstToken
	if h == nil || h.Count != 64 || h.Sum < 750 || h.Sum > 751 || len(h.Buckets) == 0 {
		t.Fatalf("ttft = %+v", h)
	}
	for _, b := range h.Buckets {
		if b.LE > 1e300 {
			t.Fatal("the +Inf bucket is dropped (it is Count)")
		}
	}
	if e.MemoryBytes["weights"] != 79299675648 || e.MemoryBytes["context"] == 0 {
		t.Fatalf("memory = %+v", e.MemoryBytes)
	}
	if _, err := json.Marshal(e); err != nil {
		t.Fatalf("stats must encode as JSON: %v", err)
	}
}

func TestSlotsFillWhatMetricsLack(t *testing.T) {
	e := &EngineStats{}
	applySlots(e, readFixture(t, "ninfer_slots.json"))
	if e.Model != "example-model" || *e.MaxConcurrency != 16 || *e.Running != 0 || *e.Waiting != 0 {
		t.Fatalf("ninfer slots = %+v", e)
	}
	l := &EngineStats{Kind: "llama.cpp"}
	applySlots(l, []byte(`[{"id":0,"is_processing":true},{"id":1,"is_processing":false}]`))
	if *l.MaxConcurrency != 2 || *l.Running != 1 {
		t.Fatalf("llama.cpp slots = %+v", l)
	}
}

func TestNinferReportsItsGPUs(t *testing.T) {
	m := parseProm(strings.NewReader(string(readFixture(t, "ninfer_metrics.txt"))))
	gpus := gpusFromMetrics(m)
	if len(gpus) != 1 {
		t.Fatalf("gpus = %+v", gpus)
	}
	g := gpus[0]
	if g.Index != 0 || g.Source != "engine" || g.Name != "Example GPU" {
		t.Fatalf("identity = %+v", g)
	}
	if *g.MemoryTotalBytes != 102641958912 || *g.MemoryUsedBytes != 101433409536 || *g.MemoryFreeBytes != 1208549376 {
		t.Fatalf("memory = %v/%v/%v", *g.MemoryUsedBytes, *g.MemoryTotalBytes, *g.MemoryFreeBytes)
	}
	if *g.TemperatureC != 33 || *g.PowerLimitW != 450 || *g.SMClockMHz != 2610 || *g.FanPct != 30 || *g.UtilizationPct != 0 || *g.MemoryUtilPct != 0 {
		t.Fatalf("gauges = %+v", g)
	}
	if gpusFromMetrics(parseProm(strings.NewReader("vllm:num_requests_running 1\n"))) != nil {
		t.Fatal("an engine without GPU metrics reports no GPUs")
	}
}

func TestVLLMMetrics(t *testing.T) {
	m := parseProm(strings.NewReader(`vllm:num_requests_running{model_name="m"} 3
vllm:num_requests_waiting{model_name="m"} 1
vllm:kv_cache_usage_perc{model_name="m"} 0.42
vllm:prompt_tokens_total{model_name="m"} 1000
vllm:generation_tokens_total{model_name="m"} 200
vllm:request_success_total{finished_reason="stop",model_name="m"} 7
vllm:time_to_first_token_seconds_bucket{le="0.5",model_name="m"} 5
vllm:time_to_first_token_seconds_bucket{le="+Inf",model_name="m"} 7
vllm:time_to_first_token_seconds_sum{model_name="m"} 3.5
vllm:time_to_first_token_seconds_count{model_name="m"} 7
`))
	e := engineFromMetrics(m)
	if e.Kind != "vllm" || e.Model != "m" || *e.Running != 3 || *e.Waiting != 1 || *e.KVCacheUsage != 0.42 || e.Requests["stop"] != 7 {
		t.Fatalf("vllm = %+v", e)
	}
	if e.TimeToFirstToken.Count != 7 || len(e.TimeToFirstToken.Buckets) != 1 {
		t.Fatalf("ttft = %+v", e.TimeToFirstToken)
	}
}

// The agent answers the stats path itself, from the engine's /metrics and
// /slots and nvidia-smi; the engine never sees a request for it.
func TestAgentServesStats(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	engine := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.URL.Path)
		mu.Unlock()
		switch r.URL.Path {
		case "/metrics":
			_, _ = w.Write(readFixture(t, "ninfer_metrics.txt"))
		case "/slots":
			_, _ = w.Write(readFixture(t, "ninfer_slots.json"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer engine.Close()
	a := &Agent{State: &State{Engine: strings.TrimPrefix(engine.URL, "http://")}}
	rr := httptest.NewRecorder()
	a.Handler()("", rr, httptest.NewRequest(http.MethodGet, StatsPath, nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("stats: %d %s", rr.Code, rr.Body.String())
	}
	var s Stats
	if err := json.Unmarshal(rr.Body.Bytes(), &s); err != nil {
		t.Fatal(err)
	}
	if s.Version != StatsVersion || s.Engine == nil || s.Engine.Kind != "ninfer" || len(s.GPUs) != 1 {
		t.Fatalf("stats = %+v", s)
	}
	if g := s.GPUs[0]; *g.TemperatureC != 33 || g.Name != "Example GPU" || *g.MemoryTotalBytes != 102641958912 || g.Source != "engine" {
		t.Fatalf("the engine's GPU figures: %+v", g)
	}
	mu.Lock()
	reached := false
	for _, p := range seen {
		reached = reached || p == StatsPath
	}
	mu.Unlock()
	if reached {
		t.Fatal("the stats path reached the engine")
	}
}
