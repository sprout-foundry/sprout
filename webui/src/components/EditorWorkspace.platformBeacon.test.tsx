import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import React from 'react';

/**
 * SP-016 P0.7 usage instrumentation — in-editor plugin-page views.
 *
 * Pins the beacon contract in `EditorWorkspace`: whenever the editor
 * surfaces an in-editor plugin page (a registered plugin view), exactly
 * one `firePlatformViewBeacon(viewId)` call fires on mount — the data
 * source for the platform's `s016_embedded_view` exit metric, which is
 * how the Phase 1 "delete the embedded page" gate gets its usage
 * signal. Non-plugin views fire nothing, and a re-mount of the same
 * view fires again (the platform counts mounts, not sessions).
 *
 * Mirrors the focused-mock setup of EditorWorkspace.mobile.test.tsx:
 * the editor manager context is mocked (the plugin branch never
 * touches it), heavy sibling components are stubbed, and the REAL
 * PluginContext + plugin registry drive view discovery so the test
 * exercises the production lookup path.
 */

const { fireBeacon } = vi.hoisted(() => ({
  fireBeacon: vi.fn().mockResolvedValue(undefined),
}));

vi.mock('../bootstrapAdapter', () => ({
  firePlatformViewBeacon: (...args: unknown[]) => fireBeacon(...args),
}));

const matchMediaDesktop = vi.fn().mockReturnValue({
  matches: false,
  addEventListener: vi.fn(),
  removeEventListener: vi.fn(),
});

beforeEach(() => {
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    configurable: true,
    value: matchMediaDesktop,
  });
});

vi.mock('../contexts/EditorManagerContext', () => ({
  __esModule: true,
  useEditorManager: () => ({
    panes: [],
    paneLayout: 'split-vertical',
    activePaneId: 'pane-1',
    activeBufferId: 'buffer-1',
    buffers: new Map([['buffer-1', { id: 'buffer-1', kind: 'file' }]]),
    switchPane: vi.fn(),
    splitPane: vi.fn(),
    closePane: vi.fn(),
    closeSplit: vi.fn(),
    paneSizes: {},
    updatePaneSize: vi.fn(),
    maxPanes: 6,
    openWorkspaceBuffer: vi.fn(),
    isAutoSaveEnabled: false,
    whitespaceRenderingMode: 'boundary',
    isFormatOnSaveEnabled: false,
  }),
  MIN_PANE_WIDTH_PERCENT: 15,
  normalizePaneSize: vi.fn((val: unknown) => val),
}));

vi.mock('./WorkspacePane', () => ({
  default: () => <div data-testid="workspace-pane-mock" />,
}));
vi.mock('./ChatView', () => ({
  default: () => <div data-testid="chat-view-mock" />,
}));
vi.mock('./EditorWithOutline', () => ({
  default: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}));
vi.mock('./EditorTabs', () => ({ default: () => null }));
vi.mock('@sprout/ui', () => ({
  SkeletonText: () => <div className="mock-skeleton" />,
  CommandInput: () => <div className="mock-command-input" />,
}));
vi.mock('./design/useDesignPresence', () => ({
  __esModule: true,
  useDesignPresence: () => ({ present: false, loading: false }),
}));

import EditorWorkspace from './EditorWorkspace';
import { PluginContextProvider } from '../contexts/PluginContext';
import { pluginRegistry } from '../services/pluginRegistry';
import type { SproutPlugin } from '../types/plugin';

const testPlugin: SproutPlugin = {
  id: 'beacon-test-plugin',
  views: [
    {
      id: 'billing-view',
      label: 'Billing',
      component: () => <div data-testid="plugin-view">plugin page</div>,
    },
  ],
};

describe('SP-016 P0.7 embedded plugin-view beacon', () => {
  // EditorWorkspace reads plugin views from the REAL plugin context — wrap
  // in the real provider so the registered test plugin drives the
  // production lookup path (without it the context returns empty views).
  const app = (view: string) => (
    <PluginContextProvider>
      <EditorWorkspace currentView={view} />
    </PluginContextProvider>
  );

  beforeEach(() => {
    fireBeacon.mockClear();
    pluginRegistry.register(testPlugin);
  });

  afterEach(() => {
    pluginRegistry.unregister(testPlugin);
  });

  it('fires one beacon when a plugin view mounts in the editor', () => {
    render(app('billing-view'));

    expect(screen.getByTestId('plugin-view')).toBeInTheDocument();
    expect(fireBeacon).toHaveBeenCalledTimes(1);
    expect(fireBeacon).toHaveBeenCalledWith('billing-view');
  });

  it('fires nothing for non-plugin views', () => {
    const { rerender } = render(app('billing-view'));
    rerender(app('chat'));

    expect(screen.queryByTestId('plugin-view')).not.toBeInTheDocument();
    expect(fireBeacon).toHaveBeenCalledTimes(1); // only the initial mount
  });

  it('fires again when the same plugin view re-mounts', () => {
    const { rerender } = render(app('billing-view'));
    rerender(app('chat'));
    rerender(app('billing-view'));

    expect(fireBeacon).toHaveBeenCalledTimes(2);
    expect(fireBeacon).toHaveBeenLastCalledWith('billing-view');
  });
});
