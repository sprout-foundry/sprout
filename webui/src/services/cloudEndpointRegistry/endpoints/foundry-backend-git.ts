import type { CloudEndpoint } from '../types';

/**
 * Category (b) — foundry-backend: Git-related endpoints that must be proxied
 * to the Foundry backend.
 */
export const gitEndpoints: CloudEndpoint[] = [
  {
    path: '/api/git/status',
    methods: ['GET'],
    category: 'foundry-backend',
    description: 'Git status',
  },
  {
    path: '/api/git/branches',
    methods: ['GET'],
    category: 'foundry-backend',
    description: 'List git branches',
  },
  {
    path: '/api/git/checkout',
    methods: ['POST'],
    category: 'foundry-backend',
    description: 'Checkout branch/commit',
  },
  {
    path: '/api/git/branch/create',
    methods: ['POST'],
    category: 'foundry-backend',
    description: 'Create branch',
  },
  {
    path: '/api/git/pull',
    methods: ['POST'],
    category: 'foundry-backend',
    description: 'Git pull',
  },
  {
    path: '/api/git/push',
    methods: ['POST'],
    category: 'foundry-backend',
    description: 'Git push',
  },
  {
    path: '/api/git/stage',
    methods: ['POST'],
    category: 'foundry-backend',
    description: 'Stage files',
  },
  {
    path: '/api/git/unstage',
    methods: ['POST'],
    category: 'foundry-backend',
    description: 'Unstage files',
  },
  {
    path: '/api/git/discard',
    methods: ['POST'],
    category: 'foundry-backend',
    description: 'Discard changes',
  },
  {
    path: '/api/git/stage-all',
    methods: ['POST'],
    category: 'foundry-backend',
    description: 'Stage all files',
  },
  {
    path: '/api/git/unstage-all',
    methods: ['POST'],
    category: 'foundry-backend',
    description: 'Unstage all files',
  },
  {
    path: '/api/git/commit',
    methods: ['POST'],
    category: 'foundry-backend',
    description: 'Git commit',
  },
  {
    path: '/api/git/commit-message',
    methods: ['POST'],
    category: 'foundry-backend',
    description: 'Generate commit message',
  },
  {
    path: '/api/git/revert',
    methods: ['POST'],
    category: 'foundry-backend',
    description: 'Revert commit',
  },
  {
    path: '/api/git/deep-review',
    methods: ['POST'],
    category: 'foundry-backend',
    description: 'Deep review',
  },
  {
    path: '/api/git/deep-review/fix',
    methods: ['POST'],
    category: 'foundry-backend',
    description: 'Fix review items',
  },
  {
    path: '/api/git/deep-review/fix/start',
    methods: ['POST'],
    category: 'foundry-backend',
    description: 'Start fix process',
  },
  {
    path: '/api/git/deep-review/fix/status',
    methods: ['GET'],
    category: 'foundry-backend',
    description: 'Fix process status',
  },
  {
    path: '/api/git/diff',
    methods: ['GET'],
    category: 'foundry-backend',
    description: 'Git diff',
  },
  {
    path: '/api/git/log',
    methods: ['GET'],
    category: 'foundry-backend',
    description: 'Git log',
  },
  {
    path: '/api/git/confirm',
    methods: ['POST'],
    category: 'foundry-backend',
    description: 'Confirm git commit',
  },
  {
    path: '/api/git/commit/show',
    methods: ['POST'],
    category: 'foundry-backend',
    description: 'Show commit details',
  },
  {
    path: '/api/git/commit/show/file',
    methods: ['POST'],
    category: 'foundry-backend',
    description: 'Show file diff in commit',
  },
  {
    path: '/api/git/worktrees',
    methods: ['GET'],
    category: 'foundry-backend',
    description: 'List worktrees',
  },
  {
    path: '/api/git/worktree/create',
    methods: ['POST'],
    category: 'foundry-backend',
    description: 'Create worktree',
  },
  {
    path: '/api/git/worktree/remove',
    methods: ['POST'],
    category: 'foundry-backend',
    description: 'Remove worktree',
  },
  {
    path: '/api/git/worktree/checkout',
    methods: ['POST'],
    category: 'foundry-backend',
    description: 'Checkout worktree',
  },
];
