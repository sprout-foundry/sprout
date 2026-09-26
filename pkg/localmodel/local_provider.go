//go:build darwin && arm64 && cgo

package localmodel

// LocalProvider implements api.ClientInterface by calling the MLX model
// engine directly — no HTTP, no separate process, no serialization. This
// file holds the provider type, lifecycle (load / set / get model), the TPS
// stats, and the surface methods (CheckConnection, ListModels, the vision
// stubs, Close). The chat engine lives in local_provider_chat.go; prompt
// building in local_provider_prompt.go; tool-call parsing + tool-prompt
// formatting in local_provider_toolcalls.go.

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/sprout-foundry/sinter/llm"
	"github.com/sprout-foundry/sinter/llm/catalog"
	_ "github.com/sprout-foundry/sinter/llm/gemma4"
	_ "github.com/sprout-foundry/sinter/llm/lfm2"
	_ "github.com/sprout-foundry/sinter/llm/qwen3"
	_ "github.com/sprout-foundry/sinter/llm/qwen35"
	"github.com/sprout-foundry/sinter/mlx"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// LocalProvider implements api.ClientInterface by calling the MLX model
// engine directly — no HTTP, no separate process, no serialization.
// The model is loaded lazily on first request and kept resident for
// subsequent requests. The idle reaper (in lifecycle.go) unloads weights to
// reclaim GPU memory after inactivity; ensureLoadedLocked reloads lazily on
// the next request rather than the process being stuck once unloaded.
type LocalProvider struct {
	mu sync.Mutex // guards every field below except the atomics

	model    *llm.Model
	modelDir string
	modelID  string
	backend  string
	debug    bool

	// targetDir, when non-empty, is the model directory SetModel most
	// recently validated and recorded — an explicit user/config choice that
	// overrides RAM auto-selection. Empty means "use
	// resolveModelForCurrentMachine's pick", cached once it's loaded (matching
	// the old sync.Once behavior) until an idle-unload or an explicit
	// SetModel call invalidates it.
	targetDir string

	loadErr error

	// TPS tracking (fixed-point: value * 1000, stored as uint64)
	lastTPS     atomic.Uint64
	avgTPS      atomic.Uint64
	totalTokens atomic.Uint64
	genCount    atomic.Uint64
}

var (
	globalProvider     *LocalProvider
	globalProviderOnce sync.Once
)

// GetLocalProvider returns the singleton in-process local provider.
// The model is loaded lazily on first use.
func GetLocalProvider() *LocalProvider {
	globalProviderOnce.Do(func() {
		globalProvider = &LocalProvider{backend: detectBackend()}
	})
	return globalProvider
}

// DisableForTesting pins the local-model provider to its inert "none"
// backend for the remainder of the process. pkg/webui (and any other
// package whose tests exercise onboarding/agent paths) calls this from
// TestMain: without it, a test that selects the sprout-local provider
// triggers CheckConnection → ensureLoaded and pulls the user's real
// multi-GB model weights into the test binary's memory — observed at
// 16-24GB RSS per webui.test process and repeated machine-freezing OOMs
// (2026-09-21). Model-loading code paths themselves are covered by
// pkg/localmodel's own tests; other packages' tests only need the
// provider to exist, not to actually load gigabytes.
func DisableForTesting() {
	GetLocalProvider().mu.Lock()
	defer GetLocalProvider().mu.Unlock()
	GetLocalProvider().backend = "none"
	GetLocalProvider().loadErr = nil
}

func detectBackend() string {
	if runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" && mlx.Available() {
		return localBackendMLX
	}
	return "none"
}

// ensureLoaded returns the currently-loaded model, loading (or switching to
// a different explicitly-selected target, or reloading after an idle
// unload) it first if necessary. Locked for the whole operation — a load or
// switch takes several seconds and there is exactly one GPU to serve from,
// so concurrent callers legitimately block on each other here rather than
// racing to load two models at once.
func (p *LocalProvider) ensureLoaded() (*llm.Model, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.ensureLoadedLocked()
}

// ensureLoadedLocked is ensureLoaded's body; callers must already hold p.mu.
func (p *LocalProvider) ensureLoadedLocked() (*llm.Model, error) {
	if p.backend == "none" {
		p.loadErr = fmt.Errorf("no local LLM backend available (requires Apple Silicon with the MLX C libraries — install with: brew install mlx-c)")
		return nil, p.loadErr
	}

	// Already loaded and either in auto mode (no explicit target recorded)
	// or already serving the explicitly selected target — nothing to do.
	// Auto mode intentionally does NOT re-resolve on every call once
	// loaded (matches the old sync.Once behavior — RAM/installed-model
	// state isn't expected to change mid-process); it only re-resolves
	// after p.model is cleared (idle-reaper Close, or SetModel picking a
	// new target).
	if p.model != nil && (p.targetDir == "" || p.targetDir == p.modelDir) {
		return p.model, nil
	}

	dir := p.targetDir
	resolvedBackend := localBackendMLX
	if dir == "" {
		var err error
		dir, resolvedBackend, err = resolveModelForCurrentMachine()
		if err != nil {
			p.loadErr = err
			return nil, err
		}
	}

	if p.model != nil {
		// Switching to a different explicit target — release the resident
		// model before loading the new one; never hold two at once.
		_ = p.model.Close()
		p.model = nil
		p.modelDir = ""
		p.modelID = ""
	}

	if err := llm.ApplyMemoryLimits(); err != nil {
		p.loadErr = fmt.Errorf("apply memory limits: %w", err)
		return nil, p.loadErr
	}

	// Load-time guard for corrupt installs: a truncated config.json would
	// panic inside sinter's LoadConfig (divide-by-zero on missing head
	// counts). Surface it as an error instead — auto-selection skips such
	// dirs, but an explicit SetModel target can still point at one.
	if !validModelConfig(dir) {
		p.loadErr = fmt.Errorf("refusing to load %s: config.json is missing, empty, or invalid — re-download the model", dir)
		return nil, p.loadErr
	}

	if localDebug() {
		log.Printf("local: resolved model dir=%s backend=%s", dir, resolvedBackend)
	}
	logMLXMemory("model-load-start")
	m, err := llm.NewModel(dir)
	logMLXMemory("model-load-end")
	if err != nil {
		p.loadErr = fmt.Errorf("load model from %s: %w", dir, err)
		return nil, p.loadErr
	}

	p.model = m
	p.modelDir = dir
	p.modelID = filepath.Base(dir)
	p.loadErr = nil
	return p.model, nil
}

// --- api.ClientInterface ---

// localDebug reports whether SPROUT_LOCAL_DEBUG=1 is set. When on, the
// provider logs prompt size and raw model output, which is the only way to
// see why a local model produced an unusable response inside the agent loop.
func localDebug() bool { return os.Getenv("SPROUT_LOCAL_DEBUG") == "1" }

// localBudgetModel is the inference-independent surface the output-budget
// helper needs. *llm.Model satisfies it; tests use a fake. Keeping the
// parameter an interface makes the budget math testable without loading
// real weights.
type localBudgetModel interface {
	TokenizerEncode(text string) []int
	ContextLength() int
}

// localMaxOutputCap bounds the output budget for local generation. A
// runaway generation (greedy repeat loop that never emits EOS) at 20-50
// tok/s would otherwise burn tens of minutes to an hour of GPU time on a
// budget derived from a large context window. Real agent turns finish well
// under this; hitting it is reported as finish_reason "length".
const localMaxOutputCap = 16384

// localMaxOutputTokens computes the MaxTokens budget for a generation.
//
// sinter's DefaultGenerateConfig caps MaxTokens at 512 — a test-scale
// default that silently truncates real agent turns (long explanations,
// multi-parameter tool calls, file edits) mid-thought. The model hits the
// cap without emitting EOS, the provider then reports finish_reason
// "stop", and the truncation is indistinguishable from a natural end:
// users see the model "end at a random spot where it didn't mean to end".
//
// This mirrors the budgeting every network provider gets in
// CalculateMaxTokensWithLimits: budget output against the model's real
// context window using the exact prompt-token count (available here —
// the tokenizer is local, no heuristic needed), and capped by
// localMaxOutputCap against runaway generation. The budget is kept
// proportionate to the remaining window: num_predict beyond the remaining
// num_ctx is fictitious (the server clamps it), and MinOutputTokens is
// only the fallback when the budget math yields nothing usable.
func localMaxOutputTokens(model localBudgetModel, prompt string) int {
	input := len(model.TokenizerEncode(prompt))
	budget, ok := api.CalculateOutputBudget(model.ContextLength(), input)
	if !ok || budget <= 0 {
		return min(api.MinOutputTokens, localMaxOutputCap)
	}
	if budget > localMaxOutputCap {
		return localMaxOutputCap
	}
	return budget
}

func logLocalExchange(tag, prompt, raw string, promptTokens int, toolCalls int) {
	if !localDebug() {
		return
	}
	trunc := func(s string, n int) string {
		if len(s) <= n {
			return s
		}
		return s[:n] + fmt.Sprintf("...[+%d bytes]", len(s)-n)
	}
	log.Printf("local[%s]: prompt_tokens=%d prompt_bytes=%d raw_bytes=%d tool_calls=%d",
		tag, promptTokens, len(prompt), len(raw), toolCalls)
	log.Printf("local[%s]: PROMPT_TAIL=%q", tag, trunc(prompt[max(0, len(prompt)-1200):], 1200))
	if os.Getenv("SPROUT_DUMP_PROMPT") != "" {
		_ = os.WriteFile(os.Getenv("SPROUT_DUMP_PROMPT"), []byte(prompt), 0o600)
	}
	log.Printf("local[%s]: RAW_OUTPUT=%q", tag, trunc(raw, 1200))
}

// logLocalTiming reports per-turn generation timing so the split between
// prefill (scales with prompt_tokens) and decode (scales with
// completion_tokens) is visible from a real agent session, not just an
// isolated benchmark harness.
func logLocalTiming(tag string, promptTokens, completionTokens int, elapsed float64) {
	if !localDebug() {
		return
	}
	tps := 0.0
	if elapsed > 0 {
		tps = float64(completionTokens) / elapsed
	}
	log.Printf("local[%s]: TIMING prompt_tokens=%d completion_tokens=%d elapsed=%.2fs tps=%.1f",
		tag, promptTokens, completionTokens, elapsed, tps)
	logMLXMemory(tag)
}

// logMLXMemory reports MLX's own allocator accounting (bytes held by live
// arrays, pooled/cached bytes, and the peak watermark since load) after
// every turn. Isolates whether growth is coming from MLX's own live-array
// footprint — a real reference leak in the Go/cgo layer — versus system-wide
// pressure (other processes, macOS accounting) that shows up in Activity
// Monitor but isn't actually MLX holding more memory.
func logMLXMemory(tag string) {
	stats, err := mlx.Snapshot()
	if err != nil {
		log.Printf("local[%s]: MLX_MEM snapshot failed: %v", tag, err)
		return
	}
	log.Printf("local[%s]: MLX_MEM active=%.1fMB cache=%.1fMB peak=%.1fMB limit=%.1fMB cacheLimit=%.1fMB",
		tag,
		float64(stats.Active)/1048576, float64(stats.Cache)/1048576, float64(stats.Peak)/1048576,
		float64(stats.Limit)/1048576, float64(stats.CacheLimit)/1048576)
}

func (p *LocalProvider) CheckConnection() error {
	_, err := p.ensureLoaded()
	return err
}

func (p *LocalProvider) SetDebug(debug bool) { p.debug = debug }

// SetModel selects which model this provider should load, by stable ID
// (catalog Name like "qwen3.5-9b", or an installed directory basename —
// see ResolveModelID). Validates the pick against this machine's RAM tier
// (catalog.SelectableForRAM): a tier-blocked pick is refused unless
// SPROUT_ALLOW_OVERWEIGHT=1 is set. Does not download or load anything
// itself — callers that want a not-yet-installed catalog pick to actually
// become available must call EnsureModel first (the CLI's /model command
// does this with visible progress before calling SetModel). Takes effect
// on the next ensureLoaded call (chat request), which reloads lazily.
func (p *LocalProvider) SetModel(model string) error {
	if strings.TrimSpace(model) == "" {
		return fmt.Errorf("empty model id")
	}
	status, err := ResolveModelID(model)
	if err != nil {
		return err
	}
	if !status.Installed {
		return fmt.Errorf("model %q is not installed — download it first", status.Name)
	}
	ram := tensorTotalSystemRAM()
	if tier, known := catalog.SelectableForRAM(status.Name, ram); known && tier == catalog.TierBlocked {
		if os.Getenv("SPROUT_ALLOW_OVERWEIGHT") != "1" {
			return fmt.Errorf("%s needs more RAM than this machine has (%.0f GB) — set SPROUT_ALLOW_OVERWEIGHT=1 to force it anyway",
				status.Name, float64(ram)/(1024*1024*1024))
		}
	}
	p.mu.Lock()
	p.targetDir = status.Dir
	p.mu.Unlock()
	return nil
}

func (p *LocalProvider) GetModel() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.model == nil {
		_, _ = p.ensureLoadedLocked() // best-effort; matches prior behavior of ignoring load failure here
	}
	if p.modelID != "" {
		return p.modelID
	}
	return "local"
}

func (p *LocalProvider) GetProvider() string { return "sprout-local" }

// isModelLoaded reports whether the model is currently in GPU memory.
func (p *LocalProvider) isModelLoaded() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.model != nil
}

func (p *LocalProvider) loadedModelDir() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.modelDir
}

func (p *LocalProvider) loadedModelID() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.modelID
}

func (p *LocalProvider) GetModelContextLimit() (int, error) {
	model, err := p.ensureLoaded()
	if err != nil {
		return 0, err
	}
	// ContextLength(), not Config().MaxPosition directly: MaxPosition is the
	// model's raw native window (e.g. 262K for Qwen3.5), which a small
	// quantized local model goes stale/cogency-degrades long before
	// reaching. ContextLength applies the cap sprout's context-profile
	// auto-detection relies on to drop into Low-Context Mode at a sane
	// point.
	return model.ContextLength(), nil
}

// ListModels returns the full RAM-tier catalog matrix for this machine —
// every model tier is visible, but only the suggested and stretch tiers
// carry EligibleRoles (selection is enforced separately, in SetModel; this
// just informs the picker). See catalog.TieredCatalogForRAM for the tier logic.
func (p *LocalProvider) ListModels(ctx context.Context) ([]api.ModelInfo, error) {
	return TieredModelInfos(tensorTotalSystemRAM()), nil
}

func (p *LocalProvider) SupportsVision() bool { return false }

func (p *LocalProvider) VisionCapabilities() api.VisionCapabilities {
	return api.VisionCapabilities{}
}

func (p *LocalProvider) GetVisionModel() string { return "" }

func (p *LocalProvider) SendVisionRequest(ctx context.Context, messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
	return nil, fmt.Errorf("local provider does not support vision")
}

func (p *LocalProvider) GetLastTPS() float64 { return float64(p.lastTPS.Load()) / 1000.0 }

func (p *LocalProvider) GetAverageTPS() float64 { return float64(p.avgTPS.Load()) / 1000.0 }

func (p *LocalProvider) GetTPSStats() map[string]float64 {
	return map[string]float64{
		"last":         p.GetLastTPS(),
		"average":      p.GetAverageTPS(),
		"total_tokens": float64(p.totalTokens.Load()),
	}
}

func (p *LocalProvider) ResetTPSStats() {
	p.lastTPS.Store(0)
	p.avgTPS.Store(0)
	p.totalTokens.Store(0)
	p.genCount.Store(0)
}

func (p *LocalProvider) recordTPS(tokens int, elapsed float64) {
	if elapsed <= 0 || tokens <= 0 {
		return
	}
	tps := float64(tokens) / elapsed
	p.lastTPS.Store(uint64(tps * 1000))
	p.totalTokens.Add(uint64(tokens))
	count := p.genCount.Add(1)
	oldAvg := float64(p.avgTPS.Load()) / 1000.0
	newAvg := (oldAvg*float64(count-1) + tps) / float64(count)
	p.avgTPS.Store(uint64(newAvg * 1000))
}

// Close unloads the model and releases GPU memory.
func (p *LocalProvider) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.model != nil {
		_ = p.model.Close()
		p.model = nil
		p.modelDir = ""
		p.modelID = ""
	}
	return nil
}

// --- prompt building and tool-call parsing ---
