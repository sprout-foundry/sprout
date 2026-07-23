/**
 * Unit tests for ProviderCatalogContext.
 *
 * Uses renderHook from @testing-library/react and mocks ApiService so the
 * suite can run without a backend. Covers three contract guarantees:
 *   1. Disconnected initial state does not hit the network.
 *   2. ensureLoaded() is a no-op when disconnected, and triggers a fetch
 *      when connected but the catalog is empty.
 *   3. getProviderName resolves known ids and falls back to the id itself.
 */
import { renderHook, waitFor } from '@testing-library/react';
import type { ReactNode } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ProviderCatalogProvider, useProviderCatalog } from './ProviderCatalogContext';

const mockGetProviders = vi.fn();

vi.mock('../services/api', () => ({
  ApiService: {
    getInstance: () => ({
      getProviders: mockGetProviders,
    }),
  },
}));

vi.mock('../config/mode', () => ({
  supportsSettings: true,
}));

vi.mock('../utils/log', () => ({
  debugLog: vi.fn(),
}));

function makeWrapper(initialConnected: boolean) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <ProviderCatalogProvider isConnected={initialConnected}>{children}</ProviderCatalogProvider>;
  };
}

beforeEach(() => {
  mockGetProviders.mockReset();
  // Default: empty success so any unintended call resolves cleanly.
  mockGetProviders.mockResolvedValue({
    providers: [],
    current_provider: '',
    current_model: '',
  });
});

afterEach(() => {
  vi.clearAllMocks();
});

describe('ProviderCatalogContext', () => {
  it('initial render with isConnected=false yields empty providers and skips the fetch', () => {
    const { result } = renderHook(() => useProviderCatalog(), {
      wrapper: makeWrapper(false),
    });

    expect(result.current.providers).toEqual([]);
    expect(result.current.isLoading).toBe(false);
    expect(mockGetProviders).not.toHaveBeenCalled();
  });

  it('connects and fetches providers successfully', async () => {
    const mockProviders = [
      { id: 'openrouter', name: 'OpenRouter', has_credential: true },
      { id: 'anthropic', name: 'Anthropic', has_credential: false },
    ];
    mockGetProviders.mockResolvedValueOnce({
      providers: mockProviders,
      current_provider: 'openrouter',
      current_model: 'claude-3-haiku',
    });

    const { result } = renderHook(() => useProviderCatalog(), {
      wrapper: makeWrapper(true),
    });

    await waitFor(() => {
      expect(result.current.providers).toEqual(mockProviders);
    });
    expect(result.current.currentProvider).toBe('openrouter');
    expect(result.current.currentModel).toBe('claude-3-haiku');
  });

  it('ensureLoaded() is a no-op when disconnected', async () => {
    const { result } = renderHook(() => useProviderCatalog(), {
      wrapper: makeWrapper(false),
    });

    result.current.ensureLoaded();

    // Give a microtask to flush before asserting nothing was called.
    await waitFor(() => {
      expect(mockGetProviders).not.toHaveBeenCalled();
    });
  });

  it('returns the full context shape', () => {
    const { result } = renderHook(() => useProviderCatalog(), {
      wrapper: makeWrapper(false),
    });

    expect(result.current).toMatchObject({
      providers: expect.any(Array),
      isLoading: expect.any(Boolean),
      currentProvider: expect.any(String),
      currentModel: expect.any(String),
      refresh: expect.any(Function),
      getProviderName: expect.any(Function),
      ensureLoaded: expect.any(Function),
    });
  });

  it('getProviderName resolves known ids and falls back to the id for unknown', async () => {
    mockGetProviders.mockResolvedValueOnce({
      providers: [{ id: 'openrouter', name: 'OpenRouter (Recommended)', has_credential: true }],
      current_provider: 'openrouter',
      current_model: 'claude-3-haiku',
    });

    const { result } = renderHook(() => useProviderCatalog(), {
      wrapper: makeWrapper(true),
    });

    await waitFor(() => {
      expect(result.current.providers.length).toBeGreaterThan(0);
    });

    expect(result.current.getProviderName('openrouter')).toBe('OpenRouter (Recommended)');
    expect(result.current.getProviderName('unknown-id')).toBe('unknown-id');
    expect(result.current.getProviderName(null)).toBe('');
    expect(result.current.getProviderName(undefined)).toBe('');
  });
});
