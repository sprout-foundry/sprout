/**
 * Tests for embeddingBackendController — the host-side handler for
 * SproutWasm.switchEmbeddingBackend.
 *
 * Mirrors the sproutONNXBridge.test.ts mock pattern: vi.mock('onnxruntime-web')
 * with a synthetic InferenceSession so BrowserONNXProvider.embed() returns
 * a deterministic Float32Array.
 */

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  installEmbeddingBackendController,
  switchEmbeddingBackend,
  embeddingBackendStatus,
  currentBackend,
  teardownEmbeddingBackend,
} from './embeddingBackendController';

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

beforeEach(() => {
  global.fetch = vi.fn(async (url: string) => {
    if (url.includes('tokenizer.json')) {
      return { ok: true, json: async () => SYNTHETIC };
    }
    return { ok: true, arrayBuffer: async () => new ArrayBuffer(0) };
  }) as unknown as typeof fetch;
  installEmbeddingBackendController();
});

afterEach(() => {
  teardownEmbeddingBackend();
  vi.restoreAllMocks();
  delete (globalThis as { __sproutSwitchEmbeddingBackend?: unknown }).__sproutSwitchEmbeddingBackend;
  delete (globalThis as { __sproutEmbeddingModel?: unknown }).__sproutEmbeddingModel;
  delete (globalThis as { __sproutONNX?: unknown }).__sproutONNX;
});

describe('installEmbeddingBackendController', () => {
  it('installs globalThis.__sproutSwitchEmbeddingBackend as a function', () => {
    const fn = (globalThis as { __sproutSwitchEmbeddingBackend?: (n: string) => string })
      .__sproutSwitchEmbeddingBackend;
    expect(typeof fn).toBe('function');
  });

  it('installs globalThis.__sproutEmbeddingModel with the default model name', () => {
    const model = (globalThis as { __sproutEmbeddingModel?: string }).__sproutEmbeddingModel;
    expect(model).toBe('gemma-300m');
  });
});

describe('switchEmbeddingBackend', () => {
  it('starts in static mode with no bridge installed', () => {
    expect(currentBackend()).toBe('static');
    expect((globalThis as { __sproutONNX?: unknown }).__sproutONNX).toBeUndefined();
  });

  it('switching to onnx-web installs the __sproutONNX bridge', async () => {
    const result = await Promise.resolve(switchEmbeddingBackend('onnx-web'));
    expect(result).toBe('onnx-web');
    expect(currentBackend()).toBe('onnx-web');
    expect((globalThis as { __sproutONNX?: unknown }).__sproutONNX).toBeDefined();
  });

  it('switching back to static removes the bridge', async () => {
    await Promise.resolve(switchEmbeddingBackend('onnx-web'));
    expect((globalThis as { __sproutONNX?: unknown }).__sproutONNX).toBeDefined();
    await Promise.resolve(switchEmbeddingBackend('static'));
    expect(currentBackend()).toBe('static');
    expect((globalThis as { __sproutONNX?: unknown }).__sproutONNX).toBeUndefined();
  });

  it('is idempotent: switching to the active backend is a no-op', async () => {
    await Promise.resolve(switchEmbeddingBackend('static'));
    const before = currentBackend();
    const after = await Promise.resolve(switchEmbeddingBackend('static'));
    expect(after).toBe(before);
    expect(after).toBe('static');
  });

  it('rejects unknown backend names with an Error', () => {
    expect(() => switchEmbeddingBackend('pytorch' as 'static')).toThrow(/unknown backend/);
  });
});

describe('embeddingBackendStatus', () => {
  it('reports static backend when no bridge is installed', () => {
    const status = embeddingBackendStatus();
    expect(status.backend).toBe('static');
    expect(status.model).toBe('gemma-300m');
    expect(status.dimensions).toBe(0);
    expect(status.ready).toBe(false);
  });

  it('reports onnx-web backend with bridge details when installed', async () => {
    await Promise.resolve(switchEmbeddingBackend('onnx-web'));
    const status = embeddingBackendStatus();
    expect(status.backend).toBe('onnx-web');
    expect(status.model).toContain('embeddinggemma-300m');
    expect(status.dimensions).toBe(768);
    expect(status.ready).toBe(true);
  });
});

describe('WASM-callable helper', () => {
  it('globalThis.__sproutSwitchEmbeddingBackend("static") returns "static"', async () => {
    const fn = (globalThis as { __sproutSwitchEmbeddingBackend?: (n: string) => string | Promise<string> })
      .__sproutSwitchEmbeddingBackend!;
    const result = await Promise.resolve(fn('static'));
    expect(result).toBe('static');
  });

  it('globalThis.__sproutSwitchEmbeddingBackend("onnx-web") returns "onnx-web"', async () => {
    const fn = (globalThis as { __sproutSwitchEmbeddingBackend?: (n: string) => string | Promise<string> })
      .__sproutSwitchEmbeddingBackend!;
    const result = await Promise.resolve(fn('onnx-web'));
    expect(result).toBe('onnx-web');
    expect((globalThis as { __sproutONNX?: unknown }).__sproutONNX).toBeDefined();
  });
});