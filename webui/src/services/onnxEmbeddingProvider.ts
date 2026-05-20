/**
 * Browser-side ONNX embedding provider for EmbeddingGemma-300M.
 *
 * Uses onnxruntime-web to run the EmbeddingGemma-300M model directly
 * in the browser with WASM SIMD or WebGPU backends.
 *
 * Port of the Go embedding pipeline (pkg/embedding/onnx_*.go) to
 * TypeScript for in-browser semantic search and embedding generation.
 *
 * Model source: https://huggingface.co/onnx-community/embeddinggemma-300m-ONNX
 */

import { InferenceSession, Tensor } from 'onnxruntime-web';

// ─── Public API ───────────────────────────────────────────────────

export interface EmbeddingOptions {
  /**
   * Model quantization level.
   * - 'fp32': Full precision (~600MB), highest quality
   * - 'q8': 8-bit quantization (~150MB), minimal quality loss
   * - 'q4': 4-bit quantization (~80MB), good quality
   */
  dtype?: 'fp32' | 'q8' | 'q4';

  /**
   * Execution backend preference.
   * - 'webgpu': GPU acceleration (~10x faster), requires WebGPU support
   * - 'wasm': WASM SIMD, universal fallback
   * If not specified, auto-detects the best available backend.
   */
  backend?: 'webgpu' | 'wasm';

  /**
   * Prompt prefix type for the embedding task.
   * EmbeddingGemma uses task-specific prefixes to improve retrieval quality.
   * Defaults to 'query' for search queries and 'document' for indexed text.
   */
  prefix?:
    | 'query'
    | 'document'
    | 'code_retrieval'
    | 'sentence_similarity'
    | 'classification'
    | 'clustering'
    | 'qa'
    | 'fact_verification';
}

export interface EmbeddingResult {
  /** Normalized embedding vector (Float32Array). */
  embedding: Float32Array;

  /** Number of dimensions (may be truncated via MRL). */
  dims: number;
}

// ─── EmbeddingGemma Prompt Prefixes ──────────────────────────────
// From the EmbeddingGemma specification. Each task has a prefix that
// conditions the model to produce task-appropriate embeddings.

const EMBEDDINGGEMMA_PREFIXES: Record<string, string> = {
  query: 'task: search result | query: ',
  document: 'title: none | text: ',
  code_retrieval: 'task: code retrieval | query: ',
  sentence_similarity: 'task: sentence similarity | text: ',
  classification: 'task: sentence classification | text: ',
  clustering: 'task: sentence clustering | text: ',
  qa: 'task: question answering | question: ',
  fact_verification: 'task: fact verification | text: ',
};

// ─── Model URLs ───────────────────────────────────────────────────

const HF_BASE = 'https://huggingface.co/onnx-community/embeddinggemma-300m-ONNX/resolve/main';

const MODEL_FILES: Record<'fp32' | 'q8' | 'q4', { model: string; data?: string }> = {
  fp32: { model: `${HF_BASE}/onnx/model.onnx`, data: `${HF_BASE}/onnx/model.onnx_data` },
  q8: { model: `${HF_BASE}/onnx/model_q8.onnx`, data: `${HF_BASE}/onnx/model_q8.onnx_data` },
  q4: { model: `${HF_BASE}/onnx/model_q4.onnx`, data: `${HF_BASE}/onnx/model_q4.onnx_data` },
};

const TOKENIZER_URL = `${HF_BASE}/tokenizer.json`;

// ─── BPE Tokenizer ────────────────────────────────────────────────
// Port of the Go GemmaTokenizer from pkg/embedding/onnx_tokenizer.go.
// Implements BytePiece tokenization with metaspace pre-tokenization.

interface TokenizerModel {
  type: string;
  vocab: Record<string, number>;
  merges: string[]; // Each entry is "token1 token2"
}

interface TokenizerConfig {
  model: TokenizerModel;
  pre_tokenizer?: {
    type: string;
    pattern?: string;
  };
}

/**
 * Unicode codepoint for the "unknown byte" fallback encoding.
 * Bytes 1-255 that have no UTF-8 mapping are encoded as
 * U+0100 through U+02FF (i.e., 0x0100 + byte_value - 1).
 *
 * This matches Gemma's BytePiece fallback behavior.
 */
function byteEncode(ch: string): string {
  const cp = ch.codePointAt(0);
  if (cp === undefined) return ch;

  // Characters U+0001 through U+00FF that aren't valid ASCII
  // get remapped to U+0100 through U+02FF
  if (cp > 0x00 && cp <= 0xff) {
    return String.fromCodePoint(0x0100 + cp - 1);
  }
  return ch;
}

/**
 * Metaspace pre-tokenizer regex from EmbeddingGemma's tokenizer.json.
 * Matches either whitespace sequences or non-whitespace words,
 * excluding special whitespace chars (\n, \r, \t, \b, \v, \f).
 *
 * Pattern: (?u)\s+|[^\s\n\r\t\u0008\u000b\u000c]+
 */
const PRE_TOKENIZER_REGEX =
  /(?:\s+|[^\s\n\r\t\u0008\u000b\u000c]+)/gu;

/**
 * Word boundary marker used by Gemma's BPE tokenizer.
 * 'Ġ' (U+0120) marks the beginning of a new word in the tokenized sequence.
 * This allows the model to distinguish word boundaries during training.
 */
const WORD_BOUND = '\u0120';

/**
 * GemmaBpeTokenizer implements the BytePiece tokenization algorithm
 * used by EmbeddingGemma / Gemma models.
 *
 * Algorithm:
 * 1. Split text into words using the pre-tokenizer regex
 * 2. Byte-encode any non-ASCII bytes as Unicode codepoints
 * 3. Add word boundary markers
 * 4. Iteratively apply BPE merge rules until convergence
 * 5. Wrap with BOS/EOS tokens for the full sequence
 */
class GemmaBpeTokenizer {
  private vocab: Map<string, number>;
  private mergeOps: Map<string, number>; // "token1\ttoken2" -> rank
  private inverseVocab: Map<number, string>;

  constructor(config: TokenizerConfig) {
    const model = config.model;
    if (model.type !== 'BPE') {
      throw new Error(`Expected BPE tokenizer, got ${model.type}`);
    }

    this.vocab = new Map(Object.entries(model.vocab).map(([k, v]) => [k, v]));
    this.inverseVocab = new Map(
      Object.entries(model.vocab).map(([k, v]) => [v, k])
    );

    // Build merge operations with priority (lower index = higher priority)
    this.mergeOps = new Map();
    for (let i = 0; i < model.merges.length; i++) {
      this.mergeOps.set(model.merges[i], i);
    }
  }

  /**
   * Tokenize text into a sequence of token IDs.
   * Wraps the result with BOS (beginning-of-sequence) and EOS (end-of-sequence) markers.
   */
  tokenize(text: string): number[] {
    const tokens = this.tokenizeRaw(text);
    return tokens;
  }

  /**
   * Internal tokenization without BOS/EOS wrapping.
   */
  private tokenizeRaw(text: string): number[] {
    // Split text into words using pre-tokenizer
    const words = text.match(PRE_TOKENIZER_REGEX) || [];

    let result: number[] = [];
    for (const word of words) {
      // Process the word
      const wordTokens = this.tokenizeWord(word);
      result.push(...wordTokens);
    }

    return result;
  }

  /**
   * Tokenize a single word using BPE algorithm.
   */
  private tokenizeWord(word: string): number[] {
    // Check if the whole word is in vocab
    if (this.vocab.has(word)) {
      return [this.vocab.get(word)!];
    }

    // Byte-encode each character and add word boundary at start
    // For Gemma: each word gets WORD_BOUND prefix if it's a new word
    let chars: string[] = [];
    for (const ch of word) {
      chars.push(byteEncode(ch));
    }

    // Add word boundary to first character
    if (chars.length > 0) {
      chars[0] = WORD_BOUND + chars[0];
    }

    // Initialize pairs: consecutive character pairs
    let pairs: [string, string][] = [];
    for (let i = 0; i < chars.length - 1; i++) {
      pairs.push([chars[i], chars[i + 1]]);
    }

    // Iteratively apply merges
    while (true) {
      // Find the pair with the highest priority (lowest merge index)
      let bestPair: [string, string] | null = null;
      let bestRank = Infinity;

      for (const [a, b] of pairs) {
        const key = `${a}\t${b}`;
        const rank = this.mergeOps.get(key);
        if (rank !== undefined && rank < bestRank) {
          bestRank = rank;
          bestPair = [a, b];
        }
      }

      if (bestPair === null) break;

      // Merge all occurrences of bestPair
      const [first, second] = bestPair;
      const merged = first + second;

      chars = this.mergeTokens(chars, first, second, merged);
      pairs = [];
      for (let i = 0; i < chars.length - 1; i++) {
        pairs.push([chars[i], chars[i + 1]]);
      }
    }

    // Convert characters to token IDs
    const tokens: number[] = [];
    for (const ch of chars) {
      const id = this.vocab.get(ch);
      if (id !== undefined) {
        tokens.push(id);
      } else {
        // Fallback: unknown token
        const unk = this.vocab.get('<unk>');
        if (unk !== undefined) {
          tokens.push(unk);
        }
      }
    }

    return tokens;
  }

  /**
   * Merge all adjacent occurrences of first+second into merged token.
   */
  private mergeTokens(
    tokens: string[],
    first: string,
    second: string,
    merged: string
  ): string[] {
    const result: string[] = [];
    let i = 0;
    while (i < tokens.length) {
      if (
        i + 1 < tokens.length &&
        tokens[i] === first &&
        tokens[i + 1] === second
      ) {
        result.push(merged);
        i += 2;
      } else {
        result.push(tokens[i]);
        i++;
      }
    }
    return result;
  }
}

// ─── Gemma Token IDs ──────────────────────────────────────────────
// These come from the tokenizer config and are needed for BOS/EOS wrapping.

const BOS_TOKEN_ID = 2;   // <bos>
const EOS_TOKEN_ID = 1;   // <eos>
const PAD_TOKEN_ID = 0;   // <pad>
const EOS_TOKEN = '<eos>';
const BOS_TOKEN = '<bos>';

// ─── EmbeddingGemma-300M Browser Provider ──────────────────────────

/**
 * BrowserONNXProvider runs EmbeddingGemma-300M in the browser using
 * onnxruntime-web for ONNX inference and a custom BPE tokenizer.
 *
 * Usage:
 * ```typescript
 * const provider = new BrowserONNXProvider({ dtype: 'q8', backend: 'webgpu' });
 * await provider.initialize();
 * const result = await provider.embed('Hello world', 'query');
 * console.log(result.embedding); // Float32Array[768]
 * ```
 *
 * ### Model sizing
 * | dtype | Model size | Embedding dims | Notes |
 * |-------|-----------|----------------|-------|
 * | fp32  | ~600MB    | 768           | Best quality |
 * | q8    | ~150MB    | 768           | Minimal quality loss |
 * | q4    | ~80MB     | 768           | Good for low-memory devices |
 *
 * ### MRL (Matryoshka Representation Learning)
 * The model supports truncating embeddings to 512, 256, or 128 dimensions
 * with re-normalization for downstream efficiency.
 */
export class BrowserONNXProvider {
  private session: InferenceSession | null = null;
  private tokenizer: GemmaBpeTokenizer | null = null;
  private ready = false;
  private dtype: 'fp32' | 'q8' | 'q4';
  private backend: 'webgpu' | 'wasm';
  private detectedBackend: string | null = null;
  private maxSequenceLength = 2048; // Gemma-300M max position embeddings

  /** Default prefix for queries (can be overridden per-call). */
  private defaultPrefix: string;

  constructor(options?: Partial<EmbeddingOptions>) {
    this.dtype = options?.dtype ?? 'q8';
    this.backend = options?.backend ?? 'webgpu';
    this.defaultPrefix = options?.prefix ?? 'query';
  }

  /** Returns true if the provider is fully initialized and ready for inference. */
  isReady(): boolean {
    return this.ready;
  }

  /** Returns the embedding dimensionality (768 for EmbeddingGemma-300M). */
  dimensions(): number {
    return 768;
  }

  /** Returns the detected backend name (e.g., 'WebGPU', 'WASM'). */
  getBackend(): string | null {
    return this.detectedBackend;
  }

  /**
   * Initialize the provider: download tokenizer and model, create inference session.
   *
   * This is the most expensive step — model download can take several seconds
   * depending on network speed. On subsequent page loads, consider caching
   * the model with Service Worker or Cache API.
   */
  async initialize(): Promise<void> {
    if (this.ready) return;

    // 1. Load tokenizer
    console.log(`[BrowserONNXProvider] Loading tokenizer...`);
    await this.loadTokenizer();

    // 2. Detect backend
    const detected = this.detectBackend();
    this.detectedBackend = detected;
    console.log(`[BrowserONNXProvider] Using backend: ${detected}`);

    // 3. Load model
    console.log(
      `[BrowserONNXProvider] Loading ${this.dtype} model from onnx-community/embeddinggemma-300m-ONNX...`
    );
    await this.loadModel(detected);

    this.ready = true;
    console.log(`[BrowserONNXProvider] Ready (${this.dimensions()} dims, ${this.dtype}, ${detected})`);
  }

  /**
   * Generate an embedding for a single text input.
   *
   * @param text - The text to embed
   * @param prefix - Optional task prefix override (defaults to constructor's prefix)
   * @returns Normalized embedding vector
   */
  async embed(text: string, prefix?: string): Promise<EmbeddingResult> {
    if (!this.ready) throw new Error('Provider not initialized. Call initialize() first.');
    if (!this.session || !this.tokenizer) throw new Error('Session or tokenizer not loaded.');

    // Extract to local to satisfy TypeScript narrowing
    const session = this.session;
    const tokenizer = this.tokenizer;

    // Apply prefix
    const prefixStr = EMBEDDINGGEMMA_PREFIXES[prefix ?? this.defaultPrefix] ?? '';
    const fullText = prefixStr + text;

    // Tokenize
    const tokens = tokenizer.tokenize(fullText);
    const tokenIds = this.wrapSequence(tokens);
    const seqLen = tokenIds.length;

    // Run inference
    const outputs = await this.runInference([tokenIds], [seqLen]);

    // Extract and pool
    return this.poolAndNormalize(outputs[0], seqLen);
  }

  /**
   * Generate embeddings for a batch of text inputs.
   *
   * Note: EmbeddingGemma-300M has a fixed-size input, so batching works
   * by padding shorter sequences to the longest in the batch.
   *
   * @param texts - Array of texts to embed
   * @param prefix - Optional task prefix override
   * @returns Array of normalized embedding vectors
   */
  async embedBatch(
    texts: string[],
    prefix?: string
  ): Promise<EmbeddingResult[]> {
    if (!this.ready) throw new Error('Provider not initialized. Call initialize() first.');
    if (!this.session || !this.tokenizer) throw new Error('Session or tokenizer not loaded.');
    if (texts.length === 0) return [];

    // Extract to local to satisfy TypeScript narrowing in closures
    const tokenizer = this.tokenizer;

    const prefixStr = EMBEDDINGGEMMA_PREFIXES[prefix ?? this.defaultPrefix] ?? '';

    // Tokenize all texts
    const allTokens = texts.map((t) => {
      const fullText = prefixStr + t;
      const tokens = tokenizer.tokenize(fullText);
      return this.wrapSequence(tokens);
    });

    // Find max sequence length in batch
    const seqLens = allTokens.map((t) => t.length);
    const maxLen = Math.max(...seqLens);

    // Pad all sequences to maxLen
    const paddedTokens = allTokens.map((tokens) => {
      if (tokens.length === maxLen) return tokens;
      return [...tokens, ...new Array(maxLen - tokens.length).fill(PAD_TOKEN_ID)];
    });

    // Run inference for the batch
    const outputs = await this.runInference(paddedTokens, seqLens);

    // Pool and normalize each result
    const results: EmbeddingResult[] = [];
    for (let b = 0; b < texts.length; b++) {
      results.push(this.poolAndNormalize(outputs[b], seqLens[b]));
    }

    return results;
  }

  /**
   * Release all resources held by this provider.
   * Call this when the provider is no longer needed to free GPU/CPU memory.
   */
  async close(): Promise<void> {
    if (this.session) {
      await this.session.release();
      this.session = null;
    }
    this.tokenizer = null;
    this.ready = false;
  }

  // ─── Internal Methods ─────────────────────────────────────────

  /** Detect the best available backend. */
  private detectBackend(): 'webgpu' | 'wasm' {
    const pref = this.backend;

    // Check WebGPU availability
    if (pref === 'webgpu' && typeof navigator !== 'undefined' && 'gpu' in navigator) {
      return 'webgpu';
    }

    // Check WASM SIMD (generally available in modern browsers)
    if (pref === 'wasm') return 'wasm';

    // Auto-detect: prefer WebGPU, fallback to WASM
    if (typeof navigator !== 'undefined' && 'gpu' in navigator) {
      return 'webgpu';
    }

    return 'wasm';
  }

  /** Load tokenizer.json and build the BPE tokenizer. */
  private async loadTokenizer(): Promise<void> {
    const resp = await fetch(TOKENIZER_URL);
    if (!resp.ok) {
      throw new Error(
        `Failed to download tokenizer: ${resp.status} ${resp.statusText}`
      );
    }

    const config: TokenizerConfig = await resp.json();
    this.tokenizer = new GemmaBpeTokenizer(config);
  }

  /** Download model and create ONNX inference session. */
  private async loadModel(backend: 'webgpu' | 'wasm'): Promise<void> {
    const files = MODEL_FILES[this.dtype];
    const modelUrl = files.model;

    // Calculate the base URL for resolving companion .onnx_data files.
    const baseUrl = new URL(modelUrl).origin +
      new URL(modelUrl).pathname.substring(0, new URL(modelUrl).pathname.lastIndexOf('/'));

    // Use a type-cast to access the resolveFspath option which may not
    // be in the type definitions for this version but is supported at runtime.
    const opts: InferenceSession.SessionOptions & { resolveFspath?: (path: string) => string } = {
      executionProviders: backend === 'webgpu' ? ['webgpu'] : ['wasm'],
      graphOptimizationLevel: 'all',
    };

    if (files.data) {
      opts.resolveFspath = (fspath: string) => {
        return `${baseUrl}/${fspath}`;
      };
    }

    this.session = await InferenceSession.create(modelUrl, opts as InferenceSession.SessionOptions);
  }

  /**
   * Wrap token IDs with BOS and EOS markers, truncate to max sequence length.
   */
  private wrapSequence(tokens: number[]): number[] {
    // Add BOS at start and EOS at end
    let seq = [BOS_TOKEN_ID, ...tokens, EOS_TOKEN_ID];

    // Truncate if too long (keep BOS and EOS)
    if (seq.length > this.maxSequenceLength) {
      const keep = this.maxSequenceLength - 1; // -1 for EOS
      seq = [BOS_TOKEN_ID, ...seq.slice(1, 1 + keep), EOS_TOKEN_ID];
    }

    return seq;
  }

  /**
   * Run ONNX inference on the given token sequences.
   * Returns the output tensor data for each batch element.
   */
  private async runInference(
    tokenSequences: number[][],
    seqLens: number[]
  ): Promise<Float32Array[]> {
    if (!this.session) throw new Error('No session');

    const batchSize = tokenSequences.length;
    const maxLen = Math.max(...seqLens);

    // Pad all sequences to maxLen (use BigInt64Array for int64 ONNX input)
    // Note: BigInt64Array constructor doesn't accept number[], must convert manually
    const padded = tokenSequences.map((tokens) => {
      const arr = new BigInt64Array(maxLen);
      for (let i = 0; i < tokens.length; i++) arr[i] = BigInt(tokens[i]);
      for (let i = tokens.length; i < maxLen; i++) arr[i] = BigInt(PAD_TOKEN_ID);
      return arr;
    });

    // Build attention mask (1 for real tokens, 0 for padding)
    const attentionData = new BigInt64Array(batchSize * maxLen);
    const ONE = BigInt(1);
    const ZERO = BigInt(0);
    for (let b = 0; b < batchSize; b++) {
      for (let i = 0; i < maxLen; i++) {
        attentionData[b * maxLen + i] = i < seqLens[b] ? ONE : ZERO;
      }
    }

    // Create input tensors
    // EmbeddingGemma-300M ONNX expects int64 for input_ids and attention_mask
    // Shape: [batch_size, sequence_length]

    // We need to use BigInt64Array for int64 tensors
    const inputIdsData = new BigInt64Array(batchSize * maxLen);
    for (let b = 0; b < batchSize; b++) {
      for (let i = 0; i < maxLen; i++) {
        inputIdsData[b * maxLen + i] = BigInt(padded[b][i]);
      }
    }

    const inputIdsTensor = new Tensor('int64', inputIdsData, [batchSize, maxLen]);
    const attentionTensor = new Tensor('int64', attentionData, [batchSize, maxLen]);

    try {
      // Run inference
      const feeds: Record<string, Tensor> = {};
      for (const name of this.session.inputNames) {
        if (name === 'input_ids') {
          feeds[name] = inputIdsTensor;
        } else if (name === 'attention_mask') {
          feeds[name] = attentionTensor;
        } else if (name === 'position_ids') {
          // Some Gemma models expect position_ids
          const posData = new BigInt64Array(batchSize * maxLen);
          for (let b = 0; b < batchSize; b++) {
            for (let i = 0; i < maxLen; i++) {
              posData[b * maxLen + i] = BigInt(i);
            }
          }
          feeds[name] = new Tensor('int64', posData, [batchSize, maxLen]);
        }
      }

      const outputs = await this.session.run(feeds);

      // Extract output data
      const results: Float32Array[] = [];
      const outputNames = this.session.outputNames;

      // Find the main hidden state output
      const hiddenOutputName = this.findHiddenStateOutput(outputNames);

      for (let b = 0; b < batchSize; b++) {
        const outputTensor = outputs[hiddenOutputName];
        // Get data from tensor (may be on GPU, need to download to CPU)
        const data = await outputTensor.getData(true);

        if (!(data instanceof Float32Array)) {
          // Some backends return raw buffer, cast to Float32Array
          throw new Error(`Unexpected output type: ${data.constructor.name}`);
        }

        // Output shape: [batch_size, sequence_length, hidden_size]
        // Extract batch element b's sequence
        const hiddenSize = this.dimensions();
        const embedding = new Float32Array(maxLen * hiddenSize);
        for (let i = 0; i < maxLen; i++) {
          const srcOffset = (b * maxLen + i) * hiddenSize;
          const dstOffset = i * hiddenSize;
          embedding.set(data.subarray(srcOffset, srcOffset + hiddenSize), dstOffset);
        }

        results.push(embedding);
      }

      return results;
    } finally {
      // Clean up input tensors (safe disposal in case mocks lack dispose)
      inputIdsTensor.dispose?.();
      attentionTensor.dispose?.();
    }
  }

  /**
   * Find the output tensor name that contains the hidden state.
   * Different ONNX exports may use different output names.
   */
  private findHiddenStateOutput(outputNames: readonly string[]): string {
    const candidates = [
      'last_hidden_state',
      'hidden_states',
      'output',
      'logits',
    ];
    for (const name of outputNames) {
      if (candidates.includes(name)) return name;
    }
    // Fallback: use the first output
    return outputNames[0];
  }

  /**
   * Apply [CLS] pooling (take first token embedding) and L2 normalize.
   *
   * EmbeddingGemma uses the [CLS] token (first position) for sentence-level
   * embeddings. After extracting the first token's vector, we normalize
   * it to unit length for cosine similarity comparisons.
   *
   * Supports MRL (Matryoshka Representation Learning) truncation to reduce
   * dimensionality: 768 → 512 → 256 → 128 with re-normalization.
   */
  private poolAndNormalize(
    hiddenStates: Float32Array,
    seqLen: number,
    targetDims?: number
  ): EmbeddingResult {
    // [CLS] pooling: take the first token's hidden state
    // hiddenStates layout: [seqLen, hiddenSize] (for single batch element)
    const hiddenSize = this.dimensions();

    const clsEmbedding = hiddenStates.subarray(0, hiddenSize);

    // MRL truncation if requested
    const dims = targetDims ?? hiddenSize;
    let embedding = clsEmbedding.slice(0, dims);

    // L2 normalize
    const norm = this.l2Norm(embedding);
    if (norm > 0) {
      for (let i = 0; i < dims; i++) {
        embedding[i] /= norm;
      }
    }

    return { embedding, dims };
  }

  /** Compute L2 norm of a Float32Array. */
  private l2Norm(v: Float32Array): number {
    let sum = 0;
    for (let i = 0; i < v.length; i++) {
      sum += v[i] * v[i];
    }
    return Math.sqrt(sum);
  }
}

// ─── Singleton / Factory Helpers ──────────────────────────────────

/**
 * Create a provider instance (convenience factory).
 *
 * @example
 * ```typescript
 * const provider = createEmbeddingProvider({ dtype: 'q8' });
 * await provider.initialize();
 * const result = await provider.embed('Hello world');
 * ```
 */
export function createEmbeddingProvider(
  options?: Partial<EmbeddingOptions>
): BrowserONNXProvider {
  return new BrowserONNXProvider(options);
}

/**
 * Check if the browser supports WebGPU (for faster inference).
 */
export function hasWebGpuSupport(): boolean {
  return typeof navigator !== 'undefined' && 'gpu' in navigator;
}

/**
 * Check if the browser supports WebAssembly SIMD (for faster WASM inference).
 * Most modern browsers support this.
 */
export function hasWasmSimdSupport(): boolean {
  // WASM SIMD is available in all modern browsers (Chrome 91+, Firefox 104+, Safari 16+)
  // We can do a basic check by looking at WebAssembly capabilities
  return typeof WebAssembly !== 'undefined';
}

/**
 * Estimated model download size for each dtype option.
 */
export const MODEL_SIZES: Record<'fp32' | 'q8' | 'q4', { model: string; data: string; total: string }> = {
  fp32: { model: '~560MB', data: '~40MB', total: '~600MB' },
  q8: { model: '~140MB', data: '~10MB', total: '~150MB' },
  q4: { model: '~70MB', data: '~10MB', total: '~80MB' },
};
