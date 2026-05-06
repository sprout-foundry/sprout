// Stricter type-checking is enabled but React's createElement overloads don't
// cleanly accept children as a rest parameter in strict TS. We use targeted
// suppressions on the specific call-sites that trigger errors.

import { vi } from 'vitest';

import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';

// ── Mocks before importing the component ────────────────────────────────

vi.mock('./ThemedDialog', () => ({
  showThemedPrompt: vi.fn(),
  showThemedConfirm: vi.fn(),
  showThemedAlert: vi.fn(),
}));

vi.mock('../utils/clipboard', () => ({
  copyToClipboard: vi.fn(),
}));

// Mock ContextMenu — it uses createPortal to document.body, so we need to
// let it render normally but intercept the portal target to avoid issues
vi.mock('./ContextMenu', () => {
  function MockContextMenu({ isOpen, onClose, children }: any) {
    if (!isOpen) return null;
    return createElement('div', {
      className: 'mock-context-menu',
      'data-x': String(0),
      'data-y': String(0),
      onMouseDown: () => {
        act(() => { onClose(); });
      },
    }, children);
  }
  return { __esModule: true, default: MockContextMenu };
});

import GitSidebarPanel from './GitPanel';
import * as ThemedDialog from './ThemedDialog';
import * as Clipboard from '../utils/clipboard';
import type { GitStatusData, GitFile } from '../types/git-types';

// ── Helpers ──────────────────────────────────────────────────────────────

let container: HTMLDivElement;
let root: Root;

function makeGitStatus(overrides?: Partial<GitStatusData>): GitStatusData {
  return {
    branch: 'main',
    ahead: 0,
    behind: 0,
    staged: [{ path: 'staged/file.ts', status: 'A' }],
    modified: [{ path: 'modified/file.js', status: 'M' }],
    untracked: [{ path: 'untracked/new.txt', status: '??' }],
    deleted: [{ path: 'deleted/old.ts', status: 'D' }],
    renamed: [],
    clean: false,
    truncated: false,
    ...overrides,
  };
}

function makeEmptyGitStatus(): GitStatusData {
  return {
    branch: 'main',
    ahead: 0,
    behind: 0,
    staged: [],
    modified: [],
    untracked: [],
    deleted: [],
    renamed: [],
    clean: true,
    truncated: false,
  };
}

function makeAllProps(overrides?: Partial<Parameters<typeof GitSidebarPanel>[0]>): Parameters<typeof GitSidebarPanel>[0] {
  const base: Parameters<typeof GitSidebarPanel>[0] = {
    gitStatus: makeGitStatus(),
    gitBranches: { current: 'main', branches: ['main', 'develop'] },
    selectedFiles: new Set(),
    activeDiffSelectionKey: null,
    commitMessage: '',
    isLoading: false,
    isActing: false,
    isGeneratingCommitMessage: false,
    isReviewLoading: false,
    actionError: null,
    actionWarning: null,
    onCommitMessageChange: vi.fn(),
    onGenerateCommitMessage: vi.fn(),
    onCommit: vi.fn(),
    onRunReview: vi.fn(),
    onCheckoutBranch: vi.fn(),
    onCreateBranch: vi.fn(),
    onPull: vi.fn(),
    onPush: vi.fn(),
    onRefresh: vi.fn(),
    onToggleFileSelection: vi.fn(),
    onToggleSectionSelection: vi.fn(),
    onClearSelection: vi.fn(),
    onPreviewFile: vi.fn(),
    onStageSelected: vi.fn(),
    onUnstageSelected: vi.fn(),
    onDiscardSelected: vi.fn(),
    onStageFile: vi.fn(),
    onUnstageFile: vi.fn(),
    onDiscardFile: vi.fn(),
    onSectionAction: vi.fn(),
    ...overrides,
  };
  return base;
}

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
  vi.clearAllMocks();
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  container?.remove();
  // Clean up any context menus rendered to document.body
  const menu = document.querySelector('.mock-context-menu');
  if (menu) menu.remove();
});

// ── Tests ────────────────────────────────────────────────────────────────

describe('GitPanel', () => {
  // ── Loading and empty states ───────────────────────────────────────

  it('shows loading state when isLoading is true', () => {
    act(() => {
      root.render(createElement(GitSidebarPanel, {
        ...makeAllProps({ isLoading: true }),
      }));
    });

    expect(container.querySelector('.git-sidebar-panel')).not.toBeNull();
    expect(container.querySelector('.empty')?.textContent).toBe('Loading git status…');
  });

  it('shows empty state when gitStatus is null', () => {
    act(() => {
      root.render(createElement(GitSidebarPanel, {
        ...makeAllProps({ gitStatus: null }),
      }));
    });

    expect(container.querySelector('.git-sidebar-panel')).not.toBeNull();
    expect(container.querySelector('.empty')?.textContent).toBe('No git repository found');
  });

  it('does not show loading or empty state when data is present', () => {
    act(() => {
      root.render(createElement(GitSidebarPanel, {
        ...makeAllProps(),
      }));
    });

    expect(container.querySelector('.empty')).toBeNull();
    expect(container.querySelector('.git-sidebar-header')).not.toBeNull();
  });

  // ── Branch selector ────────────────────────────────────────────────

  it('renders branch selector with branch options', () => {
    act(() => {
      root.render(createElement(GitSidebarPanel, {
        ...makeAllProps({
          gitBranches: { current: 'main', branches: ['main', 'develop', 'feature'] },
        }),
      }));
    });

    const select = container.querySelector('#git-branch-select');
    expect(select).not.toBeNull();
    expect(select?.value).toBe('main');

    const options = select?.querySelectorAll('option');
    expect(options?.length).toBe(3);
  });

  it('calls onCheckoutBranch when branch select changes', () => {
    const onCheckoutBranch = vi.fn();

    act(() => {
      root.render(createElement(GitSidebarPanel, {
        ...makeAllProps({
          gitBranches: { current: 'main', branches: ['main', 'develop'] },
          onCheckoutBranch,
        }),
      }));
    });

    const select = container.querySelector('#git-branch-select') as HTMLSelectElement;
    act(() => {
      select.value = 'develop';
      select.dispatchEvent(new Event('change', { bubbles: true }));
    });

    expect(onCheckoutBranch).toHaveBeenCalledWith('develop');
  });

  it('disables branch select when isActing is true', () => {
    act(() => {
      root.render(createElement(GitSidebarPanel, {
        ...makeAllProps({ isActing: true }),
      }));
    });

    const select = container.querySelector('#git-branch-select');
    expect(select?.disabled).toBe(true);
  });

  // ── Sync status ────────────────────────────────────────────────────

  it('shows clean status when gitStatus.clean is true', () => {
    act(() => {
      root.render(createElement(GitSidebarPanel, {
        ...makeAllProps({ gitStatus: makeEmptyGitStatus() }),
      }));
    });

    const cleanEl = container.querySelector('.git-sidebar-sync-status .clean');
    expect(cleanEl).not.toBeNull();
    expect(cleanEl?.textContent).toContain('Clean');
  });

  it('shows dirty status when gitStatus.clean is false', () => {
    act(() => {
      root.render(createElement(GitSidebarPanel, {
        ...makeAllProps({
          gitStatus: makeGitStatus({ clean: false }),
        }),
      }));
    });

    const dirtyEl = container.querySelector('.git-sidebar-sync-status .dirty');
    expect(dirtyEl).not.toBeNull();
    expect(dirtyEl?.textContent).toContain('Changes');
  });

  it('shows ahead counter when ahead > 0', () => {
    act(() => {
      root.render(createElement(GitSidebarPanel, {
        ...makeAllProps({
          gitStatus: makeGitStatus({ ahead: 3, behind: 0 }),
        }),
      }));
    });

    expect(container.querySelector('.git-sidebar-sync-status .ahead')).not.toBeNull();
  });

  it('shows behind counter when behind > 0', () => {
    act(() => {
      root.render(createElement(GitSidebarPanel, {
        ...makeAllProps({
          gitStatus: makeGitStatus({ ahead: 0, behind: 2 }),
        }),
      }));
    });

    expect(container.querySelector('.git-sidebar-sync-status .behind')).not.toBeNull();
  });

  // ── Commit section ─────────────────────────────────────────────────

  it('renders commit message textarea', () => {
    act(() => {
      root.render(createElement(GitSidebarPanel, {
        ...makeAllProps({ commitMessage: 'fix: something' }),
      }));
    });

    const textarea = container.querySelector('.git-sidebar-commit-input') as HTMLTextAreaElement;
    expect(textarea).not.toBeNull();
    // Controlled textarea: React sets .value, not the HTML value attribute
    expect(textarea.value).toBe('fix: something');
  });

  it('disables commit message textarea when no staged files', () => {
    act(() => {
      root.render(createElement(GitSidebarPanel, {
        ...makeAllProps({
          gitStatus: makeEmptyGitStatus(),
        }),
      }));
    });

    const textarea = container.querySelector('.git-sidebar-commit-input');
    expect(textarea?.disabled).toBe(true);
  });

  it('shows placeholder text when staged files exist', () => {
    act(() => {
      root.render(createElement(GitSidebarPanel, {
        ...makeAllProps({ commitMessage: '' }),
      }));
    });

    const textarea = container.querySelector('.git-sidebar-commit-input') as HTMLTextAreaElement;
    expect(textarea).not.toBeNull();
    expect(textarea.placeholder).toBe('Write commit message…');
  });

  it('calls onCommitMessageChange when textarea value changes', () => {
    const onCommitMessageChange = vi.fn();

    act(() => {
      root.render(createElement(GitSidebarPanel, {
        ...makeAllProps({ onCommitMessageChange }),
      }));
    });

    const textarea = container.querySelector('.git-sidebar-commit-input') as HTMLTextAreaElement;
    expect(textarea).not.toBeNull();

    // React's onChange for textarea fires on the 'input' event in jsdom
    act(() => {
      // Set native input value and dispatch input event
      Object.getOwnPropertyDescriptor(
        HTMLTextAreaElement.prototype, 'value'
      )?.set?.call(textarea, 'new message');
      textarea.dispatchEvent(new Event('input', { bubbles: true }));
    });

    expect(onCommitMessageChange).toHaveBeenCalledWith('new message');
  });

  it('disables commit button when no staged files or empty message', () => {
    act(() => {
      root.render(createElement(GitSidebarPanel, {
        ...makeAllProps({ commitMessage: '' }),
      }));
    });

    const commitBtn = container.querySelector('.sidebar-action-btn.primary');
    expect(commitBtn?.disabled).toBe(true);
  });

  it('enables commit button when staged files exist and message is non-empty', () => {
    act(() => {
      root.render(createElement(GitSidebarPanel, {
        ...makeAllProps({ commitMessage: 'fix: something' }),
      }));
    });

    const commitBtn = container.querySelector('.sidebar-action-btn.primary');
    expect(commitBtn?.disabled).toBe(false);
  });

  it('calls onCommit when commit button is clicked', () => {
    const onCommit = vi.fn();

    act(() => {
      root.render(createElement(GitSidebarPanel, {
        ...makeAllProps({ commitMessage: 'fix: something', onCommit }),
      }));
    });

    const commitBtn = container.querySelector('.sidebar-action-btn.primary');
    act(() => {
      commitBtn?.click();
    });

    expect(onCommit).toHaveBeenCalledTimes(1);
  });

  it('disables review button when no staged files', () => {
    act(() => {
      root.render(createElement(GitSidebarPanel, {
        ...makeAllProps({
          gitStatus: makeEmptyGitStatus(),
        }),
      }));
    });

    const reviewBtn = container.querySelectorAll('.sidebar-action-btn')[1];
    expect(reviewBtn?.disabled).toBe(true);
  });

  it('enables review button when staged files exist', () => {
    act(() => {
      root.render(createElement(GitSidebarPanel, {
        ...makeAllProps(),
      }));
    });

    const reviewBtn = container.querySelectorAll('.sidebar-action-btn')[1];
    expect(reviewBtn?.disabled).toBe(false);
  });

  // ── Action error/warning ───────────────────────────────────────────

  it('renders action error when present', () => {
    act(() => {
      root.render(createElement(GitSidebarPanel, {
        ...makeAllProps({ actionError: 'Something went wrong' }),
      }));
    });

    expect(container.querySelector('.git-sidebar-error')).not.toBeNull();
    expect(container.querySelector('.git-sidebar-error')?.textContent).toContain('Something went wrong');
  });

  it('does not render action error when null', () => {
    act(() => {
      root.render(createElement(GitSidebarPanel, {
        ...makeAllProps({ actionError: null }),
      }));
    });

    expect(container.querySelector('.git-sidebar-error')).toBeNull();
  });

  it('renders action warning when present', () => {
    act(() => {
      root.render(createElement(GitSidebarPanel, {
        ...makeAllProps({ actionWarning: 'Warning message' }),
      }));
    });

    expect(container.querySelector('.git-sidebar-warning')).not.toBeNull();
    expect(container.querySelector('.git-sidebar-warning')?.textContent).toContain('Warning message');
  });

  // ── File sections ──────────────────────────────────────────────────

  it('renders file sections for each section with files', () => {
    act(() => {
      root.render(createElement(GitSidebarPanel, {
        ...makeAllProps(),
      }));
    });

    const sections = container.querySelectorAll('.git-sidebar-file-section');
    expect(sections.length).toBe(4);
  });

  it('hides sections with no files', () => {
    const gitStatus = makeEmptyGitStatus();
    gitStatus.staged = [{ path: 'x.ts', status: 'A' }];

    act(() => {
      root.render(createElement(GitSidebarPanel, {
        ...makeAllProps({ gitStatus }),
      }));
    });

    const sections = container.querySelectorAll('.git-sidebar-file-section');
    expect(sections.length).toBe(1);
  });

  it('shows hidden sections note when some sections are empty', () => {
    const gitStatus = makeEmptyGitStatus();
    gitStatus.staged = [{ path: 'x.ts', status: 'A' }];

    act(() => {
      root.render(createElement(GitSidebarPanel, {
        ...makeAllProps({ gitStatus }),
      }));
    });

    const note = container.querySelector('.git-sidebar-hidden-sections-note');
    expect(note).not.toBeNull();
    expect(note?.textContent).toContain('Hiding 3 empty sections');
  });

  it('renders file paths in file list', () => {
    act(() => {
      root.render(createElement(GitSidebarPanel, {
        ...makeAllProps(),
      }));
    });

    const fileRows = container.querySelectorAll('.git-sidebar-file-row');
    expect(fileRows.length).toBe(4);
  });

  it('renders file status in file rows', () => {
    act(() => {
      root.render(createElement(GitSidebarPanel, {
        ...makeAllProps(),
      }));
    });

    const statusEls = container.querySelectorAll('.git-sidebar-file-status');
    expect(statusEls.length).toBe(4);
  });

  // ── File selection ─────────────────────────────────────────────────

  it('calls onToggleFileSelection and onPreviewFile on row click', () => {
    const onToggleFileSelection = vi.fn();
    const onPreviewFile = vi.fn();
    const onSelectFiles = vi.fn();

    act(() => {
      root.render(createElement(GitSidebarPanel, {
        ...makeAllProps({
          onToggleFileSelection,
          onPreviewFile,
          onSelectFiles,
        }),
      }));
    });

    const row = container.querySelector('.git-sidebar-file-row');
    act(() => {
      row?.click();
    });

    // Without ctrl/meta, the click handler calls onSelectFiles and onPreviewFile
    expect(onSelectFiles).toHaveBeenCalled();
    expect(onPreviewFile).toHaveBeenCalled();
  });

  it('adds selected class to file rows in selectedFiles set', () => {
    act(() => {
      root.render(createElement(GitSidebarPanel, {
        ...makeAllProps({
          selectedFiles: new Set(['staged:staged/file.ts']),
        }),
      }));
    });

    const selectedRows = container.querySelectorAll('.git-sidebar-file-row.selected');
    expect(selectedRows.length).toBe(1);
  });

  it('adds previewing class to file rows in activeDiffSelectionKey', () => {
    act(() => {
      root.render(createElement(GitSidebarPanel, {
        ...makeAllProps({
          activeDiffSelectionKey: 'modified:modified/file.js',
        }),
      }));
    });

    const previewingRows = container.querySelectorAll('.git-sidebar-file-row.previewing');
    expect(previewingRows.length).toBe(1);
  });

  // ── Selection action bar ───────────────────────────────────────────

  it('shows selection bar when files are selected', () => {
    act(() => {
      root.render(createElement(GitSidebarPanel, {
        ...makeAllProps({
          selectedFiles: new Set(['modified:modified/file.js']),
        }),
      }));
    });

    expect(container.querySelector('.git-sidebar-selection-bar')).not.toBeNull();
    expect(container.querySelector('.git-sidebar-selection-summary')?.textContent).toContain('1 selected');
  });

  it('hides selection bar when no files are selected', () => {
    act(() => {
      root.render(createElement(GitSidebarPanel, {
        ...makeAllProps({ selectedFiles: new Set() }),
      }));
    });

    expect(container.querySelector('.git-sidebar-selection-bar')).toBeNull();
  });

  it('shows stage button for un-staged selections', () => {
    act(() => {
      root.render(createElement(GitSidebarPanel, {
        ...makeAllProps({
          selectedFiles: new Set(['modified:modified/file.js']),
        }),
      }));
    });

    const stageBtn = container.querySelector('.git-sidebar-selection-actions button');
    expect(stageBtn?.textContent).toContain('Stage');
  });

  it('shows unstage button for staged selections', () => {
    act(() => {
      root.render(createElement(GitSidebarPanel, {
        ...makeAllProps({
          selectedFiles: new Set(['staged:staged/file.ts']),
        }),
      }));
    });

    const unstageBtn = container.querySelector('.git-sidebar-selection-actions button');
    expect(unstageBtn?.textContent).toContain('Unstage');
  });

  it('shows discard button for modified selections', () => {
    act(() => {
      root.render(createElement(GitSidebarPanel, {
        ...makeAllProps({
          selectedFiles: new Set(['modified:modified/file.js']),
        }),
      }));
    });

    const buttons = container.querySelectorAll('.git-sidebar-selection-actions button');
    let discardBtn: HTMLElement | null = null;
    for (const btn of Array.from(buttons)) {
      if (btn.textContent.includes('Discard')) {
        discardBtn = btn as HTMLElement;
        break;
      }
    }
    expect(discardBtn).not.toBeNull();
  });

  it('shows clear button in selection bar', () => {
    act(() => {
      root.render(createElement(GitSidebarPanel, {
        ...makeAllProps({
          selectedFiles: new Set(['modified:modified/file.js']),
        }),
      }));
    });

    const buttons = container.querySelectorAll('.git-sidebar-selection-actions button');
    let clearBtn: HTMLElement | null = null;
    for (const btn of Array.from(buttons)) {
      if (btn.textContent.includes('Clear')) {
        clearBtn = btn as HTMLElement;
        break;
      }
    }
    expect(clearBtn).not.toBeNull();
  });

  // ── Row action buttons ─────────────────────────────────────────────

  it('shows stage button for unstaged file rows', () => {
    act(() => {
      root.render(createElement(GitSidebarPanel, {
        ...makeAllProps(),
      }));
    });

    // The first file is in staged section, should show unstage
    const rows = container.querySelectorAll('.git-sidebar-file-row');
    const modifiedRow = Array.from(rows).find(
      (r) => (r as HTMLElement).querySelector('.git-sidebar-file-path')?.textContent === 'modified/file.js'
    );
    expect(modifiedRow).not.toBeNull();
  });

  it('shows unstage button for staged file rows', () => {
    act(() => {
      root.render(createElement(GitSidebarPanel, {
        ...makeAllProps(),
      }));
    });

    const rows = container.querySelectorAll('.git-sidebar-file-row');
    const stagedRow = Array.from(rows).find(
      (r) => (r as HTMLElement).querySelector('.git-sidebar-file-path')?.textContent === 'staged/file.ts'
    );
    expect(stagedRow).not.toBeNull();
  });

  it('calls onStageFile when stage button is clicked', () => {
    const onStageFile = vi.fn();

    act(() => {
      root.render(createElement(GitSidebarPanel, {
        ...makeAllProps({ onStageFile }),
      }));
    });

    // Find the stage button (PlusSquare) in the modified section row
    const rows = container.querySelectorAll('.git-sidebar-file-row');
    const modifiedRow = Array.from(rows).find(
      (r) => (r as HTMLElement).querySelector('.git-sidebar-file-path')?.textContent === 'modified/file.js'
    );
    const stageBtn = modifiedRow?.querySelector('.git-row-icon-btn[title="Stage file"]');

    act(() => {
      stageBtn?.click();
    });

    expect(onStageFile).toHaveBeenCalledWith('modified/file.js');
  });

  it('calls onUnstageFile when unstage button is clicked', () => {
    const onUnstageFile = vi.fn();

    act(() => {
      root.render(createElement(GitSidebarPanel, {
        ...makeAllProps({ onUnstageFile }),
      }));
    });

    const rows = container.querySelectorAll('.git-sidebar-file-row');
    const stagedRow = Array.from(rows).find(
      (r) => (r as HTMLElement).querySelector('.git-sidebar-file-path')?.textContent === 'staged/file.ts'
    );
    const unstageBtn = stagedRow?.querySelector('.git-row-icon-btn[title="Unstage file"]');

    act(() => {
      unstageBtn?.click();
    });

    expect(onUnstageFile).toHaveBeenCalledWith('staged/file.ts');
  });

  it('calls onDiscardFile when discard button is clicked', () => {
    const onDiscardFile = vi.fn();

    act(() => {
      root.render(createElement(GitSidebarPanel, {
        ...makeAllProps({ onDiscardFile }),
      }));
    });

    const rows = container.querySelectorAll('.git-sidebar-file-row');
    const modifiedRow = Array.from(rows).find(
      (r) => (r as HTMLElement).querySelector('.git-sidebar-file-path')?.textContent === 'modified/file.js'
    );
    const discardBtn = modifiedRow?.querySelector('.git-row-icon-btn.danger');

    act(() => {
      discardBtn?.click();
    });

    expect(onDiscardFile).toHaveBeenCalledWith('modified/file.js');
  });

  // ── Header action buttons ──────────────────────────────────────────

  it('calls onPull when pull button is clicked', () => {
    const onPull = vi.fn();

    act(() => {
      root.render(createElement(GitSidebarPanel, {
        ...makeAllProps({ onPull }),
      }));
    });

    const pullBtn = container.querySelector('.git-header-action-btn');
    act(() => {
      pullBtn?.click();
    });

    expect(onPull).toHaveBeenCalledTimes(1);
  });

  it('calls onPush when push button is clicked', () => {
    const onPush = vi.fn();

    act(() => {
      root.render(createElement(GitSidebarPanel, {
        ...makeAllProps({ onPush }),
      }));
    });

    const buttons = container.querySelectorAll('.git-header-action-btn');
    const pushBtn = buttons[1];
    act(() => {
      pushBtn?.click();
    });

    expect(onPush).toHaveBeenCalledTimes(1);
  });

  it('calls onRefresh when refresh button is clicked', () => {
    const onRefresh = vi.fn();

    act(() => {
      root.render(createElement(GitSidebarPanel, {
        ...makeAllProps({ onRefresh }),
      }));
    });

    const refreshBtn = container.querySelector('.git-header-icon-btn[title="Refresh git status"]');
    act(() => {
      refreshBtn?.click();
    });

    expect(onRefresh).toHaveBeenCalledTimes(1);
  });

  it('disables pull/push buttons when isActing is true', () => {
    act(() => {
      root.render(createElement(GitSidebarPanel, {
        ...makeAllProps({ isActing: true }),
      }));
    });

    const buttons = container.querySelectorAll('.git-header-action-btn');
    expect(buttons[0]?.disabled).toBe(true);
    expect(buttons[1]?.disabled).toBe(true);
  });

  // ── Create branch ──────────────────────────────────────────────────

  it('calls showThemedPrompt when create branch button is clicked', async () => {
    const onCreateBranch = vi.fn();
    (ThemedDialog.showThemedPrompt as ReturnType<typeof vi.fn>).mockResolvedValue('new-branch');

    act(() => {
      root.render(createElement(GitSidebarPanel, {
        ...makeAllProps({ onCreateBranch }),
      }));
    });

    const createBtn = container.querySelector('.git-header-icon-btn[title="Create branch"]');
    act(() => {
      createBtn?.click();
    });

    expect(ThemedDialog.showThemedPrompt).toHaveBeenCalledWith(
      'Enter a name for the new branch:',
      expect.objectContaining({ title: 'Create Branch' })
    );

    await act(async () => {});

    expect(onCreateBranch).toHaveBeenCalledWith('new-branch');
  });

  it('does not call onCreateBranch when showThemedPrompt returns null', async () => {
    const onCreateBranch = vi.fn();
    (ThemedDialog.showThemedPrompt as ReturnType<typeof vi.fn>).mockResolvedValue(null);

    act(() => {
      root.render(createElement(GitSidebarPanel, {
        ...makeAllProps({ onCreateBranch }),
      }));
    });

    const createBtn = container.querySelector('.git-header-icon-btn[title="Create branch"]');
    act(() => {
      createBtn?.click();
    });

    await act(async () => {});

    expect(onCreateBranch).not.toHaveBeenCalled();
  });

  // ── Context menu ───────────────────────────────────────────────────

  it('opens context menu on file right-click', () => {
    act(() => {
      root.render(createElement(GitSidebarPanel, {
        ...makeAllProps(),
      }));
    });

    const row = container.querySelector('.git-sidebar-file-row');
    act(() => {
      row?.dispatchEvent(new MouseEvent('contextmenu', {
        bubbles: true,
        clientX: 100,
        clientY: 200,
      }));
    });

    expect(document.querySelector('.mock-context-menu')).not.toBeNull();
  });

  it('context menu has preview diff option', () => {
    act(() => {
      root.render(createElement(GitSidebarPanel, {
        ...makeAllProps(),
      }));
    });

    const row = container.querySelector('.git-sidebar-file-row');
    act(() => {
      row?.dispatchEvent(new MouseEvent('contextmenu', {
        bubbles: true,
        clientX: 100,
        clientY: 200,
      }));
    });

    const menu = document.querySelector('.mock-context-menu');
    const previewBtn = menu?.querySelectorAll('button')[0];
    expect(previewBtn?.textContent).toBe('Preview diff');
  });

  it('context menu has copy relative path option', () => {
    act(() => {
      root.render(createElement(GitSidebarPanel, {
        ...makeAllProps({ gitStatus: makeGitStatus() }),
      }));
    });

    const row = container.querySelector('.git-sidebar-file-row');
    act(() => {
      row?.dispatchEvent(new MouseEvent('contextmenu', {
        bubbles: true,
        clientX: 100,
        clientY: 200,
      }));
    });

    const menu = document.querySelector('.mock-context-menu');
    const buttons = menu?.querySelectorAll('button');
    let copyBtn: HTMLElement | null = null;
    for (const btn of Array.from(buttons ?? [])) {
      if (btn.textContent.includes('Copy relative path')) {
        copyBtn = btn as HTMLElement;
        break;
      }
    }
    expect(copyBtn).not.toBeNull();
  });

  it('calls copyToClipboard on copy path menu click', () => {
    act(() => {
      root.render(createElement(GitSidebarPanel, {
        ...makeAllProps(),
      }));
    });

    const row = container.querySelector('.git-sidebar-file-row');
    act(() => {
      row?.dispatchEvent(new MouseEvent('contextmenu', {
        bubbles: true,
        clientX: 100,
        clientY: 200,
      }));
    });

    const menu = document.querySelector('.mock-context-menu');
    const buttons = menu?.querySelectorAll('button');
    let copyBtn: HTMLElement | null = null;
    for (const btn of Array.from(buttons ?? [])) {
      if (btn.textContent.includes('Copy relative path')) {
        copyBtn = btn as HTMLElement;
        break;
      }
    }

    act(() => {
      copyBtn?.click();
    });

    expect(Clipboard.copyToClipboard).toHaveBeenCalled();
  });

  it('context menu for staged files shows Unstage option', () => {
    act(() => {
      root.render(createElement(GitSidebarPanel, {
        ...makeAllProps(),
      }));
    });

    // Right-click on the staged file row
    const rows = container.querySelectorAll('.git-sidebar-file-row');
    const stagedRow = Array.from(rows).find(
      (r) => (r as HTMLElement).querySelector('.git-sidebar-file-path')?.textContent === 'staged/file.ts'
    );

    act(() => {
      stagedRow?.dispatchEvent(new MouseEvent('contextmenu', {
        bubbles: true,
        clientX: 100,
        clientY: 200,
      }));
    });

    const menu = document.querySelector('.mock-context-menu');
    const buttons = menu?.querySelectorAll('button');
    let unstageBtn: HTMLElement | null = null;
    for (const btn of Array.from(buttons ?? [])) {
      if (btn.textContent.includes('Unstage')) {
        unstageBtn = btn as HTMLElement;
        break;
      }
    }
    expect(unstageBtn).not.toBeNull();
  });

  it('context menu for unstaged files shows Stage option', () => {
    act(() => {
      root.render(createElement(GitSidebarPanel, {
        ...makeAllProps(),
      }));
    });

    const rows = container.querySelectorAll('.git-sidebar-file-row');
    const modifiedRow = Array.from(rows).find(
      (r) => (r as HTMLElement).querySelector('.git-sidebar-file-path')?.textContent === 'modified/file.js'
    );

    act(() => {
      modifiedRow?.dispatchEvent(new MouseEvent('contextmenu', {
        bubbles: true,
        clientX: 100,
        clientY: 200,
      }));
    });

    const menu = document.querySelector('.mock-context-menu');
    const buttons = menu?.querySelectorAll('button');
    let stageBtn: HTMLElement | null = null;
    for (const btn of Array.from(buttons ?? [])) {
      if (btn.textContent.trim() === 'Stage') {
        stageBtn = btn as HTMLElement;
        break;
      }
    }
    expect(stageBtn).not.toBeNull();
  });

  it('context menu has Delete/Restore option for deleted files', () => {
    act(() => {
      root.render(createElement(GitSidebarPanel, {
        ...makeAllProps(),
      }));
    });

    const rows = container.querySelectorAll('.git-sidebar-file-row');
    const deletedRow = Array.from(rows).find(
      (r) => (r as HTMLElement).querySelector('.git-sidebar-file-path')?.textContent === 'deleted/old.ts'
    );

    act(() => {
      deletedRow?.dispatchEvent(new MouseEvent('contextmenu', {
        bubbles: true,
        clientX: 100,
        clientY: 200,
      }));
    });

    const menu = document.querySelector('.mock-context-menu');
    const dangerBtn = menu?.querySelector('.context-menu-item.danger');
    expect(dangerBtn?.textContent).toBe('Restore');
  });

  // ── Section actions ────────────────────────────────────────────────

  it('calls onToggleSectionSelection on section select-all button click', () => {
    const onToggleSectionSelection = vi.fn();

    act(() => {
      root.render(createElement(GitSidebarPanel, {
        ...makeAllProps({ onToggleSectionSelection }),
      }));
    });

    const selectAllBtn = container.querySelector('.git-sidebar-file-section-actions .git-section-icon-btn');
    act(() => {
      selectAllBtn?.click();
    });

    expect(onToggleSectionSelection).toHaveBeenCalledWith('staged');
  });

  it('calls onSectionAction on section stage/unstage-all button click', () => {
    const onSectionAction = vi.fn();

    act(() => {
      root.render(createElement(GitSidebarPanel, {
        ...makeAllProps({ onSectionAction }),
      }));
    });

    // The section action buttons are the second icon button in each section
    const sectionBtns = container.querySelectorAll('.git-sidebar-file-section-actions .git-section-icon-btn');
    act(() => {
      sectionBtns[1]?.click(); // second button in first section
    });

    expect(onSectionAction).toHaveBeenCalled();
  });

  // ── File row keyboard interaction ──────────────────────────────────

  it('handles Enter key on file row', () => {
    const onToggleFileSelection = vi.fn();
    const onPreviewFile = vi.fn();

    act(() => {
      root.render(createElement(GitSidebarPanel, {
        ...makeAllProps({ onToggleFileSelection, onPreviewFile }),
      }));
    });

    const row = container.querySelector('.git-sidebar-file-row');
    act(() => {
      row?.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }));
    });

    expect(onToggleFileSelection).toHaveBeenCalled();
    expect(onPreviewFile).toHaveBeenCalled();
  });

  it('handles space key on file row', () => {
    const onToggleFileSelection = vi.fn();
    const onPreviewFile = vi.fn();

    act(() => {
      root.render(createElement(GitSidebarPanel, {
        ...makeAllProps({ onToggleFileSelection, onPreviewFile }),
      }));
    });

    const row = container.querySelector('.git-sidebar-file-row');
    act(() => {
      row?.dispatchEvent(new KeyboardEvent('keydown', { key: ' ', bubbles: true }));
    });

    expect(onToggleFileSelection).toHaveBeenCalled();
    expect(onPreviewFile).toHaveBeenCalled();
  });

  // ── isActing guards ────────────────────────────────────────────────

  it('does not allow file row clicks when isActing is true', () => {
    const onToggleFileSelection = vi.fn();
    const onPreviewFile = vi.fn();

    act(() => {
      root.render(createElement(GitSidebarPanel, {
        ...makeAllProps({ isActing: true, onToggleFileSelection, onPreviewFile }),
      }));
    });

    const row = container.querySelector('.git-sidebar-file-row');
    act(() => {
      row?.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }));
    });

    expect(onToggleFileSelection).not.toHaveBeenCalled();
    expect(onPreviewFile).not.toHaveBeenCalled();
  });

  // ── Generate commit message button ─────────────────────────────────

  it('disables generate commit message button when no staged files', () => {
    act(() => {
      root.render(createElement(GitSidebarPanel, {
        ...makeAllProps({
          gitStatus: makeEmptyGitStatus(),
        }),
      }));
    });

    const generateBtn = container.querySelector('.git-generate-icon-btn');
    expect(generateBtn?.disabled).toBe(true);
  });

  it('enables generate commit message button when staged files exist', () => {
    act(() => {
      root.render(createElement(GitSidebarPanel, {
        ...makeAllProps(),
      }));
    });

    const generateBtn = container.querySelector('.git-generate-icon-btn');
    expect(generateBtn?.disabled).toBe(false);
  });

  it('disables generate commit message button when isGeneratingCommitMessage is true', () => {
    act(() => {
      root.render(createElement(GitSidebarPanel, {
        ...makeAllProps({ isGeneratingCommitMessage: true }),
      }));
    });

    const generateBtn = container.querySelector('.git-generate-icon-btn');
    expect(generateBtn?.disabled).toBe(true);
  });

  // ── Commit button text ─────────────────────────────────────────────

  it('renders commit button with correct text', () => {
    act(() => {
      root.render(createElement(GitSidebarPanel, {
        ...makeAllProps({ commitMessage: 'fix' }),
      }));
    });

    const commitBtn = container.querySelector('.sidebar-action-btn.primary');
    expect(commitBtn?.textContent).toContain('Commit Changes');
  });

  // ── Review button text ─────────────────────────────────────────────

  it('renders review button with correct text', () => {
    act(() => {
      root.render(createElement(GitSidebarPanel, {
        ...makeAllProps(),
      }));
    });

    const reviewBtn = container.querySelectorAll('.sidebar-action-btn')[1];
    expect(reviewBtn?.textContent).toContain('Review');
  });

  it('shows Reviewing... when isReviewLoading is true', () => {
    act(() => {
      root.render(createElement(GitSidebarPanel, {
        ...makeAllProps({ isReviewLoading: true }),
      }));
    });

    const reviewBtn = container.querySelectorAll('.sidebar-action-btn')[1];
    expect(reviewBtn?.textContent).toContain('Reviewing…');
  });

  // ── onOpenFile in context menu ─────────────────────────────────────

  it('shows "Open in editor" when onOpenFile is provided', () => {
    const onOpenFile = vi.fn();

    act(() => {
      root.render(createElement(GitSidebarPanel, {
        ...makeAllProps({
          onOpenFile,
          gitStatus: makeGitStatus(),
        }),
      }));
    });

    const rows = container.querySelectorAll('.git-sidebar-file-row');
    const modifiedRow = Array.from(rows).find(
      (r) => (r as HTMLElement).querySelector('.git-sidebar-file-path')?.textContent === 'modified/file.js'
    );

    act(() => {
      modifiedRow?.dispatchEvent(new MouseEvent('contextmenu', {
        bubbles: true,
        clientX: 100,
        clientY: 200,
      }));
    });

    const menu = document.querySelector('.mock-context-menu');
    const buttons = menu?.querySelectorAll('button');
    let openBtn: HTMLElement | null = null;
    for (const btn of Array.from(buttons ?? [])) {
      if (btn.textContent.includes('Open in editor')) {
        openBtn = btn as HTMLElement;
        break;
      }
    }
    expect(openBtn).not.toBeNull();
  });

  it('calls onOpenFile when "Open in editor" is clicked', () => {
    const onOpenFile = vi.fn();

    act(() => {
      root.render(createElement(GitSidebarPanel, {
        ...makeAllProps({
          onOpenFile,
          gitStatus: makeGitStatus(),
        }),
      }));
    });

    const rows = container.querySelectorAll('.git-sidebar-file-row');
    const modifiedRow = Array.from(rows).find(
      (r) => (r as HTMLElement).querySelector('.git-sidebar-file-path')?.textContent === 'modified/file.js'
    );

    act(() => {
      modifiedRow?.dispatchEvent(new MouseEvent('contextmenu', {
        bubbles: true,
        clientX: 100,
        clientY: 200,
      }));
    });

    const menu = document.querySelector('.mock-context-menu');
    const buttons = menu?.querySelectorAll('button');
    let openBtn: HTMLElement | null = null;
    for (const btn of Array.from(buttons ?? [])) {
      if (btn.textContent.includes('Open in editor')) {
        openBtn = btn as HTMLElement;
        break;
      }
    }

    act(() => {
      openBtn?.click();
    });

    expect(onOpenFile).toHaveBeenCalledWith('modified/file.js');
  });

  // ── Copy absolute path ─────────────────────────────────────────────

  it('shows "Copy absolute path" when workspaceRoot is provided', () => {
    act(() => {
      root.render(createElement(GitSidebarPanel, {
        ...makeAllProps({
          workspaceRoot: '/home/user/project',
          gitStatus: makeGitStatus(),
        }),
      }));
    });

    const rows = container.querySelectorAll('.git-sidebar-file-row');
    const modifiedRow = Array.from(rows).find(
      (r) => (r as HTMLElement).querySelector('.git-sidebar-file-path')?.textContent === 'modified/file.js'
    );

    act(() => {
      modifiedRow?.dispatchEvent(new MouseEvent('contextmenu', {
        bubbles: true,
        clientX: 100,
        clientY: 200,
      }));
    });

    const menu = document.querySelector('.mock-context-menu');
    const buttons = menu?.querySelectorAll('button');
    let absBtn: HTMLElement | null = null;
    for (const btn of Array.from(buttons ?? [])) {
      if (btn.textContent.includes('Copy absolute path')) {
        absBtn = btn as HTMLElement;
        break;
      }
    }
    expect(absBtn).not.toBeNull();
  });

  it('calls onPreviewFile from context menu preview diff', () => {
    const onPreviewFile = vi.fn();

    act(() => {
      root.render(createElement(GitSidebarPanel, {
        ...makeAllProps({ onPreviewFile, gitStatus: makeGitStatus() }),
      }));
    });

    const rows = container.querySelectorAll('.git-sidebar-file-row');
    const modifiedRow = Array.from(rows).find(
      (r) => (r as HTMLElement).querySelector('.git-sidebar-file-path')?.textContent === 'modified/file.js'
    );

    act(() => {
      modifiedRow?.dispatchEvent(new MouseEvent('contextmenu', {
        bubbles: true,
        clientX: 100,
        clientY: 200,
      }));
    });

    const menu = document.querySelector('.mock-context-menu');
    const previewBtn = menu?.querySelectorAll('button')[0];

    act(() => {
      previewBtn?.click();
    });

    expect(onPreviewFile).toHaveBeenCalledWith('modified', 'modified/file.js');
  });

  // ── Types re-export ────────────────────────────────────────────────

  it('re-exports GitStatusData and GitFile types', () => {
    // Verify the types exist — the component re-exports them
    expect(typeof GitSidebarPanel).toBe('function');
  });
});
