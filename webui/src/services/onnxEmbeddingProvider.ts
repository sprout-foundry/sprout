/**
 * Browser-side ONNX embedding provider for EmbeddingGemma-300M.
 *
 * Uses onnxruntime-web to run Google's EmbeddingGemma-300M model directly
 * in the browser via the WASM or WebGPU backend.
 *
 * This is the TypeScript counterpart to pkg/embedding/onnx_*.go. The two
 * MUST produce byte-identical token IDs for the same input — the Go side
 * has a reference fixture (pkg/embedding/testdata/embeddinggemma_tokenizer_fixture.json)
 * that validates against the HuggingFace `tokenizers` library output, and
 * the TS implementation here is a direct port of the Go logic in
 * pkg/embedding/onnx_tokenizer.go. When changing the tokenization pipeline,
 * update BOTH sides together.
 *
 * Model source: https://huggingface.co/onnx-community/embeddinggemma-300m-ONNX
 */

// Type-only import keeps the onnxruntime-web type surface available at
// compile time without pulling the ~800 KB runtime into the initial
// bundle. The actual InferenceSession/Tensor constructors load on first
// use via loadOnnxRuntime() below.
import type { InferenceSession, Tensor } from 'onnxruntime-web';
import { loadOnnxRuntimeWeb, isOnnxRuntimeWebLoaded } from './onnxruntimeWebLoader';

let onnxModulePromise: Promise<typeof import('onnxruntime-web')> | null = null;
async function loadOnnxRuntime(): Promise<typeof import('onnxruntime-web')> {
  if (!onnxModulePromise) {
    onnxModulePromise = import('onnxruntime-web');
  }
  return onnxModulePromise;
}

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

// ─── Persistent download cache ────────────────────────────────────
// Model files and the tokenizer are large (80MB-600MB total) and don't
// change between sessions — re-downloading on every page load is wasteful
// and visibly slow. cachedFetch wraps the standard fetch and stores
// responses in the Cache Storage API, which the browser persists across
// reloads, tab restarts, and (depending on storage policy) browser restarts.
//
// The cache is named so different sprout versions can invalidate via name
// bump if the model URLs ever change.
const ONNX_CACHE_NAME = 'sprout-onnx-models-v1';

async function cachedFetch(url: string): Promise<Response> {
  // Cache Storage may be unavailable in some test/jsdom environments and in
  // service-worker-less contexts. Fall back to plain fetch transparently.
  if (typeof caches === 'undefined') {
    return fetch(url);
  }
  const cache = await caches.open(ONNX_CACHE_NAME);
  const hit = await cache.match(url);
  if (hit) return hit;
  const resp = await fetch(url);
  if (resp.ok) {
    // Clone before consuming — Response bodies are read-once streams.
    cache.put(url, resp.clone()).catch(() => {
      // Quota errors / storage policy denials are not fatal — we still have
      // the live response. Swallow to keep the embedding path unblocked.
    });
  }
  return resp;
}

// ─── Tokenizer schema ─────────────────────────────────────────────
// Tightly scoped to what EmbeddingGemma actually ships. We deliberately
// ignore decoder, post_processor, and most processor fields — the embedding
// path doesn't need them.

interface TokenizerJSON {
  model: {
    type: string;
    vocab: Record<string, number>;
    /**
     * HuggingFace ships merges in two formats:
     *   - newer (EmbeddingGemma): [["first", "second"], ...]
     *   - older: ["first second", ...]
     * We accept both; the older form is preserved so hand-written test
     * tokenizers stay readable.
     */
    merges: unknown;
  };
  added_tokens?: Array<{
    id: number;
    content: string;
    special: boolean;
  }>;
  /**
   * EmbeddingGemma uses { type: 'Replace', pattern: { String: ' ' }, content: '▁' }.
   * We model the discriminated pattern union as {String?, Regex?} — only
   * String is honored today.
   */
  normalizer?: {
    type: string;
    pattern?: { String?: string; Regex?: string };
    content?: string;
  };
}

interface BpePair {
  first: string;
  second: string;
}

/** SentencePiece space marker used by EmbeddingGemma — replaces literal space. */
const SP_SPACE = '▁';

/**
 * GemmaBpeTokenizer tokenizes text using the HuggingFace BPE pipeline as
 * configured for Google's EmbeddingGemma-300M.
 *
 * Pipeline (matches HuggingFace tokenizers semantics):
 *
 *   1. Split input around added-token strings (e.g. "\n", "\t", "<bos>").
 *      Matched runs are emitted directly as their IDs; everything else
 *      falls through to the BPE path.
 *   2. Normalize each non-added segment by replacing " " (U+0020) with the
 *      SentencePiece space marker "▁" (U+2581) per the Replace normalizer.
 *   3. Apply rank-ordered BPE merges to the normalized text treated as a
 *      sequence of single-codepoint symbols.
 *   4. Map each resulting symbol to its vocab id, falling back to <unk> on miss.
 *
 * This is a direct port of pkg/embedding/onnx_tokenizer.go. The Go side has
 * a reference fixture validating it against HuggingFace `tokenizers` v0.23.1
 * for 14 representative inputs; the TS implementation here uses the same
 * rules so cross-language tokenization stays consistent.
 */
class GemmaBpeTokenizer {
  private readonly vocab: Map<string, number>;
  private readonly bpeRanks: Map<string, number>; // key = first + "\x1f" + second

  /** Added-token content → id, with content-length buckets sorted desc for longest-match. */
  private readonly addedByContent: Map<string, number>;
  private readonly addedLengths: number[];

  readonly bosID: number;
  readonly eosID: number;
  readonly padID: number;
  readonly unkID: number;
  readonly vocabSize: number;

  // Normalizer: replace `normalizerPattern` with `normalizerContent`. Empty
  // pattern disables normalization.
  private readonly normalizerPattern: string;
  private readonly normalizerContent: string;

  constructor(config: TokenizerJSON) {
    const model = config.model;
    if (model.type !== 'BPE') {
      throw new Error(`Expected BPE tokenizer, got ${model.type}`);
    }

    this.vocab = new Map(Object.entries(model.vocab));
    this.vocabSize = this.vocab.size;

    this.bpeRanks = new Map();
    const merges = parseMerges(model.merges);
    for (let i = 0; i < merges.length; i++) {
      this.bpeRanks.set(mergeKey(merges[i].first, merges[i].second), i);
    }

    // Added-tokens: record every entry — special or not — because HF treats
    // both classes as atomic during pre-processing. Literal "\n\n\n" in
    // input must become id 109 rather than going through BPE.
    this.addedByContent = new Map();
    const lengthSet = new Set<number>();
    let bos = -1,
      eos = -1,
      pad = -1,
      unk = -1;
    for (const at of config.added_tokens ?? []) {
      if (!at.content) continue;
      this.addedByContent.set(at.content, at.id);
      lengthSet.add(at.content.length);
      switch (at.content) {
        case '<bos>':
          bos = at.id;
          break;
        case '<eos>':
          eos = at.id;
          break;
        case '<pad>':
          pad = at.id;
          break;
        case '<unk>':
          unk = at.id;
          break;
      }
    }
    this.addedLengths = Array.from(lengthSet).sort((a, b) => b - a);
    this.bosID = bos;
    this.eosID = eos;
    this.padID = pad;
    this.unkID = unk;

    // Normalizer: only the simple Replace form is recognized.
    const n = config.normalizer;
    if (n && n.type === 'Replace' && n.pattern?.String && n.content != null) {
      this.normalizerPattern = n.pattern.String;
      this.normalizerContent = n.content;
    } else {
      this.normalizerPattern = '';
      this.normalizerContent = '';
    }
  }

  /**
   * Tokenize text to a sequence of token IDs. Does NOT add BOS/EOS — use
   * encodeWithBOSAndEOS for the wrapped form. Empty input returns [].
   */
  tokenize(text: string): number[] {
    if (text.length === 0) return [];
    const out: number[] = [];
    this.encodeSegment(text, out);
    return out;
  }

  /**
   * Encode with BOS prepended and EOS appended. Mirrors the HuggingFace
   * `tokenizers` reference output for EmbeddingGemma — including the
   * [BOS, EOS] pair for empty input.
   */
  encodeWithBOSAndEOS(text: string): number[] {
    const bare = this.tokenize(text);
    const out: number[] = [];
    if (this.bosID >= 0) out.push(this.bosID);
    for (const id of bare) out.push(id);
    if (this.eosID >= 0) out.push(this.eosID);
    return out;
  }

  /** Walk the input left-to-right, peeling off the leftmost longest-matching added-token. */
  private encodeSegment(text: string, out: number[]): void {
    while (text.length > 0) {
      const match = this.findLeftmostAddedToken(text);
      if (!match) {
        this.bpeEncode(text, out);
        return;
      }
      if (match.start > 0) {
        this.bpeEncode(text.slice(0, match.start), out);
      }
      out.push(match.id);
      text = text.slice(match.start + match.length);
    }
  }

  /**
   * Find the earliest position in text where any added token matches.
   * At each position the longest matching token wins (HF semantics).
   */
  private findLeftmostAddedToken(text: string): { start: number; length: number; id: number } | null {
    if (this.addedByContent.size === 0) return null;
    for (let s = 0; s < text.length; s++) {
      for (const l of this.addedLengths) {
        if (s + l > text.length) continue;
        const id = this.addedByContent.get(text.slice(s, s + l));
        if (id !== undefined) {
          return { start: s, length: l, id };
        }
      }
    }
    return null;
  }

  private bpeEncode(segment: string, out: number[]): void {
    if (segment.length === 0) return;
    const normalized = this.normalize(segment);
    const symbols = splitIntoCodepoints(normalized);
    const merged = this.applyBPE(symbols);
    for (const sym of merged) {
      const id = this.vocab.get(sym);
      if (id !== undefined) {
        out.push(id);
      } else if (this.unkID >= 0) {
        out.push(this.unkID);
      }
    }
  }

  /** Apply the recorded Replace normalizer. */
  private normalize(s: string): string {
    if (this.normalizerPattern === '') return s;
    return s.split(this.normalizerPattern).join(this.normalizerContent);
  }

  /**
   * Classical BPE: repeatedly merge the pair with the lowest merge rank
   * until no merge applies. Operates on a slice of single-codepoint symbol
   * strings that grow as merges are applied. All occurrences of the chosen
   * pair are merged in a single pass, matching HF's behavior so the output
   * stays byte-identical to its reference.
   */
  private applyBPE(symbols: string[]): string[] {
    if (symbols.length < 2) return symbols;
    for (;;) {
      let bestRank = Number.MAX_SAFE_INTEGER;
      let bestIdx = -1;
      for (let i = 0; i < symbols.length - 1; i++) {
        const rank = this.bpeRanks.get(mergeKey(symbols[i], symbols[i + 1]));
        if (rank !== undefined && rank < bestRank) {
          bestRank = rank;
          bestIdx = i;
        }
      }
      if (bestIdx < 0) return symbols;

      const first = symbols[bestIdx];
      const second = symbols[bestIdx + 1];
      const merged: string[] = [];
      for (let i = 0; i < symbols.length; ) {
        if (i + 1 < symbols.length && symbols[i] === first && symbols[i + 1] === second) {
          merged.push(symbols[i] + symbols[i + 1]);
          i += 2;
        } else {
          merged.push(symbols[i]);
          i++;
        }
      }
      symbols = merged;
    }
  }
}

/** Build a merge-table key. Uses U+001F (unit separator) to avoid collisions
 *  with any legitimate codepoint that could appear in vocabulary strings. */
function mergeKey(a: string, b: string): string {
  return a + '\x1f' + b;
}

/** Accepts either pair-array (`[["a","b"]]`) or joined-string (`["a b"]`) merge formats. */
function parseMerges(raw: unknown): BpePair[] {
  if (raw == null) return [];
  if (!Array.isArray(raw)) throw new Error('tokenizer: merges must be an array');
  const out: BpePair[] = [];
  for (let i = 0; i < raw.length; i++) {
    const entry = raw[i];
    if (Array.isArray(entry)) {
      if (entry.length !== 2 || typeof entry[0] !== 'string' || typeof entry[1] !== 'string') {
        throw new Error(`tokenizer: merges[${i}] must be a 2-string array`);
      }
      out.push({ first: entry[0], second: entry[1] });
    } else if (typeof entry === 'string') {
      const idx = entry.indexOf(' ');
      if (idx < 0) throw new Error(`tokenizer: merges[${i}] "${entry}" has no space separator`);
      out.push({ first: entry.slice(0, idx), second: entry.slice(idx + 1) });
    } else {
      throw new Error(`tokenizer: merges[${i}] unrecognized type`);
    }
  }
  return out;
}

/**
 * Split a string into single-codepoint pieces. JavaScript's `for...of`
 * iterates code points (not UTF-16 code units), which correctly handles
 * astral-plane characters like emoji.
 */
function splitIntoCodepoints(s: string): string[] {
  const out: string[] = [];
  for (const cp of s) out.push(cp);
  return out;
}

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
 * The model exposes a pre-pooled `sentence_embedding` output (shape
 * [batch, 768]), which is what this provider consumes. Earlier versions of
 * this code did manual [CLS]-pooling on `last_hidden_state`, which produced
 * the wrong embeddings — the model is trained to be queried via the pooled
 * head.
 */
export class BrowserONNXProvider {
  private session: InferenceSession | null = null;
  private tokenizer: GemmaBpeTokenizer | null = null;
  private ready = false;
  private dtype: 'fp32' | 'q8' | 'q4';
  private backend: 'webgpu' | 'wasm';
  private detectedBackend: string | null = null;
  private maxSequenceLength = 2048; // EmbeddingGemma-300M max position embeddings
  private defaultPrefix: string;

  constructor(options?: Partial<EmbeddingOptions>) {
    this.dtype = options?.dtype ?? 'q8';
    this.backend = options?.backend ?? 'webgpu';
    this.defaultPrefix = options?.prefix ?? 'query';
  }

  isReady(): boolean {
    return this.ready;
  }
  dimensions(): number {
    return 768;
  }
  getBackend(): string | null {
    return this.detectedBackend;
  }

  async initialize(): Promise<void> {
    if (this.ready) return;

    await this.loadTokenizer();
    const detected = this.detectBackend();
    this.detectedBackend = detected;
    await this.loadModel(detected);
    this.ready = true;
  }

  /**
   * Generate an embedding for a single text input. Returns the pooled,
   * L2-normalized 768-dim vector.
   */
  async embed(text: string, prefix?: string): Promise<EmbeddingResult> {
    if (!this.ready || !this.session || !this.tokenizer) {
      throw new Error('Provider not initialized. Call initialize() first.');
    }
    const fullText = (EMBEDDINGGEMMA_PREFIXES[prefix ?? this.defaultPrefix] ?? '') + text;
    const tokenIds = this.wrapAndTruncate(this.tokenizer.encodeWithBOSAndEOS(fullText));
    const pooled = await this.runInference([tokenIds]);
    return this.normalize(pooled[0]);
  }

  /**
   * Generate embeddings for a batch of texts. Sequences are right-padded to
   * the longest length in the batch; the attention mask zeros out padded
   * positions so the pooling stays correct.
   */
  async embedBatch(texts: string[], prefix?: string): Promise<EmbeddingResult[]> {
    if (!this.ready || !this.session || !this.tokenizer) {
      throw new Error('Provider not initialized. Call initialize() first.');
    }
    if (texts.length === 0) return [];
    const prefixStr = EMBEDDINGGEMMA_PREFIXES[prefix ?? this.defaultPrefix] ?? '';
    const seqs = texts.map((t) => this.wrapAndTruncate(this.tokenizer!.encodeWithBOSAndEOS(prefixStr + t)));
    const pooled = await this.runInference(seqs);
    return pooled.map((v) => this.normalize(v));
  }

  async close(): Promise<void> {
    if (this.session) {
      await this.session.release();
      this.session = null;
    }
    this.tokenizer = null;
    this.ready = false;
  }

  // ─── Internal Methods ─────────────────────────────────────────

  private detectBackend(): 'webgpu' | 'wasm' {
    const pref = this.backend;
    if (pref === 'webgpu' && typeof navigator !== 'undefined' && 'gpu' in navigator) {
      return 'webgpu';
    }
    if (pref === 'wasm') return 'wasm';
    if (typeof navigator !== 'undefined' && 'gpu' in navigator) return 'webgpu';
    return 'wasm';
  }

  private async loadTokenizer(): Promise<void> {
    const resp = await cachedFetch(TOKENIZER_URL);
    if (!resp.ok) {
      throw new Error(`Failed to download tokenizer: ${resp.status} ${resp.statusText}`);
    }
    const config: TokenizerJSON = await resp.json();
    this.tokenizer = new GemmaBpeTokenizer(config);
  }

  private async loadModel(backend: 'webgpu' | 'wasm'): Promise<void> {
    const files = MODEL_FILES[this.dtype];

    // Pre-fetch the model graph via cachedFetch so subsequent page loads hit
    // the Cache Storage instead of the network. Passing the bytes inline to
    // InferenceSession.create (Uint8Array form) takes the URL fetch out of
    // onnxruntime-web's hands — otherwise its internal fetch bypasses our
    // cache entirely.
    const modelResp = await cachedFetch(files.model);
    if (!modelResp.ok) {
      throw new Error(`Failed to download model: ${modelResp.status} ${modelResp.statusText}`);
    }
    const modelBytes = new Uint8Array(await modelResp.arrayBuffer());

    const opts: InferenceSession.SessionOptions & {
      externalData?: Array<{ path: string; data: Uint8Array }>;
    } = {
      executionProviders: backend === 'webgpu' ? ['webgpu'] : ['wasm'],
      graphOptimizationLevel: 'all',
    };

    if (files.data) {
      // External weights blob. Same caching strategy. We pass it inline via
      // externalData so the loader doesn't issue its own (uncached) request.
      // The `path` must match the relative reference baked into the .onnx
      // graph — for embeddinggemma-300m that's the basename of the URL.
      const dataResp = await cachedFetch(files.data);
      if (!dataResp.ok) {
        throw new Error(`Failed to download model data: ${dataResp.status} ${dataResp.statusText}`);
      }
      const dataBytes = new Uint8Array(await dataResp.arrayBuffer());
      const basename = files.data.substring(files.data.lastIndexOf('/') + 1);
      opts.externalData = [{ path: basename, data: dataBytes }];
    }

    // Ensure the onnxruntime-web CDN script is loaded before using the
    // dynamic import below. loadOnnxRuntimeWeb() injects a <script> tag
    // that populates window.ort; loadOnnxRuntime() then does `import('onnxruntime-web')`
    // for the typed module API. If the CDN script is already present (static
    // backend path), loadOnnxRuntimeWeb() returns immediately.
    if (!isOnnxRuntimeWebLoaded()) {
      await loadOnnxRuntimeWeb();
    }

    const ort = await loadOnnxRuntime();
    this.session = await ort.InferenceSession.create(modelBytes, opts as InferenceSession.SessionOptions);
  }

  /** Truncate a token sequence (with BOS/EOS markers) to maxSequenceLength,
   *  preserving the BOS at the start and EOS at the end. */
  private wrapAndTruncate(tokens: number[]): number[] {
    if (tokens.length <= this.maxSequenceLength) return tokens;
    const head = tokens.slice(0, this.maxSequenceLength - 1);
    // Re-attach EOS as the final token so the model still sees the sentinel.
    const eos = tokens[tokens.length - 1];
    head.push(eos);
    return head;
  }

  /**
   * Run ONNX inference on the given token sequences. Returns the pooled
   * sentence_embedding vector per batch element (Float32Array of length 768).
   */
  private async runInference(tokenSequences: number[][]): Promise<Float32Array[]> {
    if (!this.session) throw new Error('No session');
    const batchSize = tokenSequences.length;
    const maxLen = tokenSequences.reduce((m, s) => Math.max(m, s.length), 0);
    const padID = this.tokenizer?.padID ?? 0;

    const inputIdsData = new BigInt64Array(batchSize * maxLen);
    const attentionData = new BigInt64Array(batchSize * maxLen);
    const ONE = BigInt(1);
    const ZERO = BigInt(0);
    for (let b = 0; b < batchSize; b++) {
      const seq = tokenSequences[b];
      for (let i = 0; i < maxLen; i++) {
        if (i < seq.length) {
          inputIdsData[b * maxLen + i] = BigInt(seq[i]);
          attentionData[b * maxLen + i] = ONE;
        } else {
          inputIdsData[b * maxLen + i] = BigInt(padID);
          attentionData[b * maxLen + i] = ZERO;
        }
      }
    }

    // Defensive: runInference could be reached directly without initialize()
    // in custom bridges. loadOnnxRuntimeWeb is a no-op if already loaded.
    if (!isOnnxRuntimeWebLoaded()) {
      await loadOnnxRuntimeWeb();
    }
    const ort = await loadOnnxRuntime();
    const inputIdsTensor = new ort.Tensor('int64', inputIdsData, [batchSize, maxLen]);
    const attentionTensor = new ort.Tensor('int64', attentionData, [batchSize, maxLen]);

    try {
      const feeds: Record<string, Tensor> = {};
      for (const name of this.session.inputNames) {
        if (name === 'input_ids') feeds[name] = inputIdsTensor;
        else if (name === 'attention_mask') feeds[name] = attentionTensor;
      }
      const outputs = await this.session.run(feeds);

      // Prefer the model's pre-pooled sentence_embedding output when
      // available — the Go side does the same. Fall back to manual
      // mean-pooling over last_hidden_state if the ONNX export only
      // exposes the raw hidden states (uncommon for the standard
      // EmbeddingGemma export).
      const outputName = pickEmbeddingOutput(this.session.outputNames);
      const outputTensor = outputs[outputName];
      const raw = await outputTensor.getData(true);
      if (!(raw instanceof Float32Array)) {
        throw new Error(`Unexpected output type: ${(raw as object).constructor.name}`);
      }

      const hiddenSize = this.dimensions();
      const out: Float32Array[] = [];
      if (outputName === 'sentence_embedding') {
        // Layout: [batch, hidden]
        for (let b = 0; b < batchSize; b++) {
          out.push(raw.slice(b * hiddenSize, (b + 1) * hiddenSize));
        }
      } else {
        // Manual mean-pool over real tokens (attention_mask == 1).
        for (let b = 0; b < batchSize; b++) {
          const seqLen = tokenSequences[b].length;
          const accum = new Float32Array(hiddenSize);
          for (let i = 0; i < seqLen; i++) {
            const srcOffset = (b * maxLen + i) * hiddenSize;
            for (let d = 0; d < hiddenSize; d++) {
              accum[d] += raw[srcOffset + d];
            }
          }
          if (seqLen > 0) {
            for (let d = 0; d < hiddenSize; d++) accum[d] /= seqLen;
          }
          out.push(accum);
        }
      }
      return out;
    } finally {
      inputIdsTensor.dispose?.();
      attentionTensor.dispose?.();
    }
  }

  /** Slice to targetDims (if provided) and L2-normalize in place. */
  private normalize(v: Float32Array, targetDims?: number): EmbeddingResult {
    const dims = targetDims ?? v.length;
    const out = v.length === dims ? new Float32Array(v) : v.slice(0, dims);
    let sum = 0;
    for (let i = 0; i < out.length; i++) sum += out[i] * out[i];
    if (sum > 1e-9) {
      const inv = 1 / Math.sqrt(sum);
      for (let i = 0; i < out.length; i++) out[i] *= inv;
    }
    return { embedding: out, dims };
  }
}

/**
 * Pick the embedding output tensor name out of the model's declared outputs.
 * EmbeddingGemma ships both `last_hidden_state` and `sentence_embedding`;
 * the latter is already mean-pooled and is the correct choice. Older
 * exports may only declare `last_hidden_state`, in which case the caller
 * has to pool manually.
 */
function pickEmbeddingOutput(outputNames: readonly string[]): string {
  for (const n of outputNames) {
    if (n === 'sentence_embedding') return n;
  }
  for (const n of outputNames) {
    if (n === 'last_hidden_state' || n === 'hidden_states' || n === 'output') return n;
  }
  return outputNames[0];
}

// ─── Singleton / Factory Helpers ──────────────────────────────────

export function createEmbeddingProvider(options?: Partial<EmbeddingOptions>): BrowserONNXProvider {
  return new BrowserONNXProvider(options);
}

export function hasWebGpuSupport(): boolean {
  return typeof navigator !== 'undefined' && 'gpu' in navigator;
}

export function hasWasmSimdSupport(): boolean {
  return typeof WebAssembly !== 'undefined';
}

export const MODEL_SIZES: Record<'fp32' | 'q8' | 'q4', { model: string; data: string; total: string }> = {
  fp32: { model: '~560MB', data: '~40MB', total: '~600MB' },
  q8: { model: '~140MB', data: '~10MB', total: '~150MB' },
  q4: { model: '~70MB', data: '~10MB', total: '~80MB' },
};

// Re-exported for tests that want to construct a tokenizer directly without
// going through the full BrowserONNXProvider plumbing.
export { GemmaBpeTokenizer };
export type { TokenizerJSON };
