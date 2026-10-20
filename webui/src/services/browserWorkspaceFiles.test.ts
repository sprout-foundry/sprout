/**
 * Tests for the in-browser workspace file API (the host contract's
 * move-to-container seam).
 *
 * The two public functions are capability-gated and read the in-browser
 * WASM shell's VFS. The browser-git checkout comparison and the adapter
 * (which supplies the shell) are mocked so the service's own gating,
 * enumeration and read logic can be exercised in isolation. The host is set
 * through the real active-host accessor (the same channel the app entry
 * uses), so the gate reads a genuine host.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { setActiveHost } from '../host/accessor';
import type { HostAgentBackend, SproutHost } from '../host/types';
import {
  listBrowserWorkspaceFiles,
  readBrowserWorkspaceFile,
  BrowserFilesUnavailableError,
} from './browserWorkspaceFiles';
import type { WorkspaceFileEntry } from './browserWorkspaceFiles';
import type { WasmDirEntry, WasmShell } from './wasmShell';

// ── Mocks (hoisted — must resolve before the service's imports) ─────────
//
// gitCheckoutDiffers comes from browserGit, whose module top level pulls in
// isomorphic-git + lightning-fs; the mock stands in for it so importing the
// service never touches IndexedDB. getAdapter comes from apiAdapter; mocking
// it (rather than using installAdapter's persistent singleton) lets each test
// control the adapter independently, without module-state leaking across
// cases. vi.mock/vi.hoisted are hoisted above the imports above, so the
// service loads against the mocks.

const mockGitCheckoutDiffers = vi.hoisted(() => vi.fn());
vi.mock('./browserGit', () => ({
  gitCheckoutDiffers: (...args: unknown[]) => mockGitCheckoutDiffers(...args),
}));

const mockGetAdapter = vi.hoisted(() => vi.fn(() => null));
vi.mock('./apiAdapter', () => ({
  getAdapter: (...args: unknown[]) => mockGetAdapter(...args),
  installAdapter: vi.fn(),
  hasAdapter: vi.fn(() => false),
  requiresBackendHealthCheck: vi.fn(() => false),
  ADAPTER_INSTALLED_EVENT: 'sprout:adapter-installed',
}));

// ── Fakes ────────────────────────────────────────────────────────────

const ROOT = '/workspace';
const MODE_FILE = 0o644;

/** A minimal WasmShell: enough for the service (listDir/read/bytes/root). */
function makeShell(
  opts: {
    root?: string;
    tree?: Record<string, WasmDirEntry[]>;
    bytes?: Record<string, Uint8Array>;
    text?: Record<string, string>;
    brokenListDir?: boolean;
    withBytes?: boolean;
  } = {},
): WasmShell {
  const root = opts.root ?? ROOT;
  const listDir = (path: string): { entries: WasmDirEntry[]; error?: string } => {
    if (opts.brokenListDir) return { entries: [], error: 'O_DIRECTORY' };
    return { entries: opts.tree?.[path] ?? [] };
  };
  const readFile = (path: string): { content: string; error?: string } => {
    const content = opts.text?.[path];
    return content === undefined ? { content: '', error: 'no such file' } : { content };
  };
  const readFileBytes = (path: string): { bytes?: Uint8Array; error?: string } => {
    const bytes = opts.bytes?.[path];
    return bytes ? { bytes } : { error: 'no such file' };
  };
  const shell = {
    getWorkspaceRoot: () => root,
    getCwd: () => root,
    listDir,
    readFile,
    get wasm() {
      return undefined;
    },
  } as Record<string, unknown>;
  if (opts.withBytes !== false) {
    shell.readFileBytes = readFileBytes;
  }
  return shell as unknown as WasmShell;
}

/** The adapter the service reads the shell through. */
function installAdapterWithShell(shell: WasmShell | null): void {
  if (shell === null) {
    mockGetAdapter.mockReturnValue(null);
    return;
  }
  mockGetAdapter.mockReturnValue({ getWasmShell: () => shell });
}

/** A full host, with every required capability flag present. */
function makeHost(overrides: { browserFiles?: boolean; agent?: HostAgentBackend } = {}): SproutHost {
  return {
    transport: {
      apiBaseURL: '',
      wsURL: '',
      authMode: 'bearer',
      ...(overrides.agent !== undefined ? { agent: overrides.agent } : { agent: { kind: 'wasm', modelEndpoint: '' } }),
    },
    navigation: { open() {} },
    notifications: { post() {} },
    capabilities: {
      ssh: false,
      git: true,
      chat: true,
      workspaceSwitching: false,
      folderPicker: false,
      export: false,
      instances: true,
      localTerminal: false,
      settings: true,
      automations: false,
      agentChanges: false,
      preview: false,
      mcp: false,
      localModels: false,
      verification: false,
      serverGit: false,
      chatSessions: false,
      browserFiles: overrides.browserFiles ?? true,
    },
  };
}

const enc = new TextEncoder();

describe('listBrowserWorkspaceFiles', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockGitCheckoutDiffers.mockResolvedValue(new Map());
    mockGetAdapter.mockReturnValue(null);
  });

  it('lists the project files with size, mode and the checkout-differs flag', async () => {
    const shell = makeShell({
      tree: {
        [ROOT]: [
          { name: 'src', type: 'dir', size: 0, mode: 0 },
          { name: 'README.md', type: 'file', size: 5, mode: MODE_FILE },
        ],
        [`${ROOT}/src`]: [{ name: 'a.ts', type: 'file', size: 10, mode: MODE_FILE }],
      },
    });
    installAdapterWithShell(shell);
    setActiveHost(makeHost());
    mockGitCheckoutDiffers.mockResolvedValue(
      new Map<string, boolean>([
        ['src/a.ts', true], // edited
        ['README.md', false], // identical to the checkout
      ]),
    );

    const result = await listBrowserWorkspaceFiles();
    const byPath = new Map<string, WorkspaceFileEntry>(result.map((e) => [e.path, e]));
    expect(result).toHaveLength(2);
    expect(byPath.get('src/a.ts')).toEqual({
      path: 'src/a.ts',
      size: 10,
      mode: MODE_FILE,
      differsFromCheckout: true,
    });
    expect(byPath.get('README.md')).toEqual({
      path: 'README.md',
      size: 5,
      mode: MODE_FILE,
      differsFromCheckout: false,
    });
    // The helper was asked about exactly the listed paths.
    expect(mockGitCheckoutDiffers).toHaveBeenCalledWith(['README.md', 'src/a.ts']);
  });

  it('defaults a missing checkout-differs entry to true (carry it)', async () => {
    const shell = makeShell({
      tree: { [ROOT]: [{ name: 'x.txt', type: 'file', size: 1, mode: MODE_FILE }] },
    });
    installAdapterWithShell(shell);
    setActiveHost(makeHost());
    mockGitCheckoutDiffers.mockResolvedValue(new Map()); // no entry for x.txt

    const result = await listBrowserWorkspaceFiles();
    expect(result).toEqual([{ path: 'x.txt', size: 1, mode: MODE_FILE, differsFromCheckout: true }]);
  });
});

describe('readBrowserWorkspaceFile', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockGetAdapter.mockReturnValue(null);
  });

  it('reads a file byte-exact when the binary exposes a byte reader', async () => {
    const payload = enc.encode('binary-bytes\x00\xff');
    const shell = makeShell({ bytes: { [`${ROOT}/src/a.ts`]: payload } });
    installAdapterWithShell(shell);
    setActiveHost(makeHost());

    const bytes = await readBrowserWorkspaceFile('src/a.ts');
    expect(Array.from(bytes)).toEqual(Array.from(payload));
  });

  it('falls back to the text reader (re-encoded) when there is no byte reader', async () => {
    const shell = makeShell({ text: { [`${ROOT}/src/a.ts`]: 'hello a.ts' }, withBytes: false });
    installAdapterWithShell(shell);
    setActiveHost(makeHost());

    const bytes = await readBrowserWorkspaceFile('src/a.ts');
    expect(new TextDecoder().decode(bytes)).toBe('hello a.ts');
  });

  it('refuses a runtime-dir path', async () => {
    installAdapterWithShell(makeShell());
    setActiveHost(makeHost());
    await expect(readBrowserWorkspaceFile('/home/user/.sprout/config')).rejects.toThrow(/runtime path/i);
  });

  it('refuses a path that escapes the workspace root', async () => {
    installAdapterWithShell(makeShell());
    setActiveHost(makeHost());
    await expect(readBrowserWorkspaceFile('/etc/passwd')).rejects.toThrow(/outside the workspace/i);
  });

  it('rejects an unknown file with an error naming the path', async () => {
    installAdapterWithShell(makeShell());
    setActiveHost(makeHost());
    await expect(readBrowserWorkspaceFile('src/missing.ts')).rejects.toThrow(/src\/missing\.ts/);
  });
});

describe('the capability gate', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockGitCheckoutDiffers.mockResolvedValue(new Map());
    mockGetAdapter.mockReturnValue(null);
  });

  it('rejects when the capability is off', async () => {
    installAdapterWithShell(makeShell());
    setActiveHost(makeHost({ browserFiles: false }));
    await expect(listBrowserWorkspaceFiles()).rejects.toMatchObject({ code: 'browser_files_unavailable' });
    await expect(readBrowserWorkspaceFile('src/a.ts')).rejects.toMatchObject({
      code: 'browser_files_unavailable',
    });
  });

  it('rejects when the agent is not the in-browser agent', async () => {
    installAdapterWithShell(makeShell());
    setActiveHost(makeHost({ agent: { kind: 'daemon', apiBaseURL: '', wsURL: '' } }));
    await expect(listBrowserWorkspaceFiles()).rejects.toMatchObject({ code: 'browser_files_unavailable' });
  });

  it('rejects when the browser shell is not reachable (no adapter)', async () => {
    mockGetAdapter.mockReturnValue(null); // the adapter never supplied a shell
    setActiveHost(makeHost()); // capability on, wasm agent
    await expect(listBrowserWorkspaceFiles()).rejects.toThrow(BrowserFilesUnavailableError);
    await expect(readBrowserWorkspaceFile('src/a.ts')).rejects.toThrow(BrowserFilesUnavailableError);
  });

  it('works when the capability is on, the agent is wasm and a shell is present', async () => {
    const shell = makeShell({ tree: { [ROOT]: [{ name: 'a.ts', type: 'file', size: 1, mode: MODE_FILE }] } });
    installAdapterWithShell(shell);
    setActiveHost(makeHost());
    mockGitCheckoutDiffers.mockResolvedValue(new Map([['a.ts', true]]));

    const result = await listBrowserWorkspaceFiles();
    expect(result.map((e) => e.path)).toEqual(['a.ts']);
  });
});
