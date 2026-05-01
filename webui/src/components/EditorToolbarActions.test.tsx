/**
 * EditorToolbarActions.test.tsx — Unit tests for the EditorToolbarActions component.
 *
 * EditorToolbarActions is a pure function component that returns an array of
 * ToolbarAction objects (not JSX). Tests use renderHook to inspect the return
 * value and verify action structure, active state, conditional inclusion,
 * callback wiring, and memoization.
 */

import { renderHook, act } from '@testing-library/react';
import { vi, describe, it, expect } from 'vitest';
import EditorToolbarActions, { type ToolbarAction, type EditorToolbarActionsProps } from './EditorToolbarActions';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function makeProps(overrides: Partial<EditorToolbarActionsProps> = {}): EditorToolbarActionsProps {
  return {
    wordWrapEnabled: false,
    onToggleWordWrap: vi.fn(),
    minimapEnabled: false,
    onToggleMinimap: vi.fn(),
    whitespaceRenderingMode: 'none',
    onCycleWhitespaceRendering: vi.fn(),
    relativeLineNumbersEnabled: false,
    onToggleRelativeLineNumbers: vi.fn(),
    ...overrides,
  };
}

function renderActions(overrides: Partial<EditorToolbarActionsProps> = {}) {
  return renderHook(() => EditorToolbarActions(makeProps(overrides)));
}

// ---------------------------------------------------------------------------
// Tests: Core actions always present
// ---------------------------------------------------------------------------

describe('Core actions always present', () => {
  it('returns exactly 4 actions when no optional handlers are provided', () => {
    const { result } = renderActions();
    expect(result.current).toHaveLength(4);
  });

  it('contains the four expected action IDs in order', () => {
    const { result } = renderActions();
    const ids = result.current.map((a: ToolbarAction) => a.id);
    expect(ids).toEqual([
      'word-wrap',
      'minimap',
      'whitespace-rendering',
      'relative-line-numbers',
    ]);
  });

  it('each action has a title and onClick handler', () => {
    const { result } = renderActions();
    for (const action of result.current) {
      expect(action.title).toBeDefined();
      expect(typeof action.onClick).toBe('function');
    }
  });

  it('each action has a React icon element', () => {
    const { result } = renderActions();
    for (const action of result.current) {
      expect(action.icon).toBeDefined();
    }
  });
});

// ---------------------------------------------------------------------------
// Tests: Active state reflects props
// ---------------------------------------------------------------------------

describe('Active state reflects props', () => {
  it('word-wrap action is inactive by default', () => {
    const { result } = renderActions();
    const action = result.current.find((a: ToolbarAction) => a.id === 'word-wrap');
    expect(action?.active).toBe(false);
  });

  it('word-wrap action is active when wordWrapEnabled is true', () => {
    const { result } = renderActions({ wordWrapEnabled: true });
    const action = result.current.find((a: ToolbarAction) => a.id === 'word-wrap');
    expect(action?.active).toBe(true);
  });

  it('minimap action is inactive by default', () => {
    const { result } = renderActions();
    const action = result.current.find((a: ToolbarAction) => a.id === 'minimap');
    expect(action?.active).toBe(false);
  });

  it('minimap action is active when minimapEnabled is true', () => {
    const { result } = renderActions({ minimapEnabled: true });
    const action = result.current.find((a: ToolbarAction) => a.id === 'minimap');
    expect(action?.active).toBe(true);
  });

  it('relative-line-numbers action is inactive by default', () => {
    const { result } = renderActions();
    const action = result.current.find((a: ToolbarAction) => a.id === 'relative-line-numbers');
    expect(action?.active).toBe(false);
  });

  it('relative-line-numbers action is active when relativeLineNumbersEnabled is true', () => {
    const { result } = renderActions({ relativeLineNumbersEnabled: true });
    const action = result.current.find((a: ToolbarAction) => a.id === 'relative-line-numbers');
    expect(action?.active).toBe(true);
  });
});

// ---------------------------------------------------------------------------
// Tests: Whitespace rendering active state
// ---------------------------------------------------------------------------

describe('Whitespace rendering active state', () => {
  it('whitespace action is inactive when mode is "none"', () => {
    const { result } = renderActions({ whitespaceRenderingMode: 'none' });
    const action = result.current.find((a: ToolbarAction) => a.id === 'whitespace-rendering');
    expect(action?.active).toBe(false);
  });

  it('whitespace action is active when mode is "boundary"', () => {
    const { result } = renderActions({ whitespaceRenderingMode: 'boundary' });
    const action = result.current.find((a: ToolbarAction) => a.id === 'whitespace-rendering');
    expect(action?.active).toBe(true);
  });

  it('whitespace action is active when mode is "all"', () => {
    const { result } = renderActions({ whitespaceRenderingMode: 'all' });
    const action = result.current.find((a: ToolbarAction) => a.id === 'whitespace-rendering');
    expect(action?.active).toBe(true);
  });
});

// ---------------------------------------------------------------------------
// Tests: Whitespace dynamic title
// ---------------------------------------------------------------------------

describe('Whitespace dynamic title', () => {
  it('title is "Show whitespace (boundary)" when mode is "none"', () => {
    const { result } = renderActions({ whitespaceRenderingMode: 'none' });
    const action = result.current.find((a: ToolbarAction) => a.id === 'whitespace-rendering');
    expect(action?.title).toBe('Show whitespace (boundary)');
  });

  it('title is "Show whitespace (all)" when mode is "boundary"', () => {
    const { result } = renderActions({ whitespaceRenderingMode: 'boundary' });
    const action = result.current.find((a: ToolbarAction) => a.id === 'whitespace-rendering');
    expect(action?.title).toBe('Show whitespace (all)');
  });

  it('title is "Hide whitespace" when mode is "all"', () => {
    const { result } = renderActions({ whitespaceRenderingMode: 'all' });
    const action = result.current.find((a: ToolbarAction) => a.id === 'whitespace-rendering');
    expect(action?.title).toBe('Hide whitespace');
  });
});

// ---------------------------------------------------------------------------
// Tests: Format document action
// ---------------------------------------------------------------------------

describe('Format document action', () => {
  it('format-document action is excluded when onFormatDocument is not provided', () => {
    const { result } = renderActions();
    const ids = result.current.map((a: ToolbarAction) => a.id);
    expect(ids).not.toContain('format-document');
  });

  it('format-document action is included when onFormatDocument is provided', () => {
    const { result } = renderActions({ onFormatDocument: vi.fn() });
    const action = result.current.find((a: ToolbarAction) => a.id === 'format-document');
    expect(action).toBeDefined();
    expect(action?.title).toBe('Format document (Shift+Alt+F)');
  });

  it('returns 5 actions when only onFormatDocument is provided', () => {
    const { result } = renderActions({ onFormatDocument: vi.fn() });
    expect(result.current).toHaveLength(5);
  });

  it('format-document action appears after the four core actions', () => {
    const { result } = renderActions({ onFormatDocument: vi.fn() });
    const ids = result.current.map((a: ToolbarAction) => a.id);
    expect(ids).toEqual([
      'word-wrap',
      'minimap',
      'whitespace-rendering',
      'relative-line-numbers',
      'format-document',
    ]);
  });
});

// ---------------------------------------------------------------------------
// Tests: Linked scroll action
// ---------------------------------------------------------------------------

describe('Linked scroll action', () => {
  it('linked-scroll action is excluded when onToggleLinkedScroll is not provided', () => {
    const { result } = renderActions();
    const ids = result.current.map((a: ToolbarAction) => a.id);
    expect(ids).not.toContain('linked-scroll');
  });

  it('linked-scroll action is included when onToggleLinkedScroll is provided', () => {
    const { result } = renderActions({ onToggleLinkedScroll: vi.fn() });
    const action = result.current.find((a: ToolbarAction) => a.id === 'linked-scroll');
    expect(action).toBeDefined();
  });

  it('returns 5 actions when only onToggleLinkedScroll is provided', () => {
    const { result } = renderActions({ onToggleLinkedScroll: vi.fn() });
    expect(result.current).toHaveLength(5);
  });

  it('linked-scroll active is false when linkedScrollEnabled is undefined', () => {
    const { result } = renderActions({ onToggleLinkedScroll: vi.fn(), linkedScrollEnabled: undefined });
    const action = result.current.find((a: ToolbarAction) => a.id === 'linked-scroll');
    expect(action?.active).toBe(false);
  });

  it('linked-scroll active is false when linkedScrollEnabled is false', () => {
    const { result } = renderActions({ onToggleLinkedScroll: vi.fn(), linkedScrollEnabled: false });
    const action = result.current.find((a: ToolbarAction) => a.id === 'linked-scroll');
    expect(action?.active).toBe(false);
  });

  it('linked-scroll active is true when linkedScrollEnabled is true', () => {
    const { result } = renderActions({ onToggleLinkedScroll: vi.fn(), linkedScrollEnabled: true });
    const action = result.current.find((a: ToolbarAction) => a.id === 'linked-scroll');
    expect(action?.active).toBe(true);
  });

  it('linked-scroll title is "Enable linked scroll" when not enabled', () => {
    const { result } = renderActions({ onToggleLinkedScroll: vi.fn(), linkedScrollEnabled: false });
    const action = result.current.find((a: ToolbarAction) => a.id === 'linked-scroll');
    expect(action?.title).toBe('Enable linked scroll');
  });

  it('linked-scroll title is "Disable linked scroll" when enabled', () => {
    const { result } = renderActions({ onToggleLinkedScroll: vi.fn(), linkedScrollEnabled: true });
    const action = result.current.find((a: ToolbarAction) => a.id === 'linked-scroll');
    expect(action?.title).toBe('Disable linked scroll');
  });

  it('linked-scroll title is "Enable linked scroll" when linkedScrollEnabled is undefined', () => {
    const { result } = renderActions({ onToggleLinkedScroll: vi.fn(), linkedScrollEnabled: undefined });
    const action = result.current.find((a: ToolbarAction) => a.id === 'linked-scroll');
    expect(action?.title).toBe('Enable linked scroll');
  });
});

// ---------------------------------------------------------------------------
// Tests: All actions together
// ---------------------------------------------------------------------------

describe('All actions together', () => {
  it('returns 6 actions when both optional handlers are provided', () => {
    const { result } = renderActions({
      onFormatDocument: vi.fn(),
      onToggleLinkedScroll: vi.fn(),
    });
    expect(result.current).toHaveLength(6);
  });

  it('action order is correct with both optional actions', () => {
    const { result } = renderActions({
      onFormatDocument: vi.fn(),
      onToggleLinkedScroll: vi.fn(),
    });
    const ids = result.current.map((a: ToolbarAction) => a.id);
    expect(ids).toEqual([
      'word-wrap',
      'minimap',
      'whitespace-rendering',
      'relative-line-numbers',
      'format-document',
      'linked-scroll',
    ]);
  });
});

// ---------------------------------------------------------------------------
// Tests: onClick callbacks wired correctly
// ---------------------------------------------------------------------------

describe('onClick callbacks', () => {
  it('calls onToggleWordWrap when word-wrap onClick is invoked', () => {
    const onToggleWordWrap = vi.fn();
    const { result } = renderActions({ onToggleWordWrap });

    act(() => {
      const action = result.current.find((a: ToolbarAction) => a.id === 'word-wrap');
      action?.onClick();
    });

    expect(onToggleWordWrap).toHaveBeenCalledTimes(1);
  });

  it('calls onToggleMinimap when minimap onClick is invoked', () => {
    const onToggleMinimap = vi.fn();
    const { result } = renderActions({ onToggleMinimap });

    act(() => {
      const action = result.current.find((a: ToolbarAction) => a.id === 'minimap');
      action?.onClick();
    });

    expect(onToggleMinimap).toHaveBeenCalledTimes(1);
  });

  it('calls onCycleWhitespaceRendering when whitespace onClick is invoked', () => {
    const onCycleWhitespaceRendering = vi.fn();
    const { result } = renderActions({ onCycleWhitespaceRendering });

    act(() => {
      const action = result.current.find((a: ToolbarAction) => a.id === 'whitespace-rendering');
      action?.onClick();
    });

    expect(onCycleWhitespaceRendering).toHaveBeenCalledTimes(1);
  });

  it('calls onToggleRelativeLineNumbers when relative-line-numbers onClick is invoked', () => {
    const onToggleRelativeLineNumbers = vi.fn();
    const { result } = renderActions({ onToggleRelativeLineNumbers });

    act(() => {
      const action = result.current.find((a: ToolbarAction) => a.id === 'relative-line-numbers');
      action?.onClick();
    });

    expect(onToggleRelativeLineNumbers).toHaveBeenCalledTimes(1);
  });

  it('calls onFormatDocument when format-document onClick is invoked', () => {
    const onFormatDocument = vi.fn();
    const { result } = renderActions({ onFormatDocument });

    act(() => {
      const action = result.current.find((a: ToolbarAction) => a.id === 'format-document');
      action?.onClick();
    });

    expect(onFormatDocument).toHaveBeenCalledTimes(1);
  });

  it('calls onToggleLinkedScroll when linked-scroll onClick is invoked', () => {
    const onToggleLinkedScroll = vi.fn();
    const { result } = renderActions({ onToggleLinkedScroll });

    act(() => {
      const action = result.current.find((a: ToolbarAction) => a.id === 'linked-scroll');
      action?.onClick();
    });

    expect(onToggleLinkedScroll).toHaveBeenCalledTimes(1);
  });
});

// ---------------------------------------------------------------------------
// Tests: Action static properties
// ---------------------------------------------------------------------------

describe('Action static properties', () => {
  it('word-wrap action has correct title', () => {
    const { result } = renderActions();
    const action = result.current.find((a: ToolbarAction) => a.id === 'word-wrap');
    expect(action?.title).toBe('Toggle word wrap (Alt+Z)');
  });

  it('minimap action has correct title', () => {
    const { result } = renderActions();
    const action = result.current.find((a: ToolbarAction) => a.id === 'minimap');
    expect(action?.title).toBe('Toggle minimap');
  });

  it('relative-line-numbers action has correct title', () => {
    const { result } = renderActions();
    const action = result.current.find((a: ToolbarAction) => a.id === 'relative-line-numbers');
    expect(action?.title).toBe('Toggle relative line numbers');
  });
});

// ---------------------------------------------------------------------------
// Tests: Memoization
// ---------------------------------------------------------------------------

describe('Memoization', () => {
  it('returns the same array reference when props are unchanged', () => {
    const props = makeProps();
    const { result, rerender } = renderHook(
      (p: EditorToolbarActionsProps) => EditorToolbarActions(p),
      { initialProps: props },
    );

    const firstResult = result.current;

    // Re-render with the same props
    act(() => {
      rerender(props);
    });

    // After re-render with same props, the memoized result should be identical
    expect(result.current).toBe(firstResult);
  });

  it('returns a new array reference when wordWrapEnabled changes', () => {
    const props = makeProps();
    const { result, rerender } = renderHook(
      (p: EditorToolbarActionsProps) => EditorToolbarActions(p),
      { initialProps: props },
    );

    const firstResult = result.current;

    act(() => {
      rerender({ ...props, wordWrapEnabled: true });
    });

    expect(result.current).not.toBe(firstResult);
  });

  it('returns a new array reference when minimapEnabled changes', () => {
    const props = makeProps();
    const { result, rerender } = renderHook(
      (p: EditorToolbarActionsProps) => EditorToolbarActions(p),
      { initialProps: props },
    );

    const firstResult = result.current;

    act(() => {
      rerender({ ...props, minimapEnabled: true });
    });

    expect(result.current).not.toBe(firstResult);
  });

  it('returns a new array reference when whitespaceRenderingMode changes', () => {
    const props = makeProps();
    const { result, rerender } = renderHook(
      (p: EditorToolbarActionsProps) => EditorToolbarActions(p),
      { initialProps: props },
    );

    const firstResult = result.current;

    act(() => {
      rerender({ ...props, whitespaceRenderingMode: 'boundary' });
    });

    expect(result.current).not.toBe(firstResult);
  });

  it('returns a new array reference when relativeLineNumbersEnabled changes', () => {
    const props = makeProps();
    const { result, rerender } = renderHook(
      (p: EditorToolbarActionsProps) => EditorToolbarActions(p),
      { initialProps: props },
    );

    const firstResult = result.current;

    act(() => {
      rerender({ ...props, relativeLineNumbersEnabled: true });
    });

    expect(result.current).not.toBe(firstResult);
  });

  it('returns a new array reference when onFormatDocument is added', () => {
    const { result, rerender } = renderHook(
      (p: EditorToolbarActionsProps) => EditorToolbarActions(p),
      { initialProps: makeProps() },
    );

    expect(result.current).toHaveLength(4);

    act(() => {
      rerender(makeProps({ onFormatDocument: vi.fn() }));
    });

    expect(result.current).toHaveLength(5);
  });

  it('returns a new array reference when onToggleLinkedScroll is added', () => {
    const { result, rerender } = renderHook(
      (p: EditorToolbarActionsProps) => EditorToolbarActions(p),
      { initialProps: makeProps() },
    );

    expect(result.current).toHaveLength(4);

    act(() => {
      rerender(makeProps({ onToggleLinkedScroll: vi.fn() }));
    });

    expect(result.current).toHaveLength(5);
  });

  it('returns a new array reference when linkedScrollEnabled changes', () => {
    const props = makeProps({ onToggleLinkedScroll: vi.fn(), linkedScrollEnabled: false });
    const { result, rerender } = renderHook(
      (p: EditorToolbarActionsProps) => EditorToolbarActions(p),
      { initialProps: props },
    );

    const firstResult = result.current;

    act(() => {
      rerender({ ...props, linkedScrollEnabled: true });
    });

    expect(result.current).not.toBe(firstResult);
  });

  it('returns a new array reference when a callback function reference changes', () => {
    const props = makeProps();
    const { result, rerender } = renderHook(
      (p: EditorToolbarActionsProps) => EditorToolbarActions(p),
      { initialProps: props },
    );

    const firstResult = result.current;

    // New function reference for onToggleWordWrap
    act(() => {
      rerender({ ...props, onToggleWordWrap: vi.fn() });
    });

    expect(result.current).not.toBe(firstResult);
  });
});

// ---------------------------------------------------------------------------
// Tests: Disabled state
// ---------------------------------------------------------------------------

describe('Disabled state', () => {
  it('actions do not have disabled set by default', () => {
    const { result } = renderActions();
    for (const action of result.current) {
      expect(action.disabled).toBeUndefined();
    }
  });

  it('format-document action does not have disabled set', () => {
    const { result } = renderActions({ onFormatDocument: vi.fn() });
    const action = result.current.find((a: ToolbarAction) => a.id === 'format-document');
    expect(action?.disabled).toBeUndefined();
  });

  it('linked-scroll action does not have disabled set', () => {
    const { result } = renderActions({ onToggleLinkedScroll: vi.fn() });
    const action = result.current.find((a: ToolbarAction) => a.id === 'linked-scroll');
    expect(action?.disabled).toBeUndefined();
  });
});

// ---------------------------------------------------------------------------
// Tests: Exported types
// ---------------------------------------------------------------------------

describe('Exported types', () => {
  it('exports ToolbarAction interface', () => {
    const action: ToolbarAction = {
      id: 'test',
      title: 'Test action',
      icon: null as any,
      onClick: vi.fn(),
    };
    expect(action.id).toBe('test');
  });

  it('exports EditorToolbarActionsProps interface', () => {
    const props: EditorToolbarActionsProps = makeProps();
    expect(props.wordWrapEnabled).toBe(false);
  });
});
