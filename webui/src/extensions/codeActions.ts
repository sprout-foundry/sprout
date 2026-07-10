/** Code actions extension — lightbulb gutter + Ctrl+. quick actions menu. Static analysis in ./staticAnalysis.ts. */

import { LSPPlugin } from '@codemirror/lsp-client';
import { StateField, type Extension, StateEffect as SE, Facet, RangeSetBuilder } from '@codemirror/state';
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
import { ApiService } from '../services/api';
import { uriToFilePath, getFileURI } from '../services/lspClientService';
import { debugLog } from '../utils/log';
import { resolveLanguageId } from './languageRegistry';
import { getClientForLanguageSync } from './lspExtensions';
import { computeStaticActions, kindEmoji } from './staticAnalysis';

import './codeActions.css';

/** LSP CodeAction request parameters. */
interface LSPCodeActionParams {
  textDocument: { uri: string };
  range: { start: { line: number; character: number }; end: { line: number; character: number } };
  context: { diagnostics: unknown[] };
}

/** LSP CodeAction result item. */
interface LSPCodeActionResult {
  title: string;
  kind?: string;
  edit?: {
    changes?: Record<
      string,
      Array<{
        range: { start: { line: number; character: number }; end: { line: number; character: number } };
        newText: string;
      }>
    >;
  };
}

/** LSP WorkspaceEdit fragment for convertLSPEdits. */
interface LSPWorkspaceEdit {
  changes?: Record<
    string,
    Array<{
      range: { start: { line: number; character: number }; end: { line: number; character: number } };
      newText: string;
    }>
  >;
}

/** CodeActionEdit describes a single text replacement within a file. */
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

/** Configuration provided by the host editor. */
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

const setCodeActions = SE.define<CodeActionState>();
const clearCodeActions = SE.define<void>();

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

// ─── Lightbulb Widget & Gutter Marker ─────────────────────────────

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
    span.className =
      'cm-codeAction-lightbulb' + (this._hasActions ? ' cm-codeAction-has-actions' : ' cm-codeAction-loading');
    span.setAttribute('aria-hidden', 'true');
    span.innerHTML = this._hasActions
      ? '<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M15 14c.2-1 .7-1.7 1.5-2.5 1-.9 1.5-2.2 1.5-3.5A6 6 0 0 0 6 8c0 1 .2 2.2 1.5 3.5.7.7 1.3 1.5 1.5 2.5"/><path d="M9 18h6"/><path d="M10 22h4"/></svg>'
      : '<span class="cm-codeAction-spinner"></span>';
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

// ─── Code Actions Plugin ──────────────────────────────────────────

class LightbulbGutterMarker extends GutterMarker {
  constructor(private _widget: LightbulbWidget) {
    super();
  }
  toDOM() {
    return this._widget.toDOM();
  }
}

class CodeActionsPlugin implements PluginValue {
  private view: EditorView;
  private config: Required<CodeActionsConfig>;
  private fetchTimeout: ReturnType<typeof setTimeout> | null = null;
  private fetchGeneration: number = 0;
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

    // Increment generation to track this fetch request
    this.fetchGeneration++;
    const gen = this.fetchGeneration;

    // Debounce fetches
    this.fetchTimeout = setTimeout(() => {
      if (gen === this.fetchGeneration) {
        this.fetchCodeActions(currentLine);
      }
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
      // Try LSP client first if available for this language
      let semanticActions: CodeAction[] = [];
      const lspClient = getClientForLanguageSync(languageId);
      const plugin = LSPPlugin.get(this.view);

      if (lspClient?.connected && plugin) {
        try {
          const fileURI = getFileURI(filePath);
          const startPos = plugin.toPosition(lineInfo.from);
          const endPos = plugin.toPosition(lineInfo.to);

          await lspClient.sync();

          const params: LSPCodeActionParams = {
            textDocument: { uri: fileURI },
            range: { start: startPos, end: endPos },
            context: { diagnostics: [] },
          };
          const lspResult = (await lspClient.request('textDocument/codeAction', params)) as LSPCodeActionResult[];

          if (lspResult && Array.isArray(lspResult)) {
            semanticActions = lspResult.map((action) => ({
              title: action.title,
              kind: action.kind || '',
              edits: this.convertLSPEdits(action.edit, plugin),
            }));
          }
        } catch (lspErr) {
          debugLog('[codeActions] LSP codeAction request failed, falling back to REST:', lspErr);
          semanticActions = [];
        }
      }

      // If LSP didn't return actions, fall back to REST API
      if (semanticActions.length === 0) {
        const result = await api.getSemanticCodeActions(filePath, content, languageId, lineNum, col);

        if (result) {
          semanticActions = (result.code_actions || []).map((a) => ({
            title: a.title,
            kind: a.kind,
            edits: (a.edits || []).map((e) => ({
              filePath: e.filePath,
              from: e.from,
              to: e.to,
              newText: e.newText,
            })),
          }));
        }
      }

      if (semanticActions.length === 0) {
        this.view.dispatch({
          effects: setCodeActions.of({ actions: staticActions, loading: false, line: lineNum }),
        });
        return;
      }

      // Merge static actions with semantic actions (static first, then semantic)
      // Deduplicate by title in case both provide the same action
      const seenTitles = new Set(staticActions.map((a) => a.title));
      const mergedActions = [...staticActions, ...semanticActions.filter((a) => !seenTitles.has(a.title))];

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
   * Convert LSP WorkspaceEdit changes into CodeActionEdit[] format.
   *
   * For edits in the current file, uses the LSPPlugin to convert positions.
   * For edits in other files, stores only filePath and raw position info;
   * these are forwarded via onApplyEdits.
   */
  private convertLSPEdits(edit: LSPWorkspaceEdit | undefined, plugin: LSPPlugin): CodeActionEdit[] {
    if (!edit?.changes) return [];

    const edits: CodeActionEdit[] = [];
    const currentFilePath = this.config.getFilePath() || '';

    for (const [uri, textEdits] of Object.entries(edit.changes)) {
      const editFilePath = uriToFilePath(uri);

      for (const te of textEdits) {
        if (editFilePath === currentFilePath && plugin) {
          // Convert LSP positions to CodeMirror offsets using the plugin
          const from = plugin.fromPosition(te.range.start);
          const to = plugin.fromPosition(te.range.end);
          edits.push({ filePath: editFilePath, from, to, newText: te.newText });
        } else {
          // Edits for other files — store without valid from/to
          // These will be forwarded via onApplyEdits
          edits.push({
            filePath: editFilePath,
            from: 0,
            to: 0,
            newText: te.newText,
          });
        }
      }
    }

    return edits;
  }

  /**
   * Compute static code actions that don't require LSP or backend.
   * Delegates to the standalone static analysis module.
   */
  private computeStaticActions(lineNum: number): CodeAction[] {
    return computeStaticActions(
      this.view.state.doc,
      lineNum,
      this.view.state.selection.main,
      this.config.getFilePath() || '',
    );
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
    const currentFileEdits = action.edits.filter((e) => !e.filePath || e.filePath === filePath);
    const otherFileEdits = action.edits.filter((e) => e.filePath && e.filePath !== filePath);

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
      icon.className = 'cm-codeAction-menu-icon';
      icon.innerHTML = this.kindEmoji(action.kind);
      item.appendChild(icon);

      const label = document.createElement('span');
      label.className = 'cm-codeAction-menu-label';
      label.textContent = action.title;
      item.appendChild(label);

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
    return kindEmoji(kind);
  }

  destroy() {
    // Mark generation as invalid to ignore any pending async callbacks
    this.fetchGeneration = -1;
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
    return plugin as CodeActionsPlugin | null;
  } catch {
    return null;
  }
}

// ─── Public API ───────────────────────────────────────────────────

/** Build the code actions extension with lightbulb gutter and Ctrl+. menu. */
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

/** Create a keybinding for Ctrl/Cmd+. to open the quick actions menu. */
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

// Re-export for backward compatibility
export { kindEmoji } from './staticAnalysis';
