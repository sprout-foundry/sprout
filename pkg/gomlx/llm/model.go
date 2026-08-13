//go:build arm64 && cgo && (darwin || (linux && ggml))

package llm

import (
	"context"
	"fmt"
	"log"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/sprout-foundry/sprout/pkg/tensor"
)

// Model is a local LLM running on the GPU via a tensor.Backend. It drives
// the generation loop (prefill → decode) and delegates the forward pass to
// the Architecture implementation.
type Model struct {
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

// prefixSlot is one retained conversation's prompt-only KV snapshot.
type prefixSlot struct {
	tokens   []int
	cache    *KVCache
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
const maxPrefixLen = 4096

// maxPrefixSlots bounds how many concurrent conversations keep a retained
// snapshot. Sized for sprout's real subagent parallelism rather than
// unbounded: each slot pins GPU memory for up to maxPrefixLen tokens, and
// this machine's RAM margin is already tight (see memory.go). The
// least-recently-used slot is evicted once at capacity.
const maxPrefixSlots = 4

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
func (m *Model) Generate(ctx context.Context, prompt string, genCfg GenerateConfig, onToken func(tokenID int)) error {
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
	if len(tokenIDs) <= maxPrefixLen {
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
		if err := cache.RestorePrefix(m.prefixSlots[slotIdx].cache); err != nil {
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

	// Snapshot the prompt-only K/V for the next request BEFORE decoding
	// (decode appends generated tokens to the working cache; the snapshot
	// keeps just the prompt prefix alive). Drop caching for very long
	// prompts so a huge history doesn't pin memory forever.
	if len(tokenIDs) <= maxPrefixLen {
		m.storePrefixSlot(slotIdx, append([]int(nil), tokenIDs...), cache.SnapshotPrefix())
	} else if slotIdx >= 0 {
		// This conversation grew past the cacheable size — drop its slot
		// rather than let it keep pinning memory for no future benefit.
		m.prefixSlots[slotIdx].cache.Free()
		m.prefixSlots = append(m.prefixSlots[:slotIdx], m.prefixSlots[slotIdx+1:]...)
	}

	// Fast greedy path: no repetition penalty means we can sample on the GPU
	// (argmax) and avoid transferring the full [vocab] logits vector each step.
	greedyArch, greedyOK := m.arch.(GreedyArchitecture)
	useGPUArgmax := greedyOK && genCfg.Temperature <= 0 && genCfg.RepetitionPenalty == 0
	mtpArch, mtpOK := m.arch.(MTPArchitecture)
	useMTP := useGPUArgmax && mtpOK && mtpArch.MTPAvailable() && genCfg.MaxMTPDrafts > 0 && genCfg.MaxTokens > 1
	usePromptLookup := useGPUArgmax && !useMTP && genCfg.PromptLookupMaxDrafts > 0 && genCfg.MaxTokens > 1

	nextToken := 0
	if useGPUArgmax {
		nextToken = argmax(logits)
	} else {
		if genCfg.RepetitionPenalty != 0 {
			applyRepetitionPenalty(logits, tokenIDs, genCfg.RepetitionPenalty)
		}
		nextToken = sample(logits, genCfg)
	}
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
		for i := 1; i < genCfg.MaxTokens; {
			if err := ctx.Err(); err != nil {
				return err
			}
			if m.isStopToken(nextToken) {
				break
			}

			out, err := mtpArch.ForwardDecodeMTP(nextToken, pos, cache, mtpDrafts)
			if err != nil {
				return fmt.Errorf("mtp decode: %w", err)
			}
			pos += len(out)
			for _, t := range out {
				if i >= genCfg.MaxTokens {
					break
				}
				generated = append(generated, t)
				if onToken != nil && !m.shouldFilterToken(t, genCfg) {
					onToken(t)
				}
				i++
				if m.isStopToken(t) {
					break
				}
			}
			nextToken = out[len(out)-1]
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
				t, err := greedyArch.ForwardDecodeArgmax(nextToken, pos, cache)
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
			snap := cache.SnapshotPrefix()
			verifyIDs := append([]int{nextToken}, candidates...)
			predictions, err := verifyArch.ForwardPrefillArgmaxAll(verifyIDs, pos, cache)
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
			nextToken, err = greedyArch.ForwardDecodeArgmax(nextToken, len(tokenIDs)+i-1, cache)
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
			slot.cache.Free()
		}
		m.prefixSlots = nil
		if m.stream != nil {
			m.stream.Free()
			m.stream = nil
		}
		m.arch.FreeWeights()
	})
	close(m.gpuTasks)
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
	if len(tokenIDs) < minPrefixReuse || len(tokenIDs) > maxPrefixLen {
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

		m.storePrefixSlot(-1, tokenIDs, cache.SnapshotPrefix())
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
// reporting a bounded window (64K — the design point for a 16GB-class machine)
// keeps small models in Low-Context Mode, where the lite prompt + 8-tool
// allowlist + tighter compaction keep them cogent. 32K proved too tight for
// agentic work; 64K is the pragmatic midpoint between cogency and window.
// The server advertises this via /v1/models.
func (m *Model) ContextLength() int {
	// 128K is the practical default for local inference. All models in the
	// catalog fit within 16GB RAM at 128K thanks to the hybrid DeltaNet
	// architecture (only 1/4 of layers have growing KV cache).
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

// storePrefixSlot records a freshly prefilled prompt's KV snapshot.
// matchedIdx is the slot bestPrefixSlot returned for this same call, or -1;
// when set, that slot is updated in place (it's this same conversation's
// next turn) rather than treated as a new entry. Otherwise a new slot is
// added, evicting the least-recently-used one once at capacity.
func (m *Model) storePrefixSlot(matchedIdx int, tokens []int, cache *KVCache) {
	m.prefixSeq++
	if matchedIdx >= 0 {
		slot := m.prefixSlots[matchedIdx]
		slot.cache.Free()
		slot.cache = cache
		slot.tokens = tokens
		slot.lastUsed = m.prefixSeq
		return
	}
	if len(m.prefixSlots) < maxPrefixSlots {
		m.prefixSlots = append(m.prefixSlots, &prefixSlot{tokens: tokens, cache: cache, lastUsed: m.prefixSeq})
		return
	}
	lruIdx := 0
	for i, slot := range m.prefixSlots {
		if slot.lastUsed < m.prefixSlots[lruIdx].lastUsed {
			lruIdx = i
		}
	}
	m.prefixSlots[lruIdx].cache.Free()
	m.prefixSlots[lruIdx] = &prefixSlot{tokens: tokens, cache: cache, lastUsed: m.prefixSeq}
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
