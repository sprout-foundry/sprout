/**
 * useEscalationTriggers.ts — Detects browser/WASM limitations and
 * surfaces the "Start Building" escalation path to the user.
 *
 * Three trigger categories:
 *   1. Git push failure — CloudAdapter intercepts POST /api/git/push error
 *   2. VFS quota error — wasmShell.writeFile returns ENOSPC
 *   3. Command timeout — WASM terminal long-running command timeout
 *   4. Build commands — informational detection of npm install, make, cargo build
 *
 * All triggers dispatch a custom event that the app's toast/notification
 * system can pick up.
 */

import { useCallback, useEffect, useRef } from 'react';
import { isCloud } from '../config/mode';

export interface EscalationTriggerEvent {
  /** Unique trigger identifier */
  id: string;
  /** Human-readable trigger reason */
  reason: string;
  /** Severity: 'blocking' (must escalate) or 'info' (suggestion) */
  severity: 'blocking' | 'info';
  /** Toast message to display */
  message: string;
  /** Repo URL to pass to the workspace creation endpoint */
  repoURL?: string;
}

// ── Custom event name ──────────────────────────────────────────────────────

export const ESCALATION_TRIGGER_EVENT = 'sprout:escalation-trigger';

// ── Build command patterns ─────────────────────────────────────────────────

const BUILD_COMMAND_PATTERNS = [
  /^npm\s+(install|ci|run\s+build|run\s+dev|test)/i,
  /^make\s/i,
  /^cargo\s+(build|run|test)/i,
  /^go\s+(build|run|test)/i,
  /^pip\s+(install)/i,
  /^yarn\s/i,
  /^pnpm\s/i,
  /^bundle\s+(install|exec)/i,
  /^rake\s/i,
  /^gradle\s/i,
  /^mvn\s/i,
  /^dotnet\s+(build|run)/i,
];

/**
 * Check whether a command string matches a known build command pattern.
 */
export function isBuildCommand(command: string): boolean {
  return BUILD_COMMAND_PATTERNS.some((pattern) => pattern.test(command.trim()));
}

// ── Hook ───────────────────────────────────────────────────────────────────

export interface UseEscalationTriggersOptions {
  /** Called when a blocking trigger fires. If not provided, dispatches
   *  a custom event on window. */
  onBlockingTrigger?: (event: EscalationTriggerEvent) => void;
  /** Called when an info trigger fires (e.g. build command detected). */
  onInfoTrigger?: (event: EscalationTriggerEvent) => void;
  /** Current repo URL (from ?repo= param or workspace state) */
  repoURL?: string;
}

/**
 * useEscalationTriggers hook.
 *
 * In cloud mode, intercepts fetch responses for git push failures and
 * WASM shell errors to detect when the user hits browser limitations.
 * Fires EscalationTriggerEvent custom events that the app's toast
 * system can listen for.
 *
 * Must be mounted once at the app root level. Designed to work with
 * the CloudAdapter by monkey-patching the fetch response handling.
 */
export function useEscalationTriggers({
  onBlockingTrigger,
  onInfoTrigger,
  repoURL,
}: UseEscalationTriggersOptions = {}): void {
  const repoURLRef = useRef(repoURL);
  repoURLRef.current = repoURL;

  // Track whether we've already shown each trigger to avoid spam.
  const firedRef = useRef<Set<string>>(new Set());

  const fireTrigger = useCallback(
    (event: EscalationTriggerEvent) => {
      if (firedRef.current.has(event.id)) return;
      firedRef.current.add(event.id);

      if (event.severity === 'blocking' && onBlockingTrigger) {
        onBlockingTrigger(event);
      } else if (event.severity === 'info' && onInfoTrigger) {
        onInfoTrigger(event);
      }

      // Always dispatch a DOM event so any component can listen.
      window.dispatchEvent(
        new CustomEvent<EscalationTriggerEvent>(ESCALATION_TRIGGER_EVENT, {
          detail: event,
        }),
      );
    },
    [onBlockingTrigger, onInfoTrigger],
  );

  useEffect(() => {
    if (!isCloud) return;

    // ── Intercept git push responses from CloudAdapter ──────────
    // Wrap the global fetch to detect push failures.
    const originalFetch = window.fetch;
    const pushFailureDetector: typeof window.fetch = async (...args) => {
      const response = await originalFetch(...args);

      // Only intercept git push requests.
      const url = typeof args[0] === 'string' ? args[0] : args[0] instanceof URL ? args[0].toString() : args[0].url;
      if (!response.ok && (url.includes('/api/git/push') || url.includes('/api/proxy/git/push'))) {
        fireTrigger({
          id: 'git-push-failure',
          reason: 'git_push_failed',
          severity: 'blocking',
          message:
            "Git push isn't available in the browser IDE. Start a full workspace to push changes.",
          repoURL: repoURLRef.current ?? undefined,
        });
      }
      return response;
    };

    // Only patch if we haven't already (idempotent mount).
    if (isCloud && window.fetch !== pushFailureDetector) {
      window.fetch = pushFailureDetector;
    }

    // ── Listen for VFS quota errors from wasmShell ─────────────
    const handleQuotaError = (e: Event) => {
      const detail = (e as CustomEvent).detail;
      if (detail?.error === 'ENOSPC' || detail?.error === 'QUOTA_EXCEEDED') {
        fireTrigger({
          id: 'vfs-quota',
          reason: 'vfs_quota_exceeded',
          severity: 'blocking',
          message: 'Browser storage is full. Start a full workspace for persistent storage.',
          repoURL: repoURLRef.current ?? undefined,
        });
      }
    };
    window.addEventListener('sprout:vfs-error', handleQuotaError);

    // ── Listen for terminal command timeout ────────────────────
    const handleTerminalTimeout = (e: Event) => {
      const detail = (e as CustomEvent).detail;
      if (detail?.exitCode === 124 || detail?.reason === 'timeout') {
        fireTrigger({
          id: 'terminal-timeout',
          reason: 'command_timeout',
          severity: 'blocking',
          message:
            'Browser commands are time-limited. Start a full workspace for long-running tasks.',
          repoURL: repoURLRef.current ?? undefined,
        });
      }
    };
    window.addEventListener('sprout:terminal-timeout', handleTerminalTimeout);

    // ── Listen for build commands from terminal output ─────────
    const handleTerminalCommand = (e: Event) => {
      const detail = (e as CustomEvent).detail;
      if (detail?.command && isBuildCommand(detail.command)) {
        fireTrigger({
          id: 'build-command-' + detail.command.replace(/[^a-z0-9]/gi, '-').slice(0, 40),
          reason: 'build_command_detected',
          severity: 'info',
          message:
            'Build tools may be limited in the browser. Start a full workspace for native builds.',
          repoURL: repoURLRef.current ?? undefined,
        });
      }
    };
    window.addEventListener('sprout:terminal-command', handleTerminalCommand);

    return () => {
      // Restore original fetch if we patched it.
      // Only restore if no other escalation hook instance is active.
      if (window.fetch === pushFailureDetector) {
        window.fetch = originalFetch;
      }
      window.removeEventListener('sprout:vfs-error', handleQuotaError);
      window.removeEventListener('sprout:terminal-timeout', handleTerminalTimeout);
      window.removeEventListener('sprout:terminal-command', handleTerminalCommand);
    };
  }, [fireTrigger]);
}
