/**
 * Conflict model (SP-140-7 §7b).
 *
 * Pure state machine for "the agent changed the file while you were editing".
 * The design surfaces hold the loaded base text; when an incoming
 * agent-file-changed event names the held asset, the pane flips to conflicted
 * and offers Review / Keep mine / Take theirs. Keeping mine forces through the
 * §7a seam; the agent's disk text stays recoverable for the life of the
 * selection (restoreAgentVersion).
 *
 * Pure transitions only — no React, no fetch. The Screens tab wires it.
 */

/** The editor's conflict state for one held asset. */
export interface ConflictState {
  /**
   * The disk text as the agent wrote it — captured when the conflict fired
   * (or refetched on demand). Never written by Keep-mine; kept for Review and
   * Restore.
   */
  theirs: string;
  /** The user's buffer text at conflict time (already in the editor). */
  mine: string;
}

/** Resolve decision (§7b). */
export type ConflictDecision = 'keep-mine' | 'take-theirs';

/**
 * True when an incoming change concerns the asset a buffer holds and the
 * buffer has diverged from what the incoming write delivered. Same text =
 * nothing to decide (the surface just refreshes silently).
 */
export function shouldSurfaceConflict(mine: string, incomingText: string): boolean {
  return mine !== incomingText;
}

/**
 * The state after a decision:
 *  - keep-mine: the user's text stays in the buffer; `theirs` is retained so
 *    "Restore agent's version" can bring it back (§7b: undo-like safety).
 *  - take-theirs: the buffer adopts the disk text; no retained copy.
 * Both produce the text the editor should now hold plus whether the conflict
 * banner clears.
 */
export function decideConflict(state: ConflictState, decision: ConflictDecision): { text: string; cleared: boolean } {
  if (decision === 'take-theirs') {
    return { text: state.theirs, cleared: true };
  }
  return { text: state.mine, cleared: false };
}

/**
 * Restore-agent's-version (§7b): after Keep-mine, the user can hand the pane
 * back to the agent's text. Distinct from take-theirs in intent (post-decision
 * regret vs in-conflict choice) but the same transition.
 */
export function restoreAgentVersion(state: ConflictState): string {
  return state.theirs;
}
