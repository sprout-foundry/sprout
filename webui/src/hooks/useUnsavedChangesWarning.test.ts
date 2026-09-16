// @ts-nocheck
// Unit tests for the beforeunload unsaved-changes guard and the
// workspace-changed buffer-survival rule.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook } from '@testing-library/react';
import { useUnsavedChangesWarning } from './useUnsavedChangesWarning';
import type { EditorBuffer } from '../types/editor';

function mkBuffer(overrides: Partial<EditorBuffer> = {}): EditorBuffer {
  return {
    id: 'b1',
    kind: 'file',
    file: { name: 'a.ts', path: '/w/a.ts', isDir: false, size: 10, modified: 0 },
    content: 'x',
    originalContent: 'x',
    contentLoaded: true,
    cursorPosition: { line: 0, column: 0 },
    scrollPosition: { top: 0, left: 0 },
    isModified: false,
    isActive: false,
    paneId: 'pane-1',
    ...overrides,
  } as EditorBuffer;
}

describe('useUnsavedChangesWarning — beforeunload guard', () => {
  let handler: EventListener | null = null;

  beforeEach(() => {
    handler = null;
    vi.spyOn(window, 'addEventListener').mockImplementation((type, fn) => {
      if (type === 'beforeunload') handler = fn as EventListener;
    });
    vi.spyOn(window, 'removeEventListener').mockImplementation(() => {});
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  function fireBeforeUnload(): BeforeUnloadEvent {
    const event = new Event('beforeunload') as BeforeUnloadEvent;
    event.preventDefault = vi.fn();
    Object.defineProperty(event, 'returnValue', { writable: true, value: undefined });
    handler?.(event);
    return event;
  }

  it('prevents unload when a file buffer is modified', () => {
    const buffers = new Map([['b1', mkBuffer({ isModified: true })]]);
    const buffersRef = { current: buffers };
    renderHook(() => useUnsavedChangesWarning({ buffersRef, buffers, activeBufferId: 'b1' }));

    const event = fireBeforeUnload();
    expect(event.preventDefault).toHaveBeenCalled();
    expect(event.returnValue).toBe('');
  });

  it('does not prevent unload when all buffers are clean', () => {
    const buffers = new Map([['b1', mkBuffer({ isModified: false })]]);
    const buffersRef = { current: buffers };
    renderHook(() => useUnsavedChangesWarning({ buffersRef, buffers, activeBufferId: 'b1' }));

    const event = fireBeforeUnload();
    expect(event.preventDefault).not.toHaveBeenCalled();
  });

  it('ignores non-file buffers (chat, diff) when deciding', () => {
    const buffers = new Map([['b1', mkBuffer({ kind: 'chat', isModified: true })]]);
    const buffersRef = { current: buffers };
    renderHook(() => useUnsavedChangesWarning({ buffersRef, buffers, activeBufferId: 'b1' }));

    const event = fireBeforeUnload();
    expect(event.preventDefault).not.toHaveBeenCalled();
  });

  it('sets document.title modified indicator for the active buffer', () => {
    const buffers = new Map([['b1', mkBuffer({ isModified: true })]]);
    const buffersRef = { current: buffers };
    const original = document.title;
    renderHook(() => useUnsavedChangesWarning({ buffersRef, buffers, activeBufferId: 'b1' }));
    expect(document.title).toBe('● a.ts — ledit');
    document.title = original;
  });
});
