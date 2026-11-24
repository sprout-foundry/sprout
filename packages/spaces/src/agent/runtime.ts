/**
 * Loading Sprout's WASM module for the headless agent.
 *
 * One Go runtime runs per page: the module registers a single `SproutWasm`
 * global, and its filesystem, configuration and tool registry are shared by
 * every agent on the page. The first load decides the file store; agents
 * created later reuse the same runtime.
 */

import type { AgentFileStore, SproutWasmBridge, StoredFile } from './types';

/** Name of the content-hashed asset manifest next to the WASM assets. */
export const WASM_MANIFEST_FILE = 'wasm-manifest.json';

interface WasmManifest {
  files?: Record<string, string>;
  wasm?: string;
  wasmExec?: string;
}

interface GoInstance {
  run(instance: WebAssembly.Instance): Promise<void> | void;
  importObject: WebAssembly.Imports;
  env?: Record<string, string>;
}

type GoConstructor = new () => GoInstance;

interface RuntimeGlobals {
  Go?: GoConstructor;
  SproutWasm?: SproutWasmBridge;
  __sproutStore?: {
    saveFile(path: string, content: string): void;
    loadFile(path: string): string | null;
    deleteFile(path: string): void;
    listFiles(): string;
  };
}

const globals = globalThis as unknown as RuntimeGlobals;

let loading: Promise<SproutWasmBridge> | null = null;

/** Options of `loadSproutWasm`. */
export interface LoadSproutWasmOptions {
  /** URL of the directory serving the WASM assets and their manifest. */
  wasmBase: string;
  /** Persistence for the agent's files; memory only when absent. */
  store?: AgentFileStore;
  /** fetch to use (defaults to the global one). */
  fetch?: typeof fetch;
  /**
   * Let the module fetch Sprout's published model and provider catalogs.
   * Off by default: the headless agent's model is the host's endpoint, and
   * the catalogs only describe other providers.
   */
  remoteRegistries?: boolean;
  /** Extra environment for the Go program. */
  env?: Record<string, string>;
}

/**
 * The module's environment: the registries' documented off switch
 * (`SPROUT_MODEL_REGISTRY_URL` / `SPROUT_PROVIDER_REGISTRY_URL`) unless the
 * host wants them, then the host's own variables.
 */
export function runtimeEnv(options: Pick<LoadSproutWasmOptions, 'remoteRegistries' | 'env'>): Record<string, string> {
  const env: Record<string, string> = {};
  if (!options.remoteRegistries) {
    env.SPROUT_MODEL_REGISTRY_URL = 'off';
    env.SPROUT_PROVIDER_REGISTRY_URL = 'off';
  }
  return { ...env, ...options.env };
}

/** The asset URLs for a base, from its manifest when there is one. */
export function resolveWasmAssetUrls(
  base: string,
  manifest: WasmManifest | null,
): { wasmUrl: string; wasmExecUrl: string } {
  const root = base.replace(/\/+$/, '');
  const wasm = manifest?.wasm || manifest?.files?.['sprout.wasm'] || 'sprout.wasm';
  const exec = manifest?.wasmExec || manifest?.files?.['wasm_exec.js'] || 'wasm_exec.js';
  return { wasmUrl: `${root}/${wasm}`, wasmExecUrl: `${root}/${exec}` };
}

async function loadManifest(base: string, fetchImpl: typeof fetch): Promise<WasmManifest | null> {
  try {
    const res = await fetchImpl(`${base.replace(/\/+$/, '')}/${WASM_MANIFEST_FILE}`);
    if (!res.ok) return null;
    const parsed = (await res.json()) as unknown;
    return parsed && typeof parsed === 'object' ? (parsed as WasmManifest) : null;
  } catch {
    return null;
  }
}

/** Load Go's `wasm_exec.js` runtime support with a script tag, once. */
async function ensureGoRuntime(url: string): Promise<GoConstructor> {
  if (typeof globals.Go === 'function') return globals.Go;
  if (typeof document === 'undefined') {
    throw new Error(
      'Sprout agent: no Go runtime (globalThis.Go). Outside a browser, evaluate wasm_exec.js before creating an agent.',
    );
  }
  await new Promise<void>((resolve, reject) => {
    const script = document.createElement('script');
    script.src = url;
    script.onload = () => resolve();
    script.onerror = () => reject(new Error(`Sprout agent: failed to load ${url}`));
    document.head.appendChild(script);
  });
  if (typeof globals.Go !== 'function') throw new Error(`Sprout agent: ${url} did not define Go`);
  return globals.Go;
}

/**
 * Install the file store the Go side reads at init and mirrors writes to.
 * Restored files are served once, from memory, because Go reads them
 * synchronously.
 */
function installStore(store: AgentFileStore | undefined, restored: StoredFile[]): void {
  globals.__sproutStore = {
    saveFile: (path, content) => store?.save(path, content),
    loadFile: () => null,
    deleteFile: (path) => store?.delete(path),
    listFiles: () => JSON.stringify(restored.map((f) => ({ path: f.path, content: f.content, modTime: 0 }))),
  };
}

async function instantiate(
  wasmUrl: string,
  go: GoInstance,
  fetchImpl: typeof fetch,
): Promise<WebAssembly.Instance> {
  const res = await fetchImpl(wasmUrl);
  if (!res.ok) throw new Error(`Sprout agent: fetching ${wasmUrl} failed (${res.status})`);
  const type = (res.headers.get('content-type') ?? '').toLowerCase();
  if (type && !type.includes('application/wasm') && !type.includes('octet-stream')) {
    throw new Error(`Sprout agent: ${wasmUrl} is served as ${type}, not application/wasm — check the asset route`);
  }
  if (type.includes('application/wasm') && typeof WebAssembly.instantiateStreaming === 'function') {
    const { instance } = await WebAssembly.instantiateStreaming(res, go.importObject);
    return instance;
  }
  const { instance } = await WebAssembly.instantiate(await res.arrayBuffer(), go.importObject);
  return instance;
}

/**
 * Load (once per page) and start Sprout's WASM module. A module already
 * running on the page — loaded by the workspace views, or by an earlier
 * call — is reused as is.
 */
export function loadSproutWasm(options: LoadSproutWasmOptions): Promise<SproutWasmBridge> {
  if (globals.SproutWasm && !loading) return Promise.resolve(globals.SproutWasm);
  if (loading) return loading;
  const fetchImpl = options.fetch ?? fetch;
  loading = (async () => {
    const restored = options.store ? await options.store.load() : [];
    installStore(options.store, restored);
    const manifest = await loadManifest(options.wasmBase, fetchImpl);
    const { wasmUrl, wasmExecUrl } = resolveWasmAssetUrls(options.wasmBase, manifest);
    const Go = await ensureGoRuntime(wasmExecUrl);
    const go = new Go();
    go.env = { ...go.env, ...runtimeEnv(options) };
    const instance = await instantiate(wasmUrl, go, fetchImpl);
    // run() resolves only when the Go program exits; main() blocks forever.
    void go.run(instance);
    const wasm = globals.SproutWasm;
    if (!wasm) throw new Error('Sprout agent: the WASM module did not register SproutWasm');
    const initError = wasm.init({});
    if (initError) throw new Error(`Sprout agent: init failed: ${initError}`);
    return wasm;
  })();
  loading.catch(() => {
    loading = null;
  });
  return loading;
}

/** Forget the loaded runtime (tests). The Go program itself keeps running. */
export function resetSproutWasmLoader(): void {
  loading = null;
}
