import { renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { useFileTreeAutoRefresh } from './useFileTreeAutoRefresh';
import { changesWorkspaceFiles } from './workspaceFileEvents';

const emit = (type: string, data: Record<string, unknown> = {}) =>
  window.dispatchEvent(new CustomEvent('sprout:wsevent', { detail: { type, data } }));

describe('changesWorkspaceFiles', () => {
  it('accepts file writes and file-modifying tools, and ignores the rest', () => {
    const e = (type: string, data: Record<string, unknown> = {}) => changesWorkspaceFiles({ type, data } as never);
    expect(e('file_changed', { action: 'created' })).toBe(true);
    expect(e('file_changed', { action: 'git_stage' })).toBe(false);
    expect(e('tool_end', { tool_name: 'write_file' })).toBe(true);
    expect(e('tool_end', { tool_name: 'write_file', status: 'failed' })).toBe(false);
    expect(e('tool_end', { tool_name: 'read_file' })).toBe(false);
    expect(e('query_completed')).toBe(false);
  });
});

describe('useFileTreeAutoRefresh', () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => vi.useRealTimers());

  it('refreshes once after a burst of file changes', () => {
    const refresh = vi.fn();
    const { unmount } = renderHook(() => useFileTreeAutoRefresh(refresh));

    emit('tool_end', { tool_name: 'write_file' });
    emit('file_changed', { action: 'created' });
    emit('query_completed');
    expect(refresh).not.toHaveBeenCalled();
    vi.advanceTimersByTime(1000);
    expect(refresh).toHaveBeenCalledTimes(1);

    emit('tool_end', { tool_name: 'read_file' });
    vi.advanceTimersByTime(1000);
    expect(refresh).toHaveBeenCalledTimes(1);
    unmount();
  });
});
