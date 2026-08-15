//go:build cgo && ((darwin && arm64) || (linux && ggml && (arm64 || amd64)))

package llm

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"time"

	"github.com/sprout-foundry/sprout/pkg/tensor"
)

// Model is a local LLM running on the GPU via a tensor.Backend. It drives
// the generation loop (prefill → decode) and delegates the forward pass to
// the Architecture implementation.
type Model struct {
	// inferJobs feeds the single OS thread all inference runs on; see
	// onInferenceThread for why it must be exactly one thread.
	inferJobs chan func()
	inferOnce sync.Once

	cfg       ModelConfig
	backend   tensor.Backend
	stream    tensor.Stream
	arch      Architecture
	tokenizer *Tokenizer
	mu        sync.Mutex
	closed    bool

	// Thinking-block state. Qwen3.5 emits <think>...</think> before the real
	// answer when thinking is enabled (the default per its chat template).
	// shouldFilterToken hides the block from callbacks unless ThinkingTokens
	// is set. State lives here (guarded by mu, which Generate holds for the
	// whole run) so multi-token generation tracks the open/close boundary.
	thinkID      int
	endThinkID   int
	inThinkBlock bool

	// Prefix-cache state: retained snapshots of prior generations' prompt-only
	// K/V, one per active conversation. sprout runs subagents concurrently
	// (pkg/agent/subagent_runners.go RunParallel) and they all share this one
	// Model, so a single slot would be evicted by whichever conversation's
	// turn ran last — every interleaved turn would fully re-prefill even
	// though nothing is wrong. Generate picks the slot whose full token
	// sequence is a prefix of the new prompt and prefills only the delta.
	prefixSlots []*prefixSlot
	prefixSeq   uint64 // monotonic counter for LRU eviction

	// prefixSlotsCapOverride, when set, fixes maxPrefixSlots' return value.
	// Test-only.
	prefixSlotsCapOverride int

	// spillDir holds spilled (disk-backed) prefix-slot files for this
	// Model's lifetime — see spillIdleSlots and prefixSlot. Created lazily
	// on first spill; removed entirely in Close.
	spillDir     string
	spillDirOnce sync.Once
	spillSeq     uint64

	// GPU work (warmup, Generate, WarmSystemPrefix, Close's cleanup) always
	// runs on this one dedicated, permanently OS-thread-pinned goroutine —
	// see runOnGPUThread. MLX's Metal command encoders are thread_local: a
	// stream (and any array whose lazy graph references it) only evaluates
	// on the OS thread that created it. runtime.LockOSThread per call isn't
	// enough, because it only pins for that one call's duration — the next
	// call can and does land on a different OS thread under real
	// concurrency, so a KV cache slot created on one call's thread fails
	// ("no Stream(gpu, N) in current thread") when a later call restores it
	// from a different thread. Routing everything through one thread that
	// never unlocks removes the hazard entirely.
	gpuWorkerOnce sync.Once
	gpuTasks      chan func()
}

// runOnGPUThread submits fn to the model's dedicated GPU thread (starting
// it on first use) and blocks until fn returns.
func (m *Model) runOnGPUThread(fn func()) {
	m.gpuWorkerOnce.Do(func() {
		m.gpuTasks = make(chan func())
		go func() {
			runtime.LockOSThread()
			for task := range m.gpuTasks {
				task()
			}
		}()
	})
	done := make(chan struct{})
	m.gpuTasks <- func() {
		fn()
		close(done)
	}
	<-done
}

// prefixSlot is one retained conversation's prompt-only KV snapshot. Either
// cache is resident in GPU memory (hot — the conversation this slot belongs
// to used it most recently) or diskPath points to a spilled copy (cold) and
// cache is nil. Never both, never neither once populated.
type prefixSlot struct {
	tokens   []int
	cache    *KVCache
	diskPath string
	lastUsed uint64
}

// minPrefixReuse is the smallest shared token prefix worth reusing.
// The prefix cache is safe for DeltaNet architectures ONLY when the entire
// cached prefix is a true prefix of the new request (multi-turn continuation
// of the same conversation). Partial overlaps (e.g., a subagent sharing
// just the system prompt) would restore stale DeltaNet state. bestPrefixSlot
// enforces this by requiring shared == len(slot.tokens).
const minPrefixReuse = 8

// maxPrefixLen caps the retained prefix so long histories don't pin memory
// forever. Beyond this, caching is dropped for that request.
//
// Sized by KV cost, not token count convenience: the 8 full-attention
// layers cost ~32KB/token, so the ACTIVE conversation's prefix is
// ~1.0 GB at 32K tokens; idle conversations spill to disk immediately
// (spillIdleSlots), so at most one prefix is GPU-resident. Conversations
// that outgrow this fall back to full prefill per turn (the pre-fix
// behavior for everything >4096). SPROUT_PREFIX_CACHE_MAX overrides.
func maxPrefixLenTokens() int {
	if v := os.Getenv("SPROUT_PREFIX_CACHE_MAX"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return n
		}
	}
	return 32768
}

// maxPrefixSlotsCap bounds how many conversations' prefixes stay
// remembered at all (hot or spilled to disk). Not RAM-scaled: only the
// slot for the conversation that's actively generating needs to be
// GPU-resident, and every other slot gets spilled to disk right after its
// turn finishes (see spillIdleSlots) before any of this ever pins memory.
// A generous, disk-cheap cap serves every machine the same way, small or
// large — see storePrefixSlot for the spill-then-cap flow.
const maxPrefixSlotsCap = 8

func (m *Model) maxPrefixSlots() int {
	if m.prefixSlotsCapOverride > 0 {
		return m.prefixSlotsCapOverride // test-only escape hatch; see field doc
	}
	return maxPrefixSlotsCap
}

// NewModel creates a Model from a HuggingFace model directory containing
// config.json, model.safetensors, and tokenizer.json. The architecture is
// auto-detected from config.json.
func NewModel(modelDir string) (*Model, error) {
	backend := tensor.DetectBackend()
	if backend == nil || !backend.Available() {
		return nil, fmt.Errorf("llm: no GPU backend available")
	}

	// SP-134 RAM gate: refuse to load a model whose weights already threaten
	// the machine's unified memory before we spend minutes loading it.
	if err := ModelMemoryGate(modelDir); err != nil {
		return nil, err
	}

	cfgPath := modelDir + "/config.json"
	weightsPath := modelDir + "/model.safetensors"
	tokPath := modelDir + "/tokenizer.json"

	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	arch, err := createArchitecture(cfg, backend)
	if err != nil {
		return nil, err
	}

	tok, err := LoadTokenizer(tokPath)
	if err != nil {
		return nil, fmt.Errorf("load tokenizer: %w", err)
	}

	// Chat-style Qwen models terminate with <|im_end|>, which the tokenizer
	// detects even when config.json's eos_token_id is the raw <|endoftext|>
	// (the multimodal wrapper keeps the chat terminator out of the top-level
	// config). Use the tokenizer's EOS so the decode loop breaks on the token
	// the model actually emits.
	if cfg.EOSTokenID <= 0 || tok.EOSID() > 0 {
		cfg.EOSTokenID = tok.EOSID()
	}
	log.Printf("llm: resolved EOSTokenID=%d (tokenizer EOSID=%d, config.json eos_token_id raw pre-fallback logged above if mismatched)", cfg.EOSTokenID, tok.EOSID())

	// Weights are loaded using the default stream; actual inference creates
	// a fresh GPU stream on the calling thread (MLX streams are thread-local).
	// Pin to this OS thread for the whole load so lazy transpose evals (and
	// the thread-local default stream) stay consistent.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	loadStream, err := backend.DefaultStream()
	if err != nil {
		return nil, fmt.Errorf("create load stream: %w", err)
	}

	if err := arch.InitWeights(weightsPath, loadStream); err != nil {
		loadStream.Free()
		return nil, fmt.Errorf("load weights: %w", err)
	}

	// See NewModelFromFiles: one explicit GC reclaims the released safetensors
	// file blob so it doesn't inflate the first generation's GC threshold.
	runtime.GC()

	m := &Model{
		cfg:       cfg,
		backend:   backend,
		stream:    nil, // created per Generate call on the inference thread
		arch:      arch,
		tokenizer: tok,
	}
	m.initThinking()
	m.warmupAndPreCache()

	log.Printf("llm: %s loaded on GPU", cfg)
	return m, nil
}

// NewModelFromFiles creates a Model from explicit file paths. This gives
// callers flexibility for non-standard layouts. modelPath is the safetensors
// file, configPath is config.json, tokenizerPath is tokenizer.json.
func NewModelFromFiles(modelPath, configPath, tokenizerPath string) (*Model, error) {
	backend := tensor.DetectBackend()
	if backend == nil || !backend.Available() {
		return nil, fmt.Errorf("llm: no GPU backend available")
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	arch, err := createArchitecture(cfg, backend)
	if err != nil {
		return nil, err
	}

	tok, err := LoadTokenizer(tokenizerPath)
	if err != nil {
		return nil, fmt.Errorf("load tokenizer: %w", err)
	}

	// See NewModel: chat-style Qwen models end with <|im_end|> which the
	// tokenizer detects even when config.json's eos_token_id is endoftext.
	if cfg.EOSTokenID <= 0 || tok.EOSID() > 0 {
		cfg.EOSTokenID = tok.EOSID()
	}

	// Pin to this OS thread for the whole load; see NewModel for why.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	loadStream, err := backend.DefaultStream()
	if err != nil {
		return nil, fmt.Errorf("create load stream: %w", err)
	}

	if err := arch.InitWeights(modelPath, loadStream); err != nil {
		loadStream.Free()
		return nil, fmt.Errorf("load weights: %w", err)
	}

	runtime.GC()

	m := &Model{
		cfg:       cfg,
		backend:   backend,
		arch:      arch,
		tokenizer: tok,
	}
	m.initThinking()
	m.warmupAndPreCache()

	log.Printf("llm: %s loaded on GPU", cfg)
	return m, nil
}

// initThinking resolves the Qwen3.5 thinking-block tokens (<think>, </think>)
// from the tokenizer. Models without them (plain qwen3) leave the IDs as 0,
// which makes shouldFilterToken a no-op for thinking — correct, because those
// models never emit the block.
func (m *Model) initThinking() {
	if m.tokenizer == nil {
		return
	}
	m.thinkID = m.tokenizer.IDOf("<think>")
	m.endThinkID = m.tokenizer.IDOf("</think>")
	m.inThinkBlock = false
}

// warmupAndPreCache compiles Metal kernels with a dummy forward pass so the
// first real query doesn't pay ~1-2s of shader compilation latency. MLX
// lazy-compiles Metal kernels on first use; without warmup, the user's first
// query stalls while kernels compile.
//
// Runs on the model's dedicated GPU thread (runOnGPUThread) — every later
// Generate/WarmSystemPrefix call runs on that same thread too, so the
// stream and compiled kernels this sets up stay valid for the model's
// entire lifetime instead of being tied to whichever thread happened to
// load the model.
func (m *Model) warmupAndPreCache() {
	m.runOnGPUThread(func() {
		// Enable MLX graph compilation. MLX traces ops within each eval()
		// boundary and fuses them into fewer Metal kernels, reducing kernel
		// launch overhead and memory round-trips.
		if err := m.backend.EnableCompile(); err != nil {
			log.Printf("llm: enable_compile failed (continuing without): %v", err)
		}

		s, err := m.backend.DefaultGPUStream()
		if err != nil {
			log.Printf("llm: warmup skipped (no GPU stream): %v", err)
			return
		}
		m.stream = s
		m.arch.SetStream(s)

		// Warmup: run a tiny prefill + decode to compile all Metal kernels.
		dummyTokens := m.tokenizer.Encode("Hello")
		if m.cfg.BOSTokenID > 0 {
			dummyTokens = append([]int{m.cfg.BOSTokenID}, dummyTokens...)
		}
		warmupCache := NewKVCache(m.cfg.NumLayers, s, m.backend)
		if _, err := m.arch.ForwardPrefill(m.makeIDsArray(dummyTokens), len(dummyTokens), warmupCache); err != nil {
			log.Printf("llm: warmup prefill failed: %v", err)
			warmupCache.Free()
			return
		}
		// One decode step to compile decode-path kernels (concat, argmax, etc.)
		if greedy, ok := m.arch.(GreedyArchitecture); ok {
			_, _ = greedy.ForwardDecodeArgmax(dummyTokens[len(dummyTokens)-1], len(dummyTokens), warmupCache)
		}
		warmupCache.Free()

		log.Printf("llm: warmup complete (Metal kernels compiled)")
	})
}

// GenerateConfig controls text generation behavior.
type GenerateConfig struct {
	MaxTokens         int
	Temperature       float32
	TopP              float32
	TopK              int
	RepetitionPenalty float32
	// MaxMTPDrafts enables MTP-assisted self-speculative decoding on the
	// greedy path when the architecture has a multi-token prediction head
	// (raw Qwen3.5 HF exports). 0 disables it. The MTP head drafts up to
	// this many tokens per round and the main model verifies them in one
	// batched forward; accepted drafts emit extra tokens at ~1 decode cost.
	MaxMTPDrafts int
	// PromptLookupMaxDrafts enables prompt-lookup speculative decoding.
	// When >0, the generation loop searches for n-gram matches between the
	// recent context and the remaining prompt, proposes matching tokens as
	// candidates, and verifies them in a single batched forward pass. Free
	// speedup when the model echoes context (code, file contents, patterns).
	// 0 disables it. Recommended: 4-8.
	PromptLookupMaxDrafts int
	// ThinkingTokens determines whether to include <think>...</think> blocks
	// in the output. When false (default), thinking tokens are filtered.
	ThinkingTokens bool
}

// DefaultGenerateConfig returns sensible defaults for concise generation.
func DefaultGenerateConfig() GenerateConfig {
	return GenerateConfig{
		MaxTokens:         512,
		Temperature:       0.6,
		TopP:              0.95,
		TopK:              20,
		RepetitionPenalty: 1.1,
		MaxMTPDrafts:      0, // disabled by default; caller opts in
	}
}

// Generate runs the autoregressive generation loop. It calls onToken for each
// isStopToken reports whether the token should terminate generation.
// Checks EOS and any architecture-specific StopTokenIDs (e.g. Gemma4's <turn|>).
func (m *Model) isStopToken(tokenID int) bool {
	if tokenID == m.cfg.EOSTokenID {
		return true
	}
	for _, t := range m.cfg.StopTokenIDs {
		if tokenID == t {
			return true
		}
	}
	return false
}

// Generate drives the autoregressive generation loop, calling onToken for each
// generated token ID (after filtering thinking tokens if applicable).
// Returns when EOS is produced, maxTokens is reached, or context is cancelled.
// Generate runs one generation. The work is dispatched to a single
// long-lived OS thread (see inferenceThread) rather than running on whichever
// goroutine called in.
//
// This matters more than it looks. ggml's CPU backend parallelises with
// OpenMP, and GOMP builds its worker team per calling thread and never tears
// it down. Locking a fresh OS thread per request therefore leaks a whole
// worker team per request, and those abandoned teams keep spinning: with the
// default active wait policy they starve the live team of cores. Measured on
// a 12-core Snapdragon X Elite, throughput collapsed ~10x on the third
// request (28 -> 2.7 tok/s on Qwen3-0.6B) and never recovered. Pinning all
// inference to one thread keeps exactly one team alive.
func (m *Model) Generate(ctx context.Context, prompt string, genCfg GenerateConfig, onToken func(tokenID int)) error {
	return m.onInferenceThread(func() error {
		return m.generate(ctx, prompt, genCfg, onToken)
	})
}

// onInferenceThread runs fn on the model's dedicated inference thread.
func (m *Model) onInferenceThread(fn func() error) error {
	m.inferOnce.Do(func() {
		m.inferJobs = make(chan func())
		go func() {
			// Held for the lifetime of the process: the OS thread identity is
			// what GOMP keys its worker team on.
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()
			for job := range m.inferJobs {
				job()
			}
		}()
	})
	done := make(chan error, 1)
	m.inferJobs <- func() { done <- fn() }
	return <-done
}

func (m *Model) generate(ctx context.Context, prompt string, genCfg GenerateConfig, onToken func(tokenID int)) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return fmt.Errorf("llm: model is closed")
	}

	var genErr error
	m.runOnGPUThread(func() {
		genErr = m.generateLocked(ctx, prompt, genCfg, onToken)
	})
	return genErr
}

// generateLocked runs the generation loop's GPU work on the model's
// dedicated GPU thread (see runOnGPUThread). Only called from Generate,
// which holds m.mu for the whole call.
func (m *Model) generateLocked(ctx context.Context, prompt string, genCfg GenerateConfig, onToken func(tokenID int)) error {
	tokenIDs := m.tokenizer.Encode(prompt)
	if len(tokenIDs) == 0 {
		return fmt.Errorf("llm: empty prompt after tokenization")
	}

	if m.cfg.BOSTokenID > 0 {
		tokenIDs = append([]int{m.cfg.BOSTokenID}, tokenIDs...)
	}

	// The Qwen3.5 chat template may open a <think> block in the prompt.
	// Whether the model is mid-thinking is determined by the prompt tokens:
	// a <think> whose closing </think> never appears means the model will
	// continue the block (filter it). An empty <think>\n\n</think>\n\n pair
	// (thinking disabled) leaves the model answering directly — no filter.
	m.inThinkBlock = promptEndsInsideThink(tokenIDs, m.thinkID, m.endThinkID)

	s, err := m.backend.DefaultGPUStream()
	if err != nil {
		return fmt.Errorf("get GPU stream: %w", err)
	}
	m.stream = s
	m.arch.SetStream(s)

	// KV cache: prefill stores K/V, decode appends via per-token concat.
	cache := NewKVCache(m.cfg.NumLayers, s, m.backend)
	defer cache.Free()

	// Prefix caching: if this prompt shares a prefix with a retained slot,
	// restore that slot's K/V and prefill only the delta. This skips
	// recomputing shared history (multi-turn conversations), and picking
	// among multiple slots is what lets concurrent conversations (main
	// agent + parallel subagents) each keep their own cache hit instead of
	// evicting one another.
	//
	// Skip matching entirely once the prompt itself exceeds maxPrefixLen:
	// a long-running conversation's own slot gets dropped below (too big to
	// keep pinning memory for), and without this guard bestPrefixSlot would
	// then fall back to whatever short, unrelated slot happens to still
	// qualify (e.g. a warmed system-prefix slot) — turning every subsequent
	// turn into an ever-growing "restore a tiny base, delta-prefill
	// thousands of tokens" call instead of a plain full prefill. That code
	// path is unproven at scale and not worth the risk; once a conversation
	// is too big to cache, always fall through to full prefill.
	shared, slotIdx := 0, -1
	if len(tokenIDs) <= maxPrefixLenTokens() {
		shared, slotIdx = m.bestPrefixSlot(tokenIDs)
	}
	if os.Getenv("SPROUT_LOCAL_DEBUG") == "1" {
		log.Printf("llm: prefix cache: shared=%d slots=%d matchedSlot=%d newPromptLen=%d",
			shared, len(m.prefixSlots), slotIdx, len(tokenIDs))
	}
	var logits []float32
	// Only reuse a slot when its ENTIRE token sequence is a true prefix of
	// the new request (enforced by bestPrefixSlot). This ensures multi-turn
	// conversations (same system prompt + growing history) get the cache
	// hit, while unrelated conversations (partial overlap) get a fresh
	// prefill. The DeltaNet recurrent state is sequence-dependent, so a
	// partial overlap would corrupt the linear-attention layers.
	prefillStart := time.Now()
	usedDelta := shared >= minPrefixReuse && slotIdx >= 0 && shared < len(tokenIDs)
	if usedDelta {
		slotCache, err := m.ensureSlotResident(slotIdx)
		if err != nil {
			return fmt.Errorf("load spilled prefix slot: %w", err)
		}
		if err := cache.RestorePrefix(slotCache); err != nil {
			return fmt.Errorf("restore prefix: %w", err)
		}
		delta := tokenIDs[shared:]
		logits, err = m.arch.ForwardPrefillFrom(m.makeIDsArray(delta), len(delta), shared, cache)
		if err != nil {
			return fmt.Errorf("delta prefill: %w", err)
		}
	} else {
		logits, err = m.arch.ForwardPrefill(m.makeIDsArray(tokenIDs), len(tokenIDs), cache)
		if err != nil {
			return fmt.Errorf("prefill: %w", err)
		}
	}
	if os.Getenv("SPROUT_LOCAL_DEBUG") == "1" {
		processed := len(tokenIDs)
		path := "full"
		if usedDelta {
			processed = len(tokenIDs) - shared
			path = "delta"
		}
		log.Printf("llm: prefill: path=%s tokens_processed=%d elapsed=%.2fs", path, processed, time.Since(prefillStart).Seconds())
	}

	logGenMem("before-prefix-slot-bookkeeping")
	// Snapshot the prompt-only K/V for the next request BEFORE decoding
	// (decode appends generated tokens to the working cache; the snapshot
	// keeps just the prompt prefix alive). Drop caching for very long
	// prompts so a huge history doesn't pin memory forever.
	if len(tokenIDs) <= maxPrefixLenTokens() {
		snap := cache.SnapshotPrefix()
		logGenMem("after-SnapshotPrefix")
		newIdx := m.storePrefixSlot(slotIdx, append([]int(nil), tokenIDs...), snap)
		logGenMem("after-storePrefixSlot")
		// Only the conversation that just generated needs to stay
		// GPU-resident; every other remembered conversation gets spilled to
		// disk right now, before this call returns, so peak memory never
		// includes more than one slot's worth of KV cache at rest.
		m.spillIdleSlots(newIdx)
		logGenMem("after-spillIdleSlots")
	} else if slotIdx >= 0 {
		// This conversation grew past the cacheable size — drop its slot
		// rather than let it keep pinning memory (or disk) for no future
		// benefit.
		m.releaseSlotStorage(m.prefixSlots[slotIdx])
		m.prefixSlots = append(m.prefixSlots[:slotIdx], m.prefixSlots[slotIdx+1:]...)
	}

	// Fast greedy path: no repetition penalty means we can sample on the GPU
	// (argmax) and avoid transferring the full [vocab] logits vector each step.
	greedyArch, greedyOK := m.arch.(GreedyArchitecture)
	useGPUArgmax := greedyOK && genCfg.Temperature <= 0 && genCfg.RepetitionPenalty == 0
	mtpArch, mtpOK := m.arch.(MTPArchitecture)
	useMTP := useGPUArgmax && mtpOK && mtpArch.MTPAvailable() && genCfg.MaxMTPDrafts > 0 && genCfg.MaxTokens > 1
	// Pipelined single-token decode (see PipelinedGreedyArchitecture) takes
	// priority over prompt-lookup: both are decode-loop-level throughput
	// strategies, and combining them would mean materializing tokens
	// synchronously to search for n-gram candidates, defeating pipelining's
	// whole point of NOT reading back every step. Pipelining alone recovers
	// the bulk of the gap against mlx-lm; prompt-lookup on top is future work.
	pipelinedArch, pipelinedOK := m.arch.(PipelinedGreedyArchitecture)
	// SPROUT_PIPELINE_DECODE=1 opts in. Defaults OFF: TestPipelinedDecodeParityLiveModel
	// found this path produces genuinely different output from the plain
	// per-token path on every tested prompt (diverges from the first
	// generated token in some cases) — a real correctness bug, not yet
	// root-caused. Do not flip this default without a passing parity test.
	usePipelined := useGPUArgmax && !useMTP && pipelinedOK && genCfg.MaxTokens > 1 && os.Getenv("SPROUT_PIPELINE_DECODE") == "1"
	// Compiled decode (CompiledGreedyArchitecture): the whole step runs as
	// one MLX-compiled graph closure, replaying a cached execution plan
	// instead of re-walking the ~1500-op graph per token. Opt-in via env;
	// automatically declined for long contexts: the compiled replay stages
	// every closure input/output array through the graph, so the
	// fixed-capacity K/V buffers (2x16 whole buffers, ~1.3GB at 20K
	// context) cost ~16ms/token of pure staging at long context — measured
	// (TestSpikeApply65Inputs) and unavoidable under MLX's value-semantics
	// arrays (TestSpikeCapturedBufferMutation: captured constants freeze
	// values; SliceUpdate never writes shared storage). Below the cutoff
	// the CPU graph-walk savings dominate and the path WINS (+14% measured
	// at ~300-token context); above it eager decode is faster.
	compiledArch, compiledOK := m.arch.(CompiledGreedyArchitecture)
	compiledCtxLimit := 4096
	if v := os.Getenv("SPROUT_COMPILED_DECODE_CTX_LIMIT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			compiledCtxLimit = n
		}
	}
	// Prompt-lookup speculative decoding takes priority where enabled
	// (production streaming chat): it can emit up to k+1 tokens per forward
	// — a bigger multiplier than compiled decode's +14%. Compiled serves
	// the remaining greedy callers (commit messages, non-lookup requests).
	// Default ON below the context cutoff; SPROUT_COMPILED_DECODE=0 opts
	// out. Above the cutoff the path is declined automatically (staging
	// cost scales with KV size — see the comment above).
	usePromptLookup := useGPUArgmax && !useMTP && !usePipelined && genCfg.PromptLookupMaxDrafts > 0 && genCfg.MaxTokens > 1
	useCompiled := useGPUArgmax && !useMTP && !usePipelined && !usePromptLookup && compiledOK && genCfg.MaxTokens > 1 &&
		len(tokenIDs) <= compiledCtxLimit && os.Getenv("SPROUT_COMPILED_DECODE") != "0"
	if useCompiled {
		if err := compiledArch.PrepareCompiledDecode(len(tokenIDs), genCfg.MaxTokens, cache); err != nil {
			log.Printf("llm: compiled decode unavailable (%v), falling back to eager", err)
			useCompiled = false
		}
	}
	defer func() {
		if compiledOK {
			compiledArch.ReleaseCompiledDecode()
		}
	}()

	nextToken := 0
	if useGPUArgmax {
		nextToken = argmax(logits)
	} else {
		if genCfg.RepetitionPenalty != 0 {
			applyRepetitionPenalty(logits, tokenIDs, genCfg.RepetitionPenalty)
		}
		nextToken = sample(logits, genCfg)
	}
	logGenMem("after-argmax-before-decode-branch")
	if onToken != nil && !m.shouldFilterToken(nextToken, genCfg) {
		onToken(nextToken)
	}

	// Decode loop using KV cache — each step processes only 1 new token
	generated := []int{nextToken}
	if useMTP {
		// MTP-assisted self-speculative decoding: draft k tokens with the
		// 1-layer MTP head, verify all in one batched main-model forward.
		mtpDrafts := genCfg.MaxMTPDrafts
		pos := len(tokenIDs) // position of nextToken (first generated token)
	mtpOuter:
		for i := 1; i < genCfg.MaxTokens; {
			if err := ctx.Err(); err != nil {
				return err
			}
			if m.isStopToken(nextToken) {
				break
			}

			if i == 1 {
				logGenMem("before-first-ForwardDecodeMTP")
			}
			out, err := mtpArch.ForwardDecodeMTP(nextToken, pos, cache, mtpDrafts)
			if i == 1 {
				logGenMem("after-first-ForwardDecodeMTP")
			}
			if err != nil {
				return fmt.Errorf("mtp decode: %w", err)
			}
			pos += len(out)
			// A single MTP round drafts+verifies a whole batch (out) at
			// once; the stop token can land mid-batch, not just at the end.
			// nextToken must track the LAST TOKEN ACTUALLY EMITTED — using
			// out[len(out)-1] unconditionally (the old behavior) could pick
			// a token from PAST the stop token, and breaking only the inner
			// loop left the outer loop's own isStopToken check unable to see
			// it, so generation silently continued past EOS into a
			// hallucinated next turn. break mtpOuter on stop (not break)
			// exits decode immediately, matching every other decode path.
			for _, t := range out {
				if i >= genCfg.MaxTokens {
					break mtpOuter
				}
				generated = append(generated, t)
				if onToken != nil && !m.shouldFilterToken(t, genCfg) {
					onToken(t)
				}
				i++
				nextToken = t
				if m.isStopToken(t) {
					break mtpOuter
				}
			}
		}
		return nil
	}

	if useCompiled {
		// Same lazy-array pipelining discipline as the usePipelined branch
		// (dispatch next step before blocking on the current readback), but
		// each step is one compiled-closure replay instead of a from-scratch
		// graph build. nextToken (T0) was already emitted by the shared
		// pre-branch code; the loop starts by dispatching T1.
		seedArr, err := m.backend.NewArrayFromInt64([]int64{int64(nextToken)}, []int{1, 1})
		if err != nil {
			return fmt.Errorf("compiled decode: seed token array: %w", err)
		}
		if err := seedArr.AsyncEval(); err != nil {
			seedArr.Free()
			return fmt.Errorf("compiled decode: seed async eval: %w", err)
		}
		pos := len(tokenIDs)
		stepStart := time.Now()
		pending, err := compiledArch.ForwardDecodeCompiled(seedArr, pos)
		seedArr.Free()
		if err != nil {
			return fmt.Errorf("compiled decode: dispatch first step: %w", err)
		}
		pos++
		if os.Getenv("SPROUT_LOCAL_DEBUG") == "1" {
			log.Printf("llm: compiled decode: prepare+first step: %.3fs", time.Since(stepStart).Seconds())
		}

		for i := 1; i < genCfg.MaxTokens; i++ {
			if err := ctx.Err(); err != nil {
				pending.Free()
				return err
			}
			if m.isStopToken(nextToken) {
				pending.Free()
				break
			}

			var next tensor.Array
			if i+1 < genCfg.MaxTokens {
				next, err = compiledArch.ForwardDecodeCompiled(pending, pos)
				if err != nil {
					pending.Free()
					return fmt.Errorf("compiled decode step %d: %w", i, err)
				}
				pos++
			}

			data, err := pending.Int64Data()
			pending.Free()
			if err != nil {
				if next != nil {
					next.Free()
				}
				return fmt.Errorf("compiled decode step %d: read token: %w", i, err)
			}
			if len(data) == 0 {
				if next != nil {
					next.Free()
				}
				return fmt.Errorf("compiled decode step %d: empty token readback", i)
			}
			nextToken = int(data[0])
			generated = append(generated, nextToken)
			if onToken != nil && !m.shouldFilterToken(nextToken, genCfg) {
				onToken(nextToken)
			}
			pending = next
		}
		return nil
	}

	if usePipelined {
		// nextToken (T0) was already read back and emitted by the shared
		// pre-branch code above — this loop must never re-emit it. `pending`
		// therefore always holds the NEXT not-yet-emitted token (T1 going
		// in), dispatched one step ahead of whatever this loop last emitted,
		// mirroring mlx-lm's generate_step: it calls _step(y) and
		// async_evals the result BEFORE yielding y, so the newly dispatched
		// step's GPU work overlaps with the read-back of the previous one.
		// The original version seeded `pending` with T0 itself, so its first
		// iteration dispatched T1 but then read back and re-emitted T0 —
		// producing a duplicated first token every call (caught by
		// TestPipelinedDecodeParityLiveModel).
		seedArr, err := m.backend.NewArrayFromInt64([]int64{int64(nextToken)}, []int{1, 1})
		if err != nil {
			return fmt.Errorf("pipelined decode: seed token array: %w", err)
		}
		if err := seedArr.AsyncEval(); err != nil {
			seedArr.Free()
			return fmt.Errorf("pipelined decode: seed async eval: %w", err)
		}
		pos := len(tokenIDs)
		pending, err := pipelinedArch.ForwardDecodeArgmaxArray(seedArr, pos, cache)
		seedArr.Free()
		if err != nil {
			return fmt.Errorf("pipelined decode: dispatch first step: %w", err)
		}
		pos++

		iterStart := time.Now()
		for i := 1; i < genCfg.MaxTokens; i++ {
			if err := ctx.Err(); err != nil {
				pending.Free()
				return err
			}
			if m.isStopToken(nextToken) {
				pending.Free()
				break
			}
			if os.Getenv("SPROUT_LOCAL_DEBUG") == "1" && i > 1 {
				log.Printf("llm: pipelined iter %d: pre-loop-body gap=%.3fs", i, time.Since(iterStart).Seconds())
			}

			// Dispatch the FOLLOWING step, chained onto `pending` (this
			// iteration's not-yet-read token), before blocking on its
			// read-back below — that overlap is the entire point (see
			// PipelinedGreedyArchitecture). Skipped on the final iteration:
			// there is no token after this one to dispatch for.
			stepStart := time.Now()
			var next tensor.Array
			if i+1 < genCfg.MaxTokens {
				next, err = pipelinedArch.ForwardDecodeArgmaxArray(pending, pos, cache)
				if err != nil {
					pending.Free()
					return fmt.Errorf("pipelined decode step %d: %w", i, err)
				}
				pos++
			}
			buildElapsed := time.Since(stepStart)

			readStart := time.Now()
			data, err := pending.Int64Data()
			readElapsed := time.Since(readStart)
			pending.Free()
			if os.Getenv("SPROUT_LOCAL_DEBUG") == "1" {
				log.Printf("llm: pipelined decode step %d: pos=%d build=%.3fs read=%.3fs total=%.3fs",
					i, pos, buildElapsed.Seconds(), readElapsed.Seconds(), time.Since(stepStart).Seconds())
			}
			if err != nil {
				if next != nil {
					next.Free()
				}
				return fmt.Errorf("pipelined decode step %d: read token: %w", i, err)
			}
			if len(data) == 0 {
				if next != nil {
					next.Free()
				}
				return fmt.Errorf("pipelined decode step %d: empty token readback", i)
			}
			nextToken = int(data[0])
			generated = append(generated, nextToken)
			onTokStart := time.Now()
			if onToken != nil && !m.shouldFilterToken(nextToken, genCfg) {
				onToken(nextToken)
			}
			if os.Getenv("SPROUT_LOCAL_DEBUG") == "1" {
				log.Printf("llm: pipelined iter %d: onToken+bookkeeping=%.3fs", i, time.Since(onTokStart).Seconds())
			}
			pending = next
			iterStart = time.Now()
		}
		return nil
	}

	if usePromptLookup {
		verifyArch, canVerify := m.arch.(interface {
			ForwardPrefillArgmaxAll(ids []int, startPos int, cache *KVCache) ([]int, error)
		})
		maxDraft := genCfg.PromptLookupMaxDrafts
		ngGramSize := 3
		pos := len(tokenIDs)
		allTokens := append(append([]int{}, tokenIDs...), nextToken)

		for i := 1; i < genCfg.MaxTokens; {
			if err := ctx.Err(); err != nil {
				return err
			}
			if m.isStopToken(nextToken) {
				break
			}

			candidates := findPromptLookupCandidates(allTokens, ngGramSize, maxDraft)
			if len(candidates) == 0 || !canVerify {
				stepStart := time.Now()
				t, err := greedyArch.ForwardDecodeArgmax(nextToken, pos, cache)
				if os.Getenv("SPROUT_LOCAL_DEBUG") == "1" {
					log.Printf("llm: decode step %d: cheap path pos=%d elapsed=%.3fs", i, pos, time.Since(stepStart).Seconds())
				}
				if err != nil {
					return fmt.Errorf("decode step %d: %w", i, err)
				}
				pos++
				nextToken = t
				allTokens = append(allTokens, nextToken)
				generated = append(generated, nextToken)
				if onToken != nil && !m.shouldFilterToken(nextToken, genCfg) {
					onToken(nextToken)
				}
				i++
				continue
			}

			// Batched verify: feed [nextToken, candidates...] through the model.
			// predictions[j] = what the model thinks follows verifyIDs[j].
			// Take a snapshot before verify so we can rollback on rejection.
			stepStart := time.Now()
			snap := cache.SnapshotPrefix()
			verifyIDs := append([]int{nextToken}, candidates...)
			predictions, err := verifyArch.ForwardPrefillArgmaxAll(verifyIDs, pos, cache)
			if os.Getenv("SPROUT_LOCAL_DEBUG") == "1" {
				log.Printf("llm: decode step %d: verify path pos=%d candidates=%d elapsed=%.3fs", i, pos, len(candidates), time.Since(stepStart).Seconds())
			}
			if err != nil {
				snap.Free()
				return fmt.Errorf("prompt-lookup verify: %w", err)
			}

			// Accept tokens where model prediction matches the candidate.
			accepted := 0
			newNext := predictions[0]
			for j := 0; j < len(candidates) && j+1 < len(predictions); j++ {
				if predictions[j] != candidates[j] {
					break
				}
				accepted++
				newNext = predictions[j+1]
			}

			if accepted < len(candidates) {
				// Rollback: restore snapshot and re-run accepted prefix only.
				cache.RestorePrefix(snap)
				snap.Free()
				if accepted > 0 {
					rerunIDs := append([]int{nextToken}, candidates[:accepted]...)
					verifyArch.ForwardPrefillArgmaxAll(rerunIDs, pos, cache)
				}
				pos += accepted
			} else {
				snap.Free()
				pos += len(verifyIDs)
			}

			// Emit accepted tokens + the model's next prediction.
			for j := 0; j < accepted && i < genCfg.MaxTokens; j++ {
				generated = append(generated, candidates[j])
				allTokens = append(allTokens, candidates[j])
				if onToken != nil && !m.shouldFilterToken(candidates[j], genCfg) {
					onToken(candidates[j])
				}
				i++
			}
			nextToken = newNext
			allTokens = append(allTokens, nextToken)
			if i < genCfg.MaxTokens {
				generated = append(generated, nextToken)
				if onToken != nil && !m.shouldFilterToken(nextToken, genCfg) {
					onToken(nextToken)
				}
				i++
			}
		}
		return nil
	}

	for i := 1; i < genCfg.MaxTokens; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if m.isStopToken(nextToken) {
			break
		}

		// Build recent tokens for repetition penalty (last 64 tokens)
		recentTokens := append(generated, tokenIDs...)
		recentStart := len(recentTokens) - 64
		if recentStart < 0 {
			recentStart = 0
		}
		recent := recentTokens[recentStart:]

		if useGPUArgmax {
			if i == 1 {
				logGenMem("before-first-plain-ForwardDecodeArgmax")
			}
			nextToken, err = greedyArch.ForwardDecodeArgmax(nextToken, len(tokenIDs)+i-1, cache)
			if i == 1 {
				logGenMem("after-first-plain-ForwardDecodeArgmax")
			}
			if err != nil {
				return fmt.Errorf("decode step %d: %w", i, err)
			}
		} else {
			logits, err = m.arch.ForwardDecode(nextToken, len(tokenIDs)+i-1, cache)
			if err != nil {
				return fmt.Errorf("decode step %d: %w", i, err)
			}

			if genCfg.RepetitionPenalty != 0 {
				applyRepetitionPenalty(logits, recent, genCfg.RepetitionPenalty)
			}

			nextToken = sample(logits, genCfg)
		}
		generated = append(generated, nextToken)

		if onToken != nil && !m.shouldFilterToken(nextToken, genCfg) {
			onToken(nextToken)
		}
	}

	return nil
}

// GenerateText is a convenience wrapper that collects all tokens into a string.
func (m *Model) GenerateText(ctx context.Context, prompt string, genCfg GenerateConfig) (string, error) {
	var tokenIDs []int
	err := m.Generate(ctx, prompt, genCfg, func(id int) {
		tokenIDs = append(tokenIDs, id)
	})
	if err != nil {
		return "", err
	}
	return m.tokenizer.Decode(tokenIDs), nil
}

// Close releases all GPU resources.
func (m *Model) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return nil
	}
	m.closed = true
	// Free on the same dedicated GPU thread everything was created on (see
	// runOnGPUThread), then shut the worker down.
	m.runOnGPUThread(func() {
		for _, slot := range m.prefixSlots {
			m.releaseSlotStorage(slot)
		}
		m.prefixSlots = nil
		if m.stream != nil {
			m.stream.Free()
			m.stream = nil
		}
		m.arch.FreeWeights()
	})
	close(m.gpuTasks)
	if m.spillDir != "" {
		_ = os.RemoveAll(m.spillDir)
	}
	return nil
}

// TokenizerEncode exposes the tokenizer for diagnostic purposes.
func (m *Model) TokenizerEncode(text string) []int {
	return m.tokenizer.Encode(text)
}

// FormatChat applies the chat template to a list of messages, producing the
// raw prompt string fed to the model.
func (m *Model) FormatChat(messages []ChatMessage) string {
	return m.tokenizer.FormatChat(messages)
}

// FormatChatPrefix applies the chat template without the trailing
// "generate now" cue — see Tokenizer.FormatChatPrefix. Callers use this to
// identify a static prompt prefix (e.g. system message + tool definitions)
// worth warming with WarmSystemPrefix.
func (m *Model) FormatChatPrefix(messages []ChatMessage) string {
	return m.tokenizer.FormatChatPrefix(messages)
}

// WarmSystemPrefix ensures a KV cache slot exists for the given static
// prompt prefix, prefilling it if not already cached. Many otherwise-
// unrelated conversations share this exact prefix — sprout runs subagents
// concurrently (pkg/agent/subagent_runners.go RunParallel) sharing this one
// Model, and each currently pays a full prefill for identical system
// prompt + tool definition boilerplate on its first turn. Warming it once
// lets every conversation's first turn delta-prefill instead of waiting
// until its second turn to get a cache hit.
//
// Cheap to call on every request: no-ops immediately once a matching slot
// already exists. No-ops (rather than erroring) if prompt is too short to
// bother caching or too long for maxPrefixLen, since either way the normal
// per-conversation caching in Generate still works correctly on its own.
func (m *Model) WarmSystemPrefix(prompt string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return fmt.Errorf("llm: model is closed")
	}

	tokenIDs := m.tokenizer.Encode(prompt)
	if m.cfg.BOSTokenID > 0 {
		tokenIDs = append([]int{m.cfg.BOSTokenID}, tokenIDs...)
	}
	if len(tokenIDs) < minPrefixReuse || len(tokenIDs) > maxPrefixLenTokens() {
		return nil
	}

	for _, slot := range m.prefixSlots {
		if len(slot.tokens) == len(tokenIDs) && longestCommonPrefix(tokenIDs, slot.tokens) == len(tokenIDs) {
			return nil // already warmed
		}
	}

	var warmErr error
	m.runOnGPUThread(func() {
		s, err := m.backend.DefaultGPUStream()
		if err != nil {
			warmErr = fmt.Errorf("get GPU stream: %w", err)
			return
		}
		m.stream = s
		m.arch.SetStream(s)

		cache := NewKVCache(m.cfg.NumLayers, s, m.backend)
		defer cache.Free()

		if _, err := m.arch.ForwardPrefill(m.makeIDsArray(tokenIDs), len(tokenIDs), cache); err != nil {
			warmErr = fmt.Errorf("warm system prefix: %w", err)
			return
		}

		newIdx := m.storePrefixSlot(-1, tokenIDs, cache.SnapshotPrefix())
		m.spillIdleSlots(newIdx)
	})
	return warmErr
}

// DecodeToken converts a single token ID back to text (for streaming deltas).
func (m *Model) DecodeToken(id int) string {
	return m.tokenizer.Decode([]int{id})
}

// Config returns the model's configuration.
func (m *Model) Config() ModelConfig { return m.cfg }

// MTPAvailable reports whether the loaded model has a multi-token
// prediction head (raw Qwen3.5 HF exports with mtp.* weights). When true,
// the greedy path uses MTP-assisted speculative decoding automatically
// (GenerateConfig.MaxMTPDrafts > 0).
func (m *Model) MTPAvailable() bool {
	mtpArch, ok := m.arch.(MTPArchitecture)
	return ok && mtpArch.MTPAvailable()
}

// ContextLength returns the effective context window the model can handle.
// Local models advertise a huge native window (Qwen3.5 max_position_embeddings
// is 262K), but a small quantized model goes stale/cogency-degrades long
// before that. Sprout's context-profile auto-detection keys off this value:
// reporting a bounded window keeps small models in Low-Context Mode, where
// the lite prompt + 8-tool allowlist + tighter compaction keep them cogent.
//
// The cap has moved: 32K proved too tight for agentic work; 64K was the
// next design point; it's currently 128K. That 128K figure predates this
// session's memory-pressure findings (see ApplyMemoryLimits in memory.go)
// — it was not re-validated against the 16GB-class machine's real,
// measured headroom, and the hybrid architecture's 1-in-5 full-attention
// layers still hold a KV cache that grows linearly with context even
// though DeltaNet layers don't. Worth revisiting the cap itself, not just
// this comment, given what's been learned.
func (m *Model) ContextLength() int {
	const localModelContextCap = 128_000
	if m.cfg.MaxPosition <= 0 {
		return localModelContextCap
	}
	if m.cfg.MaxPosition < localModelContextCap {
		return m.cfg.MaxPosition
	}
	return localModelContextCap
}

// BOSID returns the beginning-of-sequence token ID.
func (m *Model) BOSID() int { return m.cfg.BOSTokenID }

// makeIDsArray converts token IDs to a tensor int64 array [1, seqLen].
func (m *Model) makeIDsArray(ids []int) tensor.Array {
	data := make([]int64, len(ids))
	for i, id := range ids {
		data[i] = int64(id)
	}
	arr, _ := m.backend.NewArrayFromInt64(data, []int{1, len(ids)})
	return arr
}

// promptEndsInsideThink reports whether the token stream ends inside an open
// <think> block: the last <think> comes after the last </think> (or there is
// no close at all). Used to seed inThinkBlock for the generation run.
func promptEndsInsideThink(tokenIDs []int, thinkID, endThinkID int) bool {
	lastThink := -1
	lastEnd := -1
	for i, id := range tokenIDs {
		if id == thinkID {
			lastThink = i
		} else if id == endThinkID {
			lastEnd = i
		}
	}
	if thinkID == 0 {
		return false // model has no thinking tokens
	}
	return lastThink > lastEnd
}

// bestPrefixSlot finds the retained slot whose entire token sequence is a
// true prefix of tokenIDs, preferring the longest match. Returns shared=0,
// idx=-1 if no slot qualifies. A full-slot match is required — not just the
// longest common substring — because DeltaNet's recurrent state is
// sequence-dependent; restoring from any position other than the exact end
// of a previously-computed sequence would corrupt the linear-attention
// layers (see minPrefixReuse).
func (m *Model) bestPrefixSlot(tokenIDs []int) (shared int, idx int) {
	idx = -1
	for i, slot := range m.prefixSlots {
		s := longestCommonPrefix(tokenIDs, slot.tokens)
		if s == len(slot.tokens) && s > shared {
			shared = s
			idx = i
		}
	}
	return shared, idx
}

// storePrefixSlot records a freshly prefilled prompt's KV snapshot and
// returns the index it landed at. matchedIdx is the slot bestPrefixSlot
// returned for this same call, or -1; when set, that slot is updated in
// place (it's this same conversation's next turn) rather than treated as a
// new entry. Otherwise a new slot is added, evicting the least-recently-used
// one once at capacity. Callers should follow up with spillIdleSlots(idx) so
// only the slot just stored stays GPU-resident.
func (m *Model) storePrefixSlot(matchedIdx int, tokens []int, cache *KVCache) int {
	m.prefixSeq++
	if matchedIdx >= 0 {
		slot := m.prefixSlots[matchedIdx]
		m.releaseSlotStorage(slot)
		slot.cache = cache
		slot.diskPath = ""
		slot.tokens = tokens
		slot.lastUsed = m.prefixSeq
		return matchedIdx
	}
	if len(m.prefixSlots) < m.maxPrefixSlots() {
		m.prefixSlots = append(m.prefixSlots, &prefixSlot{tokens: tokens, cache: cache, lastUsed: m.prefixSeq})
		return len(m.prefixSlots) - 1
	}
	lruIdx := 0
	for i, slot := range m.prefixSlots {
		if slot.lastUsed < m.prefixSlots[lruIdx].lastUsed {
			lruIdx = i
		}
	}
	m.releaseSlotStorage(m.prefixSlots[lruIdx])
	m.prefixSlots[lruIdx] = &prefixSlot{tokens: tokens, cache: cache, lastUsed: m.prefixSeq}
	return lruIdx
}

// releaseSlotStorage frees whatever backing storage a slot about to be
// overwritten or evicted holds — its GPU cache if resident, or its spilled
// file on disk if cold.
func (m *Model) releaseSlotStorage(slot *prefixSlot) {
	if slot.cache != nil {
		slot.cache.Free()
	}
	if slot.diskPath != "" {
		_ = os.Remove(slot.diskPath)
	}
}

// spillIdleSlots writes every GPU-resident slot except keepIdx to disk and
// frees its GPU memory, so at most one conversation's prefix cache is ever
// resident at rest — remembering every other conversation costs disk space,
// not RAM. keepIdx is -1 to spill everything (e.g. before Close). Spill
// failures are non-fatal: a slot that can't be written to disk is dropped
// entirely (releaseSlotStorage via the next store/evict) rather than risk
// leaving it stranded in an inconsistent state.
func (m *Model) spillIdleSlots(keepIdx int) {
	for i, slot := range m.prefixSlots {
		if i == keepIdx || slot.cache == nil {
			continue
		}
		path, err := m.newSpillPath()
		if err != nil {
			continue // no spill dir available — leave it resident
		}
		if err := slot.cache.SaveToDisk(path); err != nil {
			_ = os.Remove(path)
			continue // spill failed — leave it resident rather than lose it
		}
		slot.cache.Free()
		slot.cache = nil
		slot.diskPath = path
	}
}

// ensureSlotResident loads the slot at idx back from disk if it was
// spilled, so its cache is safe to pass to KVCache.RestorePrefix. Must run
// on the model's dedicated GPU thread (runOnGPUThread) — LoadKVCacheFromDisk
// allocates new GPU arrays.
func (m *Model) ensureSlotResident(idx int) (*KVCache, error) {
	slot := m.prefixSlots[idx]
	if slot.cache != nil {
		return slot.cache, nil
	}
	loaded, err := LoadKVCacheFromDisk(slot.diskPath, m.stream, m.backend)
	if err != nil {
		return nil, err
	}
	_ = os.Remove(slot.diskPath)
	slot.cache = loaded
	slot.diskPath = ""
	return loaded, nil
}

// newSpillPath returns a fresh file path for a spilled prefix slot, creating
// this Model's spill directory on first use.
func (m *Model) newSpillPath() (string, error) {
	var mkdirErr error
	m.spillDirOnce.Do(func() {
		m.spillDir, mkdirErr = os.MkdirTemp("", "sprout-kvcache-*")
	})
	if mkdirErr != nil {
		return "", fmt.Errorf("create spill dir: %w", mkdirErr)
	}
	if m.spillDir == "" {
		return "", fmt.Errorf("spill dir unavailable")
	}
	m.spillSeq++
	return filepath.Join(m.spillDir, fmt.Sprintf("slot-%d.bin", m.spillSeq)), nil
}

// longestCommonPrefix returns the length of the shared prefix of a and b.
func longestCommonPrefix(a, b []int) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	i := 0
	for i < n && a[i] == b[i] {
		i++
	}
	return i
}

// findPromptLookupCandidates searches the token sequence for an n-gram match
// with the last nGramSize tokens. If found, returns up to maxDraft tokens
// that follow the match. This predicts what the model will likely echo from
// context (code patterns, file contents, repetitive structures).
func findPromptLookupCandidates(tokens []int, nGramSize, maxDraft int) []int {
	if len(tokens) < nGramSize+1 {
		return nil
	}
	tail := tokens[len(tokens)-nGramSize:]
	for i := len(tokens) - nGramSize - 1; i >= nGramSize-1; i-- {
		match := true
		for j := 0; j < nGramSize; j++ {
			if tokens[i-nGramSize+1+j] != tail[j] {
				match = false
				break
			}
		}
		if match {
			candidates := []int{}
			for j := i + 1; j < len(tokens)-nGramSize && len(candidates) < maxDraft; j++ {
				candidates = append(candidates, tokens[j])
			}
			if len(candidates) > 0 {
				return candidates
			}
		}
	}
	return nil
}

// shouldFilterToken returns true if the token should be hidden from the caller.
// Qwen3.5 emits a <think>...</think> block before the answer when thinking is
// enabled (the chat template default). When ThinkingTokens is false, every
// token inside the block is filtered and the open/close markers themselves are
// dropped. State (inThinkBlock) lives on the Model, which Generate owns for the
// whole run under mu.
func (m *Model) shouldFilterToken(tokenID int, genCfg GenerateConfig) bool {
	// Never surface the EOS token to callbacks: it terminates generation and
	// decoding it would inject <|im_end|> (or similar) into the output text.
	if m.isStopToken(tokenID) {
		return true
	}

	if tokenID == m.thinkID {
		m.inThinkBlock = true
		return true // drop the <think> marker itself
	}
	if tokenID == m.endThinkID {
		m.inThinkBlock = false
		return true // drop the </think> marker itself
	}

	if m.inThinkBlock && !genCfg.ThinkingTokens {
		return true // inside the thinking block — hide unless asked for it
	}
	return false
}
