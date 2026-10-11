/**
 * Shortcuts and menu commands in the real standalone app shell.
 *
 * Renders `App` (its provider stacks, AppContent and the hooks AppContent
 * mounts) as index.tsx does: the page claims the app shell, then the app
 * renders under the local host. Only the parts with no bearing on command
 * routing are stubbed: the WebSocket transport, the startup initialization,
 * the backend (every request answers 404, so the hotkeys fall back to the
 * built-in bindings), the sidebar and terminal, and the Code shell, which is replaced by the real
 * menubar plus a CodeMirror editor wired to the real `useEditorEvents` (the
 * listener the editor pane uses for menu and shortcut commands).
 *
 * Unit tests that mount a command hook on its own pass even when the app
 * never mounts that hook, or mounts two listeners that cancel each other;
 * these assert the behaviour through the app.
 */

import { Compartment, EditorSelection, EditorState } from '@codemirror/state';
import { EditorView } from '@codemirror/view';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { useEffect, useRef } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import App from '../App';
import { useEditorManager } from '../contexts/EditorManagerContext';
import { useEditorEvents } from '../hooks/useEditorEvents';
import { HostProvider, localHost } from '../host';
import { claimAppShell } from '../services/commandHandlers';

vi.mock('../services/localEventsProvider', () => ({
  LocalEventsProvider: class {
    connect() {}
    disconnect() {}
    onEvent() {}
    removeEvent() {}
    sendEvent() {}
    onReconnect() {}
    freeze() {}
    resume() {}
    resetAndReconnect() {}
    getQueuedMessageCount() {
      return 0;
    }
    flushQueuedMessages() {
      return 0;
    }
    isConnected() {
      return true;
    }
  },
}));
vi.mock('../hooks/useAppInitialization', () => ({ useAppInitialization: () => {} }));
vi.mock('./Sidebar', () => ({ default: () => null }));
vi.mock('./Terminal', () => ({ default: () => null }));
vi.mock('../workspaces/DesignShell', () => ({ default: () => null }));
vi.mock('../workspaces/CodeShell', async () => {
  const { default: MenuBar } = await import('./MenuBar');
  return {
    default: () => (
      <main>
        <MenuBar />
        <ProbeEditor />
      </main>
    ),
  };
});

const INITIAL_DOC = 'const greeting = "hello";\n';

/** A CodeMirror editor answering the editor command events like EditorPane. */
function ProbeEditor() {
  const hostRef = useRef<HTMLDivElement>(null);
  const viewRef = useRef<EditorView | null>(null);
  const bufferRef = useRef(null);
  const isActiveRef = useRef(true);
  const wrapping = useRef(new Compartment());
  const wrapOn = useRef(false);
  const { openWorkspaceBuffer } = useEditorManager();

  // An open file buffer: the Edit menu's clipboard items need a text editor.
  const opened = useRef(false);
  useEffect(() => {
    if (opened.current) return;
    opened.current = true;
    openWorkspaceBuffer({ kind: 'file', path: 'src/probe.ts', title: 'probe.ts', content: INITIAL_DOC, ext: '.ts' });
  }, [openWorkspaceBuffer]);

  useEffect(() => {
    const view = new EditorView({
      parent: hostRef.current!,
      state: EditorState.create({ doc: INITIAL_DOC, extensions: [wrapping.current.of([])] }),
    });
    viewRef.current = view;
    return () => view.destroy();
  }, []);

  const noop = () => {};
  useEditorEvents({
    viewRef,
    bufferRef,
    handleGoToLine: noop,
    onToggleWordWrap: () => {
      wrapOn.current = !wrapOn.current;
      viewRef.current?.dispatch({
        effects: wrapping.current.reconfigure(wrapOn.current ? EditorView.lineWrapping : []),
      });
    },
    onToggleMinimap: noop,
    onToggleRelativeLineNumbers: noop,
    onCycleWhitespaceRendering: noop,
    toggleLinkedScroll: noop,
    handleFindAllReferences: noop,
    isActiveRef,
  });

  return <div data-testid="probe-editor" ref={hostRef} />;
}

function editorView(): EditorView {
  const dom = screen.getByTestId('probe-editor').querySelector<HTMLElement>('.cm-editor');
  const view = dom && EditorView.findFromDOM(dom);
  if (!view) throw new Error('probe editor not mounted');
  return view;
}

function isWrapping(): boolean {
  return !!screen.getByTestId('probe-editor').querySelector('.cm-content.cm-lineWrapping');
}

async function clickMenuItem(menu: string, item: string) {
  fireEvent.click(screen.getByRole('menuitem', { name: menu }));
  const entry = await waitFor(() => {
    const items = document.querySelectorAll<HTMLElement>('.menu-bar-dropdown .context-menu-item');
    const found = Array.from(items).find((el) => el.textContent?.includes(item));
    if (!found) throw new Error(`no ${menu} > ${item} item`);
    return found;
  });
  fireEvent.click(entry);
}

function pressShortcut(init: KeyboardEventInit) {
  act(() => {
    document.body.dispatchEvent(new KeyboardEvent('keydown', { bubbles: true, cancelable: true, ...init }));
  });
}

function stubFetch() {
  vi.stubGlobal(
    'fetch',
    vi.fn(async () => new Response('{}', { status: 404, headers: { 'Content-Type': 'application/json' } })),
  );
}

async function renderApp() {
  claimAppShell();
  render(
    <HostProvider host={localHost}>
      <App />
    </HostProvider>,
  );
  await screen.findByTestId('probe-editor');
  // The shortcut listener installs once the hotkey config has loaded.
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 50));
  });
}

describe('the standalone app shell routes every command to one handler', () => {
  let clipboard: string;

  beforeEach(() => {
    stubFetch();
    clipboard = '';
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: {
        writeText: vi.fn(async (text: string) => {
          clipboard = text;
        }),
        readText: vi.fn(async () => clipboard),
      },
    });
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('opens the command palette on its shortcut', async () => {
    await renderApp();
    expect(document.querySelector('.command-palette-overlay')).toBeNull();

    pressShortcut({ key: 'P', code: 'KeyP', ctrlKey: true, shiftKey: true });

    await waitFor(() => expect(document.querySelector('.command-palette-overlay')).not.toBeNull());
  });

  it('toggles the editor word wrap from View > Toggle Word Wrap and its shortcut', async () => {
    await renderApp();
    expect(isWrapping()).toBe(false);

    await clickMenuItem('View', 'Toggle Word Wrap');
    await waitFor(() => expect(isWrapping()).toBe(true));

    pressShortcut({ key: 'z', code: 'KeyZ', altKey: true });
    await waitFor(() => expect(isWrapping()).toBe(false));
  });

  it('copies and pastes through the editor from Edit > Copy and Edit > Paste', async () => {
    await renderApp();
    const view = editorView();
    act(() => view.dispatch({ selection: EditorSelection.range(6, 14) }));

    await clickMenuItem('Edit', 'Copy');
    await waitFor(() => expect(navigator.clipboard.writeText).toHaveBeenCalledWith('greeting'));

    act(() => view.dispatch({ selection: EditorSelection.cursor(view.state.doc.length) }));
    await clickMenuItem('Edit', 'Paste');
    await waitFor(() => expect(view.state.doc.toString()).toBe(`${INITIAL_DOC}greeting`));
  });
});
