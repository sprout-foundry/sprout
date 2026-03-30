// @ts-nocheck

import React from 'react';
import { createRoot, Root } from 'react-dom/client';
import { act } from 'react';
import GitSidebarPanel from './GitSidebarPanel';
import type { GitStatusData } from './GitSidebarPanel';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

// Mock navigator.clipboard
const mockClipboardWriteText = jest.fn().mockResolvedValue(undefined);
Object.assign(navigator, {
  clipboard: { writeText: mockClipboardWriteText },
});

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const MOCK_GIT_STATUS: GitStatusData = {
  branch: 'main',
  ahead: 1,
  behind: 0,
  clean: false,
  staged: [{ path: 'src/app.tsx', status: 'M' }],
  modified: [
    { path: 'src/utils.ts', status: 'M', changes: { additions: 5, deletions: 2 } },
    { path: 'config.json', status: 'M' },
  ],
  untracked: [{ path: 'newfile.go', status: '?' }],
  deleted: [{ path: 'old.txt', status: 'D' }],
};

const MINIMAL_PROPS = {
  gitStatus: MOCK_GIT_STATUS,
  gitBranches: { current: 'main', branches: ['main', 'feature'] },
  selectedFiles: new Set<string>(),
  activeDiffSelectionKey: null,
  commitMessage: '',
  isLoading: false,
  isActing: false,
  isGeneratingCommitMessage: false,
  isReviewLoading: false,
  actionError: null,
  actionWarning: null,
  workspaceRoot: undefined as string | undefined,
  onCommitMessageChange: jest.fn(),
  onGenerateCommitMessage: jest.fn(),
  onCommit: jest.fn(),
  onRunReview: jest.fn(),
  onCheckoutBranch: jest.fn(),
  onCreateBranch: jest.fn(),
  onPull: jest.fn(),
  onPush: jest.fn(),
  onRefresh: jest.fn(),
  onToggleFileSelection: jest.fn(),
  onToggleSectionSelection: jest.fn(),
  onClearSelection: jest.fn(),
  onPreviewFile: jest.fn(),
  onStageSelected: jest.fn(),
  onUnstageSelected: jest.fn(),
  onDiscardSelected: jest.fn(),
  onStageFile: jest.fn(),
  onUnstageFile: jest.fn(),
  onDiscardFile: jest.fn(),
  onSectionAction: jest.fn(),
  onOpenFile: undefined as ((path: string) => void) | undefined,
};

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

let container: HTMLDivElement;
let root: Root;

beforeAll(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
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

const flushPromises = async () => {
  await act(async () => {
    await Promise.resolve();
  });
};

async function renderGitPanel(overrides: Partial<typeof MINIMAL_PROPS> = {}) {
  const props = { ...MINIMAL_PROPS, ...overrides };
  await act(async () => {
    root.render(<GitSidebarPanel {...(props as any)} />);
  });
  await flushPromises();
}

/**
 * Find a git file row by its path text and fire a contextmenu event on it.
 */
function fireContextMenuOnGitFile(filePath: string): void {
  const rows = document.querySelectorAll('.git-sidebar-file-row');
  for (const row of Array.from(rows)) {
    const pathEl = row.querySelector('.git-sidebar-file-path');
    if (pathEl && pathEl.textContent === filePath) {
      row.dispatchEvent(
        new MouseEvent('contextmenu', {
          bubbles: true,
          cancelable: true,
          clientX: 150,
          clientY: 250,
        })
      );
      return;
    }
  }
  throw new Error(`Could not find git file row with path "${filePath}"`);
}

/** Return all context menu buttons. */
function getContextButtons(): HTMLButtonElement[] {
  return Array.from(
    document.querySelectorAll('.file-tree-context-menu .file-tree-context-item')
  );
}

/** Get text content of all context menu buttons (trimmed). */
function getContextMenuTexts(): string[] {
  return getContextButtons().map((btn) => btn.textContent?.trim() ?? '');
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('GitSidebarPanel context menu – menu items', () => {
  it('shows "Copy relative path" and "Preview diff" for a file', async () => {
    await renderGitPanel();

    fireContextMenuOnGitFile('src/utils.ts');
    await flushPromises();

    const texts = getContextMenuTexts();
    expect(texts).toContain('Preview diff');
    expect(texts).toContain('Copy relative path');
    expect(texts).not.toContain('Open in editor');
  });

  it('shows "Copy absolute path" when workspaceRoot is provided', async () => {
    await renderGitPanel({ workspaceRoot: '/home/user/project' });

    fireContextMenuOnGitFile('src/utils.ts');
    await flushPromises();

    const texts = getContextMenuTexts();
    expect(texts).toContain('Copy absolute path');
  });

  it('does NOT show "Copy absolute path" when workspaceRoot is not provided', async () => {
    await renderGitPanel({ workspaceRoot: undefined });

    fireContextMenuOnGitFile('src/utils.ts');
    await flushPromises();

    const texts = getContextMenuTexts();
    expect(texts).not.toContain('Copy absolute path');
  });

  it('shows section-specific actions (Stage for modified, Unstage for staged)', async () => {
    await renderGitPanel();

    // Right-click a modified file → should show "Stage"
    fireContextMenuOnGitFile('src/utils.ts');
    await flushPromises();

    let texts = getContextMenuTexts();
    expect(texts).toContain('Stage');

    // Dismiss: click elsewhere
    document.body.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }));
    await flushPromises();

    // Right-click a staged file → should show "Unstage"
    fireContextMenuOnGitFile('src/app.tsx');
    await flushPromises();

    texts = getContextMenuTexts();
    expect(texts).toContain('Unstage');
  });
});

describe('GitSidebarPanel context menu – clipboard & editor actions', () => {
  it('"Copy relative path" calls clipboard with the file path', async () => {
    await renderGitPanel();

    fireContextMenuOnGitFile('config.json');
    await flushPromises();

    const copyRelBtn = getContextButtons().find(
      (btn) => btn.textContent?.trim() === 'Copy relative path'
    );
    expect(copyRelBtn).toBeDefined();

    await act(async () => {
      copyRelBtn!.click();
    });
    await flushPromises();

    expect(mockClipboardWriteText).toHaveBeenCalledWith('config.json');
  });

  it('"Copy absolute path" calls clipboard with workspaceRoot/file.path', async () => {
    await renderGitPanel({ workspaceRoot: '/home/user/project' });

    fireContextMenuOnGitFile('src/app.tsx');
    await flushPromises();

    const copyAbsBtn = getContextButtons().find(
      (btn) => btn.textContent?.trim() === 'Copy absolute path'
    );
    expect(copyAbsBtn).toBeDefined();

    await act(async () => {
      copyAbsBtn!.click();
    });
    await flushPromises();

    expect(mockClipboardWriteText).toHaveBeenCalledWith('/home/user/project/src/app.tsx');
  });

  it('"Preview diff" calls onPreviewFile with correct section and path', async () => {
    const onPreviewFile = jest.fn();
    await renderGitPanel({ onPreviewFile });

    fireContextMenuOnGitFile('newfile.go');
    await flushPromises();

    const previewBtn = getContextButtons().find(
      (btn) => btn.textContent?.trim() === 'Preview diff'
    );
    expect(previewBtn).toBeDefined();

    await act(async () => {
      previewBtn!.click();
    });
    await flushPromises();

    // newfile.go is in the 'untracked' section
    expect(onPreviewFile).toHaveBeenCalledWith('untracked', 'newfile.go');
  });

  it('context menu is dismissed after a copy action', async () => {
    await renderGitPanel();

    fireContextMenuOnGitFile('src/utils.ts');
    await flushPromises();

    expect(document.querySelector('.file-tree-context-menu')).not.toBeNull();

    const copyRelBtn = getContextButtons().find(
      (btn) => btn.textContent?.trim() === 'Copy relative path'
    );

    await act(async () => {
      copyRelBtn!.click();
    });
    await flushPromises();

    expect(document.querySelector('.file-tree-context-menu')).toBeNull();
  });

  it('"Open in editor" appears when onOpenFile is provided', async () => {
    const onOpenFile = jest.fn();
    await renderGitPanel({ onOpenFile });

    fireContextMenuOnGitFile('src/utils.ts');
    await flushPromises();

    const texts = getContextMenuTexts();
    expect(texts).toContain('Open in editor');
  });

  it('"Open in editor" does NOT appear when onOpenFile is undefined', async () => {
    await renderGitPanel({ onOpenFile: undefined });

    fireContextMenuOnGitFile('src/utils.ts');
    await flushPromises();

    const texts = getContextMenuTexts();
    expect(texts).not.toContain('Open in editor');
  });

  it('"Open in editor" calls onOpenFile with correct file path', async () => {
    const onOpenFile = jest.fn();
    await renderGitPanel({ onOpenFile });

    fireContextMenuOnGitFile('config.json');
    await flushPromises();

    const openBtn = getContextButtons().find(
      (btn) => btn.textContent?.trim() === 'Open in editor'
    );
    expect(openBtn).toBeDefined();

    await act(async () => {
      openBtn!.click();
    });
    await flushPromises();

    expect(onOpenFile).toHaveBeenCalledWith('config.json');
  });

  it('"Open in editor" does NOT appear for deleted files even when onOpenFile is provided', async () => {
    const onOpenFile = jest.fn();
    await renderGitPanel({ onOpenFile });

    fireContextMenuOnGitFile('old.txt');
    await flushPromises();

    const texts = getContextMenuTexts();
    expect(texts).not.toContain('Open in editor');
  });

  it('"Copy relative path" and "Copy absolute path" do NOT appear for deleted files', async () => {
    await renderGitPanel({ workspaceRoot: '/home/user/project' });

    fireContextMenuOnGitFile('old.txt');
    await flushPromises();

    const texts = getContextMenuTexts();
    expect(texts).not.toContain('Copy relative path');
    expect(texts).not.toContain('Copy absolute path');
  });
});
