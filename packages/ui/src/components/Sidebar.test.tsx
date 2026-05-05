// Stricter type-checking is enabled but React's createElement overloads don't
// cleanly accept children as a rest parameter in strict TS. We use targeted
// suppressions on the specific call-sites that trigger errors.

import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import Sidebar from './Sidebar';
import type { SidebarProps } from './Sidebar';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

let container: HTMLDivElement;
let root: Root;

beforeAll(() => {
  // @ts-expect-error — assigning to undeclared globalThis property for React act() mode
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

afterAll(() => {
  delete (globalThis as any).IS_REACT_ACT_ENVIRONMENT;
});

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  jest.clearAllMocks();
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  container?.remove();
});

// ---------------------------------------------------------------------------
// Tests: Sidebar
// ---------------------------------------------------------------------------

describe('Sidebar', () => {
  const onSectionChange = jest.fn();
  const onToggleCollapse = jest.fn();
  const onFileSelect = jest.fn();

  beforeEach(() => {
    onSectionChange.mockClear();
    onToggleCollapse.mockClear();
    onFileSelect.mockClear();
  });

  it('renders with default props (non-collapsed)', () => {
    act(() => {
      root.render(
        createElement(Sidebar, {
          onSectionChange,
          onToggleCollapse,
          fileTreeProps: { onFileSelect },
        })
      );
    });
    expect(container.querySelector('.sidebar')).not.toBeNull();
    // Should not be collapsed
    expect(container.querySelector('.sidebar-collapsed')).toBeNull();
  });

  it('renders collapsed sidebar when collapsed=true', () => {
    act(() => {
      root.render(
        createElement(Sidebar, {
          collapsed: true,
          onToggleCollapse,
          fileTreeProps: { onFileSelect },
        })
      );
    });
    expect(container.querySelector('.sidebar-collapsed')).not.toBeNull();
    expect(container.querySelector('.sidebar')).toBeNull();
  });

  it('calls onToggleCollapse when expand button is clicked in collapsed mode', () => {
    act(() => {
      root.render(
        createElement(Sidebar, {
          collapsed: true,
          onToggleCollapse,
          fileTreeProps: { onFileSelect },
        })
      );
    });
    act(() => {
      container.querySelector('.sidebar-expand-btn')?.click();
    });
    expect(onToggleCollapse).toHaveBeenCalled();
  });

  it('calls onToggleCollapse when collapse button is clicked', () => {
    act(() => {
      root.render(
        createElement(Sidebar, {
          onToggleCollapse,
          fileTreeProps: { onFileSelect },
        })
      );
    });
    act(() => {
      container.querySelector('.sidebar-collapse-btn')?.click();
    });
    expect(onToggleCollapse).toHaveBeenCalled();
  });

  it('renders section tabs (files, git, search, settings)', () => {
    act(() => {
      root.render(
        createElement(Sidebar, {
          fileTreeProps: { onFileSelect },
        })
      );
    });
    const tabs = container.querySelectorAll('.sidebar-tab');
    expect(tabs).toHaveLength(4);
  });

  it('sets active class on the default "files" tab', () => {
    act(() => {
      root.render(
        createElement(Sidebar, {
          fileTreeProps: { onFileSelect },
        })
      );
    });
    const tabs = container.querySelectorAll('.sidebar-tab');
    // First tab (files) should be active by default
    expect(tabs[0].className).toContain('active');
  });

  it('calls onSectionChange when a tab is clicked', () => {
    act(() => {
      root.render(
        createElement(Sidebar, {
          onSectionChange,
          fileTreeProps: { onFileSelect },
        })
      );
    });
    const tabs = container.querySelectorAll('.sidebar-tab');
    // Click the second tab (git)
    act(() => {
      tabs[1]?.click();
    });
    expect(onSectionChange).toHaveBeenCalledWith('git');
  });

  it('switches to git section when git tab is clicked', () => {
    act(() => {
      root.render(
        createElement(Sidebar, {
          fileTreeProps: { onFileSelect },
          gitPanelProps: {
            gitStatus: null,
            gitBranches: { current: 'main', branches: [] },
            selectedFiles: new Set(),
            activeDiffSelectionKey: null,
            commitMessage: '',
            isLoading: false,
            isActing: false,
            isGeneratingCommitMessage: false,
            isReviewLoading: false,
            actionError: null,
            actionWarning: null,
            onCommitMessageChange: () => {},
            onGenerateCommitMessage: () => {},
            onCommit: () => {},
            onRunReview: () => {},
            onCheckoutBranch: () => {},
            onCreateBranch: () => {},
            onPull: () => {},
            onPush: () => {},
            onRefresh: () => {},
            onToggleFileSelection: () => {},
            onToggleSectionSelection: () => {},
            onClearSelection: () => {},
            onPreviewFile: () => {},
            onStageSelected: () => {},
            onUnstageSelected: () => {},
            onDiscardSelected: () => {},
            onStageFile: () => {},
            onUnstageFile: () => {},
            onDiscardFile: () => {},
            onSectionAction: () => {},
          },
        })
      );
    });
    // Click git tab
    const tabs = container.querySelectorAll('.sidebar-tab');
    act(() => {
      tabs[1]?.click();
    });
    // Git section content should appear
    expect(container.querySelector('.git-sidebar-panel')).not.toBeNull();
  });

  it('renders search section with empty state when no searchContent', () => {
    act(() => {
      root.render(
        createElement(Sidebar, {
          fileTreeProps: { onFileSelect },
        })
      );
    });
    const tabs = container.querySelectorAll('.sidebar-tab');
    // Click search tab (index 2)
    act(() => {
      tabs[2]?.click();
    });
    expect(container.querySelector('.sidebar-search')).not.toBeNull();
    expect(container.querySelector('.sidebar-empty-state')?.textContent).toBe('Search not configured');
  });

  it('renders custom searchContent when provided', () => {
    const searchContent = createElement('div', { className: 'my-search' }, 'Custom Search');
    act(() => {
      root.render(
        createElement(Sidebar, {
          fileTreeProps: { onFileSelect },
          searchContent,
        })
      );
    });
    const tabs = container.querySelectorAll('.sidebar-tab');
    act(() => {
      tabs[2]?.click();
    });
    expect(container.querySelector('.my-search')).not.toBeNull();
    expect(container.querySelector('.my-search')?.textContent).toBe('Custom Search');
  });

  it('renders settings section with empty state when no settingsContent', () => {
    act(() => {
      root.render(
        createElement(Sidebar, {
          fileTreeProps: { onFileSelect },
        })
      );
    });
    const tabs = container.querySelectorAll('.sidebar-tab');
    // Click settings tab (index 3)
    act(() => {
      tabs[3]?.click();
    });
    expect(container.querySelector('.sidebar-settings')).not.toBeNull();
    expect(container.querySelector('.sidebar-empty-state')?.textContent).toBe('Settings not configured');
  });

  it('renders custom settingsContent when provided', () => {
    const settingsContent = createElement('div', { className: 'my-settings' }, 'Custom Settings');
    act(() => {
      root.render(
        createElement(Sidebar, {
          fileTreeProps: { onFileSelect },
          settingsContent,
        })
      );
    });
    const tabs = container.querySelectorAll('.sidebar-tab');
    act(() => {
      tabs[3]?.click();
    });
    expect(container.querySelector('.my-settings')).not.toBeNull();
  });

  it('renders branding when provided', () => {
    const branding = createElement('div', { className: 'my-brand' }, 'Brand');
    act(() => {
      root.render(
        createElement(Sidebar, {
          branding,
          fileTreeProps: { onFileSelect },
        })
      );
    });
    expect(container.querySelector('.sidebar-branding')).not.toBeNull();
    expect(container.querySelector('.my-brand')).not.toBeNull();
  });

  it('does not render branding container when branding is undefined', () => {
    act(() => {
      root.render(
        createElement(Sidebar, {
          fileTreeProps: { onFileSelect },
        })
      );
    });
    expect(container.querySelector('.sidebar-branding')).toBeNull();
  });

  it('uses custom width when provided', () => {
    act(() => {
      root.render(
        createElement(Sidebar, {
          width: 300,
          fileTreeProps: { onFileSelect },
        })
      );
    });
    const sidebar = container.querySelector('.sidebar');
    expect(sidebar?.style.width).toBe('300px');
  });

  it('uses default width of 260px when not provided', () => {
    act(() => {
      root.render(
        createElement(Sidebar, {
          fileTreeProps: { onFileSelect },
        })
      );
    });
    const sidebar = container.querySelector('.sidebar');
    expect(sidebar?.style.width).toBe('260px');
  });

  it('uses collapsed width of 42px in collapsed mode', () => {
    act(() => {
      root.render(
        createElement(Sidebar, {
          collapsed: true,
          onToggleCollapse,
          fileTreeProps: { onFileSelect },
        })
      );
    });
    const collapsed = container.querySelector('.sidebar-collapsed');
    expect(collapsed?.style.width).toBe('42px');
  });

  it('uses controlled activeSection when provided', () => {
    act(() => {
      root.render(
        createElement(Sidebar, {
          activeSection: 'git',
          onSectionChange,
          fileTreeProps: { onFileSelect },
          gitPanelProps: {
            gitStatus: null,
            gitBranches: { current: 'main', branches: [] },
            selectedFiles: new Set(),
            activeDiffSelectionKey: null,
            commitMessage: '',
            isLoading: false,
            isActing: false,
            isGeneratingCommitMessage: false,
            isReviewLoading: false,
            actionError: null,
            actionWarning: null,
            onCommitMessageChange: () => {},
            onGenerateCommitMessage: () => {},
            onCommit: () => {},
            onRunReview: () => {},
            onCheckoutBranch: () => {},
            onCreateBranch: () => {},
            onPull: () => {},
            onPush: () => {},
            onRefresh: () => {},
            onToggleFileSelection: () => {},
            onToggleSectionSelection: () => {},
            onClearSelection: () => {},
            onPreviewFile: () => {},
            onStageSelected: () => {},
            onUnstageSelected: () => {},
            onDiscardSelected: () => {},
            onStageFile: () => {},
            onUnstageFile: () => {},
            onDiscardFile: () => {},
            onSectionAction: () => {},
          },
        })
      );
    });
    // Git section should be shown since activeSection is 'git'
    expect(container.querySelector('.git-sidebar-panel')).not.toBeNull();
  });

  it('updates section when clicking tabs even with controlled activeSection', () => {
    act(() => {
      root.render(
        createElement(Sidebar, {
          activeSection: 'files',
          onSectionChange,
          fileTreeProps: { onFileSelect },
        })
      );
    });
    const tabs = container.querySelectorAll('.sidebar-tab');
    act(() => {
      tabs[2]?.click(); // search tab
    });
    expect(onSectionChange).toHaveBeenCalledWith('search');
  });

  it('renders file tree in default files section', () => {
    act(() => {
      root.render(
        createElement(Sidebar, {
          fileTreeProps: { onFileSelect },
        })
      );
    });
    expect(container.querySelector('.file-tree')).not.toBeNull();
  });

  it('renders FileTree with forwarded fileTreeProps', () => {
    const files = [
      { name: 'test.txt', path: 'test.txt', isDir: false, ext: '.txt', gitStatus: undefined },
    ];
    act(() => {
      root.render(
        createElement(Sidebar, {
          fileTreeProps: { onFileSelect, files },
        })
      );
    });
    // FileTree should render and show the file
    expect(container.querySelector('.file-tree')).not.toBeNull();
  });
});
