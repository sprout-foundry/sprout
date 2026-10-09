import { act, renderHook } from '@testing-library/react';
import { beforeEach, describe, expect, it } from 'vitest';
import { INLAY_HINTS_SETTING_KEY, useEditorBooleanSetting } from './useEditorBooleanSetting';

describe('useEditorBooleanSetting', () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it('reads the default when nothing is stored, and the stored value otherwise', () => {
    const fresh = renderHook(() => useEditorBooleanSetting(INLAY_HINTS_SETTING_KEY, true));
    expect(fresh.result.current.value).toBe(true);

    localStorage.setItem(INLAY_HINTS_SETTING_KEY, 'false');
    const stored = renderHook(() => useEditorBooleanSetting(INLAY_HINTS_SETTING_KEY, true));
    expect(stored.result.current.value).toBe(false);
  });

  it('persists and propagates a set to every instance sharing the key', () => {
    const settingsPanel = renderHook(() => useEditorBooleanSetting(INLAY_HINTS_SETTING_KEY, true));
    const pane = renderHook(() => useEditorBooleanSetting(INLAY_HINTS_SETTING_KEY, true));
    const unrelated = renderHook(() => useEditorBooleanSetting('editor:minimap-enabled', true));

    act(() => settingsPanel.result.current.set(false));

    expect(localStorage.getItem(INLAY_HINTS_SETTING_KEY)).toBe('false');
    expect(pane.result.current.value).toBe(false);
    expect(pane.result.current.ref.current).toBe(false);
    expect(unrelated.result.current.value).toBe(true);
  });

  it('propagates a toggle so other instances flip back correctly', () => {
    const a = renderHook(() => useEditorBooleanSetting(INLAY_HINTS_SETTING_KEY, true));
    const b = renderHook(() => useEditorBooleanSetting(INLAY_HINTS_SETTING_KEY, true));

    act(() => a.result.current.toggle());
    expect(b.result.current.value).toBe(false);

    act(() => b.result.current.toggle());
    expect(a.result.current.value).toBe(true);
    expect(localStorage.getItem(INLAY_HINTS_SETTING_KEY)).toBe('true');
  });
});
