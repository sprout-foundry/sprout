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
  "sidebar-brand-option-design": "sidebar-brand-option-design",
  "sidebar-design-button": "sidebar-design-button",

  // SP-092-3: Past sessions hint
  "past-sessions-hint": "past-sessions-hint",
  "past-sessions-hint-input": "past-sessions-hint-input",
  "past-sessions-hint-loading": "past-sessions-hint-loading",
  "past-sessions-hint-empty": "past-sessions-hint-empty",
  "past-sessions-hint-card-abc123": "past-sessions-hint-card-abc123",

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
  "context-panel": "context-panel",
  "context-panel-collapse": "context-panel-collapse",
  "context-panel-tab": "context-panel-tab",
  "context-panel-subagents": "context-panel-subagents",
  "context-panel-activity": "context-panel-activity",
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

  // Escalation toast, ETH-2 txn action (run in the cloud container)
  "escalation-toast-txn": "escalation-toast-txn",
  "escalation-toast-txn-progress": "escalation-toast-txn-progress",
  "escalation-toast-txn-status": "escalation-toast-txn-status",
  "escalation-toast-txn-result": "escalation-toast-txn-result",
  "escalation-toast-txn-pulled": "escalation-toast-txn-pulled",
  "escalation-toast-txn-skipped": "escalation-toast-txn-skipped",
  "escalation-toast-txn-warning": "escalation-toast-txn-warning",
  "escalation-toast-txn-error": "escalation-toast-txn-error",

  // Session working directory (services/workspaceCwd.ts) — Files panel
  // repo/cwd selector row (WorkspaceCwdBar). The chip was removed: the
  // select is the single surface showing the cwd (no duplication).
  "workspace-cwd-bar": "workspace-cwd-bar",
  "workspace-cwd-select": "workspace-cwd-select",
  "workspace-add-repo-btn": "workspace-add-repo-btn",

  // P4.2 mobile peer-buffer keep-alive topology (phone form factor):
  // chat and editor are PEER surfaces — both mounted, visibility
  // toggled by the active buffer kind. No sheet hierarchy.
  "mobile-peer-surfaces": "mobile-peer-surfaces",
  "mobile-chat-surface": "mobile-chat-surface",
  "mobile-editor-surface": "mobile-editor-surface",

  // Design (SP-140-3 DesignView / design workspace surface). Static testids
  // from webui/src/components/design/* plus representative values for the
  // dynamic `${...}` row/node/anchor patterns so the coverage gate passes.
  "design-view": "design-view",
  // Mode shell (SP-140-5 step 3): the Design mode's content column
  // (workspaces/DesignShell → design/DesignSurface).
  "design-surface": "design-surface",
  "design-surface-fallback": "design-surface-fallback",
  "design-tabpanel": "design-tabpanel",
  "design-assets-rail": "design-assets-rail",
  "design-rail-stub-row": "design-rail-stub-row",
  // Mode rail (SP-140-5): the Design mode's section rail in the sidebar
  // (workspaces/rail.ts contract). Entries follow the pattern
  // design-rail-${id} (DesignRail.tsx).
  "design-rail": "design-rail",
  "design-rail-flows": "design-rail-flows",
  "design-rail-screens": "design-rail-screens",
  "design-rail-tokens": "design-rail-tokens",
  "design-rail-library": "design-rail-library",
  "design-rail-screen-login": "design-rail-screen-login", // pattern design-rail-screen-${stem} (DesignRail.tsx)
  "design-detail-pane": "design-detail-pane",
  "design-detail-content": "design-detail-content",
  // Flows canvas
  "design-flows-canvas": "design-flows-canvas",
  "design-flows-status": "design-flows-status",
  "design-flow-graph": "design-flow-graph",
  "design-flow-node-login": "design-flow-node-login", // pattern design-flow-node-${data.flowNodeId}
  "design-flow-node-image-login": "design-flow-node-image-login", // pattern design-flow-node-image-${data.flowNodeId}
  "design-rail-row-design-wireframes-login-svg":
    "design-rail-row-design-wireframes-login-svg", // pattern design-rail-row-${asset.path}
  // Screens tab
  "design-screens-grid": "design-screens-grid",
  "design-screens-cards": "design-screens-cards",
  "design-screen-detail": "design-screen-detail",
  "design-screen-error": "design-screen-error",
  "design-screen-placeholder": "design-screen-placeholder",
  "design-screen-card-login": "design-screen-card-login", // pattern design-screen-card-${card.name}
  "design-screen-frame-login": "design-screen-frame-login", // pattern design-screen-frame-${card.name}
  "design-screen-status-login": "design-screen-status-login", // pattern design-screen-status-${card.name}
  "design-screen-thumb-login": "design-screen-thumb-login", // pattern design-screen-thumb-${card.name}
  "design-screen-thumb-box-login": "design-screen-thumb-box-login", // pattern design-screen-thumb-box-${card.name}
  "design-screen-move-up-login": "design-screen-move-up-login", // pattern design-screen-move-up-${card.name}
  "design-screen-move-down-login": "design-screen-move-down-login", // pattern design-screen-move-down-${card.name}
  // Feedback write path (SP-140-4d)
  // Annotation resolution flow (SP-140-4 §4d, DesignFeedbackResolution.tsx)
  "design-feedback-annotations": "design-feedback-annotations",
  "design-feedback-count": "design-feedback-count",
  "design-feedback-empty": "design-feedback-empty",
  "design-feedback-loading": "design-feedback-loading",
  "design-feedback-resolution": "design-feedback-resolution",
  "design-feedback-resolution-error": "design-feedback-resolution-error",
  "design-feedback-resolution-note": "design-feedback-resolution-note",
  "design-feedback-resolution-save": "design-feedback-resolution-save",
  "design-feedback-resolution-written": "design-feedback-resolution-written",
  "design-feedback-status": "design-feedback-status",
  // Dynamic row testid patterns `design-feedback-{annotation,area,toggle}-${annotation.id}`.
  // Representative registered values (match the template literals).
  "design-feedback-annotation-abc123": "design-feedback-annotation-abc123",
  "design-feedback-area-abc123": "design-feedback-area-abc123",
  "design-feedback-toggle-abc123": "design-feedback-toggle-abc123",
  // Representative seeded-feedback fixture ids used by
  // DesignFeedbackResolution.test.tsx (annotation a1, area hierarchy).
  "design-feedback-annotation-a1": "design-feedback-annotation-a1",
  "design-feedback-toggle-a1": "design-feedback-toggle-a1",
  "design-feedback-area-hierarchy": "design-feedback-area-hierarchy",
  "design-feedback-affordance": "design-feedback-affordance",
  "design-feedback-form": "design-feedback-form",
  "design-feedback-add": "design-feedback-add",
  "design-feedback-area": "design-feedback-area",
  "design-feedback-note": "design-feedback-note",
  "design-feedback-submit": "design-feedback-submit",
  "design-feedback-target": "design-feedback-target",
  "design-feedback-written": "design-feedback-written",
  "design-feedback-error": "design-feedback-error",
  // Tokens tab (DTCG tree)
  "design-tokens-tree": "design-tokens-tree",
  "design-tokens-count": "design-tokens-count",
  "design-tokens-empty": "design-tokens-empty",
  "design-token-reload": "design-token-reload",
  "design-tokens-search": "design-tokens-search",
  "design-tokens-hint": "design-tokens-hint",
  "design-tokens-token-detail": "design-tokens-token-detail",
  "design-token-open": "design-token-open",
  "design-token-schema-hint": "design-token-schema-hint",
  "design-token-detail-swatch": "design-token-detail-swatch",
  "design-token-detail-value": "design-token-detail-value",
  "design-token-detail-resolved": "design-token-detail-resolved",
  "design-token-detail-alias": "design-token-detail-alias",
  "design-token-file-colors": "design-token-file-colors", // pattern design-token-file-${grouping.file.name}
  "design-token-file-count-colors": "design-token-file-count-colors", // pattern design-token-file-count-${grouping.file.name}
  "design-token-section-color": "design-token-section-color", // pattern design-token-section-${section}
  "design-token-group-buttons": "design-token-group-buttons", // pattern design-token-group-${group}
  "design-token-row-colors-color-brand-primary":
    "design-token-row-colors-color-brand-primary", // pattern design-token-row-${token.fileName}-${token.path}
  "design-token-type-colors-color-brand-primary":
    "design-token-type-colors-color-brand-primary", // pattern design-token-type-${token.fileName}-${token.path}
  // TokenSpecimens.tsx specimen surfaces (read-only DTCG value display)
  "token-swatch": "token-swatch",
  "token-specimen-color": "token-specimen-color",
  "token-specimen-typography": "token-specimen-typography",
  "token-specimen-spacing": "token-specimen-spacing",
  "token-specimen-value": "token-specimen-value",
  "token-font-line": "token-font-line",
  "token-font-meta": "token-font-meta",
  "token-space-bar": "token-space-bar",
  // SP-140-6 loop surface: health strip (§6c, HealthStrip.tsx)
  "design-health-strip": "design-health-strip",
  "design-health-errors": "design-health-errors",
  "design-health-warnings": "design-health-warnings",
  "design-health-infos": "design-health-infos",
  "design-health-clean": "design-health-clean",
  "design-health-design-ahead": "design-health-design-ahead",
  "design-health-code-ahead": "design-health-code-ahead",
  "design-health-in-sync": "design-health-in-sync",
  "design-health-feedback": "design-health-feedback",
  "design-health-refresh": "design-health-refresh",
  // SP-140-6 annotation pins (§6e, AnnotationPins.tsx + pinPlacement.ts)
  "design-pins": "design-pins",
  "design-pin-a1": "design-pin-a1", // pattern design-pin-${annotation.id}
  "design-feedback-place": "design-feedback-place",
  "design-screen-pending-login": "design-screen-pending-login", // pattern design-screen-pending-${card.name}
  // SP-140-6 agent side column (§6f, DesignSideColumn.tsx + DesignAgentPanel.tsx)
  "design-side-column": "design-side-column",
  "design-side-tab-details": "design-side-tab-details",
  "design-side-tab-agent": "design-side-tab-agent",
  "design-side-panel-details": "design-side-panel-details",
  "design-side-panel-agent": "design-side-panel-agent",
  "design-agent-panel": "design-agent-panel",
  "design-agent-chat": "design-agent-chat",
  // Design empty state (no recognized design/ tree onboarding, DesignEmptyState.tsx)
  "design-surface-empty": "design-surface-empty",
  "design-empty-state": "design-empty-state",
  "design-empty-card-discover-code": "design-empty-card-discover-code",
  "design-empty-card-running-app": "design-empty-card-running-app",
  "design-empty-card-images": "design-empty-card-images",
  "design-empty-card-figma": "design-empty-card-figma",
  "design-empty-card-draft": "design-empty-card-draft",
  "design-empty-foreign": "design-empty-foreign",
  "design-empty-recheck": "design-empty-recheck",
  "design-empty-agent": "design-empty-agent",
  // SP-140-6 loop results (§6g, LoopResults.tsx)
  "design-loop-results": "design-loop-results",
  "design-loop-critique": "design-loop-critique",
  "design-loop-critique-meta": "design-loop-critique-meta",
  "design-loop-stale": "design-loop-stale",
  "design-loop-no-critique": "design-loop-no-critique",
  "design-loop-run-critique": "design-loop-run-critique",
  "design-loop-clean": "design-loop-clean",
  "design-loop-finding-0": "design-loop-finding-0", // pattern design-loop-finding-${index}
  "design-loop-drift": "design-loop-drift",
  "design-loop-adopt": "design-loop-adopt",
  // SP-140-7 co-editing conflict banner (§7b, ConflictBanner.tsx)
  "design-conflict": "design-conflict",
  "design-conflict-text": "design-conflict-text",
  "design-conflict-review": "design-conflict-review",
  "design-conflict-review-pane": "design-conflict-review-pane",
  "design-conflict-keep": "design-conflict-keep",
  "design-conflict-take": "design-conflict-take",
  "design-conflict-restore": "design-conflict-restore",
  "design-conflict-side-text": "design-conflict-side-text", // repeated node
  "design-conflict-base-toggle": "design-conflict-base-toggle",
  // SP-140-7 token value editor (§7c, TokenValueEditor.tsx)
  "design-token-editor": "design-token-editor",
  "design-token-color-well": "design-token-color-well",
  "design-token-input": "design-token-input",
  "design-token-warn": "design-token-warn",
  "design-token-refs": "design-token-refs",
  "design-token-save": "design-token-save",
  "design-token-saved": "design-token-saved",
  "design-token-conflict": "design-token-conflict",
  "design-token-error": "design-token-error",
  // SP-140-8 §8b screen workbench (ScreenWorkbench.tsx / ScreenWorkbenchContainer.tsx)
  "design-workbench": "design-workbench",
  "design-workbench-header": "design-workbench-header",
  "design-workbench-not-found": "design-workbench-not-found",
  "design-workbench-loading": "design-workbench-loading",
  "design-workbench-error": "design-workbench-error",
  "design-workbench-render": "design-workbench-render",
  "design-workbench-status": "design-workbench-status",
  "design-workbench-status-chip": "design-workbench-status-chip",
  "design-workbench-feedback": "design-workbench-feedback",
  "design-workbench-feedback-empty": "design-workbench-feedback-empty",
  "design-workbench-feedback-resolve": "design-workbench-feedback-resolve",
  "design-workbench-resolution": "design-workbench-resolution",
  "design-workbench-open-count": "design-workbench-open-count",
  "design-workbench-flows": "design-workbench-flows",
  "design-workbench-flows-empty": "design-workbench-flows-empty",
  "design-workbench-tokens": "design-workbench-tokens",
  "design-workbench-tokens-empty": "design-workbench-tokens-empty",
  "design-workbench-token-link": "design-workbench-token-link",
  "design-workbench-agent": "design-workbench-agent",
  "design-workbench-agent-file": "design-workbench-agent-file", // pattern design-workbench-agent-file-${index}
  "design-workbench-agent-ask": "design-workbench-agent-ask",
  "design-workbench-annotation-a1": "design-workbench-annotation-a1", // pattern design-workbench-annotation-${annotation.id}
  "design-workbench-annotation-ask-a1": "design-workbench-annotation-ask-a1", // pattern design-workbench-annotation-ask-${annotation.id}
  "design-workbench-flow-in-sign-up-open": "design-workbench-flow-in-sign-up-open", // patterns design-workbench-flow-in-${flowName} + ${testid}-open
  "design-workbench-flow-out-sign-up-open": "design-workbench-flow-out-sign-up-open", // pattern design-workbench-flow-out-${flowName}
  "design-workbench-token-ref-color.primary": "design-workbench-token-ref-color.primary", // pattern design-workbench-token-ref-${ref.path}
  // SP-140-7 status menu (§7d, ScreenStatusMenu.tsx)
  "design-status-menu-login": "design-status-menu-login", // pattern design-status-menu-${stem}
  "design-status-set-login-ready": "design-status-set-login-ready", // pattern design-status-set-${stem}-${status}
  "design-status-set-login-clear": "design-status-set-login-clear", // pattern design-status-set-${stem}-clear
  "design-status-error-login": "design-status-error-login", // pattern design-status-error-${stem}
} as const;

// Derived set for O(1) coverage lookups
export const TESTIDS_SET = new Set(Object.values(TESTIDS) as string[]);

export default TESTIDS;
