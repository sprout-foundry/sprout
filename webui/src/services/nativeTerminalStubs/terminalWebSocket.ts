/**
 * Track R (--native-terminal, R-3) stub for services/terminalWebSocket.ts.
 *
 * Vite's conditional alias (webui/vite.config.ts) swaps every import of
 * services/terminalWebSocket for this module when the dist build is invoked
 * with VITE_SPROUT_NATIVE_TERMINAL=1 (scripts/build-webui-dist.mjs
 * --native-terminal). The WASM/PTY terminal transport is provided natively by
 * the shell, so the real module (PTY WebSocket + ping/pong watchdog + session
 * persistence + freeze/resume registry) is hard-excluded from the bundle.
 *
 * This file is a no-op stand-in: it mirrors the REAL public signatures of
 * `TerminalWebSocketService` (copied from the real module so `tsc --noEmit`
 * passes in the --native-terminal build too) but every method is a safe
 * no-op that NEVER opens a WebSocket, NEVER logs, and never touches
 * localStorage. The type-only import of `WsEvent` is erased at compile time,
 * so it pulls no real code into the bundle.
 *
 * See docs/adr-0008-webui-native-seams.md (terminal seam) and
 * docs/WEBUI_DECOUPLING_AUDIT.md §1.2. In a --native-terminal dist the shell
 * owns the terminal session model, so the webui never connects a PTY itself.
 */

import type { WsEvent } from '@sprout/events';

type TerminalEventCallback = (event: WsEvent) => void;

/** Shared no-op singleton. The stub never opens a WebSocket; every method is
 *  a safe no-op with a return type matching the real module. */
class TerminalWebSocketService {
  private static instance: TerminalWebSocketService;
  /** Registry of live instances. In the native-terminal build nothing ever
   *  registers a real instance, so freezeAll / resumeAll are safe no-ops. */
  private static readonly instances = new Set<TerminalWebSocketService>();

  private constructor() {}

  // ── Static instance registry ────────────────────────────────────────────

  static registerInstance(_inst: TerminalWebSocketService): void {
    // no-op: the native shell owns the terminal; the webui never runs a live
    // PTY instance to register.
  }

  static unregisterInstance(_inst: TerminalWebSocketService): void {
    // no-op
  }

  static freezeAll(): void {
    // no-op: no live instances exist in a native-terminal build.
  }

  static resumeAll(): void {
    // no-op
  }

  /** Returns the shared no-op instance (a fresh independent instance would be
   *  meaningless — the terminal is provided natively, so all callers share the
   *  same inert stand-in). */
  static createInstance(): TerminalWebSocketService {
    return TerminalWebSocketService.getInstance();
  }

  static getInstance(): TerminalWebSocketService {
    if (!TerminalWebSocketService.instance) {
      TerminalWebSocketService.instance = new TerminalWebSocketService();
    }
    return TerminalWebSocketService.instance;
  }

  // ── Instance methods (all safe no-ops, matching the real return types) ───

  connect(): void {
    // no-op: the native shell owns the terminal session; no PTY is opened here.
  }

  disconnect(): void {
    // no-op
  }

  /** Returns an unsubscribe function so callers can treat it like a
   *  subscription; the stub never fires callbacks. */
  onEvent(_callback: TerminalEventCallback): () => void {
    return () => {};
  }

  removeEvent(_callback: TerminalEventCallback): void {
    // no-op
  }

  sendCommand(_command: string): boolean {
    return false;
  }

  sendRawInput(_input: string): boolean {
    return false;
  }

  sendResize(_cols: number, _rows: number): boolean {
    return false;
  }

  closeSession(): boolean {
    return false;
  }

  isReady(): boolean {
    return false;
  }

  setPreferredShell(_shell: string | null): void {
    // no-op
  }

  getSessionId(): string | null {
    return null;
  }

  getSessionIdForReattach(): string | null {
    return null;
  }

  clearPersistedSession(): void {
    // no-op
  }

  persistSessionId(): void {
    // no-op: session persistence is owned by the native shell.
  }

  restorePersistedSessionId(): string | null {
    return null;
  }

  clearPersistedSessionId(): void {
    // no-op
  }

  restoreSessionId(_id: string): void {
    // no-op
  }

  freeze(): void {
    // no-op
  }

  resume(): void {
    // no-op
  }

  isCurrentlyFrozen(): boolean {
    return false;
  }

  isReconnecting(): boolean {
    return false;
  }

  isConnectedToServer(): boolean {
    return false;
  }

  resetAndReconnect(): void {
    // no-op
  }
}

/**
 * Short, human-readable repr of raw PTY input for diagnostic logs. The real
 * module folds control chars / large pastes; the stub is a passthrough
 * identity (it is only ever called for log strings, which the stub never
 * emits). Type-compatible stand-in.
 */
export function reprInput(input: string): string {
  return input;
}

export { TerminalWebSocketService };
