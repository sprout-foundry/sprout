/**
 * Per-mode chat session pinning (SP-140-10c).
 *
 * Each workspace mode (Code, Design) pins its own chat session so a mode
 * switch restores that mode's conversation instead of sharing one chat
 * across modes (the dogfood gripe: "switching to Design mode still showed
 * my Code conversation"). Design mode with no pin starts a fresh
 * conversation — it never resurrects a Code session.
 *
 * The pin map is stored per instance + UI context, exactly like the
 * persisted workspace mode (`workspaceModeStorageKey`), as
 * `JSON { code?: string; design?: string }` under CHAT_MODE_PIN_STORAGE_KEY.
 *
 * Persistence is best-effort (private mode, quota, disabled storage): a
 * failed read/write degrades to the un-pinned defaults and never throws
 * into the shell.
 *
 * Wiring contract (what AppContent passes — SP-140-10c):
 * - `onSwitchSession` → the chat-session switch path (`onActiveChatChange`).
 * - `onFreshSession` → ONLY the create call (`onCreateChat`): creating a chat
 *   does not move the active chat, so `restoreFreshSession` performs the
 *   switch itself (create → switch → pin; pinning before the switch would
 *   leave the previous chat active and let its next send overwrite the pin).
 */

import { useCallback, useEffect, useRef } from 'react';
import { CHAT_MODE_PIN_STORAGE_KEY, INSTANCE_PID_STORAGE_KEY } from '../constants/app';
import type { WorkspaceModeId } from './registry';
import { readPersistedWorkspaceMode, uiContextScope } from './useWorkspaceMode';

/** Per-mode pinned session ids, as persisted. */
export interface ChatModePins {
  code?: string;
  design?: string;
}

/** Storage key for the current instance + UI context (mirrors workspaceModeStorageKey). */
export function chatModePinStorageKey(): string {
  if (typeof window === 'undefined' || !window.localStorage) {
    return `${CHAT_MODE_PIN_STORAGE_KEY}:default:local`;
  }
  const pid = window.localStorage.getItem(INSTANCE_PID_STORAGE_KEY) || 'default';
  return `${CHAT_MODE_PIN_STORAGE_KEY}:${pid}:${uiContextScope()}`;
}

/**
 * Read the pin map. Tolerant by design: absent or corrupt storage yields
 * the empty map (the un-pinned defaults) rather than a throw.
 */
export function readChatModePins(): ChatModePins {
  if (typeof window === 'undefined' || !window.localStorage) return {};
  try {
    const raw = window.localStorage.getItem(chatModePinStorageKey());
    if (!raw) return {};
    const parsed = JSON.parse(raw) as Partial<Record<WorkspaceModeId, unknown>>;
    const pins: ChatModePins = {};
    if (typeof parsed.code === 'string' && parsed.code) pins.code = parsed.code;
    if (typeof parsed.design === 'string' && parsed.design) pins.design = parsed.design;
    return pins;
  } catch {
    return {};
  }
}

/** The pin for a mode (undefined for modes without a pin slot yet). */
export function readPinFor(mode: WorkspaceModeId): string | undefined {
  if (mode === 'code') return readChatModePins().code;
  if (mode === 'design') return readChatModePins().design;
  return undefined;
}

/** Record `sessionId` as the pin for `mode`, merging with the stored map. Best-effort. */
export function writeChatModePin(mode: WorkspaceModeId, sessionId: string): void {
  if (typeof window === 'undefined' || !window.localStorage || !sessionId) return;
  if (mode !== 'code' && mode !== 'design') return; // future modes: no pin slot yet
  try {
    const pins = readChatModePins();
    if (mode === 'code') pins.code = sessionId;
    else pins.design = sessionId;
    window.localStorage.setItem(chatModePinStorageKey(), JSON.stringify(pins));
  } catch {
    // Storage unavailable — the pin simply doesn't survive a reload.
  }
}

export interface BootRestoreDecision {
  /** True when the persisted mode at boot is Design. */
  isDesignMode: boolean;
  /** The design-mode pin (null when never pinned). */
  designPin: string | null;
}

/**
 * The boot-path decision (SP-140-10c): a persisted Design mode restores the
 * design pin — or stays fresh when unpinned — instead of the cross-mode
 * "most recent non-empty" fallback. A persisted Code mode (or an unset
 * mode) keeps the existing behavior. Pure, so the init hook and the unit
 * tests share one source of truth.
 */
export function decideBootRestore(): BootRestoreDecision {
  const persisted = readPersistedWorkspaceMode();
  if (persisted !== 'design') return { isDesignMode: false, designPin: null };
  return { isDesignMode: true, designPin: readChatModePins().design ?? null };
}

export interface UseChatModePinningOptions {
  /** The active workspace-mode id. The hook reacts to CHANGES (never the mount). */
  mode: WorkspaceModeId;
  /** The currently active chat session id (null = unknown / none yet). */
  activeChatId: string | null;
  /**
   * The existing session-switch path (the switchChatSession flow). May return
   * `true` (switch landed) / `false` (failed or superseded); a `void` return
   * (fire-and-forget wiring) is treated as "assume landed."
   */
  onSwitchSession: (sessionId: string) => void | boolean | Promise<boolean>;
  /**
   * Create a fresh, empty chat session. Resolves the new session id, or null
   * when creation failed / is unsupported. Creating a chat does NOT move the
   * active chat — the hook switches to the created id itself.
   */
  onFreshSession: () => void | string | null | Promise<string | null | void>;
}

export interface UseChatModePinningResult {
  /** An explicit user session switch: pins it for the current mode, then delegates. */
  switchSession: (sessionId: string) => void;
  /** A message was sent in the current mode: pin the active session. */
  recordSend: () => void;
  /**
   * Restore the current design mode's missing pin: create a fresh session AND
   * switch the active chat to it, THEN pin it. Switching before pinning is
   * what keeps the pin and the on-screen conversation in sync — a fresh chat
   * that is created but never switched to would leave the previous chat
   * active, and its next send would overwrite this mode's pin (contamination).
   */
  restoreFreshSession: () => Promise<void>;
  /** Record a session id as the pin for the current mode, without switching to it. */
  pinSession: (sessionId: string) => void;
}

/**
 * Restores the pinned conversation on every mode switch and records pins as
 * sessions become active in the current mode.
 *
 * The restore effect deliberately depends on `mode` only: pin/active ids
 * are read through refs so a chat change can never re-trigger the restore
 * (which would clobber the user's just-chosen conversation).
 */
export function useChatModePinning({
  mode,
  activeChatId,
  onSwitchSession,
  onFreshSession,
}: UseChatModePinningOptions): UseChatModePinningResult {
  const modeRef = useRef(mode);
  modeRef.current = mode;
  const activeChatIdRef = useRef(activeChatId);
  activeChatIdRef.current = activeChatId;

  const switchRef = useRef(onSwitchSession);
  switchRef.current = onSwitchSession;
  const freshRef = useRef(onFreshSession);
  freshRef.current = onFreshSession;

  // The previous mode; the effect fires on a change only (the mount is a
  // no-op — boot restore belongs to useAppInitialization).
  const prevModeRef = useRef(mode);

  // The returned callbacks are stable (they only touch refs), so callers can
  // memoize around them.
  const switchSession = useCallback(
    (sessionId: string) => {
      writeChatModePin(modeRef.current, sessionId);
      switchRef.current(sessionId);
    },
    [],
  );

  const recordSend = useCallback(() => {
    const active = activeChatIdRef.current;
    if (active) writeChatModePin(modeRef.current, active);
  }, []);

  // Fresh design conversation = create + SWITCH + pin, in that order. The
  // switch is the step that makes it "the conversation the user is in":
  // without it the previous chat stays active and the next send re-pins it
  // over this fresh session (cross-mode contamination).
  const restoreFreshSession = useCallback(async () => {
    const created = await freshRef.current();
    if (typeof created !== 'string' || !created) return;
    // Switch first: make it the active conversation. Then pin — but only when
    // the switch did not explicitly fail. A `false` result means the session
    // never became active; pinning it would leave the pin pointing at a
    // non-active session and the next send would re-pin whatever is active.
    // A `void`/`undefined` result (fire-and-forget wiring) is treated as landed.
    const landed = await switchRef.current(created);
    if (landed !== false) writeChatModePin(modeRef.current, created);
  }, []);

  const pinSession = useCallback((sessionId: string) => {
    writeChatModePin(modeRef.current, sessionId);
  }, []);

  const apiRef = useRef<UseChatModePinningResult | null>(null);
  apiRef.current = { switchSession, recordSend, restoreFreshSession, pinSession };

  useEffect(() => {
    const previous = prevModeRef.current;
    prevModeRef.current = mode;
    if (previous === mode) return;

    const pin = readPinFor(mode);
    if (pin && pin !== activeChatIdRef.current) {
      // That mode's own conversation.
      switchRef.current(pin);
    } else if (!pin && mode === 'design') {
      // Design with no pin: a fresh conversation — never a Code session.
      void apiRef.current?.restoreFreshSession();
    }
    // Code with no pin: keep the current behavior (no cross-mode switch).
  }, [mode]);

  return { switchSession, recordSend, restoreFreshSession, pinSession };
}
