/**
 * Cross-validation tests for WASM-local endpoint classification.
 *
 * These tests verify that ALL endpoints marked with `handled_by: "wasm-shell"`
 * in the Go server (platform/internal/api/webui_compat.go) are correctly
 * classified as `wasm-local` in the TypeScript endpoint registry.
 *
 * The goal: ensure that WASM-local endpoints (file CRUD, terminal, search)
 * are ALWAYS handled by the WASM shell in the browser — NEVER proxied to
 * the Foundry backend. If any wasm-local endpoint is misclassified, the
 * CloudAdapter will proxy it to the server instead of handling it locally.
 *
 * This is the canonical cross-validation between:
 *   - Go server: platform/internal/api/webui_compat.go (handled_by markers)
 *   - TypeScript registry: services/cloudEndpointRegistry/endpoints/wasm-local.ts
 */

import {
  classifyEndpoint,
  getEndpointsByCategory,
  isWasmLocalEndpoint,
  isFoundryBackendEndpoint,
} from './cloudEndpointRegistry';

/**
 * Canonical list of endpoints the Go server marks as handled_by: "wasm-shell".
 *
 * These MUST be classified as wasm-local (or no-op) in the TypeScript registry.
 * If any of these is classified as foundry-backend or unclassified, the test fails
 * and the endpoint would be incorrectly proxied to the server.
 *
 * Mirror of webui_compat.go handled_by:wasm-shell handlers:
 *   - handleWebuiTerminalSessions      → GET  /api/terminal/sessions
 *   - handleWebuiTerminalShells        → GET  /api/terminal/shells
 *   - handleWebuiTerminalHistory       → GET  /api/terminal/history
 *   - handleWebuiTerminalHistoryAdd    → POST /api/terminal/history
 *   - handleWebuiFiles                 → GET  /api/files
 *   - handleWebuiFileCreate            → POST /api/create
 *   - handleWebuiFileDelete            → POST,DELETE /api/delete
 *   - handleWebuiFileRename            → POST /api/rename
 *   - handleWebuiBrowse                → GET  /api/browse
 *   - handleWebuiBrowse (alias)        → GET  /api/workspace/browse
 *   - handleWebuiOpenInFileBrowser     → POST /api/open-in-file-browser
 *   - handleWebuiFileCheckModified     → GET,POST /api/file/check-modified
 *   - handleWebuiFileGet               → GET  /api/file
 *   - handleWebuiFilePost              → POST /api/file
 *   - handleWebuiFileConsent           → POST /api/file/consent
 *   - handleWebuiPrettierConfig        → GET  /api/files/prettier-config
 *   - handleWebuiSearch                → GET  /api/search
 *   - handleWebuiSearchReplace         → POST /api/search/replace
 */
interface WasmLocalEndpointSpec {
  path: string;
  method: string;
  handler: string; // Go handler name for traceability
}

const wasmLocalEndpointSpecs: WasmLocalEndpointSpec[] = [
  // ── Terminal (WASM manages locally) ──────────────────────────────
  { path: '/api/terminal/sessions', method: 'GET', handler: 'handleWebuiTerminalSessions' },
  { path: '/api/terminal/shells', method: 'GET', handler: 'handleWebuiTerminalShells' },
  { path: '/api/terminal/history', method: 'GET', handler: 'handleWebuiTerminalHistory' },
  { path: '/api/terminal/history', method: 'POST', handler: 'handleWebuiTerminalHistoryAdd' },

  // ── File CRUD (WASM manages locally) ─────────────────────────────
  { path: '/api/files', method: 'GET', handler: 'handleWebuiFiles' },
  { path: '/api/create', method: 'POST', handler: 'handleWebuiFileCreate' },
  { path: '/api/delete', method: 'POST', handler: 'handleWebuiFileDelete' },
  { path: '/api/delete', method: 'DELETE', handler: 'handleWebuiFileDelete' },
  { path: '/api/rename', method: 'POST', handler: 'handleWebuiFileRename' },
  { path: '/api/browse', method: 'GET', handler: 'handleWebuiBrowse' },
  { path: '/api/workspace/browse', method: 'GET', handler: 'handleWebuiBrowse (alias)' },
  { path: '/api/open-in-file-browser', method: 'POST', handler: 'handleWebuiOpenInFileBrowser' },

  // ── File metadata (WASM manages locally) ─────────────────────────
  { path: '/api/file/check-modified', method: 'GET', handler: 'handleWebuiFileCheckModified' },
  { path: '/api/file/check-modified', method: 'POST', handler: 'handleWebuiFileCheckModified' },
  { path: '/api/file/consent', method: 'POST', handler: 'handleWebuiFileConsent' },
  { path: '/api/files/prettier-config', method: 'GET', handler: 'handleWebuiPrettierConfig' },

  // ── File read/write (WASM manages locally) ───────────────────────
  { path: '/api/file', method: 'GET', handler: 'handleWebuiFileGet' },
  { path: '/api/file', method: 'POST', handler: 'handleWebuiFilePost' },

  // ── Search (WASM manages locally) ────────────────────────────────
  { path: '/api/search', method: 'GET', handler: 'handleWebuiSearch' },
  { path: '/api/search/replace', method: 'POST', handler: 'handleWebuiSearchReplace' },
];

describe('WASM-local endpoint cross-validation (Go server ↔ TypeScript registry)', () => {
  describe('Every Go handled_by:wasm-shell endpoint must NOT be classified as foundry-backend', () => {
    it.each(wasmLocalEndpointSpecs)(
      '$method $path (handler: $handler) must NOT be proxied to Foundry backend',
      ({ path, method, handler }) => {
        // Critical assertion: wasm-local endpoints must NEVER be foundry-backend.
        // If this fails, the CloudAdapter will proxy the request to the server
        // instead of handling it locally via the WASM shell.
        expect(
          isFoundryBackendEndpoint(path, method),
          `CRITICAL: $method $path (handler: $handler) is classified as foundry-backend. ` +
            'This will cause the request to be PROXIED to the server instead of ' +
            'handled locally by the WASM shell.',
        ).toBe(false);
      },
    );
  });

  describe('Every Go handled_by:wasm-shell endpoint must be classified as wasm-local or no-op', () => {
    it.each(wasmLocalEndpointSpecs)(
      '$method $path (handler: $handler) must be wasm-local or no-op',
      ({ path, method, handler }) => {
        const endpoint = classifyEndpoint(path, method);

        // Must be registered in the endpoint registry
        expect(endpoint).not.toBeNull();

        // Must be classified as wasm-local or no-op (NOT foundry-backend or synthetic)
        const category = endpoint?.category;
        expect(
          category === 'wasm-local' || category === 'no-op',
          `$method $path (handler: $handler) has category '${category}' — must be 'wasm-local' or 'no-op'`,
        ).toBe(true);
      },
    );
  });

  describe('isWasmLocalEndpoint returns true for wasm-local and no-op endpoints marked handled_by:wasm-shell', () => {
    // Note: isWasmLocalEndpoint() only returns true for category 'wasm-local'.
    // The /api/open-in-file-browser endpoint is classified as 'no-op' in the
    // TypeScript registry, so isWasmLocalEndpoint returns false for it. However,
    // this is correct behavior — no-op endpoints are handled by getSyntheticResponse
    // in the CloudAdapter, not by the WASM shell. The key requirement is that
    // these endpoints are NOT proxied to the Foundry backend.
    it.each(
      wasmLocalEndpointSpecs.filter((spec) => {
        // open-in-file-browser is 'no-op', not 'wasm-local', so isWasmLocalEndpoint returns false
        if (spec.path === '/api/open-in-file-browser') return false;
        return true;
      }),
    )('$method $path (handler: $handler) must return true for isWasmLocalEndpoint', ({ path, method, handler }) => {
      expect(
        isWasmLocalEndpoint(path, method),
        `$method $path (handler: $handler) should be recognized as wasm-local`,
      ).toBe(true);
    });

    it('open-in-file-browser (no-op) is NOT wasm-local but IS registered', () => {
      // open-in-file-browser is classified as 'no-op' — not wasm-local — but must
      // still be classified correctly (not foundry-backend).
      const endpoint = classifyEndpoint('/api/open-in-file-browser', 'POST');
      expect(endpoint).not.toBeNull();
      expect(endpoint?.category).toBe('no-op');
      expect(isWasmLocalEndpoint('/api/open-in-file-browser', 'POST')).toBe(false);
      expect(isFoundryBackendEndpoint('/api/open-in-file-browser', 'POST')).toBe(false);
    });
  });

  describe('WASM-local registry completeness: no unclassified handled_by:wasm-shell endpoints', () => {
    it('all Go handled_by:wasm-shell endpoints are found in the registry', () => {
      const errors: string[] = [];

      for (const spec of wasmLocalEndpointSpecs) {
        const endpoint = classifyEndpoint(spec.path, spec.method);
        if (!endpoint) {
          errors.push(
            `${spec.method} ${spec.path} (handler: ${spec.handler}): ` +
              'NOT FOUND in registry — will fall through to proxy in CloudAdapter',
          );
        } else if (endpoint.category === 'foundry-backend') {
          errors.push(
            `${spec.method} ${spec.path} (handler: ${spec.handler}): ` +
              'classified as foundry-backend — will be PROXIED instead of handled by WASM',
          );
        } else if (endpoint.category === 'synthetic') {
          // Synthetic is also wrong for wasm-local endpoints — they should be
          // wasm-local or no-op, not synthetic. Synthetic endpoints are for
          // features that don't exist in cloud mode (SSH, instances).
          errors.push(
            `${spec.method} ${spec.path} (handler: ${spec.handler}): ` +
              'classified as synthetic — should be wasm-local or no-op',
          );
        }
      }

      if (errors.length > 0) {
        throw new Error(
          `${errors.length} wasm-local endpoint validation error(s):\n` + errors.map((e) => `  ✗ ${e}`).join('\n'),
        );
      }
    });
  });

  describe('WASM-local endpoint count matches expected coverage', () => {
    it('registry has expected number of wasm-local endpoint definitions', () => {
      // Count from wasm-local.ts (16 as of the in-browser agent loop addition):
      // /api/files, /api/create, /api/delete, /api/rename, /api/browse,
      // /api/file/check-modified, /api/file/consent, /api/terminal/sessions,
      // /api/terminal/shells, /api/terminal/history, /api/search/replace,
      // /api/file, /api/files/prettier-config, /api/workspace/browse, /api/search,
      // /api/query (the in-browser agent loop endpoint).
      const wasmEndpoints = getEndpointsByCategory('wasm-local');
      expect(wasmEndpoints.length).toBe(16);
    });

    it('wasm-local endpoints cover all 3 categories: file, terminal, search', () => {
      const wasmEndpoints = getEndpointsByCategory('wasm-local');
      const paths = wasmEndpoints.map((e) => e.path);

      // File endpoints
      expect(paths).toContain('/api/files');
      expect(paths).toContain('/api/create');
      expect(paths).toContain('/api/delete');
      expect(paths).toContain('/api/rename');
      expect(paths).toContain('/api/browse');
      expect(paths).toContain('/api/file');
      expect(paths).toContain('/api/file/check-modified');
      expect(paths).toContain('/api/file/consent');
      expect(paths).toContain('/api/files/prettier-config');

      // Terminal endpoints
      expect(paths).toContain('/api/terminal/sessions');
      expect(paths).toContain('/api/terminal/shells');
      expect(paths).toContain('/api/terminal/history');

      // Search endpoints
      expect(paths).toContain('/api/search');
      expect(paths).toContain('/api/search/replace');
    });
  });

  describe('No wasm-local endpoint is duplicated in another category', () => {
    it('wasm-local endpoints are not also registered as foundry-backend', () => {
      const wasmSpecs = wasmLocalEndpointSpecs.filter((s) => s.path !== '/api/open-in-file-browser');
      for (const spec of wasmSpecs) {
        expect(isFoundryBackendEndpoint(spec.path, spec.method)).toBe(false);
      }
    });

    it('wasm-local endpoints are not also registered as synthetic', () => {
      const wasmSpecs = wasmLocalEndpointSpecs.filter((s) => s.path !== '/api/open-in-file-browser');
      for (const spec of wasmSpecs) {
        const endpoint = classifyEndpoint(spec.path, spec.method);
        expect(endpoint?.category).not.toBe('synthetic');
      }
    });
  });

  describe('No-op endpoint /api/open-in-file-browser is handled correctly', () => {
    it('is classified as no-op, not foundry-backend', () => {
      const endpoint = classifyEndpoint('/api/open-in-file-browser', 'POST');
      expect(endpoint?.category).toBe('no-op');
      expect(isFoundryBackendEndpoint('/api/open-in-file-browser', 'POST')).toBe(false);
    });
  });
});
