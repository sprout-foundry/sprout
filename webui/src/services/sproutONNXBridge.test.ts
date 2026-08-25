/**
 * Tests for the host-side __sproutONNX bridge adapter.
 *
 * These verify the JS-side shape and lifecycle; the cross-language
 * round-trip (Go-WASM calling into this bridge) is covered by the Go
 * tests in pkg/embedding/onnx_wasm_bridge_test.go.
 */

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { BrowserONNXProvider } from './onnxEmbeddingProvider';
import {
  bridgeBrowserProvider,
  installSproutONNXBridge,
  uninstallSproutONNXBridge,
  type SproutONNXBridge,
} from './sproutONNXBridge';

// Reuse the same onnxruntime-web mock from onnxEmbeddingProvider.test.ts.
// We don't share the file, but we replicate just enough of the mock here
// to make BrowserONNXProvider.embed return a deterministic Float32Array.
vi.mock('onnxruntime-web', () => {
  const HIDDEN = 768;
  const mockSession = {
    inputNames: ['input_ids', 'attention_mask'],
    outputNames: ['sentence_embedding'],
    run: vi.fn(async (feeds: Record<string, { dims?: number[] }>) => {
      const batchSize = feeds['input_ids']?.dims?.[0] ?? 1;
      const pooled = new Float32Array(batchSize * HIDDEN);
      for (let i = 0; i < pooled.length; i++) pooled[i] = 0.001 * (i + 1);
      return {
        sentence_embedding: {
          dims: [batchSize, HIDDEN],
          getData: vi.fn(async () => pooled),
        },
      };
    }),
    release: vi.fn(async () => {}),
  };
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
    InferenceSession: { create: vi.fn(async () => mockSession) },
    Tensor: MockTensor,
  };
});

// Mock fetch for the tokenizer download — return a minimal synthetic
// tokenizer.json that GemmaBpeTokenizer can parse.
const SYNTHETIC = {
  model: {
    type: 'BPE',
    vocab: { '<pad>': 0, '<eos>': 1, '<bos>': 2, '<unk>': 3, h: 4, i: 5 },
    merges: [['h', 'i']],
  },
  added_tokens: [
    { id: 0, content: '<pad>', special: true },
    { id: 1, content: '<eos>', special: true },
    { id: 2, content: '<bos>', special: true },
    { id: 3, content: '<unk>', special: true },
  ],
};

describe('sproutONNXBridge', () => {
  beforeEach(() => {
    global.fetch = vi.fn(async (url: string) => {
      if (url.includes('tokenizer.json')) {
        return { ok: true, json: async () => SYNTHETIC };
      }
      return { ok: true, arrayBuffer: async () => new ArrayBuffer(0) };
    }) as unknown as typeof fetch;
  });

  afterEach(() => {
    vi.restoreAllMocks();
    uninstallSproutONNXBridge();
  });

  it('bridgeBrowserProvider exposes the contract surface', async () => {
    const provider = new BrowserONNXProvider({ dtype: 'q8', backend: 'wasm' });
    const bridge = bridgeBrowserProvider(provider);
    expect(typeof bridge.embed).toBe('function');
    expect(typeof bridge.embedBatch).toBe('function');
    expect(bridge.dimensions).toBe(768);
    expect(bridge.modelHash).toContain('embeddinggemma-300m');
    expect(bridge.modelName).toContain('embeddinggemma-300m');
  });

  it('bridge.embed returns a Float32Array of model.dimensions length', async () => {
    const provider = new BrowserONNXProvider({ dtype: 'q8', backend: 'wasm' });
    const bridge = bridgeBrowserProvider(provider);
    const vec = await bridge.embed('hi');
    expect(vec).toBeInstanceOf(Float32Array);
    expect(vec.length).toBe(768);
  });

  it('bridge.embedBatch returns one Float32Array per input, in order', async () => {
    const provider = new BrowserONNXProvider({ dtype: 'q8', backend: 'wasm' });
    const bridge = bridgeBrowserProvider(provider);
    const results = await bridge.embedBatch(['hi', 'hi', 'hi']);
    expect(results).toHaveLength(3);
    for (const r of results) {
      expect(r).toBeInstanceOf(Float32Array);
      expect(r.length).toBe(768);
    }
  });

  it('installSproutONNXBridge sets globalThis.__sproutONNX', () => {
    const provider = installSproutONNXBridge({ dtype: 'q8', backend: 'wasm' });
    expect(provider).toBeInstanceOf(BrowserONNXProvider);
    const installed = (globalThis as { __sproutONNX?: SproutONNXBridge }).__sproutONNX;
    expect(installed).toBeDefined();
    expect(typeof installed!.embed).toBe('function');
    expect(typeof installed!.embedBatch).toBe('function');
  });

  it('uninstallSproutONNXBridge removes the global', () => {
    installSproutONNXBridge({ dtype: 'q8', backend: 'wasm' });
    expect((globalThis as { __sproutONNX?: SproutONNXBridge }).__sproutONNX).toBeDefined();
    uninstallSproutONNXBridge();
    expect((globalThis as { __sproutONNX?: SproutONNXBridge }).__sproutONNX).toBeUndefined();
  });

  it('installing twice replaces the previous bridge', () => {
    installSproutONNXBridge({ dtype: 'q8', backend: 'wasm' });
    const first = (globalThis as { __sproutONNX?: SproutONNXBridge }).__sproutONNX;
    installSproutONNXBridge({ dtype: 'q4', backend: 'wasm' });
    const second = (globalThis as { __sproutONNX?: SproutONNXBridge }).__sproutONNX;
    expect(second).toBeDefined();
    expect(second).not.toBe(first);
  });
});
