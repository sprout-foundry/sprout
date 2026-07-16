/**
 * wasmShell.ts — Loads and interfaces with the sprout Go→WASM shell module.
 *
 * Usage:
 *   const shell = await initWasmShell();
 *   const result = await shell.executeCommand('ls -la');
 *   console.log(result.stdout);
 */

import { installSproutONNXBridge } from './sproutONNXBridge';
import { installEmbeddingBackendController } from './embeddingBackendController';

// ── Types ────────────────────────────────────────────────────────────────────

export interface WasmShellResult {
  stdout: string;
  stderr: string;
  exitCode: number;
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
  /** Tab-complete a partial command. */
  autoComplete(input: string): WasmCompletionResult;
  /** Get the current working directory. */
  getCwd(): string;
  /** Change directory. */
  changeDir(dir: string): WasmChangeDirResult;
  /** Write content to a file (synced to IndexedDB). */
  writeFile(path: string, content: string): string; // error or ""
  /** Read a file's content. */
  readFile(path: string): WasmReadFileResult;
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
  ): Promise<{ response: string; provider: string; model: string }>;
  /** Clear the WASM agent's conversation history (start fresh chat). */
  clearConversation(): void;
  /** Interrupt the currently running agent loop. */
  stopAgent(): void;
  /** Steer the running agent (inject a follow-up message). */
  steerAgent?(message: string): Record<string, unknown>;
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

const DEFAULT_WASM_URL = '/webui/wasm/sprout.wasm';
const DEFAULT_WASM_EXEC_URL = '/webui/wasm/wasm_exec.js';

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
  autoComplete(input: string): string;
  getCwd(): string;
  changeDir(dir: string): string;
  writeFile(path: string, content: string): string;
  readFile(path: string): string;
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
  ): Promise<{ response: string; provider: string; model: string }>;
  clearConversation?(): void;
  stopAgent?(): void;
  steerAgent?(message: string): Record<string, unknown>;
  // ── AST / symbol extraction (cmd/wasm/ast_funcs.go) ──
  parseFile?(filePath: string, content: Uint8Array | ArrayBuffer): string;
  extractSymbols?(filePath: string, content: Uint8Array | ArrayBuffer): string;
  supportedLanguages?(): string;
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

    // Install the ONNX bridge so the Go-WASM build's embedding manager can
    // delegate inference to onnxruntime-web running in this page.
    installSproutONNXBridge();
    // Install the SP-100 embedding-backend controller so the WASM shell's
    // SproutWasm.switchEmbeddingBackend / .embeddingBackendStatus / .embeddingModel
    // functions have a host-side handler to delegate to.
    installEmbeddingBackendController();

    // 2. Load wasm_exec.js.
    debug(' Step 1: Loading wasm_exec.js...');
    const script = document.createElement('script');
    const execUrl = config?.wasmExecUrl ?? DEFAULT_WASM_EXEC_URL;
    script.src = execUrl;
    document.head.appendChild(script);
    await new Promise<void>((resolve, reject) => {
      script.onload = () => {
        debug(' wasm_exec.js loaded');
        resolve();
      };
      script.onerror = () => reject(new Error(`Failed to load wasm_exec.js from ${execUrl}`));
    });

    // 3. Fetch and instantiate the WASM binary.
    debug(' Step 2: Creating Go instance...');
    const go = new window.Go();
    const wasmUrl = config?.wasmUrl ?? DEFAULT_WASM_URL;
    debug(' Step 3: Fetching sprout.wasm from', wasmUrl);
    const wasmResponse = await fetch(wasmUrl);
    if (!wasmResponse.ok) {
      throw new Error(`Failed to fetch ${wasmUrl}: ${wasmResponse.status}`);
    }

    debug(' Step 4: Reading arrayBuffer...');
    const wasmBuffer = await wasmResponse.arrayBuffer();
    debug(' ArrayBuffer size:', wasmBuffer.byteLength);
    debug(' Step 5: WebAssembly.instantiate...');
    const { instance } = await WebAssembly.instantiate(wasmBuffer, go.importObject);
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
    const shell: WasmShell = {
      executeCommand(input: string): WasmShellResult {
        const json = wasm.executeCommand(input);
        return JSON.parse(json);
      },

      autoComplete(input: string): WasmCompletionResult {
        const json = wasm.autoComplete(input);
        return JSON.parse(json);
      },

      getCwd(): string {
        return wasm.getCwd();
      },

      changeDir(dir: string): WasmChangeDirResult {
        const json = wasm.changeDir(dir);
        return JSON.parse(json);
      },

      writeFile(path: string, content: string): string {
        return wasm.writeFile(path, content);
      },

      readFile(path: string): WasmReadFileResult {
        const json = wasm.readFile(path);
        return JSON.parse(json);
      },

      listDir(path: string): WasmListDirResult {
        const json = wasm.listDir(path);
        try {
          return JSON.parse(json);
        } catch {
          return { entries: [], error: json };
        }
      },

      deleteFile(path: string): string {
        return wasm.deleteFile(path);
      },

      runAgent(
        provider: string,
        model: string,
        query: string,
        onEvent?: (eventJson: string) => void,
      ): Promise<{ response: string; provider: string; model: string }> {
        const api = wasm as SproutWasmAPI;
        if (!api.runAgent) {
          return Promise.reject(new Error('WASM binary does not expose runAgent'));
        }
        return api.runAgent(provider, model, query, onEvent);
      },

      clearConversation(): void {
        const api = wasm as SproutWasmAPI;
        if (api.clearConversation) {
          api.clearConversation();
        }
      },

      stopAgent(): void {
        const api = wasm as SproutWasmAPI;
        if (api.stopAgent) {
          api.stopAgent();
        }
      },

      steerAgent(message: string): Record<string, unknown> {
        const api = wasm as SproutWasmAPI;
        if (api.steerAgent) {
          return api.steerAgent(message);
        }
        return { steered: false, error: 'steerAgent not available' };
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
