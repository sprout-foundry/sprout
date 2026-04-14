/**
 * wasmShell.ts — Loads and interfaces with the ledit Go→WASM shell module.
 *
 * Usage:
 *   const shell = await initWasmShell();
 *   const result = await shell.executeCommand('ls -la');
 *   console.log(result.stdout);
 */

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

export interface LeditStore {
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
  /** Get the fully initialized Go global. */
  readonly wasm: typeof globalThis & { LeditWasm: unknown };
}

// ── IndexedDB store ─────────────────────────────────────────────────────────

const DB_NAME = 'ledit-wasm-fs';
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
    tx.oncomplete = () => { db.close(); resolve(); };
    tx.onerror = () => { db.close(); reject(tx.error); };
  });
}

async function idbLoadFile(path: string): Promise<string | null> {
  const db = await openDB();
  return new Promise((resolve, reject) => {
    const tx = db.transaction(STORE_NAME, 'readonly');
    const store = tx.objectStore(STORE_NAME);
    const req = store.get(path);
    req.onsuccess = () => {
      db.close();
      resolve(req.result?.content ?? null);
    };
    req.onerror = () => { db.close(); reject(req.error); };
  });
}

async function idbDeleteFile(path: string): Promise<void> {
  const db = await openDB();
  return new Promise((resolve, reject) => {
    const tx = db.transaction(STORE_NAME, 'readwrite');
    const store = tx.objectStore(STORE_NAME);
    store.delete(path);
    tx.oncomplete = () => { db.close(); resolve(); };
    tx.onerror = () => { db.close(); reject(tx.error); };
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
    req.onerror = () => { db.close(); reject(req.error); };
  });
}

// ── WASM loader ─────────────────────────────────────────────────────────────

declare global {
  interface Window {
    __leditStore: LeditStore;
    Go: new () => {
      run(instance: WebAssembly.Instance): void;
      importObject: WebAssembly.Imports;
    };
    LeditWasm: unknown;
  }
}

let sharedInstance: WasmShell | null = null;

/**
 * Initialize the ledit WASM shell.
 *
 * Must be called before any shell operations. Sets up IndexedDB bridge,
 * loads the WASM binary, and calls LeditWasm.init().
 *
 * @param config.home - Override the virtual home directory (default: /home/user)
 * @returns The WasmShell interface
 */
export async function initWasmShell(config?: { home?: string }): Promise<WasmShell> {
  if (sharedInstance) {
    return sharedInstance;
  }

  // 1. Set up the IndexedDB store bridge on window so Go can call it.
  const store: LeditStore = {
    saveFile: (path, content) => {
      idbSaveFile(path, content).catch((err) =>
        console.warn('[ledit-wasm] Failed to save file to IndexedDB:', path, err),
      );
    },
    loadFile: (path) => {
      // Synchronous not possible with IndexedDB — the store.listFiles restores all
      // files on init instead. loadFile is provided for completeness but returns null.
      return null;
    },
    deleteFile: (path) => {
      idbDeleteFile(path).catch((err) =>
        console.warn('[ledit-wasm] Failed to delete file from IndexedDB:', path, err),
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

  window.__leditStore = store;

  // 2. Load wasm_exec.js.
  const script = document.createElement('script');
  script.src = '/wasm/wasm_exec.js';
  document.head.appendChild(script);
  await new Promise<void>((resolve, reject) => {
    script.onload = () => resolve();
    script.onerror = () => reject(new Error('Failed to load wasm_exec.js'));
  });

  // 3. Fetch and instantiate the WASM binary.
  const go = new window.Go();
  const wasmResponse = await fetch('/wasm/ledit.wasm');
  if (!wasmResponse.ok) {
    throw new Error(`Failed to fetch ledit.wasm: ${wasmResponse.status}`);
  }

  const wasmBuffer = await wasmResponse.arrayBuffer();
  const { instance } = await WebAssembly.instantiate(wasmBuffer, go.importObject);

  // 4. Run the Go instance (this blocks until main() hits the channel wait).
  go.run(instance);

  // At this point window.LeditWasm should be defined by Go's main().
  const wasm = window.LeditWasm as unknown as {
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
  };

  if (!wasm || typeof wasm.init !== 'function') {
    throw new Error('LeditWasm global not found after WASM init');
  }

  // 5. Initialize the Go side (restores files from IndexedDB cache).
  const configStr = config ? JSON.stringify(config) : undefined;
  const initError = wasm.init(configStr);
  if (initError) {
    console.warn('[ledit-wasm] Init warning:', initError);
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

    get wasm() {
      return window as typeof globalThis & { LeditWasm: unknown };
    },
  };

  sharedInstance = shell;
  return shell;
}

// ── Synchronous cache for IndexedDB (used during WASM init) ──────────────

let fileIdbCache: string = '[]';

async function warmIdbCache(): Promise<void> {
  try {
    fileIdbCache = await idbListFiles();
  } catch (err) {
    console.warn('[ledit-wasm] Failed to warm IDB cache:', err);
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
}
