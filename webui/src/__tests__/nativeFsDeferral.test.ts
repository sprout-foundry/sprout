// @vitest-environment node
/**
 * R-2w: smoke tests for the Track R manifest-driven native-FS deferral leaf
 * module (webui/src/services/nativeFs/). These exercise the PURE decision,
 * normalization, mapping, and response-synthesis helpers in a plain node
 * environment (no jsdom). They do NOT drive the full stub/call-site path —
 * that is the domain of the main test authoring step.
 */

import { describe, it, expect, beforeAll } from 'vitest';
import {
  resolveNativeFsGate,
  normalizeWorkspacePath,
  workspaceErrorStatus,
  WORKSPACE_ERROR_STATUS,
  hasSproutStudioFsBridge,
  detectSproutStudio,
  mapWorkspaceListing,
  workspaceListDepth,
  sortNativeFileInfo,
  readWorkspaceResponse,
  writeWorkspaceResponse,
  __resetNativeFsGateForTests,
} from '../services/nativeFs';

/** A minimal usable bridge (the four methods) for the pure gate tests. */
function makeBridge(capabilities: Record<string, boolean>, excluded: Array<Record<string, unknown>> = []) {
  return {
    getCapabilities: async () => ({
      schemaVersion: 1,
      capabilities,
      excluded,
      manifestPresent: excluded.length > 0,
      servable: true,
    }),
    readWorkspaceFile: async () => ({ ok: true, path: 'x', content: '' }),
    writeWorkspaceFile: async () => ({ ok: true, path: 'x' }),
    listWorkspace: async () => ({ ok: true, files: [] }),
  };
}

describe('resolveNativeFsGate (pure)', () => {
  const ratifiedFs = [{ portion: 'fs', status: 'ratified' }];
  const seamOnlyFs = [{ portion: 'fs', status: 'seam-only' }];

  it('inactive when the compile-time flag is off', () => {
    const d = resolveNativeFsGate(false, makeBridge({ fs: true }, ratifiedFs), null);
    expect(d.active).toBe(false);
    expect(d.reason).toBe('native-fs-disabled');
  });

  it('inactive when there is no usable bridge', () => {
    const d = resolveNativeFsGate(true, null, null);
    expect(d.active).toBe(false);
    expect(d.reason).toBe('no-bridge');
  });

  it('inactive on malformed / missing capabilities', () => {
    const bridge = makeBridge({ fs: true }, ratifiedFs);
    expect(resolveNativeFsGate(true, bridge, null).active).toBe(false);
    expect(resolveNativeFsGate(true, bridge, {}).active).toBe(false);
  });

  it('inactive when the shell does not declare fs', () => {
    const bridge = makeBridge({ fs: false }, ratifiedFs);
    const d = resolveNativeFsGate(true, bridge, { capabilities: { fs: false }, excluded: ratifiedFs });
    expect(d.active).toBe(false);
    expect(d.reason).toBe('fs-not-declared');
  });

  it('inactive when the fs entry is seam-only (unratified)', () => {
    const d = resolveNativeFsGate(true, makeBridge({ fs: true }, seamOnlyFs), {
      capabilities: { fs: true },
      excluded: seamOnlyFs,
    });
    expect(d.active).toBe(false);
    expect(d.reason).toBe('fs-not-ratified');
  });

  it('active when fs is declared and ratified', () => {
    const d = resolveNativeFsGate(true, makeBridge({ fs: true }, ratifiedFs), {
      capabilities: { fs: true },
      excluded: ratifiedFs,
    });
    expect(d.active).toBe(true);
    expect(d.reason).toBe('active');
  });
});

describe('normalizeWorkspacePath (pure)', () => {
  it('strips leading ./ and / and normalizes backslashes', () => {
    expect(normalizeWorkspacePath('./src/main.go')).toBe('src/main.go');
    expect(normalizeWorkspacePath('/src/main.go')).toBe('src/main.go');
    expect(normalizeWorkspacePath('.\\src\\main.go')).toBe('src/main.go');
  });

  it('keeps a plain relative path intact', () => {
    expect(normalizeWorkspacePath('src/main.go')).toBe('src/main.go');
  });

  it('rejects .. segments', () => {
    expect(() => normalizeWorkspacePath('a/../../etc/passwd')).toThrow();
    expect(() => normalizeWorkspacePath('../x')).toThrow();
  });

  it('rejects the empty path', () => {
    expect(() => normalizeWorkspacePath('')).toThrow();
    expect(() => normalizeWorkspacePath('/')).toThrow();
  });
});

describe('workspace error → status mapping (pure)', () => {
  it('maps the documented codes', () => {
    expect(WORKSPACE_ERROR_STATUS.notFound).toBe(404);
    expect(WORKSPACE_ERROR_STATUS.invalidParams).toBe(400);
    expect(WORKSPACE_ERROR_STATUS.notInWorkspace).toBe(400);
    expect(WORKSPACE_ERROR_STATUS.isDirectory).toBe(400);
    expect(WORKSPACE_ERROR_STATUS.userCancelled).toBe(409);
    expect(WORKSPACE_ERROR_STATUS.workspaceNotSet).toBe(503);
    expect(WORKSPACE_ERROR_STATUS.ioFailed).toBe(500);
  });

  it('falls back to 500 for unknown / missing codes', () => {
    expect(workspaceErrorStatus('bogus')).toBe(500);
    expect(workspaceErrorStatus(undefined)).toBe(500);
    expect(workspaceErrorStatus('')).toBe(500);
  });
});

describe('bridge structural detection', () => {
  it('detects a complete bridge and rejects partial / null', () => {
    expect(hasSproutStudioFsBridge(makeBridge({}))).toBe(true);
    expect(hasSproutStudioFsBridge({ getCapabilities: async () => ({}) })).toBe(false);
    expect(hasSproutStudioFsBridge(null)).toBe(false);
    expect(hasSproutStudioFsBridge(undefined)).toBe(false);
    expect(hasSproutStudioFsBridge('x')).toBe(false);
  });

  it('detectSproutStudio returns null without window (node env)', () => {
    expect(detectSproutStudio()).toBeNull();
  });
});

describe('listWorkspace → file-tree mapping (pure)', () => {
  it('computes maxDepth relative to the requested path', () => {
    expect(workspaceListDepth('.')).toBe(1);
    expect(workspaceListDepth('')).toBe(1);
    expect(workspaceListDepth('src')).toBe(2);
    expect(workspaceListDepth('a/b/c')).toBe(4);
  });

  it('lists direct children of the root and maps + sorts them', () => {
    const result = {
      ok: true,
      files: [
        { path: 'b.txt', size: 2, isDir: false },
        { path: 'a', size: 0, isDir: true },
        { path: 'z.md', size: 1, isDir: false },
      ],
    };
    const items = mapWorkspaceListing(result, '.');
    // dirs first, then by name.
    expect(items.map((i) => i.name)).toEqual(['a', 'b.txt', 'z.md']);
    expect(items[0].isDir).toBe(true);
    expect(items[1].name).toBe('b.txt');
    expect(items[1].ext).toBe('.txt');
    expect(items[1].modified).toBe(0);
    expect(items[1].gitStatus).toBeUndefined();
  });

  it('filters to direct children of a nested requested dir', () => {
    const result = {
      ok: true,
      files: [
        { path: 'src/main.go', size: 5, isDir: false },
        { path: 'src/deep/nested.go', size: 6, isDir: false },
        { path: 'src/sub', size: 0, isDir: true },
        { path: 'other.go', size: 1, isDir: false },
      ],
    };
    const items = mapWorkspaceListing(result, 'src');
    // Only direct children of src/ (depth 2, prefix src/).
    expect(items.map((i) => i.name)).toEqual(['sub', 'main.go']);
  });

  it('returns [] on a non-ok / malformed result', () => {
    expect(mapWorkspaceListing({ ok: false, error: 'workspaceNotSet' }, '.')).toEqual([]);
    expect(mapWorkspaceListing(null, '.')).toEqual([]);
  });

  it('sortNativeFileInfo puts dirs first and honors ignored', () => {
    const items = sortNativeFileInfo([
      { name: 'b', path: 'b', size: 0, modified: 0, isDir: false, ext: '' },
      { name: 'a', path: 'a', size: 0, modified: 0, isDir: true, ext: '' },
      { name: 'c', path: 'c', size: 0, modified: 0, isDir: false, ext: '', gitStatus: 'ignored' },
    ]);
    expect(items.map((i) => i.name)).toEqual(['a', 'b', 'c']);
  });
});

describe('bridge-result → Response mapping', () => {
  it('maps an error result to the documented status + JSON body', async () => {
    const res = readWorkspaceResponse({ ok: false, error: 'notFound' });
    expect(res.status).toBe(404);
    expect(res.ok).toBe(false);
    expect(await res.json()).toEqual({ ok: false, error: 'notFound' });

    const bad = readWorkspaceResponse({ ok: false, error: 'notInWorkspace' });
    expect(bad.status).toBe(400);
    const notSet = readWorkspaceResponse({ ok: false, error: 'workspaceNotSet' });
    expect(notSet.status).toBe(503);
  });

  it('maps a utf-8 text read to a 200 Response', async () => {
    const res = readWorkspaceResponse({ ok: true, path: 'a.txt', content: 'hello' });
    expect(res.status).toBe(200);
    expect(res.ok).toBe(true);
    expect(await res.text()).toBe('hello');
  });

  it('maps a base64 binary read to an octet-stream Response', async () => {
    const b64 = Buffer.from([1, 2, 3, 250]).toString('base64');
    const res = readWorkspaceResponse({ ok: true, path: 'bin', contentBase64: b64 });
    expect(res.status).toBe(200);
    const buf = await res.arrayBuffer();
    expect(new Uint8Array(buf)).toEqual(new Uint8Array([1, 2, 3, 250]));
  });

  it('maps a write success to a 200 JSON Response and an error to its status', async () => {
    const ok = writeWorkspaceResponse({ ok: true, path: 'a.txt' });
    expect(ok.status).toBe(200);
    expect(await ok.json()).toEqual({ ok: true, path: 'a.txt' });

    const fail = writeWorkspaceResponse({ ok: false, error: 'userCancelled' });
    expect(fail.status).toBe(409);
  });

  it('maps a malformed contentBase64 in an ok:true read to a 500 ioFailed Response', async () => {
    // In a node env `atob` (a Node 18+ global) throws a DOMException on
    // malformed input, so the decode guard degrades to the documented
    // ioFailed (500) error Response instead of throwing out to consumers.
    const res = readWorkspaceResponse({
      ok: true,
      path: 'bin',
      contentBase64: '!!!not-base64!!!',
    });
    expect(res.status).toBe(500);
    expect(res.ok).toBe(false);
    expect(await res.json()).toEqual({ ok: false, error: 'ioFailed' });
  });
});

// ── R-2w: extended pure gate matrix (appended; existing 23 tests intact) ────
//
// These exercise the malformed / edge shapes of `capabilitiesResponse` that the
// base matrix above does not cover. Each asserts ACTUAL implementation behavior
// (with a note where the behavior is a deliberate "first-match-wins" or
// strict-typing consequence rather than the spec's idealized wording).

describe('resolveNativeFsGate — malformed / edge excluded[] shapes', () => {
  const bridge = makeBridge({ fs: true }, [{ portion: 'fs', status: 'ratified' }]);

  it('excluded is a string (not an array) → inactive (fs-not-ratified)', () => {
    const d = resolveNativeFsGate(true, bridge, {
      capabilities: { fs: true },
      excluded: 'fs' as unknown as Array<Record<string, unknown>>,
    });
    expect(d.active).toBe(false);
    expect(d.reason).toBe('fs-not-ratified');
  });

  it('excluded is null → inactive (fs-not-ratified)', () => {
    const d = resolveNativeFsGate(true, bridge, {
      capabilities: { fs: true },
      excluded: null as unknown as Array<Record<string, unknown>>,
    });
    expect(d.active).toBe(false);
    expect(d.reason).toBe('fs-not-ratified');
  });

  it('excluded entry missing `portion` → inactive (fs-not-ratified)', () => {
    const d = resolveNativeFsGate(true, bridge, { capabilities: { fs: true }, excluded: [{ status: 'ratified' }] });
    expect(d.active).toBe(false);
    expect(d.reason).toBe('fs-not-ratified');
  });

  it('excluded entry missing `status` → inactive (fs-not-ratified)', () => {
    const d = resolveNativeFsGate(true, bridge, { capabilities: { fs: true }, excluded: [{ portion: 'fs' }] });
    expect(d.active).toBe(false);
    expect(d.reason).toBe('fs-not-ratified');
  });

  it('a non-fs portion ratified (e.g. terminal) does NOT activate fs', () => {
    const d = resolveNativeFsGate(true, bridge, {
      capabilities: { fs: true },
      excluded: [{ portion: 'terminal', status: 'ratified' }],
    });
    expect(d.active).toBe(false);
    expect(d.reason).toBe('fs-not-ratified');
  });

  it('capabilities.fs === true but excluded is empty [] → inactive (fs-not-ratified)', () => {
    const d = resolveNativeFsGate(true, bridge, { capabilities: { fs: true }, excluded: [] });
    expect(d.active).toBe(false);
    expect(d.reason).toBe('fs-not-ratified');
  });

  // NOTE ON ACTUAL BEHAVIOR: the implementation uses `Array.prototype.some`, so
  // ANY ratified fs entry activates the gate even if a sibling fs entry is
  // seam-only. This asserts that first-match-wins consequence.
  it('duplicate fs entries (one seam-only + one ratified) → ACTIVE (first ratified wins)', () => {
    const d = resolveNativeFsGate(true, bridge, {
      capabilities: { fs: true },
      excluded: [
        { portion: 'fs', status: 'seam-only' },
        { portion: 'fs', status: 'ratified' },
      ],
    });
    expect(d.active).toBe(true);
    expect(d.reason).toBe('active');
  });
});

describe('resolveNativeFsGate — capabilities.fs strict-typing edge cases', () => {
  const bridge = makeBridge({ fs: true }, [{ portion: 'fs', status: 'ratified' }]);

  it('capabilities key missing entirely → inactive (fs-not-declared)', () => {
    const d = resolveNativeFsGate(true, bridge, { excluded: [{ portion: 'fs', status: 'ratified' }] });
    expect(d.active).toBe(false);
    expect(d.reason).toBe('fs-not-declared');
  });

  // NOTE ON ACTUAL BEHAVIOR: the gate requires `capabilities.fs === true`
  // (strict). Truthy non-boolean values (`'yes'`, `1`) therefore FAIL the
  // gate (fs-not-declared) even though they are truthy — asserted as-is.
  it('capabilities.fs truthy but not boolean (e.g. "yes") → inactive (fs-not-declared)', () => {
    const d = resolveNativeFsGate(true, bridge, {
      capabilities: { fs: 'yes' as unknown as boolean },
      excluded: [{ portion: 'fs', status: 'ratified' }],
    });
    expect(d.active).toBe(false);
    expect(d.reason).toBe('fs-not-declared');
  });

  it('capabilities.fs truthy but not boolean (e.g. 1) → inactive (fs-not-declared)', () => {
    const d = resolveNativeFsGate(true, bridge, {
      capabilities: { fs: 1 as unknown as boolean },
      excluded: [{ portion: 'fs', status: 'ratified' }],
    });
    expect(d.active).toBe(false);
    expect(d.reason).toBe('fs-not-declared');
  });
});

// Housekeeping: the gate cache is reset here so an import-order surprise
// cannot leak across the (separate) seam tests.
beforeAll(() => {
  __resetNativeFsGateForTests();
});
