/**
 * embeddingBackendController.ts — Host-side handler for the
 * SproutWasm.switchEmbeddingBackend WASM call.
 *
 * The Go-WASM shell (cmd/wasm/embedding_funcs.go, SP-100 Phase 1) exposes
 * `SproutWasm.switchEmbeddingBackend(name)`. When invoked, that Go function
 * calls `globalThis.__sproutSwitchEmbeddingBackend` if the host page has
 * installed one. This module installs that helper.
 *
 * Two valid backends:
 *   - "static"   — no ONNX bridge. Removes globalThis.__sproutONNX.
 *   - "onnx-web" — installs the bridge via sproutONNXBridge.installSproutONNXBridge.
 *
 * Idempotent: switching to the active backend is a no-op (the existing
 * bridge is left in place; no reload).
 *
 * Side effect: also installs globalThis.__sproutEmbeddingModel, which the
 * WASM shell's embeddingModelFunc reads for the active model name.
 */

import type { BrowserONNXProvider, EmbeddingOptions } from './onnxEmbeddingProvider';
import { installSproutONNXBridge, uninstallSproutONNXBridge } from './sproutONNXBridge';
import type { SproutONNXBridge } from './sproutONNXBridge';

export type EmbeddingBackend = 'static' | 'onnx-web';

/**
 * Augment the SproutWasmAPI interface with the SP-100 embedding backend
 * surface. See cmd/wasm/embedding_funcs.go for the Go side.
 *
 * We extend the canonical SproutWasmAPI rather than redeclare window.SproutWasm
 * — the existing declaration covers ~30 entries (init, executeCommand,
 * extractSymbols, runAgent, etc.) and we'd lose them all by shadowing it.
 */
declare module './wasmShell' {
  interface SproutWasmAPI {
    embeddingModel?(): string;
    switchEmbeddingBackend?(name: 'static' | 'onnx-web'): string;
    embeddingBackendStatus?(): {
      backend: 'static' | 'onnx-web';
      model: string;
      dimensions: number;
      ready: boolean;
    };
  }
}

/**
 * Active backend tracker. Mutated by setActiveBackend / read by
 * currentBackend. Keeps a handle on the underlying BrowserONNXProvider
 * so we can .close() it on a clean uninstall — without losing the
 * download cache for re-install.
 */
let activeProvider: BrowserONNXProvider | null = null;
let activeBackend: EmbeddingBackend = 'static';
const EMBEDDING_MODEL_DEFAULT = 'gemma-300m';

/**
 * Read the active backend name. Defaults to "static" (no bridge installed).
 */
export function currentBackend(): EmbeddingBackend {
  return activeBackend;
}

/**
 * Switch to the named backend.
 *
 *  - "static"   — uninstall the __sproutONNX bridge; keep activeProvider
 *                 alive so re-install doesn't re-download the weights.
 *  - "onnx-web" — install the bridge (lazy-loads onnxruntime-web if not
 *                 already present).
 *
 * Returns the active backend name. Throws on unknown backend name so the
 * WASM shell sees a JS Error and surfaces it to the caller.
 */
export function switchEmbeddingBackend(name: EmbeddingBackend): EmbeddingBackend {
  if (name !== 'static' && name !== 'onnx-web') {
    throw new Error(
      `switchEmbeddingBackend: unknown backend ${JSON.stringify(name)} (expected "static" or "onnx-web")`,
    );
  }
  if (name === activeBackend) {
    return activeBackend;
  }
  if (name === 'static') {
    uninstallSproutONNXBridge();
    activeBackend = 'static';
    return activeBackend;
  }
  // name === 'onnx-web'
  const opts: Partial<EmbeddingOptions> = { dtype: 'q8', backend: 'wasm' };
  activeProvider = installSproutONNXBridge(opts);
  activeBackend = 'onnx-web';
  return activeBackend;
}

/**
 * Tear down any active ONNX provider and remove both globalThis helpers.
 * Intended for testing and explicit host-page teardown (e.g. logout).
 */
export function teardownEmbeddingBackend(): void {
  if (activeProvider) {
    void activeProvider.close();
    activeProvider = null;
  }
  uninstallSproutONNXBridge();
  delete (globalThis as { __sproutONNX?: SproutONNXBridge }).__sproutONNX;
  delete (globalThis as { __sproutSwitchEmbeddingBackend?: unknown }).__sproutSwitchEmbeddingBackend;
  delete (globalThis as { __sproutEmbeddingModel?: unknown }).__sproutEmbeddingModel;
  activeBackend = 'static';
}

/**
 * Install the globalThis.__sproutSwitchEmbeddingBackend helper that the
 * WASM shell's switchEmbeddingBackendFunc calls. Idempotent — re-installing
 * replaces the previous helper cleanly.
 *
 * Also installs globalThis.__sproutEmbeddingModel with the active model
 * name so SproutWasm.embeddingModel() returns the right string from Go.
 */
export function installEmbeddingBackendController(): void {
  (globalThis as { __sproutSwitchEmbeddingBackend?: (n: string) => string }).__sproutSwitchEmbeddingBackend = (
    name: string,
  ) => switchEmbeddingBackend(name as EmbeddingBackend);
  (globalThis as { __sproutEmbeddingModel?: string }).__sproutEmbeddingModel = EMBEDDING_MODEL_DEFAULT;
}

/**
 * Read the active bridge state. Mirrors the SproutWasm.embeddingBackendStatus
 * contract — always returns an object with backend/model/dimensions/ready.
 */
export function embeddingBackendStatus(): {
  backend: EmbeddingBackend;
  model: string;
  dimensions: number;
  ready: boolean;
} {
  const bridge = (globalThis as { __sproutONNX?: SproutONNXBridge }).__sproutONNX;
  if (!bridge) {
    return {
      backend: 'static',
      model: EMBEDDING_MODEL_DEFAULT,
      dimensions: 0,
      ready: false,
    };
  }
  return {
    backend: 'onnx-web',
    model: bridge.modelName,
    dimensions: bridge.dimensions,
    ready: true,
  };
}
