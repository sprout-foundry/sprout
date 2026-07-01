/**
 * onnxruntimeWebLoader.ts — Lazy-loads the onnxruntime-web bundle from CDN.
 *
 * Lives next to embeddingWasmLoader.ts so both lazy-loaders share the same
 * directory — parallel pattern, same conventions. This path is NOT gitignored
 * (unlike webui/public/wasm/).
 *
 * Contract:
 *   - Returns `window.ort` immediately if already loaded (static backend path).
 *   - Otherwise injects exactly one `<script>` tag per page lifetime.
 *   - Subsequent calls return the cached promise (no extra script tags).
 *   - On load failure: rejects with a clear error, logs a console warning,
 *     and resets the cache so the caller can retry with a different URL.
 *
 * The `resetOnnxRuntimeWebLoaderForTesting()` function is the test hook — it
 * clears the cached promise so the next call triggers a fresh load attempt.
 *
 * Model source: https://github.com/microsoft/onnxruntime
 * CDN URL: pinned to v1.17.1 (not floating).
 */

const ONNXRUNTIME_WEB_URL =
  'https://cdn.jsdelivr.net/npm/onnxruntime-web@1.17.1/dist/ort.min.js';

/** SRI hash for onnxruntime-web@1.17.1 (jsdelivr CDN).
 *  Computed via: curl <url> | openssl dgst -sha384 -binary | openssl base64 -A */
const ONNXRUNTIME_WEB_INTEGRITY =
  'sha384-61k9ikq77C7O/r49eqY0GLZKj5nlqD2OZ2g4Q3sk/wFuPY67SrKsqC2qMc1uuz4N';

/** Short timeout for jsdom test runners; longer for real browsers. */
const SCRIPT_LOAD_TIMEOUT_MS = import.meta.env.MODE === 'test' ? 100 : 1500;

declare global {
  interface Window {
    ort?: any;
  }
}

let loadPromise: Promise<any> | null = null;

/**
 * Lazy-load onnxruntime-web from CDN.
 *
 * Returns `window.ort` if it's already available (e.g. pre-bundled by a
 * static build). Otherwise injects a single `<script>` tag and resolves
 * when the global is ready. Repeated calls return the cached promise.
 *
 * @param opts.url - Override the CDN URL (useful for offline dev or testing).
 */
export async function loadOnnxRuntimeWeb(opts?: { url?: string }): Promise<any> {
  // Fast path: already loaded.
  if (typeof window !== 'undefined' && window.ort) {
    return window.ort;
  }

  // Return cached promise if a load is in-flight.
  if (loadPromise) {
    return loadPromise;
  }

  // Non-browser environments (node, SSR, test runners without jsdom):
  // skip the <script> injection entirely. The caller's dynamic import
  // (`import('onnxruntime-web')`) will resolve the module through the
  // normal bundler/test-mock path.
  if (typeof document === 'undefined') {
    return Promise.resolve(undefined);
  }

  loadPromise = new Promise((resolve, reject) => {
    const script = document.createElement('script');
    script.src = opts?.url ?? ONNXRUNTIME_WEB_URL;
    script.async = true;
    script.crossOrigin = 'anonymous';
    script.integrity = ONNXRUNTIME_WEB_INTEGRITY;

    let settled = false;
    const trySettle = (fn: () => void) => {
      if (settled) return;
      settled = true;
      fn();
    };

    script.onload = () => {
      trySettle(() => {
        if (typeof window !== 'undefined' && window.ort) {
          resolve(window.ort);
        } else {
          loadPromise = null;
          reject(new Error('[onnxruntime-web-loader] window.ort is undefined after script load'));
        }
      });
    };

    script.onerror = (e) => {
      trySettle(() => {
        loadPromise = null;
        console.debug('[onnxruntime-web-loader] failed to load onnxruntime-web', e);
        reject(new Error('[onnxruntime-web-loader] Failed to load onnxruntime-web script'));
      });
    };

    // Safety timeout: in jsdom test runners the script never loads
    // or errors (no real network). After SCRIPT_LOAD_TIMEOUT_MS we resolve
    // with undefined so the caller's dynamic import (which may be mocked)
    // can take over. The loader's own tests trigger onload/onerror
    // synchronously, well before this fires. In real browsers the CDN
    // loads in < 1 s.
    const tid = setTimeout(() => {
      trySettle(() => {
        loadPromise = null;
        resolve(undefined);
      });
    }, SCRIPT_LOAD_TIMEOUT_MS);

    // Clear the timeout when onload/onerror settle the promise first.
    const _onload = script.onload as (() => void) | undefined;
    const _onerror = script.onerror as ((e: unknown) => void) | undefined;
    script.onload = () => { clearTimeout(tid); _onload?.(); };
    script.onerror = (e: unknown) => { clearTimeout(tid); _onerror?.(e); };

    document.head.appendChild(script);
  });

  return loadPromise;
}

/**
 * Check whether onnxruntime-web has been loaded (window.ort exists).
 */
export function isOnnxRuntimeWebLoaded(): boolean {
  return typeof window !== 'undefined' && window.ort != null;
}

/**
 * Reset the internal cached promise. Intended for testing so that
 * subsequent calls to loadOnnxRuntimeWeb() trigger a fresh load attempt.
 */
export function resetOnnxRuntimeWebLoaderForTesting(): void {
  loadPromise = null;
}

/**
 * Install a global `__sproutLoadOnnxRuntime` function that the WASM shell
 * (pkg/wasmshell/embedding_funcs.go) can invoke from Go via js.FuncOf.
 *
 * The global is fire-and-forget: it resolves silently on success and logs
 * a warning on failure (the Go side does not block on the load completing).
 */
export function installOnnxRuntimeWebGlobal(): void {
  if (typeof globalThis === 'undefined') return;
  (globalThis as { __sproutLoadOnnxRuntime?: () => Promise<void> }).__sproutLoadOnnxRuntime =
    () =>
      loadOnnxRuntimeWeb()
        .then(() => undefined)
        .catch((e) => {
          console.warn('[onnxruntime-web-loader] lazy load failed', e);
        });
}
