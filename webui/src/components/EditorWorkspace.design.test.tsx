// @ts-nocheck
/**
 * SP-140-3 item 3.3 — EditorWorkspace design view routing.
 *
 * Verifies the view branch: `currentView === 'design'` renders the
 * lazy-loaded DesignView (and nothing else does). Uses the same
 * lightweight-mock pattern as EditorWorkspace.costs.test.tsx to avoid OOM.
 */

import { render, screen, waitFor } from '@testing-library/react';
import React from 'react';

// ---------------------------------------------------------------------------
// Mocks — MUST be set up BEFORE importing EditorWorkspace
// ---------------------------------------------------------------------------

vi.mock('../contexts/EditorManagerContext', () => ({
  __esModule: true,
  useEditorManager: () => ({
    panes: [],
    paneLayout: 'split-vertical',
    activePaneId: null,
    activeBufferId: null,
    buffers: new Map(),
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

vi.mock('./EditorTabs', () => {
  const EditorTabs = ({ children }) => <div className="mock-editor-tabs">{children}</div>;
  return { default: EditorTabs };
});
vi.mock('./EditorWithOutline', () => {
  const EditorWithOutline = ({ children }) => <div className="mock-editor-outline">{children}</div>;
  return { default: EditorWithOutline };
});
vi.mock('./WorkspacePane', () => {
  const WorkspacePane = () => <div className="mock-workspace-pane" />;
  return { default: WorkspacePane };
});
vi.mock('./ResizeHandle', () => {
  const ResizeHandle = () => <div className="mock-resize-handle" />;
  return { default: ResizeHandle };
});
vi.mock('./ErrorBoundary', () => {
  const ErrorBoundary = ({ children }) => <div className="mock-error-boundary">{children}</div>;
  return { default: ErrorBoundary };
});

// Design presence gate — the route re-checks design/ presence (defense in
// depth). Default to present so the route branch is reachable.
let mockDesignPresent = true;
let mockDesignLoading = false;
vi.mock('./design/useDesignPresence', () => ({
  __esModule: true,
  useDesignPresence: () => ({ present: mockDesignPresent, loading: mockDesignLoading }),
}));

// Mock DesignView — the lazy import target. Proves the route reaches it; the
// dynamic-import chunk itself is asserted by the vite build, not here.
vi.mock('./design/DesignView', () => {
  const DesignView = ({ onBack, onOpenFile }) => (
    <div data-testid="design-view" data-back={onBack ? 'yes' : 'no'} data-open={onOpenFile ? 'yes' : 'no'}>
      DesignView
    </div>
  );
  return { default: DesignView };
});

vi.mock('@sprout/ui', () => ({
  SkeletonText: () => <div className="mock-skeleton" />,
  CommandInput: () => <div className="mock-command-input" />,
  Tooltip: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  Sidebar: () => <div className="mock-sidebar" />,
}));

// ---------------------------------------------------------------------------
// Import AFTER mocks are set up
// ---------------------------------------------------------------------------

import EditorWorkspace from './EditorWorkspace';

const minimalProps = {
  currentView: 'chat',
  chatProps: {},
  reviewProps: {},
  diffState: {},
  handleOutlineNavigateToSymbol: vi.fn(),
};

beforeEach(() => {
  mockDesignPresent = true;
  mockDesignLoading = false;
});

describe('EditorWorkspace design view routing', () => {
  it('renders DesignView when currentView is "design" and design/ exists', async () => {
    render(<EditorWorkspace {...minimalProps} currentView="design" />);
    await waitFor(
      () => {
        expect(screen.getByTestId('design-view')).toBeInTheDocument();
      },
      { timeout: 3000 },
    );
  });

  it('does NOT render DesignView when currentView is "chat"', () => {
    render(<EditorWorkspace {...minimalProps} currentView="chat" />);
    expect(() => screen.getByTestId('design-view')).toThrow();
  });

  it('does NOT render DesignView when design/ is absent, and redirects to chat', async () => {
    mockDesignPresent = false;
    const onViewChange = vi.fn();
    render(<EditorWorkspace {...minimalProps} currentView="design" onViewChange={onViewChange} />);

    // The lazy chunk must not be pulled in when the tree is absent.
    expect(() => screen.getByTestId('design-view')).toThrow();
    await waitFor(
      () => {
        expect(onViewChange).toHaveBeenCalledWith('chat');
      },
      { timeout: 3000 },
    );
    expect(() => screen.getByTestId('design-view')).toThrow();
  });

  it('holds the fallback (no DesignView) while design presence is still loading', () => {
    mockDesignLoading = true;
    render(<EditorWorkspace {...minimalProps} currentView="design" />);
    expect(() => screen.getByTestId('design-view')).toThrow();
  });

  it('threads onBack and onOpenDesignFile into DesignView', async () => {
    const onViewChange = vi.fn();
    const onOpenDesignFile = vi.fn();
    render(
      <EditorWorkspace
        {...minimalProps}
        currentView="design"
        onViewChange={onViewChange}
        onOpenDesignFile={onOpenDesignFile}
      />,
    );
    await waitFor(
      () => {
        expect(screen.getByTestId('design-view')).toHaveAttribute('data-back', 'yes');
      },
      { timeout: 3000 },
    );
    expect(screen.getByTestId('design-view')).toHaveAttribute('data-open', 'yes');
  });
});
