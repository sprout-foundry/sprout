/**
 * Unit tests for AppStateContext
 */

import { render, renderHook } from '@testing-library/react';
import React from 'react';
import type { AppState } from '../types/app';
import { AppStateProvider, useAppStateContext } from './AppStateContext';

// Mock the persistence service
vi.mock('../services/appStatePersistence', () => ({
  loadPersistedAppState: vi.fn(() => ({
    provider: 'openai',
    model: 'gpt-4',
    messages: [],
    sessionId: 'test-session-123',
    queryCount: 5,
  })),
}));

describe('AppStateContext', () => {
  it('should create context with default values', () => {
    const wrapper = ({ children }: { children: React.ReactNode }) => <AppStateProvider>{children}</AppStateProvider>;

    const { result } = renderHook(() => useAppStateContext(), { wrapper });

    expect(result.current).toBeDefined();
    expect(result.current.state).toBeDefined();
    expect(typeof result.current.setState).toBe('function');
  });

  it('should load persisted state and merge with defaults', () => {
    const wrapper = ({ children }: { children: React.ReactNode }) => <AppStateProvider>{children}</AppStateProvider>;

    const { result } = renderHook(() => useAppStateContext(), { wrapper });

    expect(result.current.state.provider).toBe('openai');
    expect(result.current.state.model).toBe('gpt-4');
    expect(result.current.state.sessionId).toBe('test-session-123');
    expect(result.current.state.queryCount).toBe(5);

    // Runtime-only fields should be reset to defaults
    expect(result.current.state.isConnected).toBe(false);
    expect(result.current.state.isProcessing).toBe(false);
    expect(result.current.state.lastError).toBe(null);
  });

  it('should allow state updates using functional updater', () => {
    const { act: tlsAct } = require('@testing-library/react');
    const wrapper = ({ children }: { children: React.ReactNode }) => <AppStateProvider>{children}</AppStateProvider>;

    const { result } = renderHook(() => useAppStateContext(), { wrapper });

    // Update provider
    tlsAct(() => {
      result.current.setState((prev: AppState) => ({ ...prev, provider: 'anthropic' }));
    });
    expect(result.current.state.provider).toBe('anthropic');

    // Multiple updates
    tlsAct(() => {
      result.current.setState((prev: AppState) => ({
        ...prev,
        model: 'claude-3-opus',
        queryCount: prev.queryCount + 1,
      }));
    });
    expect(result.current.state.model).toBe('claude-3-opus');
    expect(result.current.state.queryCount).toBe(6);
  });

  it('should throw error when used outside provider', () => {
    const consoleErrorSpy = vi.spyOn(console, 'error').mockImplementation(() => {});

    expect(() => {
      renderHook(() => useAppStateContext());
    }).toThrow('useAppStateContext must be used within an AppStateProvider');

    consoleErrorSpy.mockRestore();
  });

  it('should render children correctly', () => {
    const TestChild = () => {
      const { state } = useAppStateContext();
      return <div>Provider: {state.provider}</div>;
    };

    const { getByText } = render(
      <AppStateProvider>
        <TestChild />
      </AppStateProvider>,
    );

    expect(getByText('Provider: openai')).toBeInTheDocument();
  });
});
