import { render, screen } from '@testing-library/react';
import React from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

beforeEach(() => {
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    configurable: true,
    value: vi.fn().mockReturnValue({ matches: false, addEventListener: vi.fn(), removeEventListener: vi.fn() }),
  });
});

vi.mock('../contexts/EditorManagerContext', () => ({
  __esModule: true,
  useEditorManager: () => ({
    panes: [{ id: 'pane-1', bufferId: 'buffer-1', isActive: true, position: 'primary' }],
    paneLayout: 'single',
    activePaneId: 'pane-1',
    activeBufferId: 'buffer-1',
    buffers: new Map([['buffer-1', { id: 'buffer-1', kind: 'chat', paneId: 'pane-1' }]]),
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
  normalizePaneSize: vi.fn((val) => val),
}));

vi.mock('./WorkspacePane', () => ({
  default: ({ chatProps }: { chatProps: { messages?: unknown[] } }) => (
    <div data-testid="pane-messages">{chatProps?.messages?.length ?? 0}</div>
  ),
}));
vi.mock('./ChatView', () => ({ default: () => null }));
vi.mock('./EditorWithOutline', () => ({
  default: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}));
vi.mock('./EditorTabs', () => ({ default: () => null }));
vi.mock('@sprout/ui', () => ({
  SkeletonText: () => null,
  CommandInput: () => null,
}));
vi.mock('./design/useDesignPresence', () => ({
  __esModule: true,
  useDesignPresence: () => ({ present: false, loading: false }),
}));

import EditorWorkspace from './EditorWorkspace';

const baseProps = {
  currentView: 'chat' as const,
  reviewProps: {},
  diffState: {},
  handleOutlineNavigateToSymbol: vi.fn(),
};

describe('EditorWorkspace pane props', () => {
  it('hands the pane the chat state of the current render, not the previous one', () => {
    const { rerender } = render(<EditorWorkspace {...baseProps} chatProps={{ messages: [] }} />);
    expect(screen.getByTestId('pane-messages').textContent).toBe('0');

    rerender(<EditorWorkspace {...baseProps} chatProps={{ messages: [{ id: 'm1' }] }} />);
    expect(screen.getByTestId('pane-messages').textContent).toBe('1');
  });
});
