/**
 * SP-140-3 item 3.3 — useDesignPresence.
 *
 * Pins the hook's two load-bearing behaviors:
 *   1. It reports `present` only when the workspace listing contains design/.
 *   2. The probe runs once per mount — a churning `useSproutFetch` identity
 *      must not trigger a refetch loop (the regression that turned a
 *      layout-only EditorWorkspace test into an infinite render loop).
 */

import { renderHook, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';

let fetchCalls = 0;
// Deliberately unstable: a fresh function per render, like a mock adapter.
const fetchFn = (url: string) => {
  fetchCalls += 1;
  const body = url === '/api/files' ? { message: 'ok', files: [{ path: 'design/flows/a.mmd' }] } : {};
  return Promise.resolve(new Response(JSON.stringify(body), { status: 200 }));
};

vi.mock('../../contexts/SproutAdapterContext', () => ({
  __esModule: true,
  useSproutFetch: () => (url: string) => fetchFn(url),
}));

import { useDesignPresence } from './useDesignPresence';

describe('useDesignPresence', () => {
  it('reports present when design/ exists', async () => {
    fetchCalls = 0;
    const { result, rerender } = renderHook(() => useDesignPresence());
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.present).toBe(true);

    // Re-render several times: the unstable fetch identity must not refetch.
    rerender();
    rerender();
    rerender();
    await waitFor(() => expect(result.current.present).toBe(true));
    expect(fetchCalls).toBe(1);
  });
});
