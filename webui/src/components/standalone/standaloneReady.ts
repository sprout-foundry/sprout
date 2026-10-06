/**
 * standaloneReady — the capabilities handshake for the standalone pages.
 *
 * The page's first message (`ready`) tells the host WHAT it is talking to
 * and WHAT it supports, so hosts feature-detect instead of hardcoding:
 * sent commands the page can't execute are dead UI; the version pins
 * support triage without the host guessing from behavior.
 *
 * Capabilities are static per page type, except escalation, which is only
 * claimed when the cloud-escalation boot succeeded (bridge installed).
 */

import type { WasmShell } from '../../services/wasmShell';

export type StandalonePageKind = 'editor' | 'terminal';

/** What a `ready` message tells the host. Flows as the message payload. */
export interface StandaloneReady {
  /** The running WASM binary's release identity (null on source builds without git). */
  build: { version: string; commit: string; date: string } | null;
  /** The protocol messages this page handles. */
  capabilities: string[];
  /** The page kind — hosts embedding both can route by this. */
  page: StandalonePageKind;
  [key: string]: unknown;
}

/** Static capability sets. Keep in sync with the pages' message switches. */
const EDITOR_CAPABILITIES = [
  'open', // host → page: swap the edited document
  'setDoc', // host → page: programmatic content edit
  'save', // host → page: save the current doc
  'theme', // host → page: editor colors
  'put', // host → page: write a file into the VFS
  'confirm', // page → host: escalation consent request (host must answer confirmResult)
  'dirty', // page → host
  'save-event', // page → host ({type:'save', path, content})
  'ready', // page → host
];
const TERMINAL_CAPABILITIES = [
  'run', // host → page: execute a command line
  'put', // host → page: write a file into the VFS
  'confirm', // page → host: escalation consent request (host must answer confirmResult)
  'command', // page → host ({command, exitCode})
  'ready', // page → host
];

/** The ready payload for a page. */
export function buildReadyPayload(page: StandalonePageKind, shell: WasmShell | null): StandaloneReady {
  return {
    build: shell?.getBuildInfo() ?? null,
    capabilities: page === 'editor' ? EDITOR_CAPABILITIES : TERMINAL_CAPABILITIES,
    page,
  };
}
