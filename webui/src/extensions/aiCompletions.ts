/**
 * aiCompletions.ts — CodeMirror 6 extension for AI code completions.
 *
 * Renders GitHub Copilot-style ghost text suggestions fetched from the
 * backend `POST /api/completion` endpoint.
 *
 * Implementation approach:
 * - A StateField (`aiCompletionState`) holds the active suggestion and clears
 *   itself on any document or selection change (typing or moving the cursor
 *   dismisses ghost text automatically).
 * - Decorations are derived from the StateField, so the keymap and the
 *   renderer always share a single source of truth.
 * - A ViewPlugin debounces typing (500ms), fetches a completion, and dispatches
 *   a `setAiCompletion` effect to install the suggestion.
 * - An AbortController plus a generation counter cancel stale in-flight
 *   requests when the user keeps typing.
 * - Tab accepts the suggestion; Escape dismisses it.
 *
 * Theming:
 * - Uses the design-system token `var(--text-tertiary)` for ghost text color.
 */

import { Annotation, Prec, StateEffect, StateField, type Extension } from '@codemirror/state';
import { Decoration, EditorView, ViewPlugin, type ViewUpdate, WidgetType, keymap } from '@codemirror/view';
import { clientFetch } from '../services/clientSession';
import { debugLog } from '../utils/log';

// ── Constants ────────────────────────────────────────────────────────

const DEBOUNCE_MS = 500;
const MAX_TOKENS = 128;
const MIN_PREFIX_CHARS = 5;

/** Internal annotation used to skip re-fetching on our own dispatches. */
const aiCompletionAnnotation = Annotation.define<boolean>();

// ── Types ──────────────────────────────────────────────────────────────

interface CompletionResult {
  text: string;
  provider: string;
  model: string;
}

/** Raw shape returned by POST /api/completion. */
interface CompletionApiResponse {
  completion?: string;
  provider?: string;
  model?: string;
  tokens_used?: number;
}

/** The active ghost-text suggestion: insert `text` at document position `pos`. */
interface AiCompletionSuggestion {
  text: string;
  pos: number;
}

// ── Effects & StateField ─────────────────────────────────────────────

/** Effect that installs a new suggestion. */
const setAiCompletion = StateEffect.define<AiCompletionSuggestion>();

/** Effect that clears the active suggestion. */
const clearAiCompletion = StateEffect.define<null>();

/**
 * Holds the active AI completion suggestion.
 *
 * Clears itself on any document or selection change — typing or moving the
 * cursor dismisses ghost text. Decorations are derived from the field so the
 * keymap and the renderer share one source of truth.
 */
const aiCompletionState = StateField.define<AiCompletionSuggestion | null>({
  create: () => null,
  update(value, tr) {
    if (tr.docChanged || tr.selection) return null;
    for (const effect of tr.effects) {
      if (effect.is(setAiCompletion)) return effect.value;
      if (effect.is(clearAiCompletion)) return null;
    }
    return value;
  },
  provide: (field) =>
    EditorView.decorations.from(field, (suggestion) => {
      if (!suggestion) return Decoration.none;
      // The position is already clamped to the document when the suggestion is
      // installed in fetchCompletion; CM6 also clamps widget positions past
      // the document end, so no view is needed here.
      const widget = Decoration.widget({
        widget: new GhostTextWidget(suggestion.text),
        block: false, // inline ghost text, not a block widget
        side: 1, // render after the cursor
      });
      return Decoration.set([widget.range(suggestion.pos)]);
    }),
});

// ── Widget ─────────────────────────────────────────────────────────

/**
 * GhostTextWidget — Inline widget rendering the suggested (not-yet-inserted)
 * text at the cursor position.
 */
class GhostTextWidget extends WidgetType {
  constructor(private readonly text: string) {
    super();
  }

  toDOM(): HTMLElement {
    const span = document.createElement('span');
    span.className = 'cm-aiCompletion';
    span.textContent = this.text;
    span.setAttribute('role', 'presentation');
    span.setAttribute('aria-hidden', 'true');
    return span;
  }

  eq(other: GhostTextWidget): boolean {
    return other instanceof GhostTextWidget && this.text === other.text;
  }

  ignoreEvent(_event: Event): boolean {
    return true;
  }
}

// ── Suppression helpers ──────────────────────────────────────────────

/** True when we should not fetch a completion for this prefix. */
function shouldSuppress(prefix: string): boolean {
  // Skip when there is too little non-whitespace context before the cursor —
  // this covers both a very short prefix (<5 chars) and an empty line at the
  // start of the file.
  return prefix.trim().length < MIN_PREFIX_CHARS;
}

// ── ViewPlugin ─────────────────────────────────────────────────────

/**
 * The AI completions ViewPlugin class.
 *
 * Owns the fetch lifecycle (debounce, abort, generation guard); the actual
 * suggestion lives in `aiCompletionState`.
 */
class AICompletionsPlugin {
  private view: EditorView | null;
  private readonly getFilePath: () => string | undefined;
  private readonly languageId: string | undefined;
  private timeoutId: ReturnType<typeof setTimeout> | null = null;
  private abortController: AbortController | null = null;
  private generation = 0;
  private destroyed = false;

  constructor(view: EditorView, getFilePath: () => string | undefined, languageId: string | undefined) {
    this.view = view;
    this.getFilePath = getFilePath;
    this.languageId = languageId;
    // Offer a completion shortly after the editor opens (guards in
    // `shouldSuppress` keep this quiet for empty/new files).
    this.scheduleFetch();
  }

  update(update: ViewUpdate): void {
    this.view = update.view;
    // Skip re-fetching when this plugin itself installed/cleared a suggestion.
    if (update.transactions.some((t) => t.annotation(aiCompletionAnnotation))) {
      return;
    }
    if (update.docChanged) {
      // User typed something — the StateField already cleared the suggestion;
      // cancel any in-flight fetch and debounce a fresh completion.
      this.cancelPendingFetch();
      this.scheduleFetch();
    } else if (update.selectionSet) {
      // Pure cursor movement — dismiss without fetching new suggestions.
      this.cancelPendingFetch();
    }
  }

  destroy(): void {
    this.destroyed = true;
    this.cancelPendingFetch();
    this.view = null;
  }

  /** Invalidate any in-flight request and stop a pending debounce. */
  private cancelPendingFetch(): void {
    this.generation++;
    if (this.timeoutId) {
      clearTimeout(this.timeoutId);
      this.timeoutId = null;
    }
    this.abortController?.abort();
    this.abortController = null;
  }

  private scheduleFetch(): void {
    if (this.timeoutId) {
      clearTimeout(this.timeoutId);
    }
    this.timeoutId = setTimeout(() => {
      this.timeoutId = null;
      void this.fetchCompletion();
    }, DEBOUNCE_MS);
  }

  private async fetchCompletion(): Promise<void> {
    if (this.destroyed || !this.view) return;
    const view = this.view;
    const pos = view.state.selection.main.head;
    const doc = view.state.doc;
    const prefix = doc.sliceString(0, pos);

    if (shouldSuppress(prefix)) return;

    const suffix = doc.sliceString(pos);
    const filePath = this.getFilePath();
    const language = this.languageId ?? null;

    // Bump the generation so any older in-flight request becomes stale.
    const generation = ++this.generation;
    const controller = new AbortController();
    this.abortController = controller;

    try {
      const response = await clientFetch('/api/completion', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          prefix,
          suffix,
          language,
          file_path: filePath ?? null,
          max_tokens: MAX_TOKENS,
        }),
        signal: controller.signal,
      });

      if (!response.ok) {
        debugLog('[aiCompletions] Non-OK response:', response.status);
        return;
      }

      const raw = (await response.json()) as Partial<CompletionApiResponse>;
      const result: CompletionResult = {
        text: raw.completion ?? '',
        provider: raw.provider ?? '',
        model: raw.model ?? '',
      };

      // Drop stale responses (user typed/moved while the request was in flight).
      if (this.destroyed || generation !== this.generation || !this.view) return;
      if (result.text.trim().length === 0) return;

      this.view.dispatch({
        effects: setAiCompletion.of({ text: result.text, pos }),
        annotations: [aiCompletionAnnotation.of(true)],
      });
    } catch (err) {
      // Completions are non-critical — fail silently. Ignore aborts entirely.
      if (err instanceof DOMException && err.name === 'AbortError') return;
      debugLog('[aiCompletions] Fetch failed:', err);
    } finally {
      if (this.abortController === controller) {
        this.abortController = null;
      }
    }
  }
}

// ── Keymap ─────────────────────────────────────────────────────────

/** Tab: insert the active suggestion and move the cursor after it. */
function acceptCompletion(view: EditorView): boolean {
  const suggestion = view.state.field(aiCompletionState, false);
  if (!suggestion || suggestion.text.length === 0) return false;

  // If the autocomplete popup is open, let it handle Tab — the user is
  // usually accepting the popup's selected item, not the ghost text.
  if (view.dom.querySelector('.cm-tooltip-autocomplete')) return false;

  // Only accept if the cursor is still at the suggestion position. The
  // StateField normally clears itself on selection changes, but this guard
  // covers any edge case where the cursor moved without the field clearing.
  const currentPos = view.state.selection.main.head;
  if (currentPos !== suggestion.pos) return false;

  view.dispatch({
    changes: { from: suggestion.pos, insert: suggestion.text },
    selection: { anchor: suggestion.pos + suggestion.text.length },
    effects: clearAiCompletion.of(null),
    scrollIntoView: true,
    userEvent: 'input.complete',
  });
  return true;
}

/** Escape: dismiss the active suggestion. */
function dismissCompletion(view: EditorView): boolean {
  const suggestion = view.state.field(aiCompletionState, false);
  if (!suggestion) return false;
  view.dispatch({
    effects: clearAiCompletion.of(null),
    annotations: [aiCompletionAnnotation.of(true)],
  });
  return true;
}

// ── Base Theme ─────────────────────────────────────────────────────

/**
 * Base theme for AI completion ghost text styling.
 *
 * Uses the design-system token `--text-tertiary` with neutral fallbacks.
 */
const aiCompletionsTheme = EditorView.baseTheme({
  '.cm-aiCompletion': {
    color: 'var(--text-tertiary, rgba(128, 128, 128, 0.5))',
    fontStyle: 'italic',
    pointerEvents: 'none',
    whiteSpace: 'pre-wrap',
  },
  '&dark .cm-aiCompletion': {
    color: 'var(--text-tertiary, rgba(160, 160, 160, 0.4))',
  },
  '&light .cm-aiCompletion': {
    color: 'var(--text-tertiary, rgba(100, 100, 100, 0.4))',
  },
});

// ── Public API ────────────────────────────────────────────────────

/**
 * Creates a CodeMirror 6 extension for AI ghost-text completions.
 *
 * @param getFilePath - A getter function that returns the current file path.
 * @param languageId - The language identifier (e.g., "go", "typescript").
 * @returns Extension bundle containing theme, state, keymap, and ViewPlugin.
 */
export function aiCompletionsExtension(
  getFilePath: () => string | undefined,
  languageId: string | null | undefined,
): Extension[] {
  return [
    aiCompletionsTheme,
    aiCompletionState,
    // Highest precedence so Tab/Escape intercept before defaultKeymap /
    // indentWithTab when a suggestion is active (returns false otherwise).
    Prec.highest(
      keymap.of([
        { key: 'Tab', run: acceptCompletion },
        { key: 'Escape', run: dismissCompletion },
      ]),
    ),
    ViewPlugin.fromClass(
      class extends AICompletionsPlugin {
        constructor(view: EditorView) {
          super(view, getFilePath, languageId ?? undefined);
        }
      },
    ),
  ];
}
