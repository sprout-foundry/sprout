// @ts-nocheck
/**
 * Focused unit test: verifies that EditorWorkspace renders CostsPage
 * when currentView === 'costs'.
 *
 * Uses lightweight mocks to avoid OOM issues with heavy editor components.
 */

import { render, screen, waitFor } from '@testing-library/react';
import React from 'react';

// ---------------------------------------------------------------------------
// Mocks — MUST be set up BEFORE importing EditorWorkspace
// ---------------------------------------------------------------------------

// Mock EditorManagerContext
vi.mock('../contexts/EditorManagerContext', () => {
  return {
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
  };
});

// Mock EditorTabs — pass-through
vi.mock('./EditorTabs', () => {
  const EditorTabs = ({ children }) => <div className="mock-editor-tabs">{children}</div>;
  return { default: EditorTabs };
});

// Mock EditorWithOutline — pass-through
vi.mock('./EditorWithOutline', () => {
  const EditorWithOutline = ({ children }) => <div className="mock-editor-outline">{children}</div>;
  return { default: EditorWithOutline };
});

// Mock WorkspacePane — pass-through
vi.mock('./WorkspacePane', () => {
  const WorkspacePane = () => <div className="mock-workspace-pane" />;
  return { default: WorkspacePane };
});

// Mock ResizeHandle — pass-through
vi.mock('./ResizeHandle', () => {
  const ResizeHandle = () => <div className="mock-resize-handle" />;
  return { default: ResizeHandle };
});

// Mock ErrorBoundary — pass-through
vi.mock('./ErrorBoundary', () => {
  const ErrorBoundary = ({ children }) => <div className="mock-error-boundary">{children}</div>;
  return { default: ErrorBoundary };
});

// Mock CostsPage — returns a div with data-testid="costs-page" so we can assert it renders
vi.mock('./CostsPage', () => {
  const CostsPage = ({ onSessionClick }) => (
    <div
      data-testid="costs-page"
      data-on-session-click={onSessionClick ? 'yes' : 'no'}
    >
      CostsPage
    </div>
  );
  return { default: CostsPage };
});

// Mock platform pages (lazy-loaded)
vi.mock('./platform', () => ({
  TasksPage: () => <div data-testid="tasks-page">TasksPage</div>,
  BillingPage: () => <div data-testid="billing-page">BillingPage</div>,
  TeamPage: () => <div data-testid="team-page">TeamPage</div>,
}));

// Mock @sprout/ui
vi.mock('@sprout/ui', () => ({
  SkeletonText: () => <div className="mock-skeleton" />,
}));

// ---------------------------------------------------------------------------
// Import AFTER mocks are set up
// ---------------------------------------------------------------------------

import EditorWorkspace from './EditorWorkspace';

/** Minimal props for EditorWorkspace */
const minimalProps = {
  currentView: 'chat',
  chatProps: {},
  reviewProps: {},
  diffState: {},
  handleOutlineNavigateToSymbol: vi.fn(),
};

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('EditorWorkspace costs view routing', () => {
  it('renders CostsPage when currentView is "costs"', async () => {
    render(
      <EditorWorkspace
        {...minimalProps}
        currentView="costs"
      />
    );

    // CostsPage is lazy-loaded via React.lazy(), so we wait for it to resolve
    await waitFor(() => {
      expect(screen.getByTestId('costs-page')).toBeInTheDocument();
    }, { timeout: 3000 });
  });

  it('does NOT render CostsPage when currentView is "chat"', async () => {
    render(
      <EditorWorkspace
        {...minimalProps}
        currentView="chat"
      />
    );

    // CostsPage should not be in the DOM for chat view
    expect(() => screen.getByTestId('costs-page')).toThrow();
  });

  it('threads onSessionRestore to CostsPage as onSessionClick', async () => {
    const onSessionRestore = vi.fn();

    render(
      <EditorWorkspace
        {...minimalProps}
        currentView="costs"
        onSessionRestore={onSessionRestore}
      />
    );

    await waitFor(() => {
      const costsPage = screen.getByTestId('costs-page');
      expect(costsPage).toBeInTheDocument();
      expect(costsPage).toHaveAttribute('data-on-session-click', 'yes');
    }, { timeout: 3000 });
  });

  it('renders CostsPage without onSessionRestore when not provided', async () => {
    render(
      <EditorWorkspace
        {...minimalProps}
        currentView="costs"
      />
    );

    await waitFor(() => {
      const costsPage = screen.getByTestId('costs-page');
      expect(costsPage).toBeInTheDocument();
      expect(costsPage).toHaveAttribute('data-on-session-click', 'no');
    }, { timeout: 3000 });
  });
});
