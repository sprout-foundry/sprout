import type { CloudEndpoint } from '../types';

/**
 * Category (c) — synthetic: Should return pre-defined synthetic responses.
 */
export const syntheticEndpoints: CloudEndpoint[] = [
  {
    path: '/api/onboarding/status',
    methods: ['GET'],
    category: 'synthetic',
    syntheticResponse: { setup_required: false },
    description: 'Onboarding status (cloud is pre-configured)',
  },
  {
    path: '/api/onboarding/complete',
    methods: ['POST'],
    category: 'synthetic',
    syntheticResponse: { success: true },
    description: 'Complete onboarding',
  },
  {
    path: '/api/onboarding/skip',
    methods: ['POST'],
    category: 'synthetic',
    syntheticResponse: { success: true },
    description: 'Skip onboarding',
  },
  {
    path: '/api/instances',
    methods: ['GET'],
    category: 'synthetic',
    syntheticResponse: {
      instances: [],
      current_pid: 0,
      active_host_pid: 0,
      active_host_port: 0,
      desired_host_pid: 0,
    },
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
    syntheticResponse: { error: 'Not available in cloud mode' },
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
    syntheticResponse: { error: 'Not available in cloud mode' },
    description: 'Browse SSH directory (not available in cloud mode)',
  },
  {
    path: '/api/instances/ssh-close',
    methods: ['POST'],
    category: 'synthetic',
    syntheticResponse: { success: true },
    description: 'Close SSH session',
  },
  {
    path: '/api/instances/select',
    methods: ['POST'],
    category: 'synthetic',
    syntheticResponse: { success: true },
    description: 'Select instance',
  },
  {
    path: '/api/instances/ssh-launch-status',
    methods: ['GET'],
    category: 'synthetic',
    syntheticResponse: { ready: false, status: 'not_available' },
    description: 'SSH launch status',
  },
  {
    path: '/api/support-bundle',
    methods: ['GET'],
    category: 'synthetic',
    syntheticResponse: { message: 'Not available in cloud mode' },
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
    syntheticResponse: { workspace_root: '/', daemon_root: '/' },
    description: 'Workspace info (cloud mode: WASM shell owns workspace, virtual FS root)',
  },
];
