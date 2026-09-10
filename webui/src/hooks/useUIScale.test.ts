import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import { isUIScale, touchLargeEligible, resolveInitialUIScale, useUIScale, UI_SCALE_STORAGE_KEY } from './useUIScale';

describe('useUIScale', () => {
  beforeEach(() => {
    localStorage.clear();
    document.documentElement.removeAttribute('data-ui-scale');
  });
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('isUIScale accepts the canonical values and rejects others', () => {
    expect(isUIScale('compact')).toBe(true);
    expect(isUIScale('default')).toBe(true);
    expect(isUIScale('large')).toBe(true);
    expect(isUIScale('xlarge')).toBe(true);
    expect(isUIScale('huge')).toBe(false);
    expect(isUIScale(null)).toBe(false);
    expect(isUIScale(2)).toBe(false);
  });

  it('stored choice wins over the touch heuristic', () => {
    localStorage.setItem(UI_SCALE_STORAGE_KEY, 'compact');
    vi.spyOn(window, 'matchMedia').mockReturnValue({ matches: true } as MediaQueryList);
    expect(resolveInitialUIScale()).toBe('compact');
  });

  it('first run on a coarse-pointer tablet band resolves to large', () => {
    vi.spyOn(window, 'matchMedia').mockReturnValue({ matches: true } as MediaQueryList);
    vi.spyOn(window, 'innerWidth', 'get').mockReturnValue(1024);
    expect(resolveInitialUIScale()).toBe('large');
  });

  it('first run on a coarse-pointer phone width stays default (chat-first layout)', () => {
    vi.spyOn(window, 'matchMedia').mockReturnValue({ matches: true } as MediaQueryList);
    vi.spyOn(window, 'innerWidth', 'get').mockReturnValue(390);
    expect(resolveInitialUIScale()).toBe('default');
  });

  it('first run with a fine pointer (desktop) stays default', () => {
    vi.spyOn(window, 'matchMedia').mockReturnValue({ matches: false } as MediaQueryList);
    expect(resolveInitialUIScale()).toBe('default');
  });

  it('touchLargeEligible guards against matchMedia absence', () => {
    const win = { matchMedia: undefined, innerWidth: 1024 } as unknown as Window;
    expect(touchLargeEligible(win)).toBe(false);
  });

  it('useUIScale applies data-ui-scale to <html> and persists choices', () => {
    const { result } = renderHook(() => useUIScale());
    expect(document.documentElement.getAttribute('data-ui-scale')).toBe('default');

    act(() => result.current.setUIScale('large'));
    expect(document.documentElement.getAttribute('data-ui-scale')).toBe('large');
    expect(localStorage.getItem(UI_SCALE_STORAGE_KEY)).toBe('large');

    // Invalid values are ignored, not thrown.
    act(() => result.current.setUIScale('enormous' as never));
    expect(document.documentElement.getAttribute('data-ui-scale')).toBe('large');
  });
});
