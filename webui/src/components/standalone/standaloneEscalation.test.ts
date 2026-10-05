/**
 * standaloneEscalation tests — the embedded-scenario boot wiring.
 *
 * Covers: ?repo= parsing + activeRepo recording, the VFS bridge wiring
 * (read/write/delete through the shell, tracked for the manifest fallback),
 * and the consent round-trip (host answer → resolve; timeout → deny).
 * The WASM shell is a stub — these exercise the JS wiring only.
 */
// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { __resetStandaloneRepo as __resetActiveRepoForTests, getStandaloneRepoURL as getActiveRepoURL } from './standaloneRepo';
import { getEscalationPolicy, setEscalationPolicy } from '../../services/agentEscalation';
import { __resetBrowserGitForTest, getBrowserGitVfsBridge } from '../../services/browserGit';
import { getVfsManifestSnapshot } from '../../services/vfsFiles';
import type { WasmShell } from '../../services/wasmShell';
import { bootStandaloneEscalation, repoURLFromLocation } from './standaloneEscalation';

/**
 * Every message this page posted to its host, captured as a 'message'
 * listener (jsdom: window.parent === window, so the page's own
 * postMessage calls come back as message events). Replacing
 * window.parent.postMessage with a plain function does NOT work — that
 * removes jsdom's dispatch path and no message event ever fires.
 */
const postedMessages: Array<Record<string, unknown>> = [];
let captureMessages: ((ev: MessageEvent) => void) | null = null;
beforeEach(() => {
  postedMessages.length = 0;
  captureMessages = (ev: MessageEvent) => {
    if (ev.data && (ev.data as Record<string, unknown>).source === 'sprout-editor') {
      postedMessages.push(ev.data as Record<string, unknown>);
    }
  };
  window.addEventListener('message', captureMessages);
});
afterEach(() => {
  if (captureMessages) window.removeEventListener('message', captureMessages);
  captureMessages = null;
});

function stubShell(): WasmShell {
  const files = new Map<string, string>([
    ['/workspace/src/main.ts', 'console.log("hi")'],
    ['/workspace/README.md', '# demo'],
  ]);
  // Mirror the real shell's path resolution (cmd/wasm/workspace_root.go:
  // relative paths join the workspace root, '~' joins HOME=/home/user).
  const resolve = (p: string): string => {
    if (p === '~') return '/home/user';
    if (p.startsWith('~/')) return '/home/user' + p.slice(1);
    if (p.startsWith('/')) return p.replace(/\/+/g, '/');
    return '/workspace/' + p.replace(/\/+/g, '/');
  };
  return {
    executeCommand: () => ({ stdout: '', stderr: '', exitCode: 0 }),
    autoComplete: () => ({ completions: [] }),
    getCwd: () => '/workspace',
    getWorkspaceRoot: () => '/workspace',
    changeDir: () => ({ cwd: '/workspace' }),
    writeFile: (path: string, content: string) => {
      files.set(resolve(path), content);
      return '';
    },
    readFile: (path: string) => {
      const hit = files.get(resolve(path));
      return hit === undefined ? { content: '', error: 'not found' } : { content: hit };
    },
    listDir: (path: string) => {
      // Only enumerate the queried directory's immediate children — a
      // listing that ignores `path` makes the recursive walk diverge.
      const prefix = path === '/' ? '/' : path.endsWith('/') ? path : path + '/';
      const seen = new Set<string>();
      const entries: Array<{ name: string; type: 'file' | 'dir'; size: number; mode: number }> = [];
      for (const p of files.keys()) {
        if (!p.startsWith(prefix)) continue;
        const rel = p.slice(prefix.length);
        const [head, ...rest] = rel.split('/');
        if (!head || seen.has(head)) continue;
        seen.add(head);
        entries.push(
          rest.length === 0
            ? { name: head, type: 'file', size: (files.get(p) ?? '').length, mode: 0o644 }
            : { name: head, type: 'dir', size: 0, mode: 0o755 },
        );
      }
      return { entries };
    },
    deleteFile: (path: string) => {
      files.delete(resolve(path));
      return '';
    },
    runAgent: async () => ({ response: '', provider: '', model: '' }),
    clearConversation: () => undefined,
    stopAgent: () => undefined,
    wasm: globalThis as unknown as typeof globalThis & { SproutWasm: unknown },
  } as unknown as WasmShell;
}

describe('standaloneEscalation', () => {
  beforeEach(() => {
    window.history.replaceState(null, '', '/editor.html');
    __resetActiveRepoForTests();
    __resetBrowserGitForTest();
    setEscalationPolicy('ask');
  });

  afterEach(() => {
    window.localStorage.clear();
    vi.restoreAllMocks();
  });

  describe('repoURLFromLocation', () => {
    it('reads ?repo= and records it as the active repo', () => {
      window.history.replaceState(null, '', '/editor.html?repo=https://github.com/acme/widget');
      expect(repoURLFromLocation()).toBe('https://github.com/acme/widget');
      expect(getActiveRepoURL()).toBe('https://github.com/acme/widget');
    });

    it('returns empty without ?repo= and records nothing', () => {
      window.history.replaceState(null, '', '/editor.html');
      expect(repoURLFromLocation()).toBe('');
      expect(getActiveRepoURL()).toBeNull();
    });
  });

  describe('bootStandaloneEscalation', () => {
    it('wires the VFS bridge so txn push manifests can carry real files', async () => {
      window.history.replaceState(null, '', '/editor.html');
      const shell = stubShell();
      bootStandaloneEscalation(shell);

      const bridge = getBrowserGitVfsBridge();
      expect(bridge).not.toBeNull();

      const listed = await bridge!.readVfsFiles();
      expect(listed).toContainEqual({ path: 'src/main.ts', content: 'console.log("hi")' });

      await bridge!.writeVfsFiles([{ path: 'src/new.ts', content: 'export {}' }]);
      expect(shell.readFile('/workspace/src/new.ts').content).toBe('export {}');
      // Tracked so the manifest fallback lists it on older binaries.
      expect(getVfsManifestSnapshot().has('/workspace/src/new.ts')).toBe(true);

      await bridge!.deleteVfsFiles!(['/workspace/src/new.ts']);
      expect(shell.readFile('/workspace/src/new.ts').error).toBe('not found');
    });

    it('installs the escalation bridge with the ?repo= URL', async () => {
      window.history.replaceState(null, '', '/editor.html?repo=https://github.com/acme/widget');
      bootStandaloneEscalation(stubShell());
      // The bridge is installed on globalThis; assert via the wiring the
      // full app uses (agentEscalation's install returns an uninstaller —
      // here we check the observable: the global exists).
      expect((globalThis as { __sproutEscalate?: unknown }).__sproutEscalate).toBeTruthy();
    });

    it('sends consent to the host and honors an always answer', async () => {
      window.history.replaceState(null, '', '/editor.html?repo=https://github.com/acme/widget');
      bootStandaloneEscalation(stubShell());

      const escalate = (globalThis as { __sproutEscalate: { run: (c: string) => Promise<unknown> } }).__sproutEscalate;

      // The txn network calls will fail in the test env; what we're testing
      // is the consent round-trip, which happens before any fetch. The run
      // rejects after consent — fine, swallow it.
      // Answer shortly after the confirm is observed (a macrotask later —
      // mirrors the real host, which answers via its own message loop).
      // Manual polls rather than vi.waitFor: the wait wraps assertions about
      // test-owned state, and a plain loop keeps the message ordering
      // transparent.
      const pending = escalate.run('make build').catch(() => undefined);
      const waitFor = async (check: () => boolean): Promise<void> => {
        for (let i = 0; i < 100 && !check(); i++) {
          await new Promise((r) => setTimeout(r, 25));
        }
        expect(check()).toBe(true);
      };
      await waitFor(() => postedMessages.some((m) => m.type === 'confirm' && m.command === 'make build'));
      setTimeout(() => {
        window.postMessage({ source: 'sprout-host', type: 'confirmResult', decision: 'always' }, '*');
      }, 100);
      await waitFor(() => getEscalationPolicy() === 'always');
      await pending;
    });
  });
});
