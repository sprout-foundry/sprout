import { renderHook } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import type { ContextPanelProps } from './types';
import { useContextPanelState } from './useContextPanelState';

describe('context panel on tablet', () => {
  const originalWidth = window.innerWidth;
  afterEach(() => {
    Object.defineProperty(window, 'innerWidth', { configurable: true, value: originalWidth });
    window.localStorage.clear();
  });

  it('starts closed, since it is an overlay over the chat there', () => {
    Object.defineProperty(window, 'innerWidth', { configurable: true, value: 900 });
    const { result } = renderHook(() =>
      useContextPanelState({ context: 'chat', isTabletLayout: true } as unknown as ContextPanelProps),
    );
    expect(result.current.panelCollapsed).toBe(true);
    // The overlay closing is not saved as the desktop column preference.
    expect(window.localStorage.getItem('sprout.contextPanel.collapsed')).not.toBe('1');
  });
});
