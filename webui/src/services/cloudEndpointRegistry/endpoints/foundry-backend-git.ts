import type { CloudEndpoint } from '../types';

/**
 * Category (b) — browser-git: Git-related endpoints handled in-browser via
 * isomorphic-git (lightning-fs / IndexedDB). The CloudAdapter routes these to
 * handleBrowserGitRequest; they are NOT proxied to the Foundry backend.
 */
export const gitEndpoints: CloudEndpoint[] = [
  {
    path: '/api/git/status',
    methods: ['GET'],
    category: 'browser-git',
    description: 'Git status',
  },
  {
    path: '/api/git/branches',
    methods: ['GET'],
    category: 'browser-git',
    description: 'List git branches',
  },
  {
    path: '/api/git/checkout',
    methods: ['POST'],
    category: 'browser-git',
    description: 'Checkout branch/commit',
  },
  {
    path: '/api/git/branch/create',
    methods: ['POST'],
    category: 'browser-git',
    description: 'Create branch',
  },
  {
    path: '/api/git/pull',
    methods: ['POST'],
    category: 'browser-git',
    description: 'Git pull',
  },
  {
    path: '/api/git/push',
    methods: ['POST'],
    category: 'browser-git',
    description: 'Git push',
  },
  {
    path: '/api/git/stage',
    methods: ['POST'],
    category: 'browser-git',
    description: 'Stage files',
  },
  {
    path: '/api/git/unstage',
    methods: ['POST'],
    category: 'browser-git',
    description: 'Unstage files',
  },
  {
    path: '/api/git/discard',
    methods: ['POST'],
    category: 'browser-git',
    description: 'Discard changes',
  },
  {
    path: '/api/git/stage-all',
    methods: ['POST'],
    category: 'browser-git',
    description: 'Stage all files',
  },
  {
    path: '/api/git/unstage-all',
    methods: ['POST'],
    category: 'browser-git',
    description: 'Unstage all files',
  },
  {
    path: '/api/git/commit',
    methods: ['POST'],
    category: 'browser-git',
    description: 'Git commit',
  },
  {
    path: '/api/git/commit-message',
    methods: ['POST'],
    category: 'browser-git',
    description: 'Generate commit message',
  },
  {
    path: '/api/git/revert',
    methods: ['POST'],
    category: 'browser-git',
    description: 'Revert commit',
  },
  {
    path: '/api/git/deep-review',
    methods: ['POST'],
    category: 'browser-git',
    description: 'Deep review',
  },
  {
    path: '/api/git/deep-review/fix',
    methods: ['POST'],
    category: 'browser-git',
    description: 'Fix review items',
  },
  {
    path: '/api/git/deep-review/fix/start',
    methods: ['POST'],
    category: 'browser-git',
    description: 'Start fix process',
  },
  {
    path: '/api/git/deep-review/fix/status',
    methods: ['GET'],
    category: 'browser-git',
    description: 'Fix process status',
  },
  {
    path: '/api/git/diff',
    methods: ['GET'],
    category: 'browser-git',
    description: 'Git diff',
  },
  {
    path: '/api/git/log',
    methods: ['GET'],
    category: 'browser-git',
    description: 'Git log',
  },
  {
    path: '/api/git/confirm',
    methods: ['POST'],
    category: 'browser-git',
    description: 'Confirm git commit',
  },
  {
    path: '/api/git/commit/show',
    methods: ['POST'],
    category: 'browser-git',
    description: 'Show commit details',
  },
  {
    path: '/api/git/commit/show/file',
    methods: ['POST'],
    category: 'browser-git',
    description: 'Show file diff in commit',
  },
  {
    path: '/api/git/worktrees',
    methods: ['GET'],
    category: 'browser-git',
    description: 'List worktrees',
  },
  {
    path: '/api/git/worktree/create',
    methods: ['POST'],
    category: 'browser-git',
    description: 'Create worktree',
  },
  {
    path: '/api/git/worktree/remove',
    methods: ['POST'],
    category: 'browser-git',
    description: 'Remove worktree',
  },
  {
    path: '/api/git/worktree/checkout',
    methods: ['POST'],
    category: 'browser-git',
    description: 'Checkout worktree',
  },
];
