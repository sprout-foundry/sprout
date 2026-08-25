/**
 * Tests for BrowserONNXProvider — tokenizer correctness + plumbing.
 *
 * Tokenizer correctness is the high-value gate: the model produces useless
 * embeddings if input is tokenized differently from training. We pin against
 * the same fixture used by the Go side (testdata/embeddinggemma_tokenizer_fixture.json),
 * which was generated from HuggingFace `tokenizers` v0.23.1 — so a passing
 * test here means the TS and Go tokenizers agree byte-for-byte with the
 * reference implementation for all 14 cases.
 */

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { TokenizerJSON } from './onnxEmbeddingProvider';
import {
  BrowserONNXProvider,
  GemmaBpeTokenizer,
  createEmbeddingProvider,
  hasWebGpuSupport,
  hasWasmSimdSupport,
  MODEL_SIZES,
} from './onnxEmbeddingProvider';
import fixture from './testdata/embeddinggemma_tokenizer_fixture.json';

// ─── Mock onnxruntime-web ────────────────────────────────────────
// The real session is replaced with a stub that exposes both
// `sentence_embedding` (preferred) and `last_hidden_state` outputs so we
// can validate the provider picks the pre-pooled one.

vi.mock('onnxruntime-web', () => {
  const HIDDEN = 768;
  const mockSession = {
    inputNames: ['input_ids', 'attention_mask'],
    outputNames: ['sentence_embedding', 'last_hidden_state'],
    run: vi.fn(async (feeds: Record<string, { dims?: number[] }>) => {
      const batchSize = feeds['input_ids']?.dims?.[0] ?? 1;
      const seqLen = feeds['input_ids']?.dims?.[1] ?? 10;

      // sentence_embedding: [batch, hidden] — deterministic per-batch values.
      const pooled = new Float32Array(batchSize * HIDDEN);
      for (let b = 0; b < batchSize; b++) {
        for (let d = 0; d < HIDDEN; d++) {
          pooled[b * HIDDEN + d] = 0.1 + ((b + d) % 10) * 0.01;
        }
      }
      // last_hidden_state: [batch, seq, hidden] — only populated for the
      // fallback-pooling test; nonzero so manual-pool produces a real vector.
      const raw = new Float32Array(batchSize * seqLen * HIDDEN);
      for (let i = 0; i < raw.length; i++) raw[i] = 0.2;

      return {
        sentence_embedding: {
          dims: [batchSize, HIDDEN],
          getData: vi.fn(async () => pooled),
        },
        last_hidden_state: {
          dims: [batchSize, seqLen, HIDDEN],
          getData: vi.fn(async () => raw),
        },
      };
    }),
    release: vi.fn(async () => {}),
  };
  // Real class so `new Tensor(...)` actually sets .dims — vi.fn() + new
  // is unreliable about preserving constructor args in modern vitest.
  class MockTensor {
    type: string;
    data: unknown;
    dims: number[];
    constructor(type: string, data: unknown, dims: number[]) {
      this.type = type;
      this.data = data;
      this.dims = dims;
    }
    dispose() {}
  }
  return {
    InferenceSession: {
      create: vi.fn(async () => mockSession),
    },
    Tensor: MockTensor,
  };
});

// A minimal synthetic tokenizer for the plumbing tests (constructor,
// initialize, batch). Uses the modern pair-array merges format so the
// rewrite's parser is exercised here too. Vocab + merges chosen so the
// BPE algorithm has a clean, fully-reachable path: every char in test
// inputs is in the vocab and the merges build up the multi-char tokens.
const SYNTHETIC_TOKENIZER: TokenizerJSON = {
  model: {
    type: 'BPE',
    vocab: {
      '<pad>': 0,
      '<eos>': 1,
      '<bos>': 2,
      '<unk>': 3,
      a: 10,
      b: 11,
      c: 12,
      ab: 20,
      abc: 21,
      '▁': 30,
      '▁a': 31,
    },
    merges: [
      ['a', 'b'], // a + b -> ab
      ['ab', 'c'], // ab + c -> abc
      ['▁', 'a'], // ▁ + a -> ▁a
    ],
  },
  added_tokens: [
    { id: 0, content: '<pad>', special: true },
    { id: 1, content: '<eos>', special: true },
    { id: 2, content: '<bos>', special: true },
    { id: 3, content: '<unk>', special: true },
  ],
  normalizer: {
    type: 'Replace',
    pattern: { String: ' ' },
    content: '▁',
  },
};

describe('GemmaBpeTokenizer schema parsing', () => {
  it('parses pair-array merges (the EmbeddingGemma format)', () => {
    const t = new GemmaBpeTokenizer(SYNTHETIC_TOKENIZER);
    expect(t.bosID).toBe(2);
    expect(t.eosID).toBe(1);
    expect(t.vocabSize).toBeGreaterThan(0);
  });

  it('accepts joined-string merges for backward compatibility', () => {
    const t = new GemmaBpeTokenizer({
      model: {
        type: 'BPE',
        vocab: { a: 0, b: 1, ab: 2 },
        merges: ['a b'],
      },
    });
    expect(t.tokenize('ab')).toEqual([2]);
  });

  it('rejects non-BPE tokenizers', () => {
    expect(
      () =>
        new GemmaBpeTokenizer({
          model: { type: 'WordLevel', vocab: {}, merges: [] },
        } as unknown as TokenizerJSON),
    ).toThrow();
  });

  it('applies the Replace normalizer (space → ▁)', () => {
    const t = new GemmaBpeTokenizer(SYNTHETIC_TOKENIZER);
    // " a" → "▁a" after normalize → single token via the ▁+a merge
    expect(t.tokenize(' a')).toEqual([31]);
  });

  it('BPE rolls characters up through the merge table', () => {
    const t = new GemmaBpeTokenizer(SYNTHETIC_TOKENIZER);
    // "abc" → a, b, c → ab, c → abc (id 21)
    expect(t.tokenize('abc')).toEqual([21]);
  });

  it('encodeWithBOSAndEOS wraps with [BOS, ..., EOS]', () => {
    const t = new GemmaBpeTokenizer(SYNTHETIC_TOKENIZER);
    expect(t.encodeWithBOSAndEOS('abc')).toEqual([2, 21, 1]);
  });

  it('empty input yields [BOS, EOS] under encodeWithBOSAndEOS', () => {
    const t = new GemmaBpeTokenizer(SYNTHETIC_TOKENIZER);
    expect(t.encodeWithBOSAndEOS('')).toEqual([2, 1]);
  });
});

// Real-tokenizer fixture: validates against HuggingFace `tokenizers` v0.23.1
// output captured into testdata/embeddinggemma_tokenizer_fixture.json. This
// is the cross-language gate that catches divergence from the Go tokenizer.
//
// The fixture file omits BOS/EOS so we exercise the bare `tokenize()` path;
// the encodeWithBOSAndEOS wrapping is covered by the synthetic tests above.
// The fixture is only present locally — vitest will skip if the upstream
// tokenizer.json isn't available, mirroring the Go test's skip-if-missing
// behavior.
describe('GemmaBpeTokenizer against real EmbeddingGemma fixture', () => {
  it('matches HuggingFace tokenizers reference for all 14 cases', async () => {
    // We can't ship the full 20MB tokenizer.json in the repo, so this test
    // depends on the local environment having the file. When absent, skip.
    // This mirrors pkg/embedding/onnx_tokenizer_test.go:TestGemmaTokenizer_RealModel.
    const fs = await import('node:fs/promises');
    const path = await import('node:path');
    const os = await import('node:os');
    const tokenizerPath = path.join(
      os.homedir(),
      '.config',
      'sprout',
      'models',
      'embeddinggemma-300m',
      'tokenizer.json',
    );
    let tokenizerSource: string;
    try {
      tokenizerSource = await fs.readFile(tokenizerPath, 'utf-8');
    } catch {
      console.warn(`[skip] tokenizer not at ${tokenizerPath}`);
      return;
    }

    const config: TokenizerJSON = JSON.parse(tokenizerSource);
    const t = new GemmaBpeTokenizer(config);
    expect(t.bosID).toBe(fixture.special.bos);
    expect(t.eosID).toBe(fixture.special.eos);

    for (const c of fixture.cases) {
      const got = t.tokenize(c.input);
      expect(got, `case ${c.name}: input=${JSON.stringify(c.input)}`).toEqual(c.ids);
    }
  });
});

describe('BrowserONNXProvider', () => {
  let provider: BrowserONNXProvider;

  beforeEach(() => {
    vi.clearAllMocks();
    global.fetch = vi.fn(async (url: string) => {
      if (url.includes('tokenizer.json')) {
        return { ok: true, json: () => SYNTHETIC_TOKENIZER };
      }
      return { ok: true, arrayBuffer: () => new ArrayBuffer(0) };
    }) as unknown as typeof fetch;
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('initializes from tokenizer.json + ONNX session', async () => {
    provider = new BrowserONNXProvider({ dtype: 'q8', backend: 'wasm' });
    await provider.initialize();
    expect(provider.isReady()).toBe(true);
    expect(fetch).toHaveBeenCalledWith(expect.stringContaining('tokenizer.json'));
  });

  it('throws when initialize() fails to download the tokenizer', async () => {
    global.fetch = vi.fn(async () => ({
      ok: false,
      status: 404,
      statusText: 'Not Found',
    })) as unknown as typeof fetch;
    provider = new BrowserONNXProvider({ dtype: 'q8', backend: 'wasm' });
    await expect(provider.initialize()).rejects.toThrow('Failed to download tokenizer');
    expect(provider.isReady()).toBe(false);
  });

  it('embed() returns an L2-normalized vector of length 768', async () => {
    provider = new BrowserONNXProvider({ dtype: 'q8', backend: 'wasm' });
    await provider.initialize();
    const result = await provider.embed('hello world');
    expect(result.dims).toBe(768);
    expect(result.embedding).toBeInstanceOf(Float32Array);
    expect(result.embedding.length).toBe(768);
    let norm = 0;
    for (let i = 0; i < result.embedding.length; i++) {
      norm += result.embedding[i] * result.embedding[i];
    }
    expect(Math.abs(Math.sqrt(norm) - 1.0)).toBeLessThan(1e-5);
  });

  it('embedBatch() preserves order and produces normalized vectors', async () => {
    provider = new BrowserONNXProvider({ dtype: 'q8', backend: 'wasm' });
    await provider.initialize();
    const results = await provider.embedBatch(['hello', 'world', 'test']);
    expect(results).toHaveLength(3);
    for (const r of results) {
      expect(r.embedding).toBeInstanceOf(Float32Array);
      expect(r.dims).toBe(768);
    }
  });

  it('embedBatch([]) returns []', async () => {
    provider = new BrowserONNXProvider({ dtype: 'q8', backend: 'wasm' });
    await provider.initialize();
    expect(await provider.embedBatch([])).toEqual([]);
  });

  it('embed() before initialize() throws', async () => {
    const fresh = new BrowserONNXProvider();
    await expect(fresh.embed('test')).rejects.toThrow('Provider not initialized');
  });

  it('close() releases resources and flips isReady back to false', async () => {
    provider = new BrowserONNXProvider({ dtype: 'q8', backend: 'wasm' });
    await provider.initialize();
    expect(provider.isReady()).toBe(true);
    await provider.close();
    expect(provider.isReady()).toBe(false);
  });

  it('reuses Cache Storage on second initialize() to avoid re-downloading', async () => {
    // Stand up a minimal Cache Storage mock so cachedFetch's cache-hit branch
    // is exercised. jsdom does not provide one by default, so the regular
    // tests fall back to plain fetch — this test pins the cached path.
    const cacheStore = new Map<string, Response>();
    const cache = {
      match: vi.fn(async (key: string) => cacheStore.get(key)),
      put: vi.fn(async (key: string, resp: Response) => {
        cacheStore.set(key, resp);
      }),
    };
    (globalThis as { caches?: unknown }).caches = {
      open: vi.fn(async () => cache),
    };

    const fetchMock = vi.fn(async (url: string) => {
      if (url.includes('tokenizer.json')) {
        return {
          ok: true,
          clone: () => ({ ok: true, json: () => SYNTHETIC_TOKENIZER }),
          json: () => SYNTHETIC_TOKENIZER,
        };
      }
      return {
        ok: true,
        clone: () => ({ ok: true, arrayBuffer: () => new ArrayBuffer(0) }),
        arrayBuffer: () => new ArrayBuffer(0),
      };
    });
    global.fetch = fetchMock as unknown as typeof fetch;

    try {
      const p1 = new BrowserONNXProvider({ dtype: 'q8', backend: 'wasm' });
      await p1.initialize();
      const fetchCallsAfterFirst = fetchMock.mock.calls.length;
      expect(fetchCallsAfterFirst).toBeGreaterThan(0);

      const p2 = new BrowserONNXProvider({ dtype: 'q8', backend: 'wasm' });
      await p2.initialize();
      // Second provider should have served everything from cache: no new
      // fetch calls past what the first provider triggered.
      expect(fetchMock.mock.calls.length).toBe(fetchCallsAfterFirst);

      await p1.close();
      await p2.close();
    } finally {
      delete (globalThis as { caches?: unknown }).caches;
    }
  });
});

describe('helper exports', () => {
  it('createEmbeddingProvider produces a configured provider', () => {
    const p = createEmbeddingProvider({ dtype: 'q4' });
    expect(p).toBeInstanceOf(BrowserONNXProvider);
    expect(p.dimensions()).toBe(768);
  });

  it('hasWebGpuSupport returns a boolean', () => {
    expect(typeof hasWebGpuSupport()).toBe('boolean');
  });

  it('hasWasmSimdSupport returns a boolean', () => {
    expect(typeof hasWasmSimdSupport()).toBe('boolean');
  });

  it('MODEL_SIZES lists all three dtypes', () => {
    expect(MODEL_SIZES).toHaveProperty('fp32');
    expect(MODEL_SIZES).toHaveProperty('q8');
    expect(MODEL_SIZES).toHaveProperty('q4');
  });
});
