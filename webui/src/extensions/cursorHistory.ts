/**
 * cursorHistory.ts — CodeMirror 6 extension for cursor history navigation.
 *
 * Similar to VS Code's Alt+Left (go back) / Alt+Right (go forward).
 *
 * Tracks "significant" cursor jumps (goto line, clicks, search results, etc.)
 * and maintains a back/forward stack so users can navigate between them.
 *
 * Architecture:
 * - A ViewPlugin observes selection changes and schedules debounced pushes
 *   to the back stack when the cursor makes a "significant" jump.
 * - Mutable per-instance state is stored in a module-level WeakMap keyed on
 *   the ViewPlugin instance, reachable from command functions via a symbol
 *   attached to the EditorView.
 * - `navigateCursorBack` / `navigateCursorForward` are plain functions that
 *   take an EditorView and return boolean, suitable for use as CodeMirror
 *   keybinding `run` callbacks.
 */

import { type EditorView, ViewPlugin, type ViewUpdate } from '@codemirror/view';

// ── Types ───────────────────────────────────────────────────────────

interface HistoryEntry {
  /** Document position (character offset). */
  pos: number;
}

interface CursorHistoryState {
  /** Stack of previously-visited positions (bottom = oldest). */
  back: HistoryEntry[];
  /** Stack of positions available for forward navigation. */
  forward: HistoryEntry[];
  /**
   * When true the next selection change is caused by a navigate-back/forward
   * command and must NOT be recorded into the history.
   */
  isNavigating: boolean;
  /** Per-instance timer for the navigation guard (replaces shared module-level var). */
  _guardTimer: ReturnType<typeof setTimeout> | null;
  /** Per-instance timer for debounced history pushes. */
  _pendingTimer: ReturnType<typeof setTimeout> | null;
  /** Position being held by the pending push timer. */
  _pendingPos: number;
}

// ── Per-instance mutable state ──────────────────────────────────────
// A WeakMap keyed on the ViewPlugin instance gives each editor its own
// history independent of other panes / split editors.

const stateMap = new WeakMap<object, CursorHistoryState>();

function createState(): CursorHistoryState {
  return {
    back: [],
    forward: [],
    isNavigating: false,
    _guardTimer: null,
    _pendingTimer: null,
    _pendingPos: -1,
  };
}

function getState(pluginInstance: object): CursorHistoryState {
  let s = stateMap.get(pluginInstance);
  if (!s) {
    s = createState();
    stateMap.set(pluginInstance, s);
  }
  return s;
}

// ── Symbol to reach per-instance state from an EditorView ───────────

const pluginKey = Symbol('cursorHistoryPlugin');

function getPluginState(view: EditorView): CursorHistoryState | null {
  const plugin = (view as any)[pluginKey];
  if (!plugin) return null;
  return getState(plugin);
}

// ── Debounce helper ─────────────────────────────────────────────────

/**
 * Debounced push: only record a position after the cursor has been
 * stable for ~300 ms, so rapid intermediate positions during normal
 * editing (e.g. arrow-key navigation) don't flood the history.
 *
 * Timers are stored per-view in the CursorHistoryState so split panes
 * don't interfere with each other.
 */
function schedulePush(view: EditorView, pos: number): void {
  const plugin = (view as any)[pluginKey];
  if (!plugin) return;
  const state = getState(plugin);

  if (state._pendingTimer !== null) clearTimeout(state._pendingTimer);
  state._pendingPos = pos;

  state._pendingTimer = setTimeout(() => {
    state._pendingTimer = null;
    const p = state._pendingPos;

    // Re-check: the cursor must still be at (or very near) the recorded
    // position. If the user kept moving we skip this stale entry.
    const currentPos = view.state.selection.main.head;
    if (Math.abs(currentPos - p) > 5) return;

    pushPosition(view, p);
  }, 300);
}

// ── Deduplication-aware push ────────────────────────────────────────

const PROXIMITY_THRESHOLD = 5;
const MAX_HISTORY = 500;
const MIN_JUMP_DISTANCE = 10;

/**
 * Push a position onto the back stack, skipping duplicates / near-duplicates.
 * Clears the forward stack because a new user-initiated navigation
 * invalidates the forward history (standard browser / VS Code behaviour).
 */
function pushPosition(view: EditorView, pos: number): void {
  const plugin = (view as any)[pluginKey];
  if (!plugin) return;
  const state = getState(plugin);

  if (state.isNavigating) return;

  // Skip duplicates / near-duplicates of the top entry.
  if (state.back.length > 0 && Math.abs(state.back[state.back.length - 1].pos - pos) <= PROXIMITY_THRESHOLD) {
    return;
  }

  state.back.push({ pos });

  // Clear forward stack on new navigation.
  state.forward = [];

  // Cap history size.
  if (state.back.length > MAX_HISTORY) {
    state.back = state.back.slice(state.back.length - MAX_HISTORY);
  }
}

// ── ViewPlugin definition ───────────────────────────────────────────

/**
 * Detect whether an update represents a "significant" cursor jump
 * (goto-line, click, search result, etc.) vs. normal typing / small
 * navigation.  A jump is significant when the cursor moved more than
 * {@link MIN_JUMP_DISTANCE} characters and the document did NOT change
 * at the same time.
 */
function isSignificantJump(update: ViewUpdate): boolean {
  if (!update.selectionSet || update.docChanged) return false;

  const prevHead = update.startState.selection.main.head;
  const newHead = update.state.selection.main.head;

  return Math.abs(newHead - prevHead) > MIN_JUMP_DISTANCE;
}

/** Helper: dedup-aware push onto an array. */
function dedupPush(arr: HistoryEntry[], entry: HistoryEntry): void {
  if (arr.length > 0 && Math.abs(arr[arr.length - 1].pos - entry.pos) <= PROXIMITY_THRESHOLD) {
    return;
  }
  arr.push(entry);
}

const cursorHistoryPlugin = ViewPlugin.fromClass(
  class CursorHistoryPlugin {
    constructor(public view: EditorView) {
      (view as any)[pluginKey] = this;

      const state = getState(this);
      const head = view.state.selection.main.head;
      if (head > 0) {
        state.back.push({ pos: head });
      }
    }

    update(update: ViewUpdate) {
      if (this.view !== update.view) return;
      (update.view as any)[pluginKey] = this;

      const state = getState(this);

      if (state.isNavigating) return;
      if (update.transactions.some((t) => t.reconfigured)) return;

      if (isSignificantJump(update)) {
        schedulePush(this.view, update.state.selection.main.head);
      }
    }

    destroy() {
      delete (this.view as any)[pluginKey];
      const state = getState(this);
      if (state._guardTimer !== null) clearTimeout(state._guardTimer);
      if (state._pendingTimer !== null) clearTimeout(state._pendingTimer);
    }
  },
);

// ── Navigate guard ──────────────────────────────────────────────────

const NAVIGATE_GUARD_MS = 500;

function setNavigateGuard(state: CursorHistoryState): void {
  if (state._guardTimer !== null) clearTimeout(state._guardTimer);
  state.isNavigating = true;
  state._guardTimer = setTimeout(() => {
    state.isNavigating = false;
    state._guardTimer = null;
  }, NAVIGATE_GUARD_MS);
}

// ── Exported commands ───────────────────────────────────────────────

/**
 * Navigate cursor back to the previous position in the history stack.
 *
 * Pops the top of the back stack (which should be ≈ the current position
 * after the debounce has fired), pushes the *actual* current cursor
 * position onto the forward stack, and moves the cursor to the new top
 * of the back stack.
 *
 * Returns `true` if navigation occurred, `false` otherwise (empty or
 * single-entry history — nowhere to go back to).
 */
export function navigateCursorBack(view: EditorView): boolean {
  const state = getPluginState(view);
  if (!state || state.back.length === 0) return false;

  const currentPos = view.state.selection.main.head;

  // If the top of the back stack is close to where we are, treat it as
  // the "current" entry and consume it to reveal the previous one.
  if (Math.abs(state.back[state.back.length - 1].pos - currentPos) <= PROXIMITY_THRESHOLD) {
    state.back.pop();
  }

  // After potentially consuming the top, check there's still history left.
  if (state.back.length === 0) return false;

  // Save current position for forward navigation.
  state.forward.push({ pos: currentPos });

  // Set guard to prevent this selection change from being recorded.
  setNavigateGuard(state);

  // Move cursor to the new top of the back stack.
  const target = state.back[state.back.length - 1].pos;
  view.dispatch({
    selection: { anchor: target },
    scrollIntoView: true,
  });

  return true;
}

/**
 * Navigate cursor forward to the next position in the forward stack.
 *
 * Pops from the forward stack, pushes current position onto the back
 * stack (with deduplication), and moves the cursor.
 *
 * Returns `true` if navigation occurred, `false` otherwise (empty forward
 * stack).
 */
export function navigateCursorForward(view: EditorView): boolean {
  const state = getPluginState(view);
  if (!state || state.forward.length === 0) return false;

  const currentPos = view.state.selection.main.head;

  // If the top of forward stack is close to current position, skip it.
  if (Math.abs(state.forward[state.forward.length - 1].pos - currentPos) <= PROXIMITY_THRESHOLD) {
    state.forward.pop();
  }
  if (state.forward.length === 0) return false;

  // Record current position on back stack (dedup-aware).
  dedupPush(state.back, { pos: currentPos });

  // Pop the forward target.
  const next = state.forward.pop()!;

  // Set guard.
  setNavigateGuard(state);

  // Move cursor.
  view.dispatch({
    selection: { anchor: next.pos },
    scrollIntoView: true,
  });

  return true;
}

export { cursorHistoryPlugin };
