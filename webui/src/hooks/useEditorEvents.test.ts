import { describe, it, expect, vi, beforeEach } from 'vitest';
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
