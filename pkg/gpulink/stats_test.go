package gpulink

import (
	"context"
	"encoding/json"
	"errors"
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

func TestParseSMI(t *testing.T) {
	gpus := parseSMI("0, NVIDIA RTX PRO 6000, 97887, 96099, 37, 12, 28, 213.85, 450.00, 2400, 30\n1, Test GPU, 24576, 1024, [N/A], [N/A], 40, [N/A], [N/A], 1800, [N/A]\n")
	if len(gpus) != 2 {
		t.Fatalf("gpus = %+v", gpus)
	}
	g := gpus[0]
	if g.Name != "NVIDIA RTX PRO 6000" || *g.UtilizationPct != 37 || *g.TemperatureC != 28 || *g.PowerW != 213.85 || *g.MemoryTotalBytes != 97887*1024*1024 || g.Source != "nvidia-smi" {
		t.Fatalf("gpu 0 = %+v", g)
	}
	if gpus[1].UtilizationPct != nil || gpus[1].PowerW != nil || *gpus[1].TemperatureC != 40 {
		t.Fatal("[N/A] reads as absent, not zero")
	}
}

func TestEngineReportedGPUMetricsUseDCGMNames(t *testing.T) {
	m := parseProm(strings.NewReader(`DCGM_FI_DEV_GPU_UTIL{gpu="1",modelName="B"} 55
DCGM_FI_DEV_GPU_UTIL{gpu="0",modelName="A"} 80
DCGM_FI_DEV_GPU_TEMP{gpu="0",modelName="A"} 61
DCGM_FI_DEV_FB_USED{gpu="0",modelName="A"} 1000
DCGM_FI_DEV_FB_FREE{gpu="0",modelName="A"} 24
`))
	gpus := gpusFromMetrics(m)
	if len(gpus) != 2 || gpus[0].Index != 0 || gpus[1].Index != 1 {
		t.Fatalf("gpus = %+v", gpus)
	}
	if *gpus[0].UtilizationPct != 80 || *gpus[0].TemperatureC != 61 || *gpus[0].MemoryTotalBytes != 1024*1024*1024 || gpus[0].Source != "engine" || gpus[0].Name != "A" {
		t.Fatalf("gpu 0 = %+v", gpus[0])
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
	prev := runSMI
	runSMI = func(context.Context) ([]byte, error) {
		return []byte("0, Test GPU, 1024, 512, 50, 10, 45, 100, 300, 1500, 20\n"), nil
	}
	defer func() { runSMI = prev }()

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
	if s.Version != StatsVersion || s.Engine == nil || s.Engine.Kind != "ninfer" || len(s.GPUs) != 1 || *s.GPUs[0].UtilizationPct != 50 {
		t.Fatalf("stats = %+v", s)
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

	// No nvidia-smi: GPU metrics are absent, the engine's are still served.
	runSMI = func(context.Context) ([]byte, error) { return nil, errors.New("not found") }
	b := &Agent{State: &State{Engine: strings.TrimPrefix(engine.URL, "http://")}}
	if got := b.collect(context.Background()); got.Engine == nil || len(got.GPUs) != 0 {
		t.Fatalf("without nvidia-smi = %+v", got)
	}
}
