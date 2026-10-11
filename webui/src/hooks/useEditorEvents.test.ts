import { EditorSelection, EditorState } from '@codemirror/state';
import { EditorView } from '@codemirror/view';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook } from '@testing-library/react';
import { useEditorEvents } from './useEditorEvents';
import type { EditorBuffer } from '../types/editor';

/**
 * editor-save-current regression: the global save_file hotkey (HotkeyContext)
 * dispatches `editor-save-current` on the document. It previously had NO
 * listener — Cmd+S from outside the editor's own keymap (e.g. while a dialog
 * or the diff view held focus, or when the CM keymap lost the event) saved
 * nothing, silently.
 */

function fireSave() {
  document.dispatchEvent(new CustomEvent('editor-save-current'));
}

describe('useEditorEvents — editor-save-current', () => {
  beforeEach(() => {
    document.querySelectorAll('*').forEach(() => undefined);
  });

  it('calls handleSave on the active pane', () => {
    const handleSave = vi.fn().mockResolvedValue(undefined);
    const { unmount } = renderHook(() =>
      useEditorEvents({
        // Minimal stubs — the save branch never touches the view/buffer.
        viewRef: { current: null },
        bufferRef: { current: null as EditorBuffer | null },
        isActiveRef: { current: true },
        handleGoToLine: () => undefined,
        onToggleWordWrap: () => undefined,
        onToggleMinimap: () => undefined,
        onToggleRelativeLineNumbers: () => undefined,
        onCycleWhitespaceRendering: () => undefined,
        toggleLinkedScroll: () => undefined,
        handleFindAllReferences: () => undefined,
        handleSave,
      }),
    );

    fireSave();
    expect(handleSave).toHaveBeenCalledTimes(1);
    unmount();
  });

  it('ignores the event on inactive panes (split-pane guard)', () => {
    const handleSave = vi.fn().mockResolvedValue(undefined);
    const { unmount } = renderHook(() =>
      useEditorEvents({
        viewRef: { current: null },
        bufferRef: { current: null as EditorBuffer | null },
        isActiveRef: { current: false },
        handleGoToLine: () => undefined,
        onToggleWordWrap: () => undefined,
        onToggleMinimap: () => undefined,
        onToggleRelativeLineNumbers: () => undefined,
        onCycleWhitespaceRendering: () => undefined,
        toggleLinkedScroll: () => undefined,
        handleFindAllReferences: () => undefined,
        handleSave,
      }),
    );

    fireSave();
    expect(handleSave).not.toHaveBeenCalled();
    unmount();
  });

  it('unsubscribes on unmount', () => {
    const handleSave = vi.fn().mockResolvedValue(undefined);
    const { unmount } = renderHook(() =>
      useEditorEvents({
        viewRef: { current: null },
        bufferRef: { current: null as EditorBuffer | null },
        isActiveRef: { current: true },
        handleGoToLine: () => undefined,
        onToggleWordWrap: () => undefined,
        onToggleMinimap: () => undefined,
        onToggleRelativeLineNumbers: () => undefined,
        onCycleWhitespaceRendering: () => undefined,
        toggleLinkedScroll: () => undefined,
        handleFindAllReferences: () => undefined,
        handleSave,
      }),
    );
    unmount();
    fireSave();
    expect(handleSave).not.toHaveBeenCalled();
  });
});

describe('useEditorEvents — clipboard commands', () => {
  let view: EditorView;
  let clipboard: { writeText: ReturnType<typeof vi.fn>; readText: ReturnType<typeof vi.fn> };
  let unmount: () => void;

  beforeEach(() => {
    view = new EditorView({
      state: EditorState.create({ doc: 'hello world', selection: EditorSelection.range(0, 5) }),
      parent: document.body,
    });
    clipboard = { writeText: vi.fn().mockResolvedValue(undefined), readText: vi.fn().mockResolvedValue('bye') };
    Object.defineProperty(navigator, 'clipboard', { value: clipboard, configurable: true });
    ({ unmount } = renderHook(() =>
      useEditorEvents({
        viewRef: { current: view },
        bufferRef: { current: null as EditorBuffer | null },
        isActiveRef: { current: true },
        handleGoToLine: () => undefined,
        onToggleWordWrap: () => undefined,
        onToggleMinimap: () => undefined,
        onToggleRelativeLineNumbers: () => undefined,
        onCycleWhitespaceRendering: () => undefined,
        toggleLinkedScroll: () => undefined,
        handleFindAllReferences: () => undefined,
      }),
    ));
  });

  afterEach(() => {
    unmount();
    view.destroy();
  });

  function fire(type: string) {
    document.dispatchEvent(new CustomEvent(type));
  }

  it('copy writes the selection and leaves the document alone', async () => {
    fire('editor-copy');
    await vi.waitFor(() => expect(clipboard.writeText).toHaveBeenCalledWith('hello'));
    expect(view.state.doc.toString()).toBe('hello world');
  });

  it('cut writes the selection and deletes it', async () => {
    fire('editor-cut');
    await vi.waitFor(() => expect(clipboard.writeText).toHaveBeenCalledWith('hello'));
    await vi.waitFor(() => expect(view.state.doc.toString()).toBe(' world'));
  });

  it('paste replaces the selection with the clipboard text', async () => {
    fire('editor-paste');
    await vi.waitFor(() => expect(view.state.doc.toString()).toBe('bye world'));
  });
});
