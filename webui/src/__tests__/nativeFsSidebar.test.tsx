// @vitest-environment jsdom
/**
 * R-2w: SidebarFilesSection `onFetchFiles` branch-selection tests.
 *
 * We mount the REAL `SidebarFilesSection` component but mock its heavy/heavy
 * leaf dependencies so we can capture the `onFetchFiles` prop that it hands to
 * `<FileTree />` (from `@sprout/ui`) and drive it directly. This proves the
 * branch selection end-to-end:
 *
 *   - gate ACTIVE (compile-time flag on + a capable bridge + ratified fs)
 *       → `onFetchFiles` routes through the bridge's `listWorkspace`,
 *         maps via `mapWorkspaceListing`, and does NOT call `clientFetch`.
 *   - gate INACTIVE (default build, env unset)
 *       → `onFetchFiles` falls back to `clientFetch('/api/files?path=...')`
 *         and does NOT call the bridge's `listWorkspace`.
 *
 * The compile-time flag is controlled exactly like the stub-routing tests:
 * `vi.stubEnv('VITE_SPROUT_NATIVE_FS','1')` + `vi.resetModules()` + a FRESH
 * dynamic import of the component, so the `NATIVE_FS_ENABLED` constant baked
 * into that import reflects the env. The gate promise cache is reset via
 * `__resetNativeFsGateForTests()` per case.
 *
 * Why we mount the component at all (rather than just testing the helpers):
 * the helpers (`mapWorkspaceListing`, `workspaceListDepth`, `nativeFsGate`)
 * are covered by nativeFsDeferral.test.ts. The thing this file uniquely
 * verifies is the BRANCH SELECTION in `SidebarFilesSection.onFetchFiles`
 * (NATIVE_FS_ENABLED gate → bridge listWorkspace path vs. clientFetch
 * fallback path), which only exists in the component.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, cleanup, act } from '@testing-library/react';

// ── Mock the FileTree child so we can capture the onFetchFiles prop ─────────
//
// `vi.hoisted` gives us module-scoped state the (hoisted) factory can reach.
// The mock FileTree records each render's props; the LAST entry is what the
// most recent mount produced.

/**
 * The mock FileTree captures the most recent `onFetchFiles` prop by attaching
 * it to the rendered DOM node (a `data-captured-onfetch` slot). Attaching to
 * the DOM node — rather than a `globalThis` slot — guarantees the test body
 * reads the SAME object the factory wrote: vitest gives the `vi.mock` factory
 * a module scope whose `globalThis` can diverge from the test body's after
 * `vi.resetModules()`, but the DOM tree is shared across the whole jsdom
 * environment.
 */
function getCapturedOnFetchFiles(): (path: string) => Promise<unknown> {
  const node = screen.getByTestId('mock-filetree') as HTMLElement & {
    __capturedOnFetch?: (p: string) => Promise<unknown>;
  };
  expect(
    node.__capturedOnFetch,
    'onFetchFiles prop must have been captured on the FileTree node',
  ).toBeTruthy();
  return node.__capturedOnFetch!;
}

vi.mock('@sprout/ui', () => {
  // The factory runs in vitest's isolated module scope, so it cannot reference
  // top-level `React` — pull it from require('react') which resolves to the
  // same React instance the test env uses (dedupe'd to a single copy).
  const { createElement } = require('react');
  return {
    FileTree: (props: Record<string, unknown>) => {
      const ref = (node: HTMLElement | null) => {
        if (node) {
          (node as HTMLElement & { __capturedOnFetch?: unknown }).__capturedOnFetch =
            props.onFetchFiles;
        }
      };
      return createElement('div', { 'data-testid': 'mock-filetree', ref });
    },
  };
});

// ── Mock clientFetch so the fallback path is fully controllable ─────────────

const clientFetchMock = vi.hoisted(() => vi.fn());
vi.mock('../services/clientSession', () => ({
  clientFetch: (...args: unknown[]) => clientFetchMock(...args),
  appendClientIdToUrl: (u: string) => u,
  getProxyBase: () => '',
  getWebUIClientId: () => 'test-client',
  resolveWebUIClientId: async () => 'test-client',
}));

// ── Mock ApiService so getInstance() is trivially safe ──────────────────────

vi.mock('../services/api', () => ({
  ApiService: {
    getInstance: () => ({
      createItem: vi.fn().mockResolvedValue({}),
      deleteItem: vi.fn().mockResolvedValue({}),
      renameItem: vi.fn().mockResolvedValue({}),
      openInFileBrowser: vi.fn().mockResolvedValue({}),
    }),
  },
}));

// ── Bridge + flag helpers (shared with the stub-routing tests) ──────────────

function makeBridge(opts: {
  capabilities?: {
    capabilities: Record<string, boolean>;
    excluded: Array<Record<string, unknown>>;
  };
  list?: (maxDepth?: number) => Promise<unknown>;
} = {}): {
  getCapabilities: ReturnType<typeof vi.fn>;
  readWorkspaceFile: ReturnType<typeof vi.fn>;
  writeWorkspaceFile: ReturnType<typeof vi.fn>;
  listWorkspace: ReturnType<typeof vi.fn>;
} {
  const caps = opts.capabilities ?? {
    capabilities: { fs: true },
    excluded: [{ portion: 'fs', status: 'ratified' }],
  };
  return {
    getCapabilities: vi.fn(async () => ({
      schemaVersion: 1,
      capabilities: caps.capabilities,
      excluded: caps.excluded,
      manifestPresent: true,
      servable: true,
    })),
    readWorkspaceFile: vi.fn(async (p: string) => ({ ok: true, path: p, content: 'x' })),
    writeWorkspaceFile: vi.fn(async (p: string) => ({ ok: true, path: p })),
    listWorkspace: vi.fn(
      async (maxDepth?: number) =>
        opts.list
          ? opts.list(maxDepth)
          : { ok: true, files: [] as Array<{ path: string; size: number; isDir: boolean }> },
    ),
  };
}

function installBridge(bridge: unknown): void {
  (window as unknown as { SproutStudio?: unknown }).SproutStudio = bridge;
}
function clearBridge(): void {
  delete (window as unknown as { SproutStudio?: unknown }).SproutStudio;
}

type SidebarModule = typeof import('../components/SidebarFilesSection');
let Sidebar: SidebarModule['default'];

/** Fresh import of the component with the given compile-time flag value. */
async function loadComponent(enabled: boolean): Promise<ReturnType<typeof import('../components/SidebarFilesSection')>> {
  if (enabled) vi.stubEnv('VITE_SPROUT_NATIVE_FS', '1');
  else vi.stubEnv('VITE_SPROUT_NATIVE_FS', '');
  vi.resetModules();
  const mod = (await import('../components/SidebarFilesSection')) as unknown as SidebarModule;
  return mod.default;
}

beforeEach(() => {
  vi.unstubAllEnvs();
  clearBridge();
  clientFetchMock.mockReset();
});

// ── 1. Gate active → bridge listWorkspace path (root) ───────────────────────

describe('onFetchFiles — gate ACTIVE routes through the bridge', () => {
  it('resolves the sorted listing, calls listWorkspace(1), and never calls clientFetch', async () => {
    Sidebar = await loadComponent(true);

    const files = [
      { path: 'b.txt', size: 2, isDir: false },
      { path: 'a', size: 0, isDir: true },
      { path: 'z.md', size: 1, isDir: false },
    ];
    const listCalls: number[] = [];
    const bridge = makeBridge({
      list: (maxDepth) => {
        listCalls.push(maxDepth ?? 0);
        return Promise.resolve({ ok: true, files });
      },
    });
    installBridge(bridge);

    render(<Sidebar onFileClick={() => {}} workspaceRoot="/ws" />);
    expect(screen.getByTestId('mock-filetree')).toBeTruthy();

    const result = await act(async () => (await getCapturedOnFetchFiles())('.'));

    // Dirs first, then by name.
    expect(result.map((f: { name: string; isDir: boolean }) => [f.name, f.isDir])).toEqual([
      ['a', true],
      ['b.txt', false],
      ['z.md', false],
    ]);

    // maxDepth for the root is 1.
    expect(listCalls).toEqual([1]);
    // The client path must NOT have been touched.
    expect(clientFetchMock).not.toHaveBeenCalled();
  });

  it('nested path "src" → listWorkspace(2), filtered to direct children of src (dirs first)', async () => {
    Sidebar = await loadComponent(true);

    const files = [
      { path: 'src/main.go', size: 5, isDir: false },
      { path: 'src/deep/nested.go', size: 6, isDir: false },
      { path: 'src/sub', size: 0, isDir: true },
      { path: 'other.go', size: 1, isDir: false },
    ];
    const listCalls: number[] = [];
    const bridge = makeBridge({
      list: (maxDepth) => {
        listCalls.push(maxDepth ?? 0);
        return Promise.resolve({ ok: true, files });
      },
    });
    installBridge(bridge);

    render(<Sidebar onFileClick={() => {}} workspaceRoot="/ws" />);
    expect(screen.getByTestId('mock-filetree')).toBeTruthy();

    const result = await act(async () => (await getCapturedOnFetchFiles())('src'));

    // Direct children of src/ are depth-2 entries prefixed 'src/':
    // 'src/main.go' (file) and 'src/sub' (dir). 'src/deep/nested.go' is
    // depth 3 (excluded); 'other.go' is not under src (excluded).
    // Sorted dirs-first: 'sub' before 'main.go'.
    expect(result.map((f: { name: string; isDir: boolean }) => [f.name, f.isDir])).toEqual([
      ['sub', true],
      ['main.go', false],
    ]);
    // A depth-2 requested path needs maxDepth 2 (one level below it).
    expect(listCalls).toEqual([2]);
    expect(clientFetchMock).not.toHaveBeenCalled();
  });

  it('gate active but listing errors → resolves to [] (mapWorkspaceListing degrades)', async () => {
    Sidebar = await loadComponent(true);

    const bridge = makeBridge({
      list: () => Promise.resolve({ ok: false, error: 'workspaceNotSet' }),
    });
    installBridge(bridge);

    render(<Sidebar onFileClick={() => {}} workspaceRoot="/ws" />);
    expect(screen.getByTestId('mock-filetree')).toBeTruthy();
    const result = await act(async () => (await getCapturedOnFetchFiles())('.'));

    // NOTE ON ACTUAL BEHAVIOR: `mapWorkspaceListing` returns `[]` (NOT a
    // throw) for a non-ok result — a failed native listing degrades to an
    // empty file tree rather than rejecting. So onFetchFiles RESOLVES to [].
    expect(result).toEqual([]);
  });
});

// ── 3. Gate inactive (default build) → clientFetch fallback ─────────────────

describe('onFetchFiles — gate INACTIVE falls back to clientFetch', () => {
  it('default build (env unset): clientFetch("/api/files?path=.") runs, listWorkspace not called', async () => {
    Sidebar = await loadComponent(false);

    // A fully-capable bridge would pass the gate in a --native-fs build, but
    // the compile-time flag is off, so the gate is dead → fallback path.
    const bridge = makeBridge();
    installBridge(bridge);

    clientFetchMock.mockResolvedValue(
      new Response(
        JSON.stringify({
          message: 'success',
          files: [
            { name: 'c.txt', path: 'c.txt', size: 3, isDir: false },
            { name: 'a', path: 'a', size: 0, isDir: true },
            { name: 'b', path: 'b', size: 1, isDir: false },
          ],
        }),
        { status: 200, headers: { 'Content-Type': 'application/json' } },
      ),
    );

    render(<Sidebar onFileClick={() => {}} workspaceRoot="/ws" />);
    expect(screen.getByTestId('mock-filetree')).toBeTruthy();
    const result = await act(async () => (await getCapturedOnFetchFiles())('.'));

    // Mapped + sorted as today: dirs first, then by name.
    expect(result.map((f: { name: string; isDir: boolean }) => [f.name, f.isDir])).toEqual([
      ['a', true],
      ['b', false],
      ['c.txt', false],
    ]);

    // The clientFetch path ran with the encoded path; the bridge listing did NOT.
    expect(clientFetchMock).toHaveBeenCalledTimes(1);
    expect(String(clientFetchMock.mock.calls[0][0])).toContain('/api/files?path=');
    expect(bridge.listWorkspace).not.toHaveBeenCalled();
  });
});

// ── Housekeeping ────────────────────────────────────────────────────────────

describe('teardown', () => {
  it('cleans up the render', () => {
    cleanup();
  });
});