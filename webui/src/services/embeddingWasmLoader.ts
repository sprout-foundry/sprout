/**
 * embeddingWasmLoader.ts — Lazy-loads the embedding.wasm module.
 *
 * The shell-only sprout.wasm loads immediately on page load (fast, small).
 * The embedding.wasm module (which includes pkg/embedding + pkg/agent for
 * semantic search and memory features) is only fetched and instantiated
 * when the user actually triggers a search or opens the memory panel.
 *
 * Architecture:
 *   - sprout.wasm exposes: SproutWasm (shell commands, file CRUD, etc.)
 *   - embedding.wasm exposes: SproutEmbedWasm (semantic search, memory CRUD)
 *
 * Both modules share the same wasm_exec.js runtime. The embedding module
 * gets its own MEMFS, but file content is passed in via JS calls from the
 * host page when building the semantic index.
 */

// ── Types ────────────────────────────────────────────────────────────────────

export interface SemanticSearchResult {
  id: string;
  file: string;
  name: string;
  type: string;
  signature: string;
  startLine: number;
  endLine: number;
  similarity: number;
}

export interface SemanticStatus {
  initialized: boolean;
  building: boolean;
  indexSize: number;
}

export interface MemoryEntry {
  name: string;
  path: string;
  content: string;
}

export interface MemorySearchResult {
  name: string;
  similarity: number;
  preview: string;
}

export interface SproutEmbedWasmAPI {
  buildSemanticIndex(): Promise<{
    filesProcessed: number;
    unitsExtracted: number;
    unitsEmbedded: number;
    durationMs: number;
  }>;
  getSemanticStatus(): Promise<SemanticStatus>;
  searchSemantic(
    query: string,
    topK?: number,
    threshold?: number,
  ): Promise<SemanticSearchResult[]>;
  updateSemanticFile(filePath: string): Promise<{ ok: boolean }>;
  listMemories(): Promise<MemoryEntry[]>;
  readMemory(name: string): Promise<{ name: string; content: string }>;
  saveMemory(name: string, content: string): Promise<{ ok: boolean; name: string }>;
  deleteMemory(name: string): Promise<{ ok: boolean }>;
  searchMemories(
    query: string,
    topK?: number,
    threshold?: number,
  ): Promise<MemorySearchResult[]>;
}

declare global {
  interface Window {
    SproutEmbedWasm?: SproutEmbedWasmAPI;
  }
}

// ── Lazy loader ─────────────────────────────────────────────────────────────

const DEFAULT_EMBEDDING_WASM_URL = '/wasm/embedding.wasm';

let embeddingPromise: Promise<SproutEmbedWasmAPI> | null = null;

/**
 * Lazy-load the embedding WASM module.
 *
 * Returns a cached promise — once the module is loaded, all subsequent
 * calls reuse the same instance.
 *
 * @param opts.wasmUrl - Override the WASM URL (default: '/wasm/embedding.wasm')
 */
export async function loadEmbeddingWasm(opts?: {
  wasmUrl?: string;
}): Promise<SproutEmbedWasmAPI> {
  if (embeddingPromise) {
    return embeddingPromise;
  }

  // If the embedding module was already loaded (e.g., by a previous page
  // navigation in the same SPA), use it directly.
  if (window.SproutEmbedWasm) {
    return window.SproutEmbedWasm;
  }

  embeddingPromise = (async () => {
    const wasmUrl = opts?.wasmUrl ?? DEFAULT_EMBEDDING_WASM_URL;

    // wasm_exec.js should already be loaded by the shell module.
    // The Go constructor is on window.Go.
    if (!window.Go) {
      throw new Error(
        'wasm_exec.js not loaded — the shell WASM must be initialized first',
      );
    }

    const go = new window.Go();
    const response = await fetch(wasmUrl);
    if (!response.ok) {
      throw new Error(`Failed to fetch embedding.wasm: ${response.status}`);
    }

    const buffer = await response.arrayBuffer();
    const { instance } = await WebAssembly.instantiate(
      buffer,
      go.importObject,
    );

    // Run the Go instance (blocks until main() hits channel wait).
    go.run(instance);

    const api = window.SproutEmbedWasm;
    if (!api) {
      throw new Error('SproutEmbedWasm global not found after embedding WASM init');
    }

    return api;
  })();

  return embeddingPromise;
}

/**
 * Check whether the embedding module has been loaded.
 */
export function isEmbeddingWasmLoaded(): boolean {
  return window.SproutEmbedWasm != null;
}
