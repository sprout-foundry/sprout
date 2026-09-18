import { describe, it, expect, vi, beforeEach } from 'vitest';

/**
 * Unit tests for the experimental embedding gate in useSearchState.
 *
 * The semantic-search surface (Brain toggle, index status probing,
 * auto-build) must stay hidden unless the workspace config opts in with
 * BOTH embedding_index.enabled AND embedding_index.experimental (SP-137).
 */

const mocks = vi.hoisted(() => ({
  getSettings: vi.fn(),
  searchSemantic: vi.fn(),
  searchSemanticStatus: vi.fn(),
  searchSemanticBuild: vi.fn(),
  search: vi.fn(),
}));

vi.mock('../../services/api', () => {
  // getInstance must return a stable identity — useSearchState re-reads it
  // every render and keys effects off it.
  const instance = {
    getSettings: mocks.getSettings,
    searchSemantic: mocks.searchSemantic,
    searchSemanticStatus: mocks.searchSemanticStatus,
    searchSemanticBuild: mocks.searchSemanticBuild,
    search: mocks.search,
  };
  return { ApiService: { getInstance: () => instance } };
});

vi.mock('../../utils/log', () => {
  // Stable identity: performSearch closes over `log`, and a per-render
  // object would re-trigger the re-search effect on every render.
  const stableLog = { error: vi.fn(), debug: vi.fn(), info: vi.fn(), warn: vi.fn() };
  return { useLog: () => stableLog };
});

import { renderHook, waitFor } from '@testing-library/react';
import { useSearchState } from './useSearchState';

describe('useSearchState embedding gate', () => {
  beforeEach(() => {
    mocks.getSettings.mockReset();
    mocks.searchSemantic.mockReset();
    mocks.searchSemanticStatus.mockReset();
    mocks.searchSemanticBuild.mockReset();
    mocks.search.mockReset();
    mocks.search.mockResolvedValue({ results: [], total_matches: 0, total_files: 0, truncated: false });
    mocks.searchSemantic.mockResolvedValue({ results: [], duplicate_clusters: [] });
    mocks.searchSemanticStatus.mockResolvedValue({
      available: true,
      initialized: true,
      building: false,
      record_count: 1,
    });
    mocks.searchSemanticBuild.mockResolvedValue({ status: 'ok' });
  });

  it('embeddingsEnabled is false when the setting is absent', async () => {
    mocks.getSettings.mockResolvedValue({});
    const { result } = renderHook(() => useSearchState());
    await waitFor(() => expect(mocks.getSettings).toHaveBeenCalled());
    expect(result.current.embeddingsEnabled).toBe(false);
  });

  it('embeddingsEnabled is false when only enabled is set (missing experimental gate)', async () => {
    mocks.getSettings.mockResolvedValue({ embedding_index: { enabled: true } });
    const { result } = renderHook(() => useSearchState());
    await waitFor(() => expect(mocks.getSettings).toHaveBeenCalled());
    expect(result.current.embeddingsEnabled).toBe(false);
  });

  it('embeddingsEnabled is false when only experimental is set', async () => {
    mocks.getSettings.mockResolvedValue({ embedding_index: { experimental: true } });
    const { result } = renderHook(() => useSearchState());
    await waitFor(() => expect(mocks.getSettings).toHaveBeenCalled());
    expect(result.current.embeddingsEnabled).toBe(false);
  });

  it('embeddingsEnabled is true when enabled && experimental are both set', async () => {
    mocks.getSettings.mockResolvedValue({ embedding_index: { enabled: true, experimental: true } });
    const { result } = renderHook(() => useSearchState());
    await waitFor(() => expect(result.current.embeddingsEnabled).toBe(true));
  });

  it('defaults to false while settings are loading', () => {
    mocks.getSettings.mockReturnValue(new Promise(() => {}));
    const { result } = renderHook(() => useSearchState());
    expect(result.current.embeddingsEnabled).toBe(false);
  });

  it('falls back to false when getSettings rejects', async () => {
    mocks.getSettings.mockRejectedValue(new Error('offline'));
    const { result } = renderHook(() => useSearchState());
    await waitFor(() => expect(mocks.getSettings).toHaveBeenCalled());
    expect(result.current.embeddingsEnabled).toBe(false);
  });

  it('semantic query with gate off returns a note instead of hitting the API', async () => {
    mocks.getSettings.mockResolvedValue({});
    const { result } = renderHook(() => useSearchState());
    await waitFor(() => expect(mocks.getSettings).toHaveBeenCalled());

    result.current.toggleSemanticMode();
    result.current.setSearchQuery('fetch handler');
    await waitFor(
      () =>
        expect(result.current.semanticNote).toBe(
          'Semantic search requires enabling the experimental embedding index in Settings.',
        ),
      { timeout: 3000 },
    );
    expect(result.current.semanticResults).toEqual([]);
    expect(mocks.searchSemantic).not.toHaveBeenCalled();
  });

  it('semantic query with gate on hits the API', async () => {
    mocks.getSettings.mockResolvedValue({ embedding_index: { enabled: true, experimental: true } });
    const { result } = renderHook(() => useSearchState());
    await waitFor(() => expect(result.current.embeddingsEnabled).toBe(true));

    result.current.toggleSemanticMode();
    result.current.setSearchQuery('fetch handler');
    await waitFor(() => expect(mocks.searchSemantic).toHaveBeenCalled(), { timeout: 3000 });
  });
});
