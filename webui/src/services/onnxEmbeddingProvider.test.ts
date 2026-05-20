/**
 * Tests for BrowserONNXProvider - tokenizer and core logic.
 *
 * Note: Full integration tests require a real ONNX model download,
 * which is too slow for unit tests. These tests verify the tokenizer
 * and embedding logic without loading the model.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import {
  BrowserONNXProvider,
  EmbeddingResult,
  EmbeddingOptions,
  createEmbeddingProvider,
  hasWebGpuSupport,
  hasWasmSimdSupport,
  MODEL_SIZES,
} from './onnxEmbeddingProvider';

// ─── Mock onnxruntime-web ────────────────────────────────────────
vi.mock('onnxruntime-web', () => {
  const mockSession = {
    inputNames: ['input_ids', 'attention_mask'],
    outputNames: ['last_hidden_state'],
    run: vi.fn(async (feeds) => {
      const batchSize = feeds['input_ids']?.dims?.[0] ?? 1;
      const seqLen = feeds['input_ids']?.dims?.[1] ?? 10;
      const hiddenSize = 768;
      // Return dummy float data for [batch, seq, hidden]
      const data = new Float32Array(batchSize * seqLen * hiddenSize);
      // Set first token to a known non-zero value for [CLS] pooling tests
      for (let i = 0; i < hiddenSize; i++) {
        data[i] = 0.1 + (i % 10) * 0.01; // Non-zero, non-uniform values
      }
      return {
        last_hidden_state: {
          dims: [batchSize, seqLen, hiddenSize],
          dataType: 'float32',
          size: batchSize * seqLen * hiddenSize,
          getData: vi.fn(async () => data),
        },
      };
    }),
    release: vi.fn(async () => {}),
  };

  const MockTensor = vi.fn().mockImplementation(() => ({
    dispose: vi.fn(),
    getData: vi.fn(),
  }));

  return {
    InferenceSession: {
      create: vi.fn(async () => mockSession),
    },
    Tensor: MockTensor,
  };
});

// ─── Mock fetch for tokenizer ─────────────────────────────────────
const mockTokenizerJson = {
  model: {
    type: 'BPE',
    vocab: {
      '<pad>': 0,
      '<s>': 2,
      '</s>': 1,
      '<unk>': 3,
      'Ġhello': 456,
      'Ġworld': 789,
      'Ġ': 144,
      'h': 145,
      'e': 146,
      'l': 147,
      'o': 148,
      'w': 149,
      'r': 150,
      'd': 151,
      'he': 200,
      'll': 201,
      'lo': 202,
      'wor': 203,
      'ld': 204,
    },
    merges: ['h e', 'e l', 'l l', 'l o', 'w o', 'o r', 'r d', 'he ll', 'wor ld'],
  },
  pre_tokenizer: {
    type: 'metaspace',
    pattern: '(?u)\\s+|[^\\s\\n\\r\\t\\u0008\\u000b\\u000c]+',
  },
};

// ─── Tests ────────────────────────────────────────────────────────

describe('BrowserONNXProvider', () => {
  let provider: BrowserONNXProvider;

  beforeEach(() => {
    vi.clearAllMocks();
    // Mock fetch for tokenizer
    global.fetch = vi.fn(async (url: string) => {
      if (url.includes('tokenizer.json')) {
        return { ok: true, json: () => mockTokenizerJson };
      }
      return { ok: true, arrayBuffer: () => new ArrayBuffer(0) };
    });
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  describe('constructor', () => {
    it('should use default options when none provided', () => {
      provider = new BrowserONNXProvider();
      expect(provider.isReady()).toBe(false);
      expect(provider.dimensions()).toBe(768);
      expect(provider.getBackend()).toBe(null);
    });

    it('should accept custom dtype', () => {
      provider = new BrowserONNXProvider({ dtype: 'q4' });
      expect(provider.isReady()).toBe(false);
    });

    it('should accept custom backend preference', () => {
      provider = new BrowserONNXProvider({ backend: 'wasm' });
      expect(provider.isReady()).toBe(false);
    });

    it('should accept custom prefix', () => {
      provider = new BrowserONNXProvider({ prefix: 'document' });
      expect(provider.isReady()).toBe(false);
    });
  });

  describe('initialize', () => {
    it('should download tokenizer and create session', async () => {
      provider = new BrowserONNXProvider({ dtype: 'q8', backend: 'wasm' });
      await provider.initialize();
      expect(provider.isReady()).toBe(true);
      expect(fetch).toHaveBeenCalledWith(
        expect.stringContaining('tokenizer.json')
      );
    });

    it('should not re-initialize if already ready', async () => {
      provider = new BrowserONNXProvider({ dtype: 'q8', backend: 'wasm' });
      await provider.initialize();
      await provider.initialize();
      // Should only call fetch once for tokenizer
      expect(fetch).toHaveBeenCalledTimes(1);
    });

    it('should throw if tokenizer download fails', async () => {
      global.fetch = vi.fn(async () => ({ ok: false, status: 404, statusText: 'Not Found' }));
      provider = new BrowserONNXProvider({ dtype: 'q8', backend: 'wasm' });
      await expect(provider.initialize()).rejects.toThrow('Failed to download tokenizer');
      expect(provider.isReady()).toBe(false);
    });
  });

  describe('embed', () => {
    beforeEach(async () => {
      provider = new BrowserONNXProvider({ dtype: 'q8', backend: 'wasm' });
      await provider.initialize();
    });

    it('should embed a single text with default prefix', async () => {
      const result = await provider.embed('hello world');
      expect(result).toMatchObject({
        dims: 768,
      });
      expect(result.embedding).toBeInstanceOf(Float32Array);
      expect(result.embedding.length).toBe(768);
    });

    it('should embed with custom prefix', async () => {
      const result = await provider.embed('hello world', 'document');
      expect(result.embedding).toBeInstanceOf(Float32Array);
      expect(result.dims).toBe(768);
    });

    it('should throw if not initialized', async () => {
      const freshProvider = new BrowserONNXProvider();
      await expect(freshProvider.embed('test')).rejects.toThrow(
        'Provider not initialized'
      );
    });

    it('should produce normalized embeddings (unit vector)', async () => {
      const result = await provider.embed('test');
      // Calculate norm
      let norm = 0;
      for (let i = 0; i < result.embedding.length; i++) {
        norm += result.embedding[i] * result.embedding[i];
      }
      // Should be very close to 1.0 after L2 normalization
      expect(Math.abs(Math.sqrt(norm) - 1.0)).toBeLessThan(0.01);
    });
  });

  describe('embedBatch', () => {
    beforeEach(async () => {
      provider = new BrowserONNXProvider({ dtype: 'q8', backend: 'wasm' });
      await provider.initialize();
    });

    it('should embed multiple texts', async () => {
      const results = await provider.embedBatch(['hello', 'world', 'test']);
      expect(results).toHaveLength(3);
      for (const result of results) {
        expect(result.embedding).toBeInstanceOf(Float32Array);
        expect(result.dims).toBe(768);
      }
    });

    it('should return empty array for empty input', async () => {
      const results = await provider.embedBatch([]);
      expect(results).toHaveLength(0);
    });

    it('should throw if not initialized', async () => {
      const freshProvider = new BrowserONNXProvider();
      await expect(freshProvider.embedBatch(['test'])).rejects.toThrow(
        'Provider not initialized'
      );
    });
  });

  describe('close', () => {
    it('should release resources', async () => {
      provider = new BrowserONNXProvider({ dtype: 'q8', backend: 'wasm' });
      await provider.initialize();
      expect(provider.isReady()).toBe(true);
      await provider.close();
      expect(provider.isReady()).toBe(false);
    });
  });
});

describe('helper functions', () => {
  it('createEmbeddingProvider should return a provider instance', () => {
    const provider = createEmbeddingProvider({ dtype: 'q4' });
    expect(provider).toBeInstanceOf(BrowserONNXProvider);
    expect(provider.dimensions()).toBe(768);
  });

  it('hasWebGpuSupport should check navigator.gpu', () => {
    // In test environment (jsdom), navigator.gpu doesn't exist
    expect(hasWebGpuSupport()).toBe(false);
  });

  it('hasWasmSimdSupport should check WebAssembly', () => {
    // In test environment, WebAssembly may or may not be available
    // Just verify the function doesn't throw
    const result = hasWasmSimdSupport();
    expect(typeof result).toBe('boolean');
  });

  it('MODEL_SIZES should have expected keys', () => {
    expect(MODEL_SIZES).toHaveProperty('fp32');
    expect(MODEL_SIZES).toHaveProperty('q8');
    expect(MODEL_SIZES).toHaveProperty('q4');
    expect(MODEL_SIZES.fp32.total).toContain('600MB');
    expect(MODEL_SIZES.q4.total).toContain('80MB');
  });
});
