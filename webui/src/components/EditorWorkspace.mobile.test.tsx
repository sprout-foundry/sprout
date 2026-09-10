import { describe, expect, it, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import React from 'react';

/**
 * P4.2 render tests — mobile peer-buffer keep-alive topology.
 *
 * Doctrine (2026-09-10): on the phone form factor the editor and chat
 * are PEER surfaces — neither is a sheet over the other, both stay
 * mounted, and the active buffer kind toggles visibility. These tests
 * pin the three load-bearing properties:
 *  1. the peer container + chat surface render and the chat surface is
 *     the visible one when the active buffer is chat;
 *  2. the editor surface renders when the active buffer is a file;
 *  3. when it does, the chat surface REMAINS MOUNTED (keep-alive:
 *     display:none, not unmount — chat state survives file opens).
 *
 * The editor manager is mocked with a controllable active buffer kind
 * (same pattern as EditorWorkspace.costs.test.tsx); jsdom matchMedia
 * reports mobile (<768px) so the mobile branch renders.
 */

const matchMediaMobile = vi.fn().mockReturnValue({
  matches: true,
  addEventListener: vi.fn(),
  removeEventListener: vi.fn(),
});

beforeEach(() => {
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    configurable: true,
    value: matchMediaMobile,
  });
});

// Controllable active buffer kind: 'chat' (default) or 'file'.
const activeBufferKind = { current: 'chat' as string };

vi.mock('../contexts/EditorManagerContext', () => ({
  __esModule: true,
  useEditorManager: () => ({
    panes: [],
    paneLayout: 'split-vertical',
    activePaneId: 'pane-1',
    activeBufferId: 'buffer-1',
    buffers: new Map([['buffer-1', { id: 'buffer-1', kind: activeBufferKind.current }]]),
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

import EditorWorkspace from './EditorWorkspace';

const minimalProps = {
  currentView: 'chat' as const,
  chatProps: {},
  reviewProps: {},
  diffState: {},
  handleOutlineNavigateToSymbol: vi.fn(),
};

describe('P4.2 mobile peer-buffer topology', () => {
  it('renders the peer-surfaces container with chat as the visible surface when chat is active', () => {
    activeBufferKind.current = 'chat';
    render(<EditorWorkspace {...minimalProps} />);

    expect(screen.getByTestId('mobile-peer-surfaces')).toBeInTheDocument();
    const chat = screen.getByTestId('mobile-chat-surface');
    expect(chat).toBeInTheDocument();
    expect(chat.getAttribute('data-active')).toBe('true');
    expect(screen.getByTestId('chat-view-mock')).toBeInTheDocument();
    // Editor surface is not rendered while chat is the active buffer.
    expect(screen.queryByTestId('mobile-editor-surface')).toBeNull();
  });

  it('editor surface is the visible one when a file is active', () => {
    activeBufferKind.current = 'file';
    render(<EditorWorkspace {...minimalProps} />);

    const editor = screen.getByTestId('mobile-editor-surface');
    expect(editor).toBeInTheDocument();
    expect(editor.getAttribute('data-active')).toBe('true');
    expect(screen.getByTestId('workspace-pane-mock')).toBeInTheDocument();
  });

  it('chat surface stays MOUNTED (keep-alive) while the editor surface is visible', () => {
    activeBufferKind.current = 'file';
    render(<EditorWorkspace {...minimalProps} />);

    const chat = screen.getByTestId('mobile-chat-surface');
    expect(chat).toBeInTheDocument();
    expect(chat.getAttribute('data-active')).toBe('false');
    // The keep-alive contract: chat DOM persists (state survives the
    // file open); only visibility toggles.
    expect(chat.querySelector('[data-testid="chat-view-mock"]')).not.toBeNull();
  });
});
