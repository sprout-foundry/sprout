/**
 * testids.ts - Canonical registry of all data-testid attributes used in the
 * Sprout webui for Playwright E2E testing.
 *
 * Convention:
 *   - kebab-case names, prefixed by area (chat-*, sidebar-*, editor-*, etc.)
 *
 * Adding a new testid:
 *   1. Add the name to the TESTIDS object below.
 *   2. Add the data-testid attribute to the target component element.
 *   3. Run: npx vitest run test/webui/testids.test.ts
 *
 * Coverage check:
 *   testids.test.ts scans webui/src for every data-testid and asserts it
 *   appears in TESTIDS_SET. It also checks forward-references: every key
 *   in TESTIDS must be used by at least one component.
 *
 * Documentation:
 *   See docs/webui-e2e.md (SP-087-7) for the full E2E testing strategy.
 */

// Canonical testid registry
const TESTIDS = {
  // Chat
  'chat-shell': 'chat-shell',
  'chat-main': 'chat-main',
  'chat-input': 'chat-input',
  'chat-send': 'chat-send',
  'chat-message': 'chat-message',
  'chat-message-user': 'chat-message-user',
  'chat-message-assistant': 'chat-message-assistant',
  'chat-message-list': 'chat-message-list',
  'chat-scroll-bottom': 'chat-scroll-bottom',
  'chat-export-button': 'chat-export-button',
  'chat-offline-panel': 'chat-offline-panel',
  'chat-offline-retry': 'chat-offline-retry',
  'chat-no-provider': 'chat-no-provider',
  'chat-provider-setup': 'chat-provider-setup',
  'chat-welcome': 'chat-welcome',
  'chat-processing': 'chat-processing',
  'chat-error': 'chat-error',
  'chat-tool-timeline': 'chat-tool-timeline',
  'chat-subagent-feed': 'chat-subagent-feed',
  'chat-query-progress': 'chat-query-progress',
  'chat-sessions-empty': 'chat-sessions-empty',
  'chat-item': 'chat-item',
  'chat-new-button': 'chat-new-button',

  // Sidebar
  'sidebar-container': 'sidebar-container',
  'sidebar-brand': 'sidebar-brand',
  'sidebar-sessions-search-input': 'sidebar-session-search-input',
  'sidebar-sessions-search-clear': 'sidebar-session-search-clear',
  'sidebar-sessions-search-dropdown': 'sidebar-session-search-dropdown',
  'sidebar-sessions-search-loading': 'sidebar-session-search-loading',
  'sidebar-sessions-search-error': 'sidebar-session-search-error',
  'sidebar-costs-button': 'sidebar-costs-button',
  'sidebar-settings-toggle': 'sidebar-settings-toggle',
  'sidebar-export-all': 'sidebar-export-all',
  'sidebar-icon-rail': 'sidebar-icon-rail',
  'sidebar-git-tab': 'sidebar-git-tab',
  'sidebar-files-tab': 'sidebar-files-tab',
  'sidebar-search-tab': 'sidebar-search-tab',
  'sidebar-automations-tab': 'sidebar-automations-tab',
  'sidebar-logs-tab': 'sidebar-logs-tab',

  // SP-092-3: Past sessions hint
  'past-sessions-hint': 'past-sessions-hint',
  'past-sessions-hint-input': 'past-sessions-hint-input',
  'past-sessions-hint-loading': 'past-sessions-hint-loading',
  'past-sessions-hint-empty': 'past-sessions-hint-empty',
  'past-sessions-hint-card-abc123': 'past-sessions-hint-card-abc123',

  // Editor
  'editor': 'editor',
  'editor-pane': 'editor-pane',
  'editor-empty': 'editor-empty',
  'editor-language-switcher': 'language-switcher-button',
  'editor-language-popup': 'language-switcher-popup',
  'editor-footer': 'editor-footer',
  'editor-welcome-tab': 'editor-welcome-tab',
  'editor-image-viewer': 'image-viewer',

  // Terminal
  'terminal-pane': 'terminal-pane',
  'terminal-container': 'terminal-container',
  'terminal-toggle': 'terminal-toggle',
  'terminal-collapse': 'terminal-collapse',
  'terminal-tab-bar': 'terminal-tab-bar',

  // Onboarding
  'onboarding-overlay': 'onboarding-overlay',
  'onboarding-card': 'onboarding-card',
  'onboarding-step': 'onboarding-step',
  'onboarding-skip': 'onboarding-skip',
  'onboarding-done': 'onboarding-done',
  'onboarding-provider-grid': 'onboarding-provider-grid',
  'onboarding-provider-card': 'onboarding-provider-card',
  'onboarding-model-input': 'onboarding-model-input',
  'onboarding-api-key': 'onboarding-api-key',
  'onboarding-close': 'onboarding-close',
  'onboarding-toggle-providers': 'onboarding-toggle-providers',
  'onboarding-refresh': 'onboarding-refresh',

  // Worktree
  'worktree-panel': 'worktree-panel',
  'worktree-create-button': 'worktree-create-button',
  'worktree-list': 'worktree-list',
  'worktree-item': 'worktree-item',
  'worktree-switch': 'worktree-switch',
  'worktree-delete': 'worktree-delete',

  // Context panel
  'context-panel': 'context-panel',
  'context-panel-collapse': 'context-panel-collapse',
  'context-panel-tab': 'context-panel-tab',
  'context-panel-sessions': 'context-panel-sessions',
  'context-panel-tools': 'context-panel-tools',
  'context-panel-subagents': 'context-panel-subagents',
  'context-panel-changes': 'context-panel-changes',
  'context-panel-tasks': 'context-panel-tasks',
  'context-panel-status': 'context-panel-status',

  // Status bar
  'status-bar': 'status-bar',
  'status-bar-workspace': 'status-bar-workspace',
  'status-bar-notification': 'status-bar-notification',

  // Settings
  'settings-panel': 'settings-panel',
  'settings-filter': 'settings-filter',
  'settings-section': 'settings-section',
  'settings-providers-tab': 'settings-providers-tab',
  'settings-skills-tab': 'settings-skills-tab',
  'settings-mcp-tab': 'settings-mcp-tab',
  'settings-credentials-link': 'settings-credentials-link',
  'settings-primary-provider': 'settings-primary-provider',
  'settings-primary-model': 'settings-primary-model',
  'settings-current-provider': 'settings-current-provider',
  'settings-whitespace-mode': 'settings-whitespace-mode',
  'settings-subagent-tab': 'settings-subagent-tab',
  'settings-agent-general-tab': 'settings-agent-general-tab',
  'settings-agent-behavior-tab': 'settings-agent-behavior-tab',
  'settings-agent-skills-tab': 'settings-agent-skills-tab',
  'settings-agent-memory-tab': 'settings-agent-memory-tab',
  'settings-workspace-embeddings-tab': 'settings-workspace-embeddings-tab',
  'settings-workspace-lsp-tab': 'settings-workspace-lsp-tab',
  'settings-workspace-mcp-tab': 'settings-workspace-mcp-tab',
  'settings-editor-preferences-tab': 'settings-editor-preferences-tab',
  'settings-editor-notifications-tab': 'settings-editor-notifications-tab',
  'settings-commit-review-tab': 'settings-env-commit-review-tab',
  'settings-ocr-tab': 'settings-env-ocr-tab',
  'settings-performance-tab': 'settings-env-performance-tab',
  'settings-computer-use-tab': 'settings-experimental-computer-use-tab',

  // Skills
  'skills-install-source': 'skills-install-source',
  'skills-install-ref': 'skills-install-ref',
  'skills-install-force': 'skills-install-force',
  'skills-registry-dropdown': 'skills-registry-dropdown',
  'skills-install-button': 'skills-install-button',
  'skills-list-item-my-skill': 'skills-list-item-my-skill',
  'skills-update-button-my-skill': 'skills-update-button-my-skill',
  'skills-remove-button-my-skill': 'skills-remove-button-my-skill',

  // Model picker
  'model-picker': 'model-picker',
  'model-picker-option': 'model-picker-option',
  'model-picker-current': 'model-picker-current',

  // Export dialog
  'export-dialog': 'export-dialog',
  'export-format-markdown': 'export-format-markdown',
  'export-format-json': 'export-format-json',
  'export-format-text': 'export-format-html',
  'export-include-tool-calls': 'export-include-tool-calls',
  'export-include-cost': 'export-include-cost',
  'export-redact-secrets': 'export-redact-secrets',
  'export-cancel': 'export-cancel',
  'export-download': 'export-download',

  // Costs page
  'costs-page': 'costs-page',
  'costs-time-range-all': 'costs-time-range-all',
  'costs-time-range-month': 'costs-time-range-30d',
  'costs-time-range-week': 'costs-time-range-7d',
  'costs-loading': 'costs-loading',
  'costs-error': 'costs-error',
  'costs-empty': 'costs-empty',
  'costs-summary-total': 'costs-summary-total',
  'costs-token-value': 'costs-token-value',
  'costs-billing-breakdown': 'costs-billing-breakdown',
  'costs-billing-pay_per_token': 'costs-billing-pay_per_token',
  'costs-billing-subscription': 'costs-billing-subscription',
  'costs-billing-free': 'costs-billing-free',
  'costs-stale-banner': 'costs-stale-banner',
  'costs-back-btn': 'costs-back-btn',
  'cost-summary-cards': 'cost-summary-cards',
  'cost-card-this-month': 'cost-card-month',
  'cost-card-this-week': 'cost-card-week',
  'cost-card-today': 'cost-card-today',
  'cost-card-month-value': 'cost-card-month-value',
  'by-model-chart': 'by-model-chart',
  'by-model-empty': 'by-model-empty',
  'by-model-row-0': 'by-model-row-0',
  'daily-spend-chart': 'daily-spend-chart',
  'daily-spend-empty': 'daily-spend-empty',
  'daily-spend-bar-2025-01-01': 'daily-spend-bar-2025-01-01',
  'top-sessions-table': 'top-sessions-table',
  'top-sessions-skeleton-row-0': 'top-sessions-skeleton-row-0',
  'top-sessions-sort-provider': 'sort-provider',
  'top-sessions-row-abc123': 'row-abc123',

  // Provider table
  'provider-table': 'provider-table',
  'provider-row-openai': 'provider-row-openai',
  'provider-delta-openai-up': 'provider-delta-openai-up',
  'provider-skeleton-row-0': 'provider-skeleton-row-0',

  // Platform (billing / admin)
  'platform-payment-failed-warning': 'payment-failed-warning',
  'platform-suspension-notice': 'suspension-notice',
  'platform-reactivation-flow': 'reactivation-flow',
  'platform-reactivation-step-1': 'reactivation-step-1',
  'platform-reactivation-step-2': 'reactivation-step-2',
  'platform-reactivation-step-3': 'reactivation-step-3',
  'platform-reactivation-step-4': 'reactivation-step-4',
  'platform-dismiss-reactivation': 'dismiss-reactivation',
  'platform-current-tier': 'current-tier',
  'platform-proration-preview': 'proration-preview',
  'platform-invoice-history': 'invoice-history',
  'platform-refunds-table': 'refunds-table',
  'platform-refund-row': 'refund-row',
  'platform-refund-modal': 'refund-modal',
  'platform-success-message': 'success-message',
  'platform-error-message': 'error-message',
  'platform-refund-history': 'refund-history',
  'platform-refund-details-modal': 'refund-details-modal',
  'platform-refund-id': 'refund-id',
  'platform-charge-id': 'charge-id',
  'platform-refund-amount': 'refund-amount',
  'platform-refund-reason': 'refund-reason',
  'platform-refund-status': 'refund-status',
  'platform-refund-date': 'refund-date',
  'platform-admin-user': 'admin-user',
  'platform-dunning-report': 'dunning-report',
  'platform-dunning-attempts': 'dunning-attempts',
  'platform-proration-credit': 'proration-credit',
  'platform-proration-charge': 'proration-charge',

  // App
  'app-error-banner': 'app-error-banner',

  // SP-090: Phase 2 testids added to close Playwright coverage gap
  'file-tree': 'file-tree',
  'file-tree-item': 'file-tree-item',
  'file-tree-empty': 'file-tree-empty',
  'background-tasks-trigger': 'background-tasks-trigger',
  'background-tasks-popover': 'background-tasks-popover',
  'background-task-item': 'background-task-item',
  'background-task-attach': 'background-task-attach',
  'background-task-kill': 'background-task-kill',
  'mcp-server-form': 'mcp-server-form',
  'mcp-server-name-input': 'mcp-server-name-input',
  'mcp-server-command-input': 'mcp-server-command-input',
  'mcp-server-add-button': 'mcp-server-add-button',
  'mcp-server-row': 'mcp-server-row',
  'mcp-server-delete-button': 'mcp-server-delete-button',
  'git-push-button': 'git-push-button',
  'git-remote-url': 'git-remote-url',
  'status-bar-cost': 'status-bar-cost',
  'markdown-preview': 'markdown-preview',
  'binary-viewer': 'binary-viewer',
  'workspace-picker': 'workspace-picker',
  'workspace-picker-option': 'workspace-picker-option',
  'theme-toggle': 'theme-toggle',
  'disconnected-overlay': 'disconnected-overlay',

  // Notifications
  'notification-center': 'notification-center',
  'notification-center-mark-all-read': 'notification-center-mark-all-read',

  // Shell approval
  'shell-approval-accept-all': 'shell-approval-accept-all',
  'shell-approval-command': 'shell-approval-command',
  'shell-approval-part-abc123': 'shell-approval-part-abc123',
  'shell-approval-part-toggle-abc123': 'shell-approval-part-toggle-abc123',
  'shell-approval-reject-all': 'shell-approval-reject-all',
  'shell-approval-reset': 'shell-approval-reset',
  'shell-approval-risk-badge': 'shell-approval-risk-badge',
  'shell-approval-submit': 'shell-approval-submit',
} as const;

// Derived set for O(1) coverage lookups
export const TESTIDS_SET = new Set(Object.values(TESTIDS) as string[]);

export default TESTIDS;
