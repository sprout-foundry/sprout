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
  "chat-shell": "chat-shell",
  "diff-surface": "diff-surface",
  "chat-main": "chat-main",
  "chat-input": "chat-input",
  "chat-send": "chat-send",
  "chat-message": "chat-message",
  "chat-message-user": "chat-message-user",
  "chat-message-assistant": "chat-message-assistant",
  "chat-message-list": "chat-message-list",
  "chat-scroll-bottom": "chat-scroll-bottom",
  "chat-export-button": "chat-export-button",
  "chat-offline-panel": "chat-offline-panel",
  "chat-offline-retry": "chat-offline-retry",
  "chat-no-provider": "chat-no-provider",
  "chat-provider-setup": "chat-provider-setup",
  "chat-welcome": "chat-welcome",
  "chat-processing": "chat-processing",
  "chat-error": "chat-error",
  "chat-tool-timeline": "chat-tool-timeline",
  "chat-subagent-feed": "chat-subagent-feed",
  "turn-changes-strip": "turn-changes-strip",
  "diff-view": "diff-view",
  "chat-metrics-strip": "chat-metrics-strip",
  "progress-strip": "progress-strip", // active-run progress strip
  "chs-trigger": "chs-trigger",
  "chs-popover": "chs-popover",
  "chs-search-input": "chs-search-input",
  "chs-row": "chs-row",
  "chs-export-all": "chs-export-all",
  "chat-query-progress": "chat-query-progress",
  "chat-new-button": "chat-new-button",

  // Header
  "header-usage-chip": "header-usage-chip", // SP-016 P0.6 ambient usage chip (cloud mode)

  // Sidebar
  "sidebar-container": "sidebar-container",
  "sidebar-brand": "sidebar-brand",
  "sidebar-settings-toggle": "sidebar-settings-toggle",
  "sidebar-icon-rail": "sidebar-icon-rail",
  "sidebar-mode-rail": "sidebar-mode-rail",
  "sidebar-git-tab": "sidebar-git-tab",
  "sidebar-files-tab": "sidebar-files-tab",
  "sidebar-search-tab": "sidebar-search-tab",
  "sidebar-automations-tab": "sidebar-automations-tab",
  "sidebar-logs-tab": "sidebar-logs-tab",
  "sidebar-collapse-toggle": "sidebar-collapse-toggle",
  // Workspace mode switcher (top-left). Derived ids: <testId>-trigger,
  // <testId>-menu, <testId>-option-<mode>.
  "sidebar-brand-trigger": "sidebar-brand-trigger",
  "sidebar-brand-menu": "sidebar-brand-menu",
  "sidebar-brand-option-code": "sidebar-brand-option-code",

  // SP-092-3: Past sessions hint

  // Editor
  editor: "editor",
  "editor-pane": "editor-pane",
  "editor-empty": "editor-empty",
  "editor-language-switcher": "language-switcher-button",
  "editor-language-popup": "language-switcher-popup",
  "editor-footer": "editor-footer",
  "editor-welcome-tab": "editor-welcome-tab",
  "editor-image-viewer": "image-viewer",

  // Terminal
  "terminal-pane": "terminal-pane",
  "terminal-container": "terminal-container",
  "terminal-toggle": "terminal-toggle",
  "terminal-collapse": "terminal-collapse",
  "terminal-tab-bar": "terminal-tab-bar",
  "workspace-terminal": "workspace-terminal", // the arranged terminal view (SproutWorkspace `terminal` kind)
  "workspace-terminal-unavailable": "workspace-terminal-unavailable",

  // Onboarding
  "onboarding-overlay": "onboarding-overlay",
  "onboarding-card": "onboarding-card",
  "onboarding-step": "onboarding-step",
  "onboarding-skip": "onboarding-skip",
  "onboarding-done": "onboarding-done",
  "onboarding-provider-grid": "onboarding-provider-grid",
  "onboarding-provider-card": "onboarding-provider-card",
  "onboarding-model-input": "onboarding-model-input",
  "onboarding-api-key": "onboarding-api-key",
  "onboarding-close": "onboarding-close",
  "onboarding-toggle-providers": "onboarding-toggle-providers",
  "onboarding-refresh": "onboarding-refresh",

  // Worktree
  "worktree-panel": "worktree-panel",
  "worktree-create-button": "worktree-create-button",
  "worktree-list": "worktree-list",
  "worktree-item": "worktree-item",
  "worktree-switch": "worktree-switch",
  "worktree-delete": "worktree-delete",

  // Context panel
  "context-panel-changes": "context-panel-changes",
  "context-panel-tasks": "context-panel-tasks",

  // Status bar
  "status-bar": "status-bar",
  "status-bar-workspace": "status-bar-workspace",
  "status-bar-notification": "status-bar-notification",

  // Settings
  "settings-panel": "settings-panel",
  "settings-filter": "settings-filter",
  "settings-section": "settings-section",
  "settings-credentials-link": "settings-credentials-link",
  // Subsection tab buttons — IDs generated as `settings-${sub.id}-tab` (SettingsPanel.tsx)
  "settings-agent-general-tab": "settings-agent-general-tab",
  "settings-agent-behavior-tab": "settings-agent-behavior-tab",
  "settings-agent-subagents-tab": "settings-agent-subagents-tab",
  "settings-agent-skills-tab": "settings-agent-skills-tab",
  "settings-agent-memory-tab": "settings-agent-memory-tab",
  "settings-workspace-embeddings-tab": "settings-workspace-embeddings-tab",
  "settings-workspace-mcp-tab": "settings-workspace-mcp-tab",
  "settings-workspace-lsp-tab": "settings-workspace-lsp-tab",
  "settings-env-providers-tab": "settings-env-providers-tab",
  "settings-env-advanced-tab": "settings-env-advanced-tab",
  "settings-editor-preferences-tab": "settings-editor-preferences-tab",
  "settings-editor-notifications-tab": "settings-editor-notifications-tab",
  "settings-experimental-computer-use-tab":
    "settings-experimental-computer-use-tab",
  // Tab content containers (static data-testid on the content root of each tab)
  "settings-subagent-tab": "settings-subagent-tab",
  "settings-providers-tab": "settings-providers-tab",
  "settings-skills-tab": "settings-skills-tab",
  "settings-mcp-tab": "settings-mcp-tab",
  // Provider tab controls
  "settings-primary-provider": "settings-primary-provider",
  "settings-primary-model": "settings-primary-model",
  "settings-current-provider": "settings-current-provider",
  // Editor footer whitespace mode
  "settings-whitespace-mode": "settings-whitespace-mode",

  // Role models (RoleModelsSection.tsx). Row/input/save testids
  // are generated per role: `role-model-{row,input,save}-${role}` and
  // `role-provider-input-${role}` (BUILTIN_ROLES: planner|coder|summarizer|
  // reviewer|commit); `coder` is the representative registered value.
  "role-models-section": "role-models-section",
  "role-model-row-coder": "role-model-row-coder", // pattern role-model-row-${role}
  "role-model-input-coder": "role-model-input-coder", // pattern role-model-input-${role}
  "role-provider-input-coder": "role-provider-input-coder", // pattern role-provider-input-${role}
  "role-model-save-coder": "role-model-save-coder", // pattern role-model-save-${role}

  // Skills
  "skills-install-source": "skills-install-source",
  "skills-install-ref": "skills-install-ref",
  "skills-install-force": "skills-install-force",
  "skills-registry-dropdown": "skills-registry-dropdown",
  "skills-install-button": "skills-install-button",
  // MCP test message — template literal `mcp-server-test-message-${name}`
  "skills-list-item-my-skill": "skills-list-item-my-skill",
  "skills-update-button-my-skill": "skills-update-button-my-skill",
  "skills-remove-button-my-skill": "skills-remove-button-my-skill",

  // Model picker
  "model-picker": "model-picker",
  "model-picker-option": "model-picker-option",
  "model-picker-current": "model-picker-current",
  "model-picker-download": "model-picker-download",

  // Export dialog
  "export-dialog": "export-dialog",
  "export-format-markdown": "export-format-markdown",
  "export-format-json": "export-format-json",
  "export-format-text": "export-format-html",
  "export-include-tool-calls": "export-include-tool-calls",
  "export-include-cost": "export-include-cost",
  "export-redact-secrets": "export-redact-secrets",
  "export-cancel": "export-cancel",
  "export-download": "export-download",

  // Workspace browser / gate
  "workspace-browser": "workspace-browser",
  "workspace-browser-list": "workspace-browser-list",
  "workspace-browser-entry": "workspace-browser-entry",
  "workspace-browser-confirm": "workspace-browser-confirm",
  "workspace-gate-modal": "workspace-gate-modal",
  "workspace-gate-error": "workspace-gate-error",
  "workspace-gate-home-btn": "workspace-gate-home-btn",
  // Studio workspace-gate variant (native folder picker / new-project)
  "workspace-gate-pick-btn": "workspace-gate-pick-btn",
  "workspace-gate-new-btn": "workspace-gate-new-btn",
  "workspace-gate-create-input": "workspace-gate-create-input",
  "workspace-gate-create-cancel": "workspace-gate-create-cancel",
  "workspace-gate-create-submit": "workspace-gate-create-submit",
  // Starter chooser in the studio new-project create form.
  "workspace-gate-starter-select": "workspace-gate-starter-select",

  // Command output
  "command-output-panel": "command-output-panel",
  "command-output-panel-body": "command-output-panel-body",

  // Status bar
  "status-bar-auto-tier": "status-bar-auto-tier",

  // Security approval dialog
  "chain-stepper": "chain-stepper",
  "chain-stepper-pill": "chain-stepper-pill",

  // App
  "app-error-banner": "app-error-banner",

  // SP-090: Phase 2 testids added to close Playwright coverage gap
  "file-tree": "file-tree",
  "file-tree-item": "file-tree-item",
  "file-tree-empty": "file-tree-empty",
  "background-tasks-trigger": "background-tasks-trigger",
  "background-tasks-popover": "background-tasks-popover",
  "background-task-item": "background-task-item",
  "background-task-attach": "background-task-attach",
  "background-task-kill": "background-task-kill",
  "mcp-server-form": "mcp-server-form",
  "mcp-server-name-input": "mcp-server-name-input",
  "mcp-server-type-select": "mcp-server-type-select",
  "mcp-server-url-input": "mcp-server-url-input",
  "mcp-server-command-input": "mcp-server-command-input",
  "mcp-server-add-button": "mcp-server-add-button",
  "mcp-server-row": "mcp-server-row",
  "mcp-server-delete-button": "mcp-server-delete-button",
  "mcp-server-test-button": "mcp-server-test-button",
  "mcp-server-test-message-stub": "mcp-server-test-message-stub",
  "git-push-button": "git-push-button",
  "git-remote-url": "git-remote-url",

  // GitHub account / repo picker (authenticated clone)
  "gh-account-card": "gh-account-card",
  "gh-signout-btn": "gh-signout-btn",
  "home-nav": "home-nav",
  "platform-home": "platform-home",
  "phone-tab-bar": "phone-tab-bar",
  "header-search": "header-search",
  "project-nav": "project-nav",
  "project-rail": "project-rail",
  "chat-open-repo": "chat-open-repo",
  "new-project-dialog": "new-project-dialog",
  "open-repo-input": "open-repo-input",
  "open-repo-submit": "open-repo-submit",
  "header-credits-chip": "header-credits-chip",
  "platform-gh-card": "platform-gh-card",
  "platform-gh-manage": "platform-gh-manage",
  "gh-signin-form": "gh-signin-form",
  "gh-signin-input": "gh-signin-input",
  "gh-signin-submit": "gh-signin-submit",
  "gh-signin-error": "gh-signin-error",
  "gh-device-flow": "gh-device-flow",
  "gh-device-code": "gh-device-code",
  "gh-device-open": "gh-device-open",
  "gh-device-waiting": "gh-device-waiting",
  "gh-device-cancel": "gh-device-cancel",
  "gh-device-signin": "gh-device-signin",
  "gh-device-error": "gh-device-error",
  "gh-use-pat": "gh-use-pat",
  "gh-picker-overlay": "gh-picker-overlay",
  "gh-picker-close": "gh-picker-close",
  "gh-picker-search": "gh-picker-search",
  "gh-picker-list": "gh-picker-list",
  "gh-picker-open-typed": "gh-picker-open-typed",
  "gh-picker-loading": "gh-picker-loading",
  "gh-picker-empty": "gh-picker-empty",
  "gh-picker-clone-error": "gh-picker-clone-error",
  "gh-picker-list-error": "gh-picker-list-error",
  "gh-picker-retry": "gh-picker-retry",
  "gh-repo-item": "gh-repo-item",
  "status-bar-cost": "status-bar-cost",
  "markdown-preview": "markdown-preview",
  "binary-viewer": "binary-viewer",
  "workspace-picker": "workspace-picker",
  "workspace-picker-option": "workspace-picker-option",
  "workspace-picker-error": "workspace-picker-error",
  "theme-toggle": "theme-toggle",
  "ui-scale-select": "ui-scale-select",
  "disconnected-overlay": "disconnected-overlay",

  // Notifications
  "notification-center": "notification-center",
  "notification-center-mark-all-read": "notification-center-mark-all-read",
  "notification-history": "notification-history",

  // Shell approval
  "shell-approval-accept-all": "shell-approval-accept-all",
  "shell-approval-command": "shell-approval-command",
  "shell-approval-part-abc123": "shell-approval-part-abc123",
  "shell-approval-part-toggle-abc123": "shell-approval-part-toggle-abc123",
  "shell-approval-reject-all": "shell-approval-reject-all",
  "shell-approval-reset": "shell-approval-reset",
  "shell-approval-risk-badge": "shell-approval-risk-badge",
  "shell-approval-submit": "shell-approval-submit",

  // Escalation toast (browser-limitation → Mode A cloud task / Mode B workspace)
  "escalation-toast-cloud-task": "escalation-toast-cloud-task",
  "escalation-toast-cloud-task-status": "escalation-toast-cloud-task-status",
  "escalation-toast-cloud-task-link": "escalation-toast-cloud-task-link",
  "escalation-toast-cloud-task-error": "escalation-toast-cloud-task-error",
  "escalation-toast-start-workspace": "escalation-toast-start-workspace",
  "escalation-toast-workspace-error": "escalation-toast-workspace-error",

  // Escalation toast, ETH-2 txn action (run in the cloud container)
  "escalation-toast-txn": "escalation-toast-txn",
  "escalation-toast-txn-progress": "escalation-toast-txn-progress",
  "escalation-toast-txn-status": "escalation-toast-txn-status",
  "escalation-toast-txn-result": "escalation-toast-txn-result",
  "escalation-toast-txn-empty-push": "escalation-toast-txn-empty-push",
  "escalation-toast-txn-pulled": "escalation-toast-txn-pulled",
  "escalation-toast-txn-skipped": "escalation-toast-txn-skipped",
  "escalation-toast-txn-warning": "escalation-toast-txn-warning",
  "escalation-toast-txn-error": "escalation-toast-txn-error",
  "escalation-toast-txn-cloud-fallback": "escalation-toast-txn-cloud-fallback",

  // Escalation host choice (RunHostPicker): runner vs cloud
  "run-host-picker": "run-host-picker",
  "run-host-option": "run-host-option",
  "run-host-option-cloud": "run-host-option-cloud",
  "run-host-bare-metal-warning": "run-host-bare-metal-warning",
  "run-host-notice": "run-host-notice",

  // Session working directory (services/workspaceCwd.ts) — Files panel
  // repo/cwd selector row (WorkspaceCwdBar). The chip was removed: the
  // select is the single surface showing the cwd (no duplication).
  "workspace-cwd-bar": "workspace-cwd-bar",
  "editor-model-section": "editor-model-section",
  "workspace-cwd-select": "workspace-cwd-select",
  "workspace-add-repo-btn": "workspace-add-repo-btn",

  // P4.2 mobile peer-buffer keep-alive topology (phone form factor):
  // chat and editor are PEER surfaces — both mounted, visibility
  // toggled by the active buffer kind. No sheet hierarchy.
  "mobile-peer-surfaces": "mobile-peer-surfaces",
  "mobile-chat-surface": "mobile-chat-surface",
  "mobile-editor-surface": "mobile-editor-surface",

  // The mockup, in the Code surface's preview slot.
  "mockup-panel": "mockup-panel",
  "mockup-panel-title": "mockup-panel-title",
  "mockup-panel-close": "mockup-panel-close",
  "mockup-panel-frame": "mockup-panel-frame",
  "mockup-panel-empty": "mockup-panel-empty",

  "workspace-busy-notice": "workspace-busy-notice",
  "workspace-busy-send-anyway": "workspace-busy-send-anyway",
  "workspace-busy-dismiss": "workspace-busy-dismiss",

  // Preview (the preview pane's dev-server lifecycle surface,
  // packages/ui/src/components/PreviewPane.tsx)
  "preview-pane": "preview-pane", // the pane root
  "preview-pane-title": "preview-pane-title", // the pane header
  "preview-pane-status": "preview-pane-status", // the live status line
  "preview-pane-starting": "preview-pane-starting", // the starting placeholder
  "preview-pane-iframe": "preview-pane-iframe", // the embed (localhost URL)
  "preview-pane-stopped": "preview-pane-stopped", // the stopped affordance
  "preview-pane-restart": "preview-pane-restart", // the restart action
  "preview-pane-failed": "preview-pane-failed", // the failure reason surface
  "preview-pane-no-url": "preview-pane-no-url", // running-but-no-URL guard
  "preview-pane-close": "preview-pane-close", // the close affordance (panel)
  // Preview placement (the Code-mode panel)
  "preview-panel": "preview-panel", // the Code-mode preview panel container
  "pane-preview": "pane-preview", // the preview buffer's editor pane body
  "preview-panel-toggle": "preview-panel-toggle", // the HeaderBar toggle button

  // Views layout configuration:
  // webui/src/views/ViewsLayout.tsx + ExampleEmbedding.tsx
  "views-layout": "views-layout", // the arrangement renderer's root
  "views-slot-left": "views-slot-left", // pattern views-slot-${slot}, slot ∈ ViewSlot
  "views-slot-center": "views-slot-center",
  "views-slot-right": "views-slot-right",
  "views-slot-overlay": "views-slot-overlay",
  "views-example-embedding": "views-example-embedding", // the example composition's root

  // Language guard (chat message original-text disclosure, MessageItem)
  "language-guard-original": "language-guard-original",

  // Project timeline (components/changes/ProjectTimeline.tsx)
  "project-timeline": "project-timeline", // the timeline root
  "ptl-row": "ptl-row", // one timeline entry
  "ptl-restore": "ptl-restore", // the restore action
  "ptl-restore-cancel": "ptl-restore-cancel", // cancel the restore confirm
  "ptl-restore-confirm": "ptl-restore-confirm", // the restore confirm dialog
  "ptl-restore-confirm-yes": "ptl-restore-confirm-yes", // confirm the restore

  // Ship surface (components/ship/ShipSurface.tsx)
  "ship-surface": "ship-surface", // the surface root
  "ship-status": "ship-status", // the deploy status line
  "ship-deploy": "ship-deploy", // the deploy action
  "ship-deploy-blocked": "ship-deploy-blocked", // the blocked-deploy reason
  "ship-error": "ship-error", // the error surface
  "ship-live-state": "ship-live-state", // the live deployment state
  "ship-live-url": "ship-live-url", // the live deployment URL
  "ship-live-url-empty": "ship-live-url-empty", // running-but-no-URL guard
  "ship-live-version": "ship-live-version", // the live deployment version
  "ship-live-deployed-at": "ship-live-deployed-at", // the live deploy time
  "ship-history-list": "ship-history-list", // the deploy history list
  "ship-history-empty": "ship-history-empty", // empty history placeholder
  "ship-history-revision": "ship-history-revision", // one history entry
  "ship-history-state": "ship-history-state", // entry state
  "ship-history-summary": "ship-history-summary", // entry summary
  "ship-history-time": "ship-history-time", // entry timestamp
  "ship-history-version": "ship-history-version", // entry version
  // Dynamic per-entry actions follow ship-history-{row,open,rollback}-${id};
  // the registry keeps one concrete exemplar per pattern (the same approach
  // the role-model rows use).
  "ship-history-row-x": "ship-history-row-x", // pattern ship-history-row-${id}
  "ship-history-open-x": "ship-history-open-x", // pattern ship-history-open-${id}
  "ship-history-rollback-x": "ship-history-rollback-x", // pattern ship-history-rollback-${id}

  // Host contract (host/example/ExampleHost.tsx, host/HostNotificationCount.tsx)
  "host-example": "host-example", // the example host shell root
  "host-example-chrome": "host-example-chrome", // the shell chrome bar
  "host-example-chrome-left": "host-example-chrome-left", // chrome left cluster
  "host-example-nav-log": "host-example-nav-log", // nav to the log view
  "host-example-notifications": "host-example-notifications", // notifications view
  "host-example-notify": "host-example-notify", // post a notification action
  "host-example-open-account": "host-example-open-account", // open the account surface
  "host-example-space-x": "host-example-space-x", // pattern host-example-space-${id}
  "host-example-space": "host-example-space", // a space row container
  "host-example-spaces": "host-example-spaces", // the spaces list
  "host-example-usage": "host-example-usage", // the usage view
  "host-notification-count": "host-notification-count", // the notification badge

  // Workspace views (views/SproutWorkspace.tsx)
  "sprout-workspace": "sprout-workspace", // the workspace view root
  "workspace-editor": "workspace-editor", // the assembled editor view (views/WorkspaceEditorView.tsx)
  "workspace-git": "workspace-git", // the source control view root (views/WorkspaceGitView.tsx)
  "workspace-git-empty": "workspace-git-empty", // the source control view's no-repository / no-git state
  "workspace-git-diff": "workspace-git-diff", // the source control view's inline diff
  "workspace-git-diff-open": "workspace-git-diff-open", // opens the diffed file in the editor
  "workspace-search": "workspace-search", // the search view root (views/WorkspaceSearchView.tsx)
  "workspace-space-shell": "workspace-space-shell", // the composed space shell root (views/SproutWorkspaceSpaceShell.tsx)
  "workspace-sidebar": "workspace-sidebar", // the composed shell's project sidebar (views/WorkspaceSidebar.tsx)
  "workspace-sidebar-rail": "workspace-sidebar-rail", // the collapsed sidebar's rail
  "workspace-docked-terminal": "workspace-docked-terminal", // the composed shell's docked terminal (views/WorkspaceDockedTerminal.tsx)
  "user-menu-report-bug": "user-menu-report-bug", // UserMenu "Report a bug" action
} as const;

// Derived set for O(1) coverage lookups
export const TESTIDS_SET = new Set(Object.values(TESTIDS) as string[]);

export default TESTIDS;
