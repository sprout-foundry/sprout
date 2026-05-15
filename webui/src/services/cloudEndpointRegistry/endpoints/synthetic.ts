import type { CloudEndpoint } from '../types';

/**
 * Category (c) — synthetic: Should return pre-defined synthetic responses.
 *
 * These responses MUST match the Go server stub responses in
 * platform/internal/api/webui_compat.go so that both the client-side
 * CloudAdapter interception and the server-side handlers return identical data.
 */
export const syntheticEndpoints: CloudEndpoint[] = [
  {
    path: '/api/onboarding/status',
    methods: ['GET'],
    category: 'synthetic',
    syntheticResponse: { setup_required: false, onboarding_complete: true, providers: [] },
    description: 'Onboarding status (cloud is pre-configured)',
  },
  {
    path: '/api/onboarding/complete',
    methods: ['POST'],
    category: 'synthetic',
    syntheticResponse: { message: 'ok' },
    description: 'Complete onboarding',
  },
  {
    path: '/api/onboarding/skip',
    methods: ['POST'],
    category: 'synthetic',
    syntheticResponse: { message: 'ok' },
    description: 'Skip onboarding',
  },
  {
    path: '/api/instances',
    methods: ['GET'],
    category: 'synthetic',
    syntheticResponse: { instances: [] },
    description: 'List instances (no local instances in cloud mode)',
  },
  {
    path: '/api/instances/ssh-hosts',
    methods: ['GET'],
    category: 'synthetic',
    syntheticResponse: { hosts: [] },
    description: 'List SSH hosts (not available in cloud mode)',
  },
  {
    path: '/api/instances/ssh-open',
    methods: ['POST'],
    category: 'synthetic',
    syntheticResponse: { error: 'SSH not available in cloud mode' },
    description: 'Open SSH workspace (not available in cloud mode)',
  },
  {
    path: '/api/instances/ssh-sessions',
    methods: ['GET'],
    category: 'synthetic',
    syntheticResponse: { sessions: [] },
    description: 'List SSH sessions (not available in cloud mode)',
  },
  {
    path: '/api/instances/ssh-browse',
    methods: ['POST'],
    category: 'synthetic',
    syntheticResponse: { error: 'SSH not available in cloud mode' },
    description: 'Browse SSH directory (not available in cloud mode)',
  },
  {
    path: '/api/instances/ssh-close',
    methods: ['POST'],
    category: 'synthetic',
    syntheticResponse: { message: 'ok' },
    description: 'Close SSH session',
  },
  {
    path: '/api/instances/select',
    methods: ['POST'],
    category: 'synthetic',
    syntheticResponse: { error: 'Instance management not available in cloud mode' },
    description: 'Select instance (not available in cloud mode)',
  },
  {
    path: '/api/instances/ssh-launch-status',
    methods: ['GET'],
    category: 'synthetic',
    syntheticResponse: { in_progress: false },
    description: 'SSH launch status',
  },
  {
    path: '/api/support-bundle',
    methods: ['GET'],
    category: 'synthetic',
    syntheticResponse: { error: 'Support bundles not available in cloud mode' },
    description: 'Support bundle (not available in cloud mode)',
  },
  {
    path: '/api/config',
    methods: ['GET'],
    category: 'synthetic',
    syntheticResponse: {},
    description: 'App config (empty in cloud mode)',
  },
  {
    path: '/api/workspace',
    methods: ['GET', 'POST'],
    category: 'synthetic',
    // NOTE: This response intentionally omits is_project, needs_workspace_selection,
    // suggested_projects, and recent_workspaces. The consuming code (workspaceApi.ts
    // toWorkspaceResponse) defaults these to false/[] via nullish coalescing.
    // In cloud mode, workspace selection is not needed — the WASM shell owns the
    // virtual filesystem root, so needs_workspace_selection is effectively false.
    syntheticResponse: { message: 'ok', workspace_root: '/home/user', daemon_root: '/home/user' },
    description: 'Workspace info (cloud mode: WASM shell owns workspace, virtual FS root)',
  },
];
