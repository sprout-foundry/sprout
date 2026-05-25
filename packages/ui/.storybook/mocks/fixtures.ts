import type { FileInfo } from '../../src/types/file-tree';
import type { GitStatusData } from '../../src/types/git-types';
import type { NotificationData } from '../../src/types/notification';
import type { CursorPosition } from '../../src/components/StatusBar';

/**
 * Sample file tree data for FileTree stories
 */
export const mockFileTree: FileInfo[] = [
  {
    name: 'src',
    path: 'src',
    isDir: true,
    size: 4096,
    modified: Date.now(),
    children: [
      {
        name: 'components',
        path: 'src/components',
        isDir: true,
        size: 4096,
        modified: Date.now(),
        children: [
          {
            name: 'Button.tsx',
            path: 'src/components/Button.tsx',
            isDir: false,
            size: 456,
            modified: Date.now(),
            ext: '.tsx',
          },
          {
            name: 'Card.tsx',
            path: 'src/components/Card.tsx',
            isDir: false,
            size: 789,
            modified: Date.now(),
            ext: '.tsx',
          },
          {
            name: 'Modal.tsx',
            path: 'src/components/Modal.tsx',
            isDir: false,
            size: 1234,
            modified: Date.now(),
            ext: '.tsx',
          },
        ],
      },
      {
        name: 'utils',
        path: 'src/utils',
        isDir: true,
        size: 4096,
        modified: Date.now(),
        children: [
          {
            name: 'helpers.ts',
            path: 'src/utils/helpers.ts',
            isDir: false,
            size: 567,
            modified: Date.now(),
            ext: '.ts',
          },
          {
            name: 'api.ts',
            path: 'src/utils/api.ts',
            isDir: false,
            size: 890,
            modified: Date.now(),
            ext: '.ts',
          },
        ],
      },
      {
        name: 'App.tsx',
        path: 'src/App.tsx',
        isDir: false,
        size: 2345,
        modified: Date.now(),
        ext: '.tsx',
      },
      {
        name: 'index.tsx',
        path: 'src/index.tsx',
        isDir: false,
        size: 567,
        modified: Date.now(),
        ext: '.tsx',
      },
      {
        name: 'styles.css',
        path: 'src/styles.css',
        isDir: false,
        size: 3456,
        modified: Date.now(),
        ext: '.css',
      },
    ],
  },
  {
    name: 'public',
    path: 'public',
    isDir: true,
    size: 4096,
    modified: Date.now(),
    children: [
      {
        name: 'index.html',
        path: 'public/index.html',
        isDir: false,
        size: 456,
        modified: Date.now(),
        ext: '.html',
      },
      {
        name: 'favicon.ico',
        path: 'public/favicon.ico',
        isDir: false,
        size: 15000,
        modified: Date.now(),
        ext: '.ico',
      },
    ],
  },
  {
    name: 'package.json',
    path: 'package.json',
    isDir: false,
    size: 890,
    modified: Date.now(),
    ext: '.json',
  },
  {
    name: 'README.md',
    path: 'README.md',
    isDir: false,
    size: 2345,
    modified: Date.now(),
    ext: '.md',
  },
  {
    name: 'tsconfig.json',
    path: 'tsconfig.json',
    isDir: false,
    size: 678,
    modified: Date.now(),
    ext: '.json',
  },
  {
    name: '.gitignore',
    path: '.gitignore',
    isDir: false,
    size: 234,
    modified: Date.now(),
    gitStatus: 'ignored',
  },
];

/**
 * Empty file tree for empty state stories
 */
export const emptyFileTree: FileInfo[] = [];

/**
 * Sample git status data for GitPanel stories
 */
export const mockGitStatus: GitStatusData = {
  branch: 'main',
  ahead: 2,
  behind: 0,
  staged: [
    {
      path: 'src/App.tsx',
      status: 'M',
      changes: { additions: 15, deletions: 5 },
    },
    {
      path: 'src/components/Button.tsx',
      status: 'M',
      changes: { additions: 8, deletions: 2 },
    },
  ],
  modified: [
    {
      path: 'src/components/Card.tsx',
      status: 'M',
      changes: { additions: 20, deletions: 10 },
    },
    {
      path: 'src/utils/api.ts',
      status: 'M',
    },
  ],
  untracked: [
    {
      path: 'src/components/Modal.tsx',
      status: '?',
    },
    {
      path: 'src/styles.css',
      status: '?',
    },
  ],
  deleted: [
    {
      path: 'src/old-file.tsx',
      status: 'D',
    },
  ],
  renamed: [],
  clean: false,
  truncated: false,
};

/**
 * Clean git status (no changes)
 */
export const cleanGitStatus: GitStatusData = {
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

/**
 * Sample git branches state
 */
export const mockGitBranches = {
  current: 'main',
  branches: ['main', 'develop', 'feature/awesome-feature', 'bugfix/fix-issue'],
};

/**
 * Sample notifications for NotificationStack stories
 */
export const mockNotifications: NotificationData[] = [
  {
    id: '1',
    type: 'info',
    title: 'Information',
    message: 'This is an informational notification.',
    createdAt: Date.now() - 5000,
    duration: 5000,
    read: false,
  },
  {
    id: '2',
    type: 'success',
    title: 'Success',
    message: 'Your changes have been saved successfully.',
    createdAt: Date.now() - 10000,
    duration: 4000,
    read: false,
  },
  {
    id: '3',
    type: 'warning',
    title: 'Warning',
    message: 'You have unsaved changes that may be lost.',
    createdAt: Date.now() - 15000,
    duration: 6000,
    read: false,
  },
  {
    id: '4',
    type: 'error',
    title: 'Error',
    message: 'Failed to save file. Please try again.',
    createdAt: Date.now() - 20000,
    duration: 8000,
    read: false,
  },
];

/**
 * Sample single notifications by type
 */
export const singleInfoNotification: NotificationData[] = [
  {
    id: '1',
    type: 'info',
    title: 'Information',
    message: 'This is an informational notification.',
    createdAt: Date.now(),
    duration: 5000,
    read: false,
  },
];

export const singleSuccessNotification: NotificationData[] = [
  {
    id: '1',
    type: 'success',
    title: 'Success',
    message: 'Your changes have been saved successfully.',
    createdAt: Date.now(),
    duration: 4000,
    read: false,
  },
];

export const singleWarningNotification: NotificationData[] = [
  {
    id: '1',
    type: 'warning',
    title: 'Warning',
    message: 'You have unsaved changes that may be lost.',
    createdAt: Date.now(),
    duration: 6000,
    read: false,
  },
];

export const singleErrorNotification: NotificationData[] = [
  {
    id: '1',
    type: 'error',
    title: 'Error',
    message: 'Failed to save file. Please try again.',
    createdAt: Date.now(),
    duration: 8000,
    read: false,
  },
];

/**
 * Sample status bar cursor positions
 */
export const mockCursorPosition: CursorPosition = {
  line: 42,
  column: 15,
};

/**
 * Mock file handlers for FileTree stories
 */
export const mockFileHandlers = {
  onFileSelect: (file: FileInfo) => console.log('File selected:', file.path),
  onRefresh: () => console.log('Refresh requested'),
  onItemCreated: () => console.log('Item created'),
  onDeleteItem: (path: string) => console.log('Item deleted:', path),
  onCreateFile: async (parentPath: string, name: string) => {
    console.log('Create file:', parentPath, name);
  },
  onCreateFolder: async (parentPath: string, name: string) => {
    console.log('Create folder:', parentPath, name);
  },
  onDeletePath: async (path: string, isDir: boolean) => {
    console.log('Delete path:', path, isDir);
  },
  onRenamePath: async (oldPath: string, newPath: string) => {
    console.log('Rename path:', oldPath, newPath);
  },
  onOpenInFileBrowser: async (path: string) => {
    console.log('Open in file browser:', path);
  },
};

/**
 * Mock git panel handlers
 */
export const mockGitHandlers = {
  onCommitMessageChange: (value: string) => console.log('Commit message changed:', value),
  onGenerateCommitMessage: () => console.log('Generate commit message'),
  onCommit: () => console.log('Commit'),
  onRunReview: () => console.log('Run review'),
  onCheckoutBranch: (branch: string) => console.log('Checkout branch:', branch),
  onCreateBranch: (name: string) => console.log('Create branch:', name),
  onPull: () => console.log('Pull'),
  onPush: () => console.log('Push'),
  onRefresh: () => console.log('Refresh git status'),
  onToggleFileSelection: (section: 'staged' | 'modified' | 'untracked' | 'deleted', path: string) =>
    console.log('Toggle file selection:', section, path),
  onToggleSectionSelection: (section: 'staged' | 'modified' | 'untracked' | 'deleted') =>
    console.log('Toggle section selection:', section),
  onClearSelection: () => console.log('Clear selection'),
  onPreviewFile: (section: 'staged' | 'modified' | 'untracked' | 'deleted', path: string) =>
    console.log('Preview file:', section, path),
  onStageSelected: () => console.log('Stage selected'),
  onUnstageSelected: () => console.log('Unstage selected'),
  onDiscardSelected: () => console.log('Discard selected'),
  onStageFile: (path: string) => console.log('Stage file:', path),
  onUnstageFile: (path: string) => console.log('Unstage file:', path),
  onDiscardFile: (path: string) => console.log('Discard file:', path),
  onSectionAction: (section: 'staged' | 'modified' | 'untracked' | 'deleted') =>
    console.log('Section action:', section),
};
