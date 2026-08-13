//go:build cgo && ((darwin && arm64) || (linux && ggml && (arm64 || amd64)))

package llm

import (
	"context"
	"fmt"
	"log"
	"runtime"
	"sync"

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

	// Prefix-cache state: a retained snapshot of the prompt's K/V from the
	// previous generation, plus the token IDs it covers. Generate computes
	// the longest common prefix with the new prompt and prefills only the
	// delta, skipping recomputation of a shared history (multi-turn win).
	// The snapshot is prompt-only — generated tokens are not part of the
	// next prompt, so they're not cached.
	prefixCache  *KVCache
	prefixTokens []int
}

// minPrefixReuse is the smallest shared token prefix worth reusing.
// The prefix cache is safe for DeltaNet architectures ONLY when the entire
// cached prefix is a true prefix of the new request (multi-turn continuation
// of the same conversation). Partial overlaps (e.g., a subagent sharing
// just the system prompt) would restore stale DeltaNet state. The check
// in Generate uses `shared == len(m.prefixTokens)` to enforce this.
const minPrefixReuse = 8

// maxPrefixLen caps the retained prefix so long histories don't pin memory
// forever. Beyond this, caching is dropped for that request.
const maxPrefixLen = 4096

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
// Note: we can't pre-fill the system prompt KV cache here because MLX arrays
// are thread-local — the warmup runs on the load thread, but Generate creates
// a fresh GPU stream on the calling thread. The prefix cache is populated
// on the first Generate call instead (the existing prefix-cache logic
// handles this transparently).
func (m *Model) warmupAndPreCache() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

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
	defer s.Free()
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

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	s, err := m.backend.DefaultGPUStream()
	if err != nil {
		return fmt.Errorf("get GPU stream: %w", err)
	}
	m.stream = s
	m.arch.SetStream(s)

	// KV cache: prefill stores K/V, decode appends via per-token concat.
	cache := NewKVCache(m.cfg.NumLayers, s, m.backend)
	defer cache.Free()

	// Prefix caching: if this prompt shares a prefix with the previous one,
	// restore the retained prefix K/V and prefill only the delta. This skips
	// recomputing shared history (multi-turn conversations).
	shared := longestCommonPrefix(tokenIDs, m.prefixTokens)
	var logits []float32
	// Only reuse the prefix cache when the ENTIRE cached prefix is a true
	// prefix of the new request. This ensures multi-turn conversations
	// (same system prompt + growing history) get the cache hit, while
	// subagents or different conversations (partial overlap) get a fresh
	// prefill. The DeltaNet recurrent state is sequence-dependent, so a
	// partial overlap would corrupt the linear-attention layers.
	if shared >= minPrefixReuse && shared == len(m.prefixTokens) && shared < len(tokenIDs) && m.prefixCache != nil {
		if err := cache.RestorePrefix(m.prefixCache); err != nil {
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

	// Snapshot the prompt-only K/V for the next request BEFORE decoding
	// (decode appends generated tokens to the working cache; the snapshot
	// keeps just the prompt prefix alive). Drop caching for very long
	// prompts so a huge history doesn't pin memory forever.
	if len(tokenIDs) <= maxPrefixLen {
		newPrefix := cache.SnapshotPrefix()
		if m.prefixCache != nil {
			m.prefixCache.Free()
		}
		m.prefixCache = newPrefix
		m.prefixTokens = append([]int(nil), tokenIDs...)
	} else if m.prefixCache != nil {
		m.prefixCache.Free()
		m.prefixCache = nil
		m.prefixTokens = nil
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
	if m.prefixCache != nil {
		m.prefixCache.Free()
		m.prefixCache = nil
	}
	m.prefixTokens = nil
	if m.stream != nil {
		m.stream.Free()
		m.stream = nil
	}
	m.arch.FreeWeights()
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
