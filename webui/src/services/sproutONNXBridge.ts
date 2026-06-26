/**
 * Host-side adapter for the WASM ONNX bridge.
 *
 * The Go-WASM build's `pkg/embedding/onnx_wasm_stub.go` looks at
 * `globalThis.__sproutONNX` when constructing its `ONNXEmbeddingProvider`.
 * If the global is set, the WASM side delegates inference to it via
 * `syscall/js`; if absent, the WASM side returns `errWASMNotSupported`
 * and the embedding manager falls back to the static provider.
 *
 * This module wraps the existing `BrowserONNXProvider` from
 * `./onnxEmbeddingProvider` in the exact shape the WASM stub expects, and
 * exposes a one-line `installSproutONNXBridge()` helper so host pages can
 * enable ONNX-quality embeddings in the WASM build without re-implementing
 * the contract.
 *
 * Contract on `globalThis.__sproutONNX`:
 *
 *   embed(text: string): Promise<Float32Array>
 *   embedBatch(texts: string[]): Promise<Float32Array[]>
 *   modelHash:  string   (optional; defaults to "browser-bridge")
 *   modelName:  string   (optional; defaults to onnx-embeddinggemma-300m-web-bridge)
 *   dimensions: number   (optional; the WASM side will override its dims with this)
 *
 * See pkg/embedding/onnx_wasm_stub.go for the consumer side and
 * docs/WASM_API.md for the full contract.
 */

import { BrowserONNXProvider, type EmbeddingOptions } from './onnxEmbeddingProvider';

export interface SproutONNXBridge {
  embed(text: string): Promise<Float32Array>;
  embedBatch(texts: string[]): Promise<Float32Array[]>;
  modelHash: string;
  modelName: string;
  dimensions: number;
}

/**
 * Wrap a BrowserONNXProvider in the __sproutONNX shape. The provider must
 * already be initialized (or initialize() must succeed before the first
 * call from the WASM side — initialize is lazy so concurrent first-calls
 * will all wait on the same in-flight load).
 */
export function bridgeBrowserProvider(provider: BrowserONNXProvider): SproutONNXBridge {
  const ensureReady = async () => {
    if (!provider.isReady()) {
      await provider.initialize();
    }
  };
  return {
    async embed(text: string): Promise<Float32Array> {
      await ensureReady();
      const result = await provider.embed(text);
      return result.embedding;
    },
    async embedBatch(texts: string[]): Promise<Float32Array[]> {
      await ensureReady();
      const results = await provider.embedBatch(texts);
      return results.map((r) => r.embedding);
    },
    // BrowserONNXProvider doesn't expose a stable modelHash today; this
    // constant is enough for the JSONL store to spot model changes inside
    // a single browser session. If we add a real per-weights hash later,
    // surface it here so the WASM-side index can re-key its records.
    modelHash: 'browser-bridge:embeddinggemma-300m',
    modelName: 'onnx-embeddinggemma-300m-web-bridge',
    dimensions: provider.dimensions(),
  };
}

/**
 * One-call enabler: spin up a BrowserONNXProvider with the given options,
 * wrap it in the bridge shape, and install it on `globalThis.__sproutONNX`
 * so subsequent calls into Go-WASM use it automatically.
 *
 * Returns the underlying BrowserONNXProvider so the host page can keep a
 * handle (e.g. to call `.close()` on teardown).
 *
 * Idempotent — calling twice replaces the existing bridge cleanly.
 */
export function installSproutONNXBridge(options?: Partial<EmbeddingOptions>): BrowserONNXProvider {
  const provider = new BrowserONNXProvider(options);
  const bridge = bridgeBrowserProvider(provider);
  (globalThis as { __sproutONNX?: SproutONNXBridge }).__sproutONNX = bridge;
  return provider;
}

/**
 * Remove the bridge from globalThis. The underlying provider is NOT closed
 * by this — callers manage that themselves (so they can re-install without
 * paying the re-download cost).
 */
export function uninstallSproutONNXBridge(): void {
  delete (globalThis as { __sproutONNX?: SproutONNXBridge }).__sproutONNX;
}
