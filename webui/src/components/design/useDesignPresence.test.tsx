/**
 * SP-140-3 item 3.3 — useDesignPresence.
 *
 * Pins the hook's load-bearing behaviors:
 *   1. It reports `present` only when the workspace listing contains design/.
 *   2. The probe runs once per mount — a churning `useSproutFetch` identity
 *      must not trigger a refetch loop (the regression that turned a
 *      layout-only EditorWorkspace test into an infinite render loop).
 *   3. `treeState` distinguishes a foreign design/ folder (files that match
 *      no Sprout asset vocabulary) from a recognized tree.
 *   4. `frontendLike` carries the position signal for the empty state's
 *      card ordering.
 *   5. `recheck` re-runs the probe (latest wins).
 */

import { act, renderHook, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { useDesignPresence } from './useDesignPresence';
let fetchCalls = 0;
let filesBody: unknown = { message: 'ok', files: [{ path: 'design/flows/a.mmd' }] };
// Deliberately unstable: a fresh function per render, like a mock adapter.
const fetchFn = (url: string) => {
  fetchCalls += 1;
  const body = url === '/api/files' ? filesBody : {};
  return Promise.resolve(new Response(JSON.stringify(body), { status: 200 }));
};

vi.mock('../../contexts/SproutAdapterContext', () => ({
  __esModule: true,
  useSproutFetch: () => (url: string) => fetchFn(url),
}));

describe('useDesignPresence', () => {
  it('reports present when design/ exists', async () => {
    filesBody = { message: 'ok', files: [{ path: 'design/flows/a.mmd' }] };
    fetchCalls = 0;
    const { result, rerender } = renderHook(() => useDesignPresence());
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.present).toBe(true);
    expect(result.current.treeState).toBe('recognized');

    // Re-render several times: the unstable fetch identity must not refetch.
    rerender();
    rerender();
    rerender();
    await waitFor(() => expect(result.current.present).toBe(true));
    expect(fetchCalls).toBe(1);
  });

  it('reports a foreign design/ folder as present but not recognized', async () => {
    filesBody = { message: 'ok', files: [{ path: 'design/mockups/homepage.psd' }] };
    const { result } = renderHook(() => useDesignPresence());
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.present).toBe(true);
    expect(result.current.treeState).toBe('foreign');
  });

  it('reports none for a workspace without design/', async () => {
    filesBody = { message: 'ok', files: [{ path: 'src/app.tsx' }, { path: 'package.json' }] };
    const { result } = renderHook(() => useDesignPresence());
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.present).toBe(false);
    expect(result.current.treeState).toBe('none');
    expect(result.current.frontendLike).toBe(true);
  });

  it('recheck re-probes and the latest answer wins', async () => {
    filesBody = { message: 'ok', files: [{ path: 'README.md' }] };
    fetchCalls = 0;
    const { result } = renderHook(() => useDesignPresence());
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.treeState).toBe('none');

    filesBody = { message: 'ok', files: [{ path: 'design/README.md' }, { path: 'design/tokens/a.tokens.json' }] };
    act(() => result.current.recheck());
    await waitFor(() => expect(result.current.treeState).toBe('recognized'));
    expect(fetchCalls).toBe(2);
  });
});
