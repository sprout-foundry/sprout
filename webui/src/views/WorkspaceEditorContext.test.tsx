/**
 * The editor unit.
 *
 * Runs the real buffer manager (`EditorManagerProvider`) and the real file
 * tree against a fake transport, so the cases cover the whole path a host
 * gets: selecting a file in the tree opens it in the editor view, and saving
 * writes the buffer through the transport. The editor pane itself (CodeMirror)
 * is stubbed with a node that shows the buffer it was given and can edit it.
 */

import { FileTree } from '@sprout/ui';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const transport = vi.hoisted(() => ({
  fetch: vi.fn<(url: string, init?: RequestInit) => Promise<Response>>(),
}));

vi.mock('../services/apiFileCheck', () => ({ checkFilesModified: vi.fn().mockResolvedValue({ modified: [] }) }));
vi.mock('../contexts/SproutAdapterContext', () => ({
  SproutAdapterProvider: ({ children }: { children: unknown }) => children,
  useSproutAdapter: () => ({ clientFetch: transport.fetch }),
  useSproutFetch: () => transport.fetch,
}));
vi.mock('../components/EditorPane', async () => {
  const { useEditorManager } = await import('../contexts/EditorManagerContext');
  return {
    default: ({ paneId }: { paneId: string }) => {
      const { panes, buffers, updateBufferContent } = useEditorManager();
      const buffer = buffers.get(panes.find((p) => p.id === paneId)?.bufferId ?? '');
      return (
        <div data-testid="editor-pane" data-path={buffer?.file.path}>
          <button type="button" onClick={() => buffer && updateBufferContent(buffer.id, 'edited text')}>
            edit
          </button>
        </div>
      );
    },
  };
});

import { EditorManagerProvider } from '../contexts/EditorManagerContext';
import { useWorkspaceFileTree } from './useWorkspaceFileTree';
import { WorkspaceEditorProvider, useWorkspaceEditor, useWorkspaceEditorProps } from './WorkspaceEditorContext';
import type { WorkspaceEditorValue } from './WorkspaceEditorContext';
import { WorkspaceEditorView } from './WorkspaceEditorView';

function jsonResponse(body: unknown): Response {
  return {
    ok: true,
    status: 200,
    statusText: 'OK',
    headers: new Headers({ 'content-type': 'application/json' }),
    json: async () => body,
    text: async () => JSON.stringify(body),
  } as unknown as Response;
}

const LISTING = {
  message: 'success',
  files: [
    { name: 'README.md', path: 'README.md', size: 10, is_dir: false },
    { name: 'src', path: 'src', size: 0, is_dir: true },
  ],
};

let editor: WorkspaceEditorValue;

function Probe(): null {
  editor = useWorkspaceEditor();
  return null;
}

/** A tree wired to the editor the way the composed workspace wires it. */
function Tree(): JSX.Element {
  const { fileTree } = useWorkspaceEditorProps();
  const props = useWorkspaceFileTree({ ...fileTree, fetchFn: transport.fetch as unknown as typeof fetch });
  return <FileTree {...props} />;
}

function mount() {
  return render(
    <EditorManagerProvider>
      <WorkspaceEditorProvider>
        <Probe />
        <Tree />
        <WorkspaceEditorView />
      </WorkspaceEditorProvider>
    </EditorManagerProvider>,
  );
}

beforeEach(() => {
  // The buffer manager persists its layout; each case starts from none.
  localStorage.clear();
  localStorage.setItem('sprout-welcome-dismissed', 'true');
  transport.fetch.mockReset();
  // Services below the unit (the layout's workspace-root probe) use the
  // global fetch; route them to the same fake.
  vi.stubGlobal('fetch', transport.fetch);
  transport.fetch.mockImplementation(async (url: string) => {
    if (url.startsWith('/api/files')) return jsonResponse(LISTING);
    if (url.startsWith('/api/file?')) return jsonResponse({ message: 'File saved successfully', mod_time: 1 });
    return jsonResponse({});
  });
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe('WorkspaceEditorProvider', () => {
  it('shows the empty state until a file is opened', () => {
    mount();
    expect(screen.getByText('Open a file')).toBeInTheDocument();
    expect(screen.queryByTestId('editor-pane')).not.toBeInTheDocument();
    expect(editor.activeFile).toBeNull();
    // The buffer manager's chat tab is not a file: it gets no tab here.
    expect(screen.queryByRole('tab')).not.toBeInTheDocument();
  });

  it('opens a file selected in the tree in the editor view', async () => {
    mount();
    fireEvent.click(await screen.findByText('README.md'));

    await waitFor(() => expect(screen.getByTestId('editor-pane')).toHaveAttribute('data-path', 'README.md'));
    expect(screen.getByRole('tab', { name: /README\.md/ })).toHaveAttribute('aria-selected', 'true');
    expect(editor.activeFile).toMatchObject({ path: 'README.md', workspacePath: 'README.md', name: 'README.md' });
    expect(editor.openFiles.map((f) => f.path)).toEqual(['README.md']);
  });

  it('switches between open files and does not duplicate a reopened one', () => {
    mount();
    act(() => {
      editor.openFile('a.ts');
      editor.openFile('b.ts');
    });
    expect(screen.getByTestId('editor-pane')).toHaveAttribute('data-path', 'b.ts');

    fireEvent.click(screen.getByRole('tab', { name: /a\.ts/ }));
    expect(screen.getByTestId('editor-pane')).toHaveAttribute('data-path', 'a.ts');
    act(() => {
      editor.openFile('b.ts');
    });
    expect(editor.openFiles.map((f) => f.path)).toEqual(['a.ts', 'b.ts']);
  });

  it('moves to the requested line once the file is shown', () => {
    vi.useFakeTimers();
    const gotoLine = vi.fn();
    document.addEventListener('editor-goto-line', gotoLine);
    mount();
    act(() => {
      editor.openFile('main.go', { line: 42 });
    });
    act(() => {
      vi.runOnlyPendingTimers();
    });
    document.removeEventListener('editor-goto-line', gotoLine);
    expect(gotoLine).toHaveBeenCalledTimes(1);
    expect((gotoLine.mock.calls[0][0] as CustomEvent).detail).toEqual({ line: 42 });
  });

  it('saves the active file through the transport', async () => {
    mount();
    act(() => {
      editor.openFile('notes.txt');
    });
    fireEvent.click(screen.getByText('edit'));
    expect(editor.activeFile?.isModified).toBe(true);

    await act(async () => {
      await editor.saveFile();
    });

    const write = transport.fetch.mock.calls.find(([url]) => url.startsWith('/api/file?'));
    expect(write).toBeDefined();
    expect(write?.[0]).toBe(`/api/file?path=${encodeURIComponent('notes.txt')}`);
    expect(JSON.parse(String(write?.[1]?.body))).toEqual({ content: 'edited text' });
    await waitFor(() => expect(editor.activeFile?.isModified).toBe(false));
  });

  it('closes a file by path', async () => {
    mount();
    act(() => {
      editor.openFile('a.ts');
      editor.openFile('b.ts');
    });
    await act(async () => {
      await editor.closeFile('b.ts');
    });
    expect(editor.openFiles.map((f) => f.path)).toEqual(['a.ts']);
  });

  it('throws when read outside the provider', () => {
    const spy = vi.spyOn(console, 'error').mockImplementation(() => undefined);
    expect(() => render(<Probe />)).toThrow(/WorkspaceEditorProvider/);
    spy.mockRestore();
  });
});
