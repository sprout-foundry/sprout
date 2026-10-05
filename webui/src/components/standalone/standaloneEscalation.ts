/**
 * Standalone escalation boot — the editor/terminal pages' wiring for the
 * cloud-escalation path (the embedded scenario).
 *
 * The full webui wires two things in useAppInitialization that the standalone
 * pages historically lacked, which made escalation from an embedded editor
 * useless: (1) the browser-git VFS bridge, so a txn push manifest carries the
 * visitor's actual files, and (2) the agent escalation bridge, so exit-127
 * commands can run in the user's cloud workspace at all.
 *
 * Both need three inputs the standalone pages don't have on their own:
 * the WASM shell (passed in by the caller after initWasmShell resolves), the
 * active repo URL (the ?repo= query param — the same contract the full app's
 * activeRepo service uses), and the user identity for git commits (unknown
 * in an embedded page — the 'Browser IDE' fallback the full app also uses).
 *
 * Consent for escalation runs through the host: a `sprout-host` `confirm`
 * request over postMessage, with a 30s timeout counting as decline. Hosts
 * that don't answer keep the default-safe behavior (nothing runs).
 */
import { setActiveRepoURL } from './standaloneRepo';
import { installEscalationBridge, type ConsentDecision } from '../../services/agentEscalation';
import { configureBrowserGit } from '../../services/browserGit';
import { trackFileWrite } from '../../services/vfsFiles';
import { isFromTrustedParent, postTargetOrigin } from './standaloneOrigin';
import type { WasmShell } from '../../services/wasmShell';

/** Consent asks the host and time out rather than hanging the agent loop. */
const CONSENT_TIMEOUT_MS = 30_000;

export interface StandaloneEscalationBoot {
  /** The repo URL escalation will resolve workspaces against ('' when none). */
  repoURL: string;
}

/**
 * Read the repo URL the host opened this page with: ?repo=<url>, the same
 * param bootstrapAdapter honors in the full app. Also records it as the
 * active repo so the platform-side flows agree on one answer.
 */
export function repoURLFromLocation(): string {
  try {
    const repo = new URLSearchParams(window.location.search).get('repo');
    const url = repo?.trim() ?? '';
    if (url) setActiveRepoURL(url);
    return url;
  } catch {
    return '';
  }
}

/**
 * Wire the VFS bridge + escalation bridge for a standalone page.
 * Call once, after initWasmShell resolves and before (or right after) the
 * `ready` postMessage — escalation cannot run until both are in place.
 */
export function bootStandaloneEscalation(shell: WasmShell): StandaloneEscalationBoot {
  const repoURL = repoURLFromLocation();

  // 1. The VFS bridge: browser-git (clone/push/commit UI paths) and the txn
  //    push manifest both read the visitor's files through here.
  configureBrowserGit({
    name: 'Browser IDE',
    email: 'browser-ide@sprout.dev',
    readVfsFiles: async () => {
      const { listAllVfsFiles } = await import('../../services/vfsFiles');
      return listAllVfsFiles(shell);
    },
    writeVfsFiles: async (files) => {
      for (const f of files) {
        const err = shell.writeFile(f.path, f.content);
        if (err) throw new Error(`write ${f.path}: ${err}`);
        trackFileWrite(f.path);
      }
    },
    deleteVfsFiles: async (paths) => {
      for (const p of paths) {
        shell.deleteFile(p);
      }
    },
  });

  // 2. The escalation bridge: exit-127 commands ask the host (postMessage),
  //    run transactionally in the user's workspace, pull deltas back into
  //    the VFS the editor is showing.
  installEscalationBridge({
    repoURL: repoURL || undefined,
    requestConsent: (command) =>
      new Promise<ConsentDecision>((resolve) => {
        let settled = false;
        const done = (decision: ConsentDecision) => {
          if (settled) return;
          settled = true;
          window.clearTimeout(timer);
          window.removeEventListener('message', onMessage);
          resolve(decision);
        };
        const timer = window.setTimeout(() => done('deny'), CONSENT_TIMEOUT_MS);
        const onMessage = (ev: MessageEvent) => {
          if (!isFromTrustedParent(ev)) return;
          const data = ev.data;
          if (!data || data.source !== 'sprout-host' || data.type !== 'confirmResult') return;
          done(data.decision === 'always' ? 'always' : data.decision === 'once' ? 'once' : 'deny');
        };
        window.addEventListener('message', onMessage);
        window.parent?.postMessage({ source: 'sprout-editor', type: 'confirm', command }, postTargetOrigin());
      }),
  });

  return { repoURL };
}
