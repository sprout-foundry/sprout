/**
 * wasmShell.ts — Loads and interfaces with the sprout Go→WASM shell module.
 *
 * Usage:
 *   const shell = await initWasmShell();
 *   const result = await shell.executeCommand('ls -la');
 *   console.log(result.stdout);
 */

import { safeJsonParse, safeJsonParseOrNull } from '../utils/json';
import { resolveActiveWasmBase } from '../contexts/WasmAssetsContext';

// ── Types ────────────────────────────────────────────────────────────────────

export interface WasmShellResult {
  stdout: string;
  stderr: string;
  exitCode: number;
  /** stdout and stderr in the order they were written, for display. */
  output?: Array<{ err?: boolean; text: string }>;
}

export interface WasmCompletionResult {
  completions: string[];
}

export interface WasmDirEntry {
  name: string;
  type: 'file' | 'dir';
  size: number;
  mode: number;
}

export interface WasmListDirResult {
  entries: WasmDirEntry[];
  error?: string;
}

export interface WasmReadFileResult {
  content: string;
  error?: string;
}

export interface WasmReadFileBytesResult {
  bytes?: Uint8Array;
  error?: string;
}

export interface WasmSaveImageResult {
  path?: string;
  filename?: string;
  error?: string;
}

export interface WasmChangeDirResult {
  cwd: string;
  error?: string;
}

export interface SproutStore {
  saveFile(path: string, content: string): void;
  loadFile(path: string): string | null;
  deleteFile(path: string): void;
  listFiles(): string; // JSON-encoded {path, content, modTime}[]
}

export interface WasmShell {
  /** Execute a shell command string. */
  executeCommand(input: string): WasmShellResult;
  /**
   * Execute off the JS event loop. Required for commands backed by JS
   * Promises (git): the synchronous call deadlocks on them.
   */
  executeCommandAsync?(input: string): Promise<WasmShellResult>;
  /** Tab-complete a partial command. */
  autoComplete(input: string): WasmCompletionResult;
  /** Get the current working directory (the terminal's; `cd` moves it). */
  getCwd(): string;
  /** The project directory: relative paths given to the shell resolve here. */
  getWorkspaceRoot(): string;
  /** Make a directory the workspace. */
  changeDir(dir: string): WasmChangeDirResult;
  /** Write content to a file (synced to IndexedDB). */
  writeFile(path: string, content: string): string; // error or ""
  /** Read a file's content. */
  readFile(path: string): WasmReadFileResult;
  /** Read a file byte-exact (images, fonts). */
  readFileBytes(path: string): WasmReadFileBytesResult;
  /** Store an uploaded image the way the daemon's /api/upload/image does. */
  saveImage(bytes: Uint8Array): WasmSaveImageResult;
  /** List directory entries. */
  listDir(path: string): WasmListDirResult;
  /** Delete a file. */
  deleteFile(path: string): string; // error or ""
  /** Run the full agent loop (ProcessQuery) in-browser.
   *  Returns the agent's response and dispatches events via the callback. */
  runAgent(
    provider: string,
    model: string,
    query: string,
    onEvent?: (eventJson: string) => void,
    chatId?: string,
    /** JSON [{role, content}] seeding a chat's agent when it is created fresh. */
    history?: string,
    /** Workspace mode the query was sent from; selects the agent's mode skills. */
    mode?: string,
  ): Promise<{ response: string; provider: string; model: string }>;
  /** Clear a chat's agent history (every chat's when no id is given). */
  clearConversation(chatId?: string): void;
  /** Interrupt a chat's running agent loop (every chat's when no id is given). */
  stopAgent(chatId?: string): void;
  /** Steer a chat's running agent (the most recent chat's when no id is given). */
  steerAgent?(message: string, chatId?: string): Record<string, unknown>;
  /** Deliver a response to a pending ask_user request. */
  respondToAskUser?(requestId: string, response: string): { delivered: boolean };
  /** Deliver an edit approval decision to a pending edit approval request. */
  respondToEditDecision?(requestId: string, approved: boolean, acceptedHunks: string[]): { delivered: boolean };
  /** Deliver a shell approval decision to a pending shell approval request. */
  respondToShellApproval?(requestId: string, decisions: Record<string, boolean>): { delivered: boolean };
  /** The §6b design status JSON for the workspace root (GET /api/design/status). */
  designStatus?(): string;
  /** Build identity of the running binary (version/commit/date). Null on binaries built before the export existed. */
  getBuildInfo(): { version: string; commit: string; date: string } | null;
  /** Get the fully initialized Go global. */
  readonly wasm: typeof globalThis & { SproutWasm: unknown };
}

// ── IndexedDB store ─────────────────────────────────────────────────────────

const DB_NAME = 'sprout-wasm-fs';
const DB_VERSION = 1;
const STORE_NAME = 'files';

function openDB(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const req = indexedDB.open(DB_NAME, DB_VERSION);
    req.onupgradeneeded = () => {
      const db = req.result;
      if (!db.objectStoreNames.contains(STORE_NAME)) {
        db.createObjectStore(STORE_NAME, { keyPath: 'path' });
      }
    };
    req.onsuccess = () => resolve(req.result);
    req.onerror = () => reject(req.error);
  });
}

async function idbSaveFile(path: string, content: string): Promise<void> {
  const db = await openDB();
  return new Promise((resolve, reject) => {
    const tx = db.transaction(STORE_NAME, 'readwrite');
    const store = tx.objectStore(STORE_NAME);
    store.put({ path, content, modTime: Date.now() });
    tx.oncomplete = () => {
      db.close();
      resolve();
    };
    tx.onerror = () => {
      db.close();
      reject(tx.error);
    };
  });
}

async function _idbLoadFile(path: string): Promise<string | null> {
  const db = await openDB();
  return new Promise((resolve, reject) => {
    const tx = db.transaction(STORE_NAME, 'readonly');
    const store = tx.objectStore(STORE_NAME);
    const req = store.get(path);
    req.onsuccess = () => {
      db.close();
      resolve(req.result?.content ?? null);
    };
    req.onerror = () => {
      db.close();
      reject(req.error);
    };
  });
}

async function idbDeleteFile(path: string): Promise<void> {
  const db = await openDB();
  return new Promise((resolve, reject) => {
    const tx = db.transaction(STORE_NAME, 'readwrite');
    const store = tx.objectStore(STORE_NAME);
    store.delete(path);
    tx.oncomplete = () => {
      db.close();
      resolve();
    };
    tx.onerror = () => {
      db.close();
      reject(tx.error);
    };
  });
}

async function idbListFiles(): Promise<string> {
  const db = await openDB();
  return new Promise((resolve, reject) => {
    const tx = db.transaction(STORE_NAME, 'readonly');
    const store = tx.objectStore(STORE_NAME);
    const req = store.getAll();
    req.onsuccess = () => {
      db.close();
      resolve(JSON.stringify(req.result || []));
    };
    req.onerror = () => {
      db.close();
      reject(req.error);
    };
  });
}

// ── WASM loader ─────────────────────────────────────────────────────────────

// Resolve the wasm asset base relative to the document when the app is
// NOT mounted under the daemon's /webui prefix. The dist ships wasm
// assets at /wasm/* (vite copies public/wasm to the bundle root), so a
// root-served bundle (Sprout Studio's WKWebView/Telegraph, static
// hosts) must load /wasm/sprout.wasm, while the daemon mount serves
// them at /webui/wasm/*. Probe document location: under-mount pages
// keep the /webui prefix; everything else uses the root path.
function resolveWasmBase(): string {
  if (typeof window === 'undefined' || !window.location) return '/webui/wasm';
  // jsdom/test environments may not define pathname.
  const path = window.location.pathname ?? '/';
  const underMount =
    path === '/webui' || path.startsWith('/webui/') || (window.location.search ?? '').includes('mount=/webui');
  return underMount ? '/webui/wasm' : '/wasm';
}

/**
 * Resolve the WASM base for a load: an explicit `wasmBase` override wins,
 * otherwise the host-supplied base (a host serving the package's `dist/wasm/`
 * at its own path), otherwise the location-probing default the standalone and
 * cloud builds rely on. Keeping the default last is what leaves both shipped
 * builds unchanged.
 */
function resolveLoadWasmBase(explicitBase?: string | null): string {
  return resolveActiveWasmBase(explicitBase) ?? resolveWasmBase();
}

/** Name of the content-hashed WASM asset manifest emitted by the dist build. */
export const WASM_MANIFEST_FILE = 'wasm-manifest.json';

/** Manifest emitted alongside the WASM assets by the dist build. */
export interface WasmManifest {
  version?: number;
  /** Logical asset name (e.g. `sprout.wasm`) → content-hashed filename. */
  files?: Record<string, string>;
  /** Content-hashed sprout WASM filename. */
  wasm?: string;
  /** Content-hashed wasm_exec.js filename. */
  wasmExec?: string;
}

/**
 * Pure translation of a manifest (or nothing) into concrete loader URLs.
 *
 * When a manifest is present and names an asset, the returned URL points at
 * the content-hashed file; otherwise it falls back to the fixed name so an
 * older bundle (or a local dev server without a manifest) keeps working.
 * Exported so the fallback rules can be unit-tested without a DOM.
 */
export function resolveWasmUrls(
  base: string,
  manifest: WasmManifest | null | undefined,
): {
  wasmUrl: string;
  wasmExecUrl: string;
} {
  const wasmName = manifest?.wasm || manifest?.files?.['sprout.wasm'] || 'sprout.wasm';
  const wasmExecName = manifest?.wasmExec || manifest?.files?.['wasm_exec.js'] || 'wasm_exec.js';
  return {
    wasmUrl: `${base}/${wasmName}`,
    wasmExecUrl: `${base}/${wasmExecName}`,
  };
}

/**
 * Fetch the content-hashed WASM manifest for a base, degrading to null on
 * any failure (missing file on an older bundle, cache, network hiccup) so
 * the loader falls back to the fixed asset names.
 */
export async function loadWasmManifest(base: string, fetchImpl: typeof fetch = fetch): Promise<WasmManifest | null> {
  try {
    const response = await fetchImpl(`${base}/${WASM_MANIFEST_FILE}`);
    if (!response.ok) return null;
    const text = await response.text();
    const parsed = safeJsonParseOrNull(text);
    return parsed && typeof parsed === 'object' ? (parsed as WasmManifest) : null;
  } catch {
    return null;
  }
}

/** Debug logger — only logs when localStorage flag is set or VITE_DEBUG is enabled. */
// eslint-disable-next-line no-console
const debug = (...args: unknown[]) => {
  if (typeof localStorage !== 'undefined' && localStorage.getItem('sprout-debug-wasm')) {
    // eslint-disable-next-line no-console
    console.debug('[wasm]', ...args);
  }
};

/** Interface of the Go→WASM SproutWasm global exposed by the compiled binary. */
export interface SproutWasmAPI {
  init(config?: string): string;
  executeCommand(input: string): string;
  /** Absent in binaries built before the async export existed. */
  executeCommandAsync?(input: string): Promise<string>;
  autoComplete(input: string): string;
  getCwd(): string;
  /** Absent in binaries built before the workspace root existed. */
  getWorkspaceRoot?(): string;
  changeDir(dir: string): string;
  writeFile(path: string, content: string): string;
  readFile(path: string): string;
  readFileBytes?(path: string): WasmReadFileBytesResult;
  saveImage?(bytes: Uint8Array): WasmSaveImageResult;
  listDir(path: string): string;
  deleteFile(path: string): string;
  getHistory(): string;
  getEnv(): string;
  // ── Agent loop (cmd/wasm/agent_funcs.go) ──
  // Runs the full sprout agent loop (ProcessQuery) in-browser.
  // Returns a Promise resolving to { response, provider, model }.
  // The onEvent callback receives JSON-stringified UI events.
  runAgent?(
    provider: string,
    model: string,
    query: string,
    onEvent?: (eventJson: string) => void,
    chatId?: string,
    history?: string,
    mode?: string,
  ): Promise<{ response: string; provider: string; model: string }>;
  clearConversation?(chatId?: string): void;
  stopAgent?(chatId?: string): void;
  steerAgent?(message: string, chatId?: string): Record<string, unknown>;
  respondToAskUser?(requestId: string, response: string): { delivered: boolean };
  respondToEditDecision?(requestId: string, approved: boolean, acceptedHunks: string[]): { delivered: boolean };
  respondToShellApproval?(requestId: string, decisions: Record<string, boolean>): { delivered: boolean };
  // ── AST / symbol extraction (cmd/wasm/ast_funcs.go) ──
  parseFile?(filePath: string, content: Uint8Array | ArrayBuffer): string;
  extractSymbols?(filePath: string, content: Uint8Array | ArrayBuffer): string;
  supportedLanguages?(): string;
  // ── Design health (cmd/wasm/design_funcs.go) ──
  /** The §6b design status JSON for the workspace root. Absent in binaries built before the export existed. */
  designStatus?(root?: string): string;
  // ── Build identity (cmd/wasm/main.go getBuildInfoFunc) ──
  /** Version/commit/date of the running binary. Absent in binaries built before the export existed. */
  getBuildInfo?(): string;
}

declare global {
  interface Window {
    __sproutStore: SproutStore;
    Go: new () => {
      run(instance: WebAssembly.Instance): void;
      importObject: WebAssembly.Imports;
    };
    SproutWasm?: SproutWasmAPI;
  }
}

let sharedInstance: WasmShell | null = null;
let initPromise: Promise<WasmShell> | null = null;

/**
 * Initialize the sprout WASM shell.
 *
 * Must be called before any shell operations. Safe for concurrent calls —
 * only one initialization runs; subsequent callers receive the same promise.
 *
 * @param config.home - Override the virtual home directory (default: /home/user)
 * @returns The WasmShell interface
 */
export async function initWasmShell(config?: {
  home?: string;
  wasmUrl?: string; // default: '/webui/wasm/sprout.wasm'
  wasmExecUrl?: string; // default: '/webui/wasm/wasm_exec.js'
  /** Base URL for the WASM manifest + assets (overrides the host-provided base). */
  wasmBase?: string;
}): Promise<WasmShell> {
  debug(' initWasmShell called');
  if (sharedInstance) {
    debug(' returning existing instance');
    return sharedInstance;
  }
  if (initPromise) {
    debug(' returning existing init promise');
    return initPromise;
  }

  debug(' starting new init');

  initPromise = (async () => {
    const store: SproutStore = {
      saveFile: (path, content) => {
        idbSaveFile(path, content).catch((err) =>
          console.warn('[sprout-wasm] Failed to save file to IndexedDB:', path, err),
        );
      },
      loadFile: (_path) => {
        // Synchronous not possible with IndexedDB — the store.listFiles restores all
        // files on init instead. loadFile is provided for completeness but returns null.
        return null;
      },
      deleteFile: (path) => {
        idbDeleteFile(path).catch((err) =>
          console.warn('[sprout-wasm] Failed to delete file from IndexedDB:', path, err),
        );
      },
      listFiles: () => {
        // listFiles is called synchronously from Go init. Since IndexedDB is async,
        // we return a cached JSON string. The store updates the cache lazily.
        // For the initial load, we return empty — this is fine because the
        // JS side will call listFiles before WASM init and cache the result.
        return idbListFilesSync();
      },
    };

    // Warm up the cache by loading all files before WASM init.
    await warmIdbCache();

    window.__sproutStore = store;

    // 2. Resolve asset URLs. The dist bundle emits content-hashed WASM
    //    names plus a manifest so a host caching the bundle as immutable
    //    can never serve an old binary next to new JS. The base is the
    //    host-supplied one when a host serves the package's dist/wasm/ at
    //    its own path; otherwise the location probe (the shipped builds).
    //    Read the manifest first; when it is absent (older bundle / local
    //    dev) fall back to the fixed names. An explicit config override
    //    always wins.
    const wasmBase = resolveLoadWasmBase(config?.wasmBase);
    let { wasmUrl: resolvedWasmUrl, wasmExecUrl: resolvedWasmExecUrl } = resolveWasmUrls(wasmBase, null);
    if (!config?.wasmUrl || !config?.wasmExecUrl) {
      const manifest = await loadWasmManifest(wasmBase);
      const urls = resolveWasmUrls(wasmBase, manifest);
      resolvedWasmUrl = urls.wasmUrl;
      resolvedWasmExecUrl = urls.wasmExecUrl;
      if (manifest) {
        debug(' manifest resolved:', WASM_MANIFEST_FILE, urls);
      } else {
        debug(' no manifest — using fixed WASM asset names');
      }
    }

    // 3. Load wasm_exec.js.
    debug(' Step 1: Loading wasm_exec.js...');
    const script = document.createElement('script');
    const execUrl = config?.wasmExecUrl ?? resolvedWasmExecUrl;
    script.src = execUrl;
    document.head.appendChild(script);
    await new Promise<void>((resolve, reject) => {
      script.onload = () => {
        debug(' wasm_exec.js loaded');
        resolve();
      };
      script.onerror = () => reject(new Error(`Failed to load wasm_exec.js from ${execUrl}`));
    });

    // 4. Fetch and instantiate the WASM binary.
    debug(' Step 2: Creating Go instance...');
    const go = new window.Go();
    const wasmUrl = config?.wasmUrl ?? resolvedWasmUrl;
    debug(' Step 3: Fetching sprout.wasm from', wasmUrl);
    const wasmResponse = await fetch(wasmUrl);
    if (!wasmResponse.ok) {
      throw new Error(`Failed to fetch ${wasmUrl}: ${wasmResponse.status}`);
    }
    // Guard against a misrouted asset: a server answering the .wasm URL
    // with the SPA's index.html (or any non-wasm type) passes the ok check
    // and only dies later inside WebAssembly.instantiate with an opaque
    // "invalid magic number". Name the actual problem instead.
    const wasmType = (wasmResponse.headers.get('content-type') ?? '').toLowerCase();
    if (wasmType && !wasmType.includes('application/wasm') && !wasmType.includes('octet-stream')) {
      throw new Error(
        `${wasmUrl} answered Content-Type ${wasmType}, not application/wasm — ` +
          `the server is not serving the WASM asset (check the /wasm/ route or the asset path)`,
      );
    }

    debug(' Step 4: Instantiating (streaming when possible)...');
    // Streaming compile overlaps download and compilation — roughly halves
    // time-to-ready on the 55MB binary. It requires the response to be
    // application/wasm (browsers enforce the MIME type), so octet-stream
    // servers and missing headers fall back to the buffered path.
    //
    // The streaming attempt consumes its OWN fetch; the original
    // wasmResponse stays untouched and is the fallback body — a failed
    // stream never re-downloads 55MB.
    let compile: Promise<WebAssembly.WebAssemblyInstantiatedSource>;
    if (wasmType.includes('application/wasm') && typeof WebAssembly.instantiateStreaming === 'function') {
      debug(' Step 4a: instantiateStreaming...');
      compile = WebAssembly.instantiateStreaming(fetch(wasmUrl), go.importObject).catch((streamErr) => {
        debug(' streaming compile failed, falling back to buffered:', streamErr);
        return wasmResponse.arrayBuffer().then((buf) => WebAssembly.instantiate(buf, go.importObject));
      });
    } else {
      const wasmBuffer = await wasmResponse.arrayBuffer();
      debug(' ArrayBuffer size:', wasmBuffer.byteLength);
      compile = WebAssembly.instantiate(wasmBuffer, go.importObject);
    }
    const { instance } = await compile;
    debug(' Step 5: Instantiated');

    // 4. Run the Go instance (this blocks until main() hits the channel wait).
    debug(' Step 6: go.run(instance)...');
    go.run(instance);
    debug(' Step 6: go.run returned');

    // At this point window.SproutWasm should be defined by Go's main().
    const wasm = window.SproutWasm;
    debug(' Step 7: SproutWasm =', typeof wasm);

    if (!wasm || typeof wasm.init !== 'function') {
      throw new Error('SproutWasm global not found after WASM init');
    }

    // 5. Initialize the Go side (restores files from IndexedDB cache).
    debug(' Step 8: Calling wasm.init()...');
    const configStr = config ? JSON.stringify(config) : undefined;
    const initError = wasm.init(configStr);
    debug(' Step 8: init returned:', initError || 'ok');
    if (initError) {
      console.warn('[sprout-wasm] Init warning:', initError);
    }

    // 6. Create the shell interface.
    //
    // Every bridge call returns a JSON string produced by the Go side. If the
    // binary ever emits malformed (or non-JSON) output — version skew, a panic
    // string, a truncated frame — `safeJsonParse` degrades to a typed fallback
    // instead of throwing into the caller's line editor or render path.
    const shell: WasmShell = {
      executeCommand(input: string): WasmShellResult {
        const json = wasm.executeCommand(input);
        return safeJsonParse<WasmShellResult>(json, {
          stdout: '',
          stderr: `shell returned an unreadable response${json ? `: ${String(json).slice(0, 120)}` : ''}`,
          exitCode: 1,
        });
      },

      async executeCommandAsync(input: string): Promise<WasmShellResult> {
        if (!wasm.executeCommandAsync) return shell.executeCommand(input);
        const json = await wasm.executeCommandAsync(input);
        return safeJsonParse<WasmShellResult>(json, {
          stdout: '',
          stderr: `shell returned an unreadable response${json ? `: ${String(json).slice(0, 120)}` : ''}`,
          exitCode: 1,
        });
      },

      autoComplete(input: string): WasmCompletionResult {
        const json = wasm.autoComplete(input);
        return safeJsonParse<WasmCompletionResult>(json, { completions: [] });
      },

      getCwd(): string {
        return wasm.getCwd();
      },

      getWorkspaceRoot(): string {
        return wasm.getWorkspaceRoot ? wasm.getWorkspaceRoot() : wasm.getCwd();
      },

      changeDir(dir: string): WasmChangeDirResult {
        const json = wasm.changeDir(dir);
        return safeJsonParse<WasmChangeDirResult>(json, {
          cwd: '',
          error: 'shell returned an unreadable response',
        });
      },

      writeFile(path: string, content: string): string {
        return wasm.writeFile(path, content);
      },

      readFile(path: string): WasmReadFileResult {
        const json = wasm.readFile(path);
        return safeJsonParse<WasmReadFileResult>(json, { content: '', error: 'unreadable response' });
      },

      readFileBytes(path: string): WasmReadFileBytesResult {
        const api = wasm as SproutWasmAPI;
        if (!api.readFileBytes) return { error: 'WASM binary does not expose readFileBytes' };
        return api.readFileBytes(path);
      },

      saveImage(bytes: Uint8Array): WasmSaveImageResult {
        const api = wasm as SproutWasmAPI;
        if (!api.saveImage) return { error: 'WASM binary does not expose saveImage' };
        return api.saveImage(bytes);
      },

      listDir(path: string): WasmListDirResult {
        const json = wasm.listDir(path);
        // The WASM export returns a bare array of entries (JSON null for an
        // empty directory) on success and {"error": "..."} on failure.
        const parsed = safeJsonParse<WasmDirEntry[] | WasmListDirResult | null | undefined>(json, undefined);
        if (parsed === undefined) return { entries: [], error: json };
        if (parsed === null) return { entries: [] };
        if (Array.isArray(parsed)) return { entries: parsed };
        return { entries: parsed.entries ?? [], error: parsed.error };
      },

      deleteFile(path: string): string {
        return wasm.deleteFile(path);
      },

      getBuildInfo(): { version: string; commit: string; date: string } | null {
        if (typeof wasm.getBuildInfo !== 'function') return null;
        const json = wasm.getBuildInfo();
        return safeJsonParse<{ version: string; commit: string; date: string } | null>(json, null);
      },

      runAgent(
        provider: string,
        model: string,
        query: string,
        onEvent?: (eventJson: string) => void,
        chatId?: string,
        history?: string,
        mode?: string,
      ): Promise<{ response: string; provider: string; model: string }> {
        const api = wasm as SproutWasmAPI;
        if (!api.runAgent) {
          return Promise.reject(new Error('WASM binary does not expose runAgent'));
        }
        return api.runAgent(provider, model, query, onEvent, chatId, history, mode);
      },

      clearConversation(chatId?: string): void {
        const api = wasm as SproutWasmAPI;
        if (api.clearConversation) {
          if (chatId === undefined) api.clearConversation();
          else api.clearConversation(chatId);
        }
      },

      stopAgent(chatId?: string): void {
        const api = wasm as SproutWasmAPI;
        if (api.stopAgent) {
          if (chatId === undefined) api.stopAgent();
          else api.stopAgent(chatId);
        }
      },

      steerAgent(message: string, chatId?: string): Record<string, unknown> {
        const api = wasm as SproutWasmAPI;
        if (api.steerAgent) {
          return chatId === undefined ? api.steerAgent(message) : api.steerAgent(message, chatId);
        }
        return { steered: false, error: 'steerAgent not available' };
      },

      respondToAskUser(requestId: string, response: string): { delivered: boolean } {
        const api = wasm as SproutWasmAPI;
        if (api.respondToAskUser) {
          return api.respondToAskUser(requestId, response);
        }
        return { delivered: false };
      },

      respondToEditDecision(requestId: string, approved: boolean, acceptedHunks: string[]): { delivered: boolean } {
        const api = wasm as SproutWasmAPI;
        if (api.respondToEditDecision) {
          return api.respondToEditDecision(requestId, approved, acceptedHunks);
        }
        return { delivered: false };
      },

      respondToShellApproval(requestId: string, decisions: Record<string, boolean>): { delivered: boolean } {
        const api = wasm as SproutWasmAPI;
        if (api.respondToShellApproval) {
          return api.respondToShellApproval(requestId, decisions);
        }
        return { delivered: false };
      },

      get wasm() {
        return window as typeof globalThis & { SproutWasm: unknown };
      },
    };

    sharedInstance = shell;
    return shell;
  })();

  return initPromise;
}

// ── Synchronous cache for IndexedDB (used during WASM init) ──────────────

let fileIdbCache: string = '[]';

async function warmIdbCache(): Promise<void> {
  try {
    fileIdbCache = await idbListFiles();
  } catch (err) {
    console.warn('[sprout-wasm] Failed to warm IDB cache:', err);
    fileIdbCache = '[]';
  }
}

function idbListFilesSync(): string {
  return fileIdbCache;
}

/**
 * Reset the singleton (useful for testing / hot reload).
 */
export function resetWasmShell(): void {
  sharedInstance = null;
  initPromise = null;
}
