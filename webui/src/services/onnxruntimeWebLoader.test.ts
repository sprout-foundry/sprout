/**
 * Tests for onnxruntimeWebLoader
 *
 * Covers the lazy-load + cached-promise pattern used to dynamically
 * inject onnxruntime-web from CDN.  Tests run under jsdom (configured
 * globally in vite.config.ts) and exercise real DOM manipulation without
 * any network calls.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import {
  loadOnnxRuntimeWeb,
  isOnnxRuntimeWebLoaded,
  resetOnnxRuntimeWebLoaderForTesting,
  installOnnxRuntimeWebGlobal,
} from './onnxruntimeWebLoader';

// ── Helpers ──────────────────────────────────────────────────────────

/** Remove any <script> tags the loader injected into document.head. */
function removeInjectedScripts(): void {
  const scripts = document.head.querySelectorAll('script');
  scripts.forEach((s) => s.remove());
}

/**
 * Find the most recently injected <script> tag (the last child of
 * document.head that is a script element).
 */
function getLastScript(): HTMLScriptElement | null {
  const scripts = document.head.querySelectorAll('script');
  return scripts.length ? (scripts[scripts.length - 1] as HTMLScriptElement) : null;
}

// ── Setup / Teardown ─────────────────────────────────────────────────

beforeEach(() => {
  // Reset module-level cache so each test starts fresh.
  resetOnnxRuntimeWebLoaderForTesting();
  // Clear any pre-existing global.
  (window as { ort?: unknown }).ort = undefined;
  // Remove any leftover script tags from previous tests.
  removeInjectedScripts();
  // Restore console.warn after spying in individual tests.
  vi.restoreAllMocks();
});

afterEach(() => {
  // Clean up global state.
  (window as { ort?: unknown }).ort = undefined;
  removeInjectedScripts();
  // Remove the __sproutLoadOnnxRuntime global if it was installed.
  (globalThis as { __sproutLoadOnnxRuntime?: unknown }).__sproutLoadOnnxRuntime = undefined;
});

// ── Tests ────────────────────────────────────────────────────────────

describe('loadOnnxRuntimeWeb', () => {
  it('resolves immediately with window.ort when already set — no script injected', async () => {
    // Arrange: pre-set window.ort (simulates static backend path).
    (window as { ort?: unknown }).ort = { stub: true };

    // Act.
    const result = await loadOnnxRuntimeWeb();

    // Assert: returns the existing ort, no <script> was appended.
    expect(result).toStrictEqual({ stub: true });
    expect(result).toBe(window.ort);
    expect(document.head.querySelectorAll('script')).toHaveLength(0);
  });

  it('injects exactly one <script> for two concurrent calls — cached promise', async () => {
    // Arrange: no window.ort, clean state.
    const fakeUrl = 'http://fake.test/ort.js';

    // Act: call twice WITHOUT awaiting in between.
    const promiseA = loadOnnxRuntimeWeb({ url: fakeUrl });
    const promiseB = loadOnnxRuntimeWeb({ url: fakeUrl });

    // Assert: both calls share the same underlying load (proven by single script).
    // Note: we can't assert promiseA === promiseB because the async function
    // wrapper creates a new promise each invocation even when returning the
    // same cached promise. The real proof of caching is the DOM state.
    const scripts = document.head.querySelectorAll('script');
    expect(scripts).toHaveLength(1);
    expect(scripts[0]).toHaveAttribute('src', fakeUrl);

    // Simulate the script loading successfully so the promises resolve.
    (window as { ort?: unknown }).ort = { fake: true };
    const script = scripts[0] as HTMLScriptElement;
    script.onload!();

    // Both should resolve to window.ort.
    const [resultA, resultB] = await Promise.all([promiseA, promiseB]);
    expect(resultA).toStrictEqual({ fake: true });
    expect(resultB).toStrictEqual({ fake: true });
    // Both resolve to the same window.ort reference.
    expect(resultA).toBe(resultB);
  });

  it('rejects on script error, logs console.debug, and clears cache for retry', async () => {
    // Arrange.
    const debugSpy = vi.spyOn(console, 'debug');
    const fakeUrl = 'http://fake.test/ort-fail.js';

    // Act: start the load (creates script + cached promise).
    const loadPromise = loadOnnxRuntimeWeb({ url: fakeUrl });
    const script = getLastScript();
    expect(script).not.toBeNull();

    // Simulate the script failing to load.
    script!.onerror!(new ErrorEvent('error'));

    // Assert: promise rejects with a descriptive error.
    await expect(loadPromise).rejects.toThrow(
      '[onnxruntime-web-loader] Failed to load onnxruntime-web script'
    );

    // console.debug was called (loader's internal signal; global wrapper
    // uses console.warn for user-visible noise).
    expect(debugSpy).toHaveBeenCalledTimes(1);
    expect(debugSpy.mock.calls[0][0]).toBe(
      '[onnxruntime-web-loader] failed to load onnxruntime-web'
    );

    // The cache should be cleared automatically (loadPromise = null in onerror).
    // Verify by calling again — it should create a NEW script tag.
    const retryPromise = loadOnnxRuntimeWeb({ url: fakeUrl });
    const scriptsAfterRetry = document.head.querySelectorAll('script');
    expect(scriptsAfterRetry).toHaveLength(2); // original + retry

    // Clean up: resolve the retry so there's no unhandled rejection.
    (window as { ort?: unknown }).ort = { retry: true };
    const retryScript = scriptsAfterRetry[1] as HTMLScriptElement;
    retryScript.onload!();
    const retryResult = await retryPromise;
    expect(retryResult).toStrictEqual({ retry: true });
  });

  it('rejects when script loads but window.ort remains undefined', async () => {
    // Arrange.
    const fakeUrl = 'http://fake.test/ort-ghost.js';

    // Act.
    const loadPromise = loadOnnxRuntimeWeb({ url: fakeUrl });
    const script = getLastScript();
    expect(script).not.toBeNull();

    // Simulate onload firing but window.ort was NOT set (malformed script).
    script!.onload!();

    // Assert: promise rejects because ort is missing.
    await expect(loadPromise).rejects.toThrow(
      '[onnxruntime-web-loader] window.ort is undefined after script load'
    );

    // Cache should be cleared so retry is possible.
    const retryPromise = loadOnnxRuntimeWeb({ url: fakeUrl });
    const scriptsAfterRetry = document.head.querySelectorAll('script');
    expect(scriptsAfterRetry).toHaveLength(2);

    // Clean up the retry.
    (window as { ort?: unknown }).ort = { fixed: true };
    const retryScript = scriptsAfterRetry[1] as HTMLScriptElement;
    retryScript.onload!();
    const retryResult = await retryPromise;
    expect(retryResult).toStrictEqual({ fixed: true });
  });

  it('uses default CDN URL when no override is provided', async () => {
    // Arrange & Act.
    loadOnnxRuntimeWeb();
    const script = getLastScript();

    // Assert.
    expect(script).not.toBeNull();
    expect(script!.src).toContain('onnxruntime-web@1.17.1');
    expect(script!.src).toContain('ort.min.js');
  });
});

describe('resetOnnxRuntimeWebLoaderForTesting', () => {
  it('clears the cache so a second call appends a new <script>', async () => {
    // Arrange: trigger first load.
    const fakeUrl = 'http://fake.test/ort.js';
    const firstPromise = loadOnnxRuntimeWeb({ url: fakeUrl });
    expect(document.head.querySelectorAll('script')).toHaveLength(1);

    // Act: reset the cache.
    resetOnnxRuntimeWebLoaderForTesting();

    // Trigger second load.
    const secondPromise = loadOnnxRuntimeWeb({ url: fakeUrl });

    // Assert: a second <script> was appended (cache was cleared).
    expect(document.head.querySelectorAll('script')).toHaveLength(2);

    // The two promises are different instances.
    expect(firstPromise).not.toBe(secondPromise);

    // Clean up both.
    (window as { ort?: unknown }).ort = { done: true };
    const scripts = document.head.querySelectorAll('script');
    (scripts[0] as HTMLScriptElement).onload!();
    (scripts[1] as HTMLScriptElement).onload!();
    await firstPromise;
    await secondPromise;
  });
});

describe('isOnnxRuntimeWebLoaded', () => {
  it('returns false when window.ort is undefined', () => {
    (window as { ort?: unknown }).ort = undefined;
    expect(isOnnxRuntimeWebLoaded()).toBe(false);
  });

  it('returns true when window.ort is set', () => {
    (window as { ort?: unknown }).ort = { env: 'fake' };
    expect(isOnnxRuntimeWebLoaded()).toBe(true);
  });
});

describe('installOnnxRuntimeWebGlobal', () => {
  it('installs globalThis.__sproutLoadOnnxRuntime as a function', () => {
    // Act.
    installOnnxRuntimeWebGlobal();

    // Assert.
    const fn = (globalThis as { __sproutLoadOnnxRuntime?: () => Promise<void> }).__sproutLoadOnnxRuntime;
    expect(typeof fn).toBe('function');
  });

  it('calling the global returns a resolved promise when window.ort is already set', async () => {
    // Arrange: ort already available.
    (window as { ort?: unknown }).ort = { stub: true };

    // Act.
    installOnnxRuntimeWebGlobal();
    const fn = (globalThis as { __sproutLoadOnnxRuntime?: () => Promise<void> }).__sproutLoadOnnxRuntime;

    // Assert: resolves to undefined (fire-and-forget contract).
    const result = await fn!();
    expect(result).toBeUndefined();
  });

  it('calling the global swallows errors and returns undefined on load failure', async () => {
    // Arrange: no ort, clean state.
    const warnSpy = vi.spyOn(console, 'warn');

    installOnnxRuntimeWebGlobal();
    const fn = (globalThis as { __sproutLoadOnnxRuntime?: () => Promise<void> }).__sproutLoadOnnxRuntime;

    // Act: call the global — it will try to load, fail, and swallow the error.
    const resultPromise = fn!();

    // Trigger the script error.
    const script = getLastScript();
    expect(script).not.toBeNull();
    script!.onerror!(new ErrorEvent('error'));

    // Assert: the global's promise resolves to undefined (not rejected).
    const result = await resultPromise;
    expect(result).toBeUndefined();

    // But console.warn was called (from the global wrapper's .catch() —
    // the loader's onerror uses console.debug internally).
    expect(warnSpy).toHaveBeenCalled();
  });
});
