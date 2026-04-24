/**
 * Code actions / Quick Actions extension for CodeMirror.
 *
 * Provides:
 * - Lightbulb icon in the gutter when code actions are available at the cursor line
 * - Ctrl/Cmd+. keyboard shortcut to trigger the quick actions menu
 * - A floating menu showing available actions that can be applied to the document
 *
 * This is the defining IDE feature: hoverable and click-to-trigger lightbulb
 * that shows refactor/arbitrary code-fix suggestions from the backend.
 */

import {
  type EditorView,
  type KeyBinding,
  ViewPlugin,
  type ViewUpdate,
  type PluginValue,
  WidgetType,
  gutter,
  GutterMarker,
} from '@codemirror/view';
import { StateField, type Extension, StateEffect as SE, Facet, RangeSetBuilder } from '@codemirror/state';
import { ApiService } from '../services/api';
import { resolveLanguageId } from './languageRegistry';
import { debugLog } from '../utils/log';

import './codeActions.css';

// ─── Types ────────────────────────────────────────────────────────

export interface CodeActionEdit {
  filePath: string;
  from: number;
  to: number;
  newText: string;
}

export interface CodeAction {
  title: string;
  kind: string;
  edits: CodeActionEdit[];
}

export interface CodeActionState {
  actions: CodeAction[];
  loading: boolean;
  line: number;
}

// ─── Configuration Facet ──────────────────────────────────────────

interface CodeActionsConfig {
  getFilePath: () => string | undefined;
  getContent: () => string;
  onApplyEdits?: (edits: CodeActionEdit[]) => void;
}

export const codeActionsConfig = Facet.define<CodeActionsConfig, Required<CodeActionsConfig>>({
  combine(configs) {
    return configs[0] as Required<CodeActionsConfig>;
  },
});

// ─── State Effects ────────────────────────────────────────────────

const setCodeActions = SE.define<CodeActionState>();
const clearCodeActions = SE.define<void>();

// ─── State Field ──────────────────────────────────────────────────

const codeActionsField = StateField.define<CodeActionState>({
  create() {
    return { actions: [], loading: false, line: -1 };
  },
  update(state, tr) {
    for (const effect of tr.effects) {
      if (effect.is(setCodeActions)) return effect.value;
      if (effect.is(clearCodeActions)) return { actions: [], loading: false, line: -1 };
    }
    return state;
  },
});

// ─── Lightbulb Widget ─────────────────────────────────────────────

class LightbulbWidget extends WidgetType {
  private _onClick: (e: MouseEvent) => void;
  private _hasActions: boolean;

  constructor(onClick: (e: MouseEvent) => void, hasActions: boolean) {
    super();
    this._onClick = onClick;
    this._hasActions = hasActions;
  }

  toDOM() {
    const span = document.createElement('span');
    span.className = 'cm-codeAction-lightbulb' + (this._hasActions ? ' cm-codeAction-has-actions' : ' cm-codeAction-loading');
    span.setAttribute('aria-hidden', 'true');
    span.innerHTML = this._hasActions ? '💡' : '<span class="cm-codeAction-spinner"></span>';
    span.addEventListener('mousedown', (e) => {
      e.preventDefault();
      e.stopPropagation();
      this._onClick(e);
    });
    return span;
  }

  eq(other: LightbulbWidget) {
    return this._hasActions === other._hasActions;
  }

  ignoreEvent() {
    return true;
  }
}

// ─── Gutter Marker ────────────────────────────────────────────────

class LightbulbGutterMarker extends GutterMarker {
  private _widget: LightbulbWidget;

  constructor(widget: LightbulbWidget) {
    super();
    this._widget = widget;
  }

  toDOM() {
    return this._widget.toDOM();
  }
}

// ─── Code Actions Plugin ──────────────────────────────────────────

class CodeActionsPlugin implements PluginValue {
  private view: EditorView;
  private config: Required<CodeActionsConfig>;
  private fetchTimeout: ReturnType<typeof setTimeout> | null = null;
  private menuEl: HTMLDivElement | null = null;
  private lastFetchedLine: number = -1;
  private lastFetchedContent: string = '';
  private disposeListeners: (() => void)[] = [];

  constructor(view: EditorView) {
    this.view = view;
    this.config = view.state.facet(codeActionsConfig);
  }

  update(update: ViewUpdate) {
    this.config = update.state.facet(codeActionsConfig);

    if (update.docChanged || update.selectionSet) {
      this.scheduleFetch(update.state);
    }
  }

  private scheduleFetch(state: typeof this.view.state) {
    // Cancel any pending fetch
    if (this.fetchTimeout) {
      clearTimeout(this.fetchTimeout);
      this.fetchTimeout = null;
    }

    // Clear loading state quickly on rapid navigation
    const currentLine = state.doc.lineAt(state.selection.main.head).number;

    if (currentLine === this.lastFetchedLine && this.lastFetchedContent === state.doc.toString()) {
      return;
    }

    // Debounce fetches
    this.fetchTimeout = setTimeout(() => {
      this.fetchCodeActions(currentLine);
    }, 400);
  }

  private async fetchCodeActions(line: number) {
    const filePath = this.config.getFilePath();
    if (!filePath || filePath.startsWith('__workspace/')) return;

    const content = this.config.getContent();
    if (!content) return;

    const ext = filePath.split('.').pop() || '';
    const name = filePath.split('/').pop() || '';
    const { languageId } = resolveLanguageId(undefined, ext.replace(/^\./, ''), name);
    if (!languageId) return;

    const api = ApiService.getInstance();
    const head = this.view.state.selection.main.head;
    const lineInfo = this.view.state.doc.lineAt(head);
    const col = head - lineInfo.from + 1;
    const lineNum = lineInfo.number;

    // Compute static actions immediately and show them
    const staticActions = this.computeStaticActions(lineNum);

    // Show loading state with static actions (if any)
    this.view.dispatch({
      effects: setCodeActions.of({ actions: staticActions, loading: true, line: lineNum }),
    });

    this.lastFetchedLine = lineNum;
    this.lastFetchedContent = content;

    try {
      const result = await api.getSemanticCodeActions(filePath, content, languageId, lineNum, col);

      const lspActions = (result.code_actions || []).map((a) => ({
        title: a.title,
        kind: a.kind,
        edits: (a.edits || []).map((e) => ({
          filePath: e.filePath,
          from: e.from,
          to: e.to,
          newText: e.newText,
        })),
      }));

      // Merge static actions with LSP actions (static first, then LSP)
      // Deduplicate by title in case both provide the same action
      const seenTitles = new Set(staticActions.map(a => a.title));
      const mergedActions = [...staticActions, ...lspActions.filter(a => !seenTitles.has(a.title))];

      this.view.dispatch({
        effects: setCodeActions.of({ actions: mergedActions, loading: false, line: lineNum }),
      });
    } catch (err) {
      debugLog('[codeActions] fetch failed:', err);
      // Keep static actions on error, just stop loading
      this.view.dispatch({
        effects: setCodeActions.of({ actions: staticActions, loading: false, line: lineNum }),
      });
    }
  }

  /**
   * Compute static code actions that don't require LSP or backend.
   * These provide universal actions for any file type.
   */
  private computeStaticActions(lineNum: number): CodeAction[] {
    const actions: CodeAction[] = [];
    const doc = this.view.state.doc;

    // Get the line content
    const lineInfo = doc.line(lineNum);
    const lineContent = lineInfo.text;

    // Action: Remove trailing whitespace on current line
    const trailingMatch = lineContent.match(/(\s+)$/);
    if (trailingMatch && trailingMatch[1].length > 0) {
      const trailingStart = lineInfo.from + (lineContent.length - trailingMatch[1].length);
      const trailingEnd = lineInfo.to;
      actions.push({
        title: 'Remove trailing whitespace',
        kind: 'refactor.remove',
        edits: [
          {
            filePath: this.config.getFilePath() || '',
            from: trailingStart,
            to: trailingEnd,
            newText: '',
          },
        ],
      });
    }

    // Action: Remove empty lines around cursor (collapse consecutive empty lines)
    // Check previous and next lines for emptiness
    const hasEmptyBefore = lineNum > 1 && doc.line(lineNum - 1).length === 0;
    const hasEmptyAfter = lineNum < doc.lines && doc.line(lineNum + 1).length === 0;

    if (hasEmptyBefore || hasEmptyAfter) {
      // Collect all empty line ranges to remove
      const edits: CodeActionEdit[] = [];
      if (hasEmptyBefore) {
        const emptyLineInfo = doc.line(lineNum - 1);
        edits.push({
          filePath: this.config.getFilePath() || '',
          from: emptyLineInfo.from,
          to: emptyLineInfo.to,
          newText: '',
        });
      }
      if (hasEmptyAfter && lineNum < doc.lines) {
        const emptyLineInfo = doc.line(lineNum + 1);
        edits.push({
          filePath: this.config.getFilePath() || '',
          from: emptyLineInfo.from,
          to: emptyLineInfo.to,
          newText: '',
        });
      }
      if (edits.length > 0) {
        actions.push({
          title: 'Remove empty lines',
          kind: 'refactor.remove',
          edits,
        });
      }
    }

    // Action: Sort selected lines alphabetically
    const selection = this.view.state.selection.main;
    if (!selection.empty) {
      // Get content of selected lines
      const fromLine = doc.lineAt(selection.from).number;
      const toLine = doc.lineAt(selection.to).number;

      if (fromLine !== toLine) {
        // Collect all lines in selection
        const lines: string[] = [];
        for (let i = fromLine; i <= toLine; i++) {
          lines.push(doc.line(i).text);
        }

        // Check if lines can be sorted (not already sorted)
        const sortedLines = [...lines].sort((a, b) =>
          a.localeCompare(b, undefined, { numeric: true, sensitivity: 'base' }),
        );
        const isAlreadySorted = lines.every((line, idx) => line === sortedLines[idx]);

        if (!isAlreadySorted) {
          // Generate edits: replace all selected content with sorted lines
          const fromPos = doc.line(fromLine).from;
          const toPos = doc.line(toLine).to;
          actions.push({
            title: 'Sort lines alphabetically',
            kind: 'refactor.sort',
            edits: [
              {
                filePath: this.config.getFilePath() || '',
                from: fromPos,
                to: toPos,
                newText: sortedLines.join('\n'),
              },
            ],
          });
        }
      }
    }

    return actions;
  }

  /**
   * Show the quick actions menu at the cursor position.
   * Called from the Ctrl+. keybinding.
   */
  showMenu() {
    const state = this.view.state.field(codeActionsField);

    if (state.loading) {
      // Force immediate fetch if loading
      if (this.fetchTimeout) {
        clearTimeout(this.fetchTimeout);
        this.fetchTimeout = null;
      }
      const head = this.view.state.selection.main.head;
      const lineInfo = this.view.state.doc.lineAt(head);
      void this.fetchCodeActions(lineInfo.number);
      return;
    }

    if (state.actions.length === 0) {
      // No actions available — show a transient "no actions" tooltip
      this.showNoActionsToast();
      return;
    }

    this.openMenu(state.actions);
  }

  /**
   * Apply a code action's edits to the editor.
   */
  applyAction(action: CodeAction) {
    this.closeMenu();

    // Collect edits for the current file only
    const filePath = this.config.getFilePath();
    const currentFileEdits = action.edits.filter(
      (e) => !e.filePath || e.filePath === filePath,
    );
    const otherFileEdits = action.edits.filter(
      (e) => e.filePath && e.filePath !== filePath,
    );

    if (currentFileEdits.length > 0) {
      // Sort edits in reverse order so positions remain valid
      const sorted = [...currentFileEdits].sort((a, b) => b.from - a.from);
      const changes = sorted.map((e) => ({
        from: Math.max(0, Math.min(e.from, this.view.state.doc.length)),
        to: Math.max(0, Math.min(e.to, this.view.state.doc.length)),
        insert: e.newText,
      }));

      this.view.dispatch({
        changes,
        userEvent: 'input.codeAction',
      });
    }

    // Notify parent of edits in other files (if any)
    if (otherFileEdits.length > 0 && this.config.onApplyEdits) {
      this.config.onApplyEdits(action.edits);
    }

    // Clear the lightbulb after applying
    this.view.dispatch({ effects: clearCodeActions.of(undefined) });
  }

  private showNoActionsToast() {
    const toast = document.createElement('div');
    toast.className = 'cm-codeAction-toast';
    toast.textContent = 'No code actions available';
    document.body.appendChild(toast);

    requestAnimationFrame(() => toast.classList.add('cm-codeAction-toast-visible'));

    setTimeout(() => {
      toast.classList.remove('cm-codeAction-toast-visible');
      setTimeout(() => toast.remove(), 200);
    }, 1500);
  }

  private openMenu(actions: CodeAction[]) {
    this.closeMenu();

    const menu = document.createElement('div');
    menu.className = 'cm-codeAction-menu';
    menu.setAttribute('role', 'menu');
    menu.setAttribute('aria-label', 'Code Actions');

    const header = document.createElement('div');
    header.className = 'cm-codeAction-menu-header';
    header.textContent = 'Code Actions';
    menu.appendChild(header);

    for (const action of actions) {
      const item = document.createElement('div');
      item.className = 'cm-codeAction-menu-item';
      item.setAttribute('role', 'menuitem');
      item.setAttribute('tabindex', '0');

      const icon = document.createElement('span');
      icon.className = 'cm-codeAction-menu-icon' + this.kindIcon(action.kind);
      icon.innerHTML = this.kindEmoji(action.kind);
      item.appendChild(icon);

      const label = document.createElement('span');
      label.className = 'cm-codeAction-menu-label';
      label.textContent = action.title;
      item.appendChild(label);

      const shortcut = document.createElement('span');
      shortcut.className = 'cm-codeAction-menu-shortcut';
      shortcut.textContent = '';
      item.appendChild(shortcut);

      item.addEventListener('mousedown', (e) => {
        e.preventDefault();
        e.stopPropagation();
        this.applyAction(action);
      });

      item.addEventListener('keydown', (e) => {
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault();
          this.applyAction(action);
        }
      });

      menu.appendChild(item);
    }

    // Position the menu near the cursor
    const coords = this.view.coordsAtPos(this.view.state.selection.main.head);
    if (coords) {
      menu.style.position = 'fixed';
      menu.style.top = `${coords.bottom + 6}px`;
      menu.style.left = `${coords.left}px`;
    }

    document.body.appendChild(menu);
    this.menuEl = menu;

    // Focus the first item
    const firstItem = menu.querySelector('.cm-codeAction-menu-item') as HTMLElement;
    firstItem?.focus();

    // Close on click outside
    const closeHandler = (e: MouseEvent) => {
      if (!menu.contains(e.target as Node) && !this.view.dom.contains(e.target as Node)) {
        this.closeMenu();
      }
    };
    document.addEventListener('mousedown', closeHandler);

    // Close on Escape
    const keyHandler = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        this.closeMenu();
      } else if (e.key === 'ArrowDown') {
        e.preventDefault();
        const focused = document.activeElement as HTMLElement;
        if (focused && focused.nextElementSibling) {
          (focused.nextElementSibling as HTMLElement).focus();
        }
      } else if (e.key === 'ArrowUp') {
        e.preventDefault();
        const focused = document.activeElement as HTMLElement;
        if (focused && focused.previousElementSibling) {
          (focused.previousElementSibling as HTMLElement).focus();
        }
      }
    };
    document.addEventListener('keydown', keyHandler);

    this.disposeListeners.push(() => {
      document.removeEventListener('mousedown', closeHandler);
      document.removeEventListener('keydown', keyHandler);
    });
  }

  closeMenu() {
    if (this.menuEl) {
      this.menuEl.remove();
      this.menuEl = null;
    }
  }

  private kindEmoji(kind: string): string {
    if (kind.includes('organizeImports') || kind.includes('import')) return '📦';
    if (kind.includes('quickfix') || kind.includes('fix')) return '🔧';
    if (kind.includes('remove') || kind.includes('delete')) return '🗑️';
    if (kind.includes('refactor') || kind.includes('sort')) return '♻️';
    return '⚡';
  }

  private kindIcon(_kind: string): string {
    return '';
  }

  destroy() {
    if (this.fetchTimeout) {
      clearTimeout(this.fetchTimeout);
      this.fetchTimeout = null;
    }
    this.closeMenu();
    for (const dispose of this.disposeListeners) {
      dispose();
    }
    this.disposeListeners = [];
  }
}

const codeActionsPlugin = ViewPlugin.fromClass(CodeActionsPlugin);

// ─── Lightbulb Gutter ────────────────────────────────────────────

const codeActionGutter = gutter({
  class: 'cm-codeActionGutter',
  markers: (view) => {
    const state = view.state.field(codeActionsField);

    // No actions and not loading → hide gutter
    if (!state.loading && state.actions.length === 0) return [];

    // Get the line number of the cursor
    const headPos = view.state.selection.main.head;
    const headLine = view.state.doc.lineAt(headPos).number;
    if (state.line !== headLine) return [];

    const marker = new LightbulbGutterMarker(
      new LightbulbWidget(
        () => pluginForView(view)?.showMenu(),
        // Show loading spinner if still fetching; lightbulb if actions ready
        state.actions.length > 0,
      ),
    );

    const builder = new RangeSetBuilder<GutterMarker>();
    builder.add(headPos, headPos, marker);
    return builder.finish();
  },
});

function pluginForView(view: EditorView): CodeActionsPlugin | null {
  try {
    const plugin = view.plugin(codeActionsPlugin);
    return plugin as unknown as CodeActionsPlugin | null;
  } catch {
    return null;
  }
}

// ─── Public API ───────────────────────────────────────────────────

/**
 * Build the code actions extension that provides a lightbulb gutter icon
 * and Ctrl+. quick actions menu.
 *
 * @param getFilePath - returns the current file path
 * @param getContent - returns the current document content
 * @param onApplyEdits - optional callback for edits in other files
 */
export function createCodeActionsExtension(
  getFilePath: () => string | undefined,
  getContent: () => string,
  onApplyEdits?: (edits: CodeActionEdit[]) => void,
): Extension {
  return [
    codeActionsConfig.of({ getFilePath, getContent, onApplyEdits }),
    codeActionsField,
    codeActionsPlugin,
    codeActionGutter,
  ];
}

/**
 * Create a keybinding for Ctrl/Cmd+. to open the quick actions menu.
 */
export function codeActionsKeybinding(): KeyBinding {
  return {
    key: 'Mod-.',
    preventDefault: true,
    run(view: EditorView) {
      const plugin = pluginForView(view);
      if (plugin) {
        plugin.showMenu();
        return true;
      }
      return false;
    },
  };
}
