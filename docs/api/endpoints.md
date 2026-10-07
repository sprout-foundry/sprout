# API endpoint inventory

> **Generated file — do not edit by hand.** Regenerate with
> `go run ./cmd/api_inventory` from the repo root. A Go test in
> `cmd/api_inventory` fails when this file is stale.

Every route registered by the web server is listed below, grouped by family:
the plain mux routes from the `pkg/webui` `registerXxxRoutes` functions and
the Huma operations, read straight from the in-process API object (the live
registration set), so the list cannot drift from the handlers.

**Served-by semantics.** In the local product every route is served by the
**daemon** (the Go web server). In a hosted/cloud deployment the same route is
served by whichever of the **in-browser WASM agent**, **browser-local**
storage, or the **host** backend the web UI's routing sends it to (the cloud
endpoint registry and the dynamic intercepts in
`webui/src/services/cloudAdapter.ts`). The Served-by column lists the applicable
components. In cloud mode, a route the web UI does not explicitly classify
falls through to the standard host proxy as a safety net; the column records
only explicit classifications, so a daemon-only row means the route is proxied
to the host backend by that catch-all.

| Column | Meaning |
|---|---|
| Method | HTTP methods the route accepts. For a Huma operation, the method registered in its OpenAPI spec; otherwise, for a route listed in the endpoint registry (or a registry prefix entry covering it), the registry's method list; otherwise derived from the handler source in `pkg/webui/*.go`. `any` means the handler performs no method check and accepts any method. |
| Path | The mux pattern as registered. A trailing `/` marks a prefix route; `{name}` is a Go 1.22 wildcard segment. |
| Handler | The handler registered for the route (a `ReactWebServer` method, an inline closure, or a package-qualified handler), or the Huma `operationId` for a Huma operation. |
| Served by | The components that serve the route, in the order: daemon, in-browser WASM agent, browser-local, host. |

## conversation/query

| Method | Path | Handler | Served by |
|---|---|---|---|
| POST | `/api/command/complete` | commandComplete | daemon |
| POST | `/api/command/execute` | commandExecute | daemon |
| POST | `/api/completion` | completionGenerate | daemon |
| POST | `/api/query` | queryStart | daemon, in-browser WASM agent |
| POST | `/api/query/rewind` | queryRewind | daemon |
| GET | `/api/query/status` | queryStatus | daemon, host |
| POST | `/api/query/steer` | querySteer | daemon, in-browser WASM agent |
| POST | `/api/query/steer/retract` | querySteerRetract | daemon |
| POST | `/api/query/stop` | queryStop | daemon, in-browser WASM agent |
| POST | `/api/subagent/` | subagentCancel | daemon |

Notes:
- `/api/command/complete` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)
- `/api/command/execute` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)
- `/api/completion` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)
- `/api/query/rewind` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)
- `/api/query/steer/retract` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)
- `/api/subagent/` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)

## files

| Method | Path | Handler | Served by |
|---|---|---|---|
| GET | `/api/browse` | browseDir | daemon, in-browser WASM agent |
| POST | `/api/create` | createFile | daemon, in-browser WASM agent |
| DELETE | `/api/delete` | deleteItemHttp | daemon, in-browser WASM agent |
| POST | `/api/delete` | deleteItem | daemon, in-browser WASM agent |
| GET | `/api/edits/` | editStatus | daemon, in-browser WASM agent |
| POST | `/api/edits/` | editDecision | daemon, in-browser WASM agent |
| GET | `/api/file` | fileRead | daemon, in-browser WASM agent |
| POST | `/api/file` | fileWrite | daemon, in-browser WASM agent |
| POST | `/api/file/check-modified` | fileCheckModified | daemon, in-browser WASM agent |
| POST | `/api/file/consent` | fileConsent | daemon, in-browser WASM agent |
| GET | `/api/files` | filesList | daemon, in-browser WASM agent |
| GET | `/api/files/prettier-config` | prettierConfig | daemon, in-browser WASM agent |
| POST | `/api/password/` | passwordRespond | daemon |
| POST | `/api/rename` | renameItem | daemon, in-browser WASM agent |
| POST | `/api/shell-approvals/` | shellApprovalDecision | daemon, in-browser WASM agent |
| POST | `/api/upload/image` | uploadImage | daemon |

Notes:
- `/api/password/` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)
- `/api/upload/image` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)

## git

| Method | Path | Handler | Served by |
|---|---|---|---|
| POST | `/api/git/branch/create` | gitBranchCreate | daemon, browser-local |
| GET | `/api/git/branches` | gitBranches | daemon, browser-local |
| POST | `/api/git/checkout` | gitCheckout | daemon, browser-local |
| POST | `/api/git/commit` | gitCommit | daemon, browser-local |
| POST | `/api/git/commit-message` | gitCommitMessage | daemon, browser-local |
| GET | `/api/git/commit/show` | gitCommitShow | daemon, browser-local |
| GET | `/api/git/commit/show/file` | gitCommitFileDiff | daemon, browser-local |
| POST | `/api/git/confirm` | gitConfirm | daemon, browser-local |
| POST | `/api/git/deep-review` | gitDeepReview | daemon, browser-local |
| POST | `/api/git/deep-review/fix` | gitDeepReviewFix | daemon, browser-local |
| POST | `/api/git/deep-review/fix/start` | gitDeepReviewFixStart | daemon, browser-local |
| GET | `/api/git/deep-review/fix/status` | gitDeepReviewFixStatus | daemon, browser-local |
| GET | `/api/git/diff` | gitDiff | daemon, browser-local |
| POST | `/api/git/discard` | gitDiscard | daemon, browser-local |
| GET | `/api/git/log` | gitLog | daemon, browser-local |
| POST | `/api/git/pull` | gitPull | daemon, browser-local |
| POST | `/api/git/pull-request` | gitPullRequest | daemon, browser-local |
| POST | `/api/git/push` | gitPush | daemon, browser-local |
| POST | `/api/git/revert` | gitRevert | daemon, browser-local |
| POST | `/api/git/stage` | gitStage | daemon, browser-local |
| POST | `/api/git/stage-all` | gitStageAll | daemon, browser-local |
| GET | `/api/git/status` | gitStatus | daemon, browser-local |
| POST | `/api/git/unstage` | gitUnstage | daemon, browser-local |
| POST | `/api/git/unstage-all` | gitUnstageAll | daemon, browser-local |
| POST | `/api/git/worktree/checkout` | gitWorktreeCheckout | daemon, browser-local |
| POST | `/api/git/worktree/create` | gitWorktreeCreate | daemon, browser-local |
| POST | `/api/git/worktree/remove` | gitWorktreeRemove | daemon, browser-local |
| GET | `/api/git/worktrees` | gitWorktrees | daemon, browser-local |

## settings

| Method | Path | Handler | Served by |
|---|---|---|---|
| POST | `/api/computer-use/test` | computerUseTest | daemon |
| GET | `/api/config` | get-config | daemon, browser-local |
| GET | `/api/hotkeys` | hotkeysGet | daemon, browser-local |
| PUT | `/api/hotkeys` | hotkeysPut | daemon, browser-local |
| POST | `/api/hotkeys/preset` | hotkeysPreset | daemon, browser-local |
| POST | `/api/hotkeys/validate` | hotkeysValidate | daemon, browser-local |
| POST | `/api/local-llm/download` | localLLMDownload | daemon |
| POST | `/api/local-llm/download/cancel` | localLLMDownloadCancel | daemon |
| GET | `/api/local-llm/models` | localLLMModels | daemon |
| POST | `/api/local-llm/start` | localLLMStart | daemon |
| GET | `/api/local-llm/status` | localLLMStatus | daemon |
| POST | `/api/onboarding/complete` | onboardingComplete | daemon, browser-local |
| POST | `/api/onboarding/skip` | onboardingSkip | daemon, browser-local |
| GET | `/api/onboarding/status` | onboardingStatus | daemon, browser-local |
| GET | `/api/providers` | providersList | daemon, host |
| GET | `/api/providers/models` | providersModels | daemon, browser-local |
| GET | `/api/settings` | settingsGet | daemon, host |
| PUT | `/api/settings` | settingsPut | daemon, host |
| DELETE | `/api/settings/credentials` | settingsCredentialsDelete | daemon, host |
| GET | `/api/settings/credentials` | settingsCredentialsList | daemon, host |
| POST | `/api/settings/credentials` | settingsCredentialsTest | daemon, host |
| PUT | `/api/settings/credentials` | settingsCredentialsSet | daemon, host |
| DELETE | `/api/settings/credentials/` | settingsCredentialsPoolDelete | daemon, host |
| GET | `/api/settings/credentials/` | settingsCredentialsPoolGet | daemon, host |
| POST | `/api/settings/credentials/` | settingsCredentialsPoolPost | daemon, host |
| PUT | `/api/settings/credentials/` | settingsCredentialsPoolSet | daemon, host |
| GET | `/api/settings/mcp` | settingsMcpGet | daemon, browser-local |
| PUT | `/api/settings/mcp` | settingsMcpPut | daemon, browser-local |
| DELETE | `/api/settings/mcp/servers/` | settingsMcpServersDelete | daemon, browser-local |
| POST | `/api/settings/mcp/servers/` | settingsMcpServersPost | daemon, browser-local |
| PUT | `/api/settings/mcp/servers/` | settingsMcpServersPut | daemon, browser-local |
| GET | `/api/settings/providers` | settingsProvidersList | daemon, host |
| POST | `/api/settings/providers` | settingsProvidersCreate | daemon, host |
| DELETE | `/api/settings/providers/` | settingsProvidersDelete | daemon, host |
| PUT | `/api/settings/providers/` | settingsProvidersUpdate | daemon, host |
| GET | `/api/settings/skills` | settingsSkillsGet | daemon, browser-local |
| PUT | `/api/settings/skills` | settingsSkillsPut | daemon, browser-local |
| GET | `/api/settings/subagent-types` | settingsSubagentTypes | daemon, browser-local |
| GET | `/api/settings/subagent-types/` | settingsSubagentTypesSubtree | daemon, browser-local |
| GET | `/api/skills` | skillsList | daemon |
| GET | `/api/skills/` | skillsListSubtree | daemon |
| POST | `/api/skills/` | skillsAction | daemon |

Notes:
- `/api/computer-use/test` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)
- `/api/local-llm/download` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)
- `/api/local-llm/download/cancel` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)
- `/api/local-llm/models` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)
- `/api/local-llm/start` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)
- `/api/local-llm/status` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)
- `/api/skills` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)
- `/api/skills/` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)
- `/api/skills/` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)

## terminal

| Method | Path | Handler | Served by |
|---|---|---|---|
| GET | `/api/lsp/status` | lspStatus | daemon, browser-local |
| GET | `/api/lsp/ws` | lspproxy.BridgeHandler | daemon, browser-local |
| GET | `/api/terminal/agent-sessions` | agentSessionsList | daemon, host |
| GET | `/api/terminal/agent-sessions/` | agentSessionAction | daemon, host |
| POST | `/api/terminal/agent-sessions/` | agentSessionActionPost | daemon, host |
| GET | `/api/terminal/history` | terminalHistoryGet | daemon, in-browser WASM agent |
| POST | `/api/terminal/history` | terminalHistoryPost | daemon, in-browser WASM agent |
| GET | `/api/terminal/sessions` | terminalSessions | daemon, in-browser WASM agent |
| GET | `/api/terminal/shells` | terminalShells | daemon, in-browser WASM agent |
| any | `/terminal` | handleTerminalWebSocket | daemon |

## workspace/instances

| Method | Path | Handler | Served by |
|---|---|---|---|
| GET | `/api/instances` | instancesList | daemon, browser-local |
| POST | `/api/instances/select` | instanceSelect | daemon, browser-local |
| POST | `/api/instances/ssh-browse` | sshBrowse | daemon, browser-local |
| POST | `/api/instances/ssh-close` | sshClose | daemon, browser-local |
| GET | `/api/instances/ssh-hosts` | sshHosts | daemon, browser-local |
| GET | `/api/instances/ssh-launch-status` | sshLaunchStatus | daemon, browser-local |
| POST | `/api/instances/ssh-open` | sshOpen | daemon, browser-local |
| GET | `/api/instances/ssh-sessions` | sshSessions | daemon, browser-local |
| GET | `/api/workspace` | workspaceGet | daemon, browser-local |
| POST | `/api/workspace` | workspaceSet | daemon, browser-local |
| GET | `/api/workspace/browse` | workspaceBrowse | daemon, in-browser WASM agent |
| GET | `/api/workspace/projects` | workspaceProjects | daemon |
| GET | `/api/workspace/symbols` | workspaceSymbols | daemon, browser-local |
| POST | `/api/workspace/sync` | workspaceSync | daemon |
| POST | `/api/workspace/takeover` | workspaceTakeover | daemon |

Notes:
- `/api/workspace/projects` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)
- `/api/workspace/sync` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)
- `/api/workspace/takeover` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)

## sync/txn

| Method | Path | Handler | Served by |
|---|---|---|---|
| GET | `/api/sync` | syncStatus | daemon |
| POST | `/api/sync` | syncPull | daemon |
| POST | `/api/sync/batch` | syncBatch | daemon |
| POST | `/api/sync/op` | syncOp | daemon |
| GET | `/api/sync/status` | syncAgentStatus | daemon |
| POST | `/api/txn/pull` | txnPull | daemon |
| POST | `/api/txn/push` | txnPush | daemon |
| POST | `/api/txn/run` | txnRun | daemon |
| GET | `/api/txn/status` | txnStatus | daemon |

Notes:
- `/api/sync` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)
- `/api/sync` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)
- `/api/sync/batch` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)
- `/api/sync/op` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)
- `/api/sync/status` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)
- `/api/txn/pull` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)
- `/api/txn/push` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)
- `/api/txn/run` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)
- `/api/txn/status` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)

## sessions

| Method | Path | Handler | Served by |
|---|---|---|---|
| GET | `/api/changes/diff` | handleAPIChangesDiff | daemon |
| POST | `/api/changes/revert` | handleAPIChangesRevert | daemon |
| GET | `/api/changes/session` | handleAPIChangesSession | daemon |
| GET | `/api/changes/summary` | handleAPIChangesSummary | daemon |
| GET | `/api/changes/timeline` | handleAPIChangesTimeline | daemon |
| GET | `/api/chat-session/` | chatSessionWorktreeGet | daemon, browser-local |
| POST | `/api/chat-session/` | chatSessionWorktreeSet | daemon, browser-local |
| GET | `/api/chat-sessions` | chatSessionsList | daemon, browser-local |
| POST | `/api/chat-sessions/breakpoints` | chatSessionBreakpoints | daemon |
| POST | `/api/chat-sessions/compact` | chatSessionsCompact | daemon, browser-local |
| POST | `/api/chat-sessions/create` | chatSessionsCreate | daemon, browser-local |
| POST | `/api/chat-sessions/create-in-worktree` | chatSessionsCreateInWorktree | daemon, browser-local |
| POST | `/api/chat-sessions/delete` | chatSessionsDelete | daemon, browser-local |
| POST | `/api/chat-sessions/delete-all` | chatSessionsDeleteAll | daemon, browser-local |
| POST | `/api/chat-sessions/fork` | chatSessionFork | daemon |
| POST | `/api/chat-sessions/history` | chatSessionClearHistory | daemon |
| GET | `/api/chat-sessions/messages` | chatSessionMessages | daemon, browser-local |
| POST | `/api/chat-sessions/pin` | chatSessionsPin | daemon, browser-local |
| POST | `/api/chat-sessions/rename` | chatSessionsRename | daemon, browser-local |
| POST | `/api/chat-sessions/switch` | chatSessionsSwitch | daemon, browser-local |
| POST | `/api/chat-sessions/unpin` | chatSessionsUnpin | daemon, browser-local |
| GET | `/api/chat-sessions/worktree-mappings` | chatSessionWorktreeList | daemon, browser-local |
| GET | `/api/sessions` | sessionsList | daemon, browser-local |
| POST | `/api/sessions/restore` | sessionRestore | daemon, browser-local |
| GET | `/api/sessions/search` | sessionsSearch | daemon, browser-local |
| GET | `/api/sessions/{id}/export` | sessionExport | daemon |

Notes:
- `/api/changes/diff` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)
- `/api/changes/revert` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)
- `/api/changes/session` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)
- `/api/changes/summary` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)
- `/api/changes/timeline` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)
- `/api/chat-sessions/breakpoints` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)
- `/api/chat-sessions/fork` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)
- `/api/chat-sessions/history` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)
- `/api/sessions/{id}/export` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)

## search

| Method | Path | Handler | Served by |
|---|---|---|---|
| GET | `/api/search` | searchQuery | daemon, in-browser WASM agent |
| POST | `/api/search/replace` | searchReplace | daemon, in-browser WASM agent |

## diagnostics

| Method | Path | Handler | Served by |
|---|---|---|---|
| POST | `/api/diagnostics` | diagnosticsValidate | daemon, browser-local |
| POST | `/api/semantic` | semanticRun | daemon, browser-local |
| GET | `/api/stats` | get-stats | daemon, host |
| GET | `/api/support-bundle` | supportBundle | daemon, browser-local |
| GET | `/api/ws-metrics` | wsMetrics | daemon |

Notes:
- `/api/ws-metrics` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)

## design

| Method | Path | Handler | Served by |
|---|---|---|---|
| GET | `/api/design/status` | designStatus | daemon, in-browser WASM agent |

## starters

| Method | Path | Handler | Served by |
|---|---|---|---|
| GET | `/api/starters` | startersList | daemon |
| POST | `/api/starters/instantiate` | startersInstantiate | daemon |

Notes:
- `/api/starters` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)
- `/api/starters/instantiate` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)

## misc/static

| Method | Path | Handler | Served by |
|---|---|---|---|
| any | `/` | handleIndex | daemon |
| POST | `/api/automate/run` | handleAPIAutomateRun | daemon |
| GET | `/api/automate/sessions` | handleAPIAutomateSessionsList | daemon |
| GET, POST | `/api/automate/sessions/` | handleAPIAutomateSessionsAll | daemon |
| GET | `/api/automate/workflows` | handleAPIAutomateWorkflows | daemon |
| GET | `/api/bootstrap` | handleAPIBootstrap | daemon |
| POST | `/api/open-in-file-browser` | openInFileBrowser | daemon, browser-local |
| POST | `/api/preview/restart` | handleAPIPreviewRestart | daemon |
| POST | `/api/preview/start` | handleAPIPreviewStart | daemon |
| GET | `/api/preview/status` | handleAPIPreviewStatus | daemon |
| POST | `/api/preview/stop` | handleAPIPreviewStop | daemon |
| POST | `/api/proxy/chat` | proxyChat | daemon |
| GET | `/api/proxy/chat/status` | proxyChatStatus | daemon |
| POST | `/api/proxy/chat/stop` | proxyChatStop | daemon |
| GET | `/api/proxy/stats` | proxyStats | daemon |
| any | `/asset-manifest.json` | handleAssetManifest | daemon |
| any | `/assets/` | handleAssets | daemon |
| any | `/browserconfig.xml` | handleBrowserConfig | daemon |
| any | `/debug/goroutines` | inline closure | daemon |
| any | `/editor.html` | handleStandalonePage | daemon |
| any | `/favicon.ico` | handleFavicon | daemon |
| any | `/health` | inline closure | daemon |
| any | `/icon-192.png` | handleIcon192 | daemon |
| any | `/icon-512.png` | handleIcon512 | daemon |
| any | `/logo-mark.svg` | handleLogoMark | daemon |
| any | `/manifest.json` | handleManifest | daemon |
| any | `/ssh/` | handleSSHProxy | daemon |
| any | `/static/` | handleStaticFiles | daemon |
| any | `/sw.js` | handleServiceWorker | daemon |
| any | `/terminal.html` | handleStandalonePage | daemon |
| any | `/wasm/` | handleWasmAssets | daemon |
| any | `/ws` | handleWebSocket | daemon |

Notes:
- `/api/automate/run` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)
- `/api/automate/sessions` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)
- `/api/automate/sessions/` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)
- `/api/automate/workflows` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)
- `/api/bootstrap` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)
- `/api/preview/restart` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)
- `/api/preview/start` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)
- `/api/preview/status` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)
- `/api/preview/stop` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)
- `/api/proxy/chat` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)
- `/api/proxy/chat/status` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)
- `/api/proxy/chat/stop` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)
- `/api/proxy/stats` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)
