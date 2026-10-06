# API endpoint inventory

> **Generated file — do not edit by hand.** Regenerate with
> `go run ./cmd/api_inventory` from the repo root. A Go test in
> `cmd/api_inventory` fails when this file is stale.

Every route registered in `pkg/webui/routes.go` is listed below, grouped by
family.

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
| Method | HTTP methods the route accepts. For a route listed in the endpoint registry (or a registry prefix entry covering it), the registry's method list; otherwise derived from the handler source in `pkg/webui/*.go`. `any` means the handler performs no method check and accepts any method. |
| Path | The mux pattern as registered in `pkg/webui/routes.go`. A trailing `/` marks a prefix route; `{name}` is a Go 1.22 wildcard segment. |
| Handler | The handler registered for the route (a `ReactWebServer` method, an inline closure, or a package-qualified handler). |
| Served by | The components that serve the route, in the order: daemon, in-browser WASM agent, browser-local, host. |

## conversation/query

| Method | Path | Handler | Served by |
|---|---|---|---|
| POST | `/api/command/complete` | handleAPICommandComplete | daemon |
| POST | `/api/command/execute` | handleAPICommandExecute | daemon |
| POST | `/api/completion` | handleAPICompletion | daemon |
| POST | `/api/query` | handleAPIQuery | daemon, in-browser WASM agent |
| POST | `/api/query/rewind` | handleAPIQueryRewind | daemon |
| GET | `/api/query/status` | handleAPIQueryStatus | daemon, host |
| POST | `/api/query/steer` | handleAPIQuerySteer | daemon, in-browser WASM agent |
| POST | `/api/query/steer/retract` | handleAPIQuerySteerRetract | daemon |
| POST | `/api/query/stop` | handleAPIQueryStop | daemon, in-browser WASM agent |
| POST | `/api/subagent/` | handleAPISubagentCancel | daemon |

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
| GET | `/api/browse` | handleAPIBrowse | daemon, in-browser WASM agent |
| POST | `/api/create` | handleAPICreateFile | daemon, in-browser WASM agent |
| POST, DELETE | `/api/delete` | handleAPIDeleteItem | daemon, in-browser WASM agent |
| GET, POST | `/api/edits/` | handleAPIEdits | daemon, in-browser WASM agent |
| GET, POST | `/api/file` | handleAPIFile | daemon, in-browser WASM agent |
| GET, POST | `/api/file/check-modified` | handleAPIFileCheckModified | daemon, in-browser WASM agent |
| POST | `/api/file/consent` | handleAPIFileConsent | daemon, in-browser WASM agent |
| GET | `/api/files` | handleAPIFiles | daemon, in-browser WASM agent |
| GET | `/api/files/prettier-config` | handleAPIGetPrettierConfig | daemon, in-browser WASM agent |
| POST | `/api/password/` | handleAPIPasswordRoutes | daemon |
| POST | `/api/rename` | handleAPIRenameItem | daemon, in-browser WASM agent |
| POST | `/api/shell-approvals/` | handleAPIShellApprovals | daemon, in-browser WASM agent |
| POST | `/api/upload/image` | handleUploadImage | daemon, browser-local |

Notes:
- `/api/file/check-modified` — registry methods GET, POST vs handler methods POST
- `/api/password/` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)

## git

| Method | Path | Handler | Served by |
|---|---|---|---|
| POST | `/api/git/branch/create` | handleAPIGitCreateBranch | daemon, browser-local |
| GET | `/api/git/branches` | handleAPIGitBranches | daemon, browser-local |
| POST | `/api/git/checkout` | handleAPIGitCheckout | daemon, browser-local |
| POST | `/api/git/commit` | handleAPIGitCommit | daemon, browser-local |
| POST | `/api/git/commit-message` | handleAPIGitCommitMessage | daemon, browser-local |
| POST | `/api/git/commit/show` | handleAPIGitCommitShow | daemon, browser-local |
| POST | `/api/git/commit/show/file` | handleAPIGitCommitFileDiff | daemon, browser-local |
| POST | `/api/git/confirm` | handleAPIConfirm | daemon, browser-local |
| POST | `/api/git/deep-review` | handleAPIGitDeepReview | daemon, browser-local |
| POST | `/api/git/deep-review/fix` | handleAPIGitDeepReviewFix | daemon, browser-local |
| POST | `/api/git/deep-review/fix/start` | handleAPIGitDeepReviewFixStart | daemon, browser-local |
| GET | `/api/git/deep-review/fix/status` | handleAPIGitDeepReviewFixStatus | daemon, browser-local |
| GET | `/api/git/diff` | handleAPIGitDiff | daemon, browser-local |
| POST | `/api/git/discard` | handleAPIGitDiscard | daemon, browser-local |
| GET | `/api/git/log` | handleAPIGitLog | daemon, browser-local |
| POST | `/api/git/pull` | handleAPIGitPull | daemon, browser-local |
| POST | `/api/git/pull-request` | handleAPIGitPullRequest | daemon, browser-local |
| POST | `/api/git/push` | handleAPIGitPush | daemon, browser-local |
| POST | `/api/git/revert` | handleAPIGitRevert | daemon, browser-local |
| POST | `/api/git/stage` | handleAPIGitStage | daemon, browser-local |
| POST | `/api/git/stage-all` | handleAPIGitStageAll | daemon, browser-local |
| GET | `/api/git/status` | handleAPIGitStatus | daemon, browser-local |
| POST | `/api/git/unstage` | handleAPIGitUnstage | daemon, browser-local |
| POST | `/api/git/unstage-all` | handleAPIGitUnstageAll | daemon, browser-local |
| POST | `/api/git/worktree/checkout` | handleAPIGitWorktreeCheckout | daemon, browser-local |
| POST | `/api/git/worktree/create` | handleAPIGitWorktreeCreate | daemon, browser-local |
| POST | `/api/git/worktree/remove` | handleAPIGitWorktreeRemove | daemon, browser-local |
| GET | `/api/git/worktrees` | handleAPIGitWorktrees | daemon, browser-local |

Notes:
- `/api/git/commit/show` — registry methods POST vs handler methods GET
- `/api/git/commit/show/file` — registry methods POST vs handler methods GET

## settings

| Method | Path | Handler | Served by |
|---|---|---|---|
| POST | `/api/computer-use/test` | handleAPIComputerUseTest | daemon |
| GET | `/api/config` | handleAPIConfig | daemon, browser-local |
| GET, PUT | `/api/hotkeys` | handleAPIHotkeys | daemon, browser-local |
| POST | `/api/hotkeys/preset` | handleAPIHotkeysPreset | daemon, browser-local |
| POST | `/api/hotkeys/validate` | handleAPIHotkeysValidate | daemon, browser-local |
| POST | `/api/local-llm/download` | handleLocalLLMDownload | daemon |
| POST | `/api/local-llm/download/cancel` | handleLocalLLMDownloadCancel | daemon |
| GET | `/api/local-llm/models` | handleLocalLLMModels | daemon |
| POST | `/api/local-llm/start` | handleLocalLLMStart | daemon |
| GET | `/api/local-llm/status` | handleLocalLLMStatus | daemon |
| POST | `/api/onboarding/complete` | handleAPIOnboardingComplete | daemon, browser-local |
| POST | `/api/onboarding/skip` | handleAPIOnboardingSkip | daemon, browser-local |
| GET | `/api/onboarding/status` | handleAPIOnboardingStatus | daemon, browser-local |
| GET | `/api/providers` | handleAPIProviders | daemon, host |
| GET | `/api/providers/models` | handleGetModels | daemon, browser-local |
| GET, PUT | `/api/settings` | handleAPISettings | daemon, host |
| GET | `/api/settings/credentials` | handleAPISettingsCredentials | daemon, host |
| GET, POST, PUT, DELETE | `/api/settings/credentials/` | handleAPISettingsCredentials | daemon, host |
| GET, PUT | `/api/settings/mcp` | handleAPISettingsMCP | daemon, browser-local |
| GET, POST, PUT, DELETE | `/api/settings/mcp/servers/` | handleAPISettingsMCPServers | daemon, browser-local |
| GET, PUT | `/api/settings/providers` | handleAPISettingsProviders | daemon, host |
| GET, PUT, DELETE | `/api/settings/providers/` | handleAPISettingsProviders | daemon, host |
| GET, PUT | `/api/settings/skills` | handleAPISettingsSkills | daemon, browser-local |
| GET | `/api/settings/subagent-types` | handleAPISettingsSubagentTypes | daemon, browser-local |
| GET | `/api/settings/subagent-types/` | handleAPISettingsSubagentTypes | daemon, browser-local |
| GET | `/api/skills` | handleAPIListSkills | daemon |
| GET, POST | `/api/skills/` | handleAPISkillsRoutes | daemon |

Notes:
- `/api/computer-use/test` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)
- `/api/local-llm/download` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)
- `/api/local-llm/download/cancel` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)
- `/api/local-llm/models` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)
- `/api/local-llm/start` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)
- `/api/local-llm/status` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)
- `/api/settings/credentials` — registry methods GET vs handler methods GET, POST, PUT, DELETE
- `/api/settings/providers` — registry methods GET, PUT vs handler methods GET, POST, PUT, DELETE
- `/api/settings/providers/` — registry methods GET, PUT, DELETE vs handler methods GET, POST, PUT, DELETE
- `/api/skills` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)
- `/api/skills/` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)

## terminal

| Method | Path | Handler | Served by |
|---|---|---|---|
| GET | `/api/lsp/status` | handleLSPStatus | daemon, browser-local |
| GET | `/api/lsp/ws` | lspproxy.BridgeHandler | daemon, browser-local |
| GET | `/api/terminal/agent-sessions` | handleAPIAgentSessions | daemon, host |
| GET, POST | `/api/terminal/agent-sessions/` | handleAPIAgentSessionActions | daemon, host |
| GET, POST | `/api/terminal/history` | handleTerminalHistory | daemon, in-browser WASM agent |
| GET | `/api/terminal/sessions` | handleAPITerminalSessions | daemon, in-browser WASM agent |
| GET | `/api/terminal/shells` | handleAPITerminalShells | daemon, in-browser WASM agent |
| any | `/terminal` | handleTerminalWebSocket | daemon |

## workspace/instances

| Method | Path | Handler | Served by |
|---|---|---|---|
| GET | `/api/instances` | handleAPIInstances | daemon, browser-local |
| POST | `/api/instances/select` | handleAPIInstanceSelect | daemon, browser-local |
| POST | `/api/instances/ssh-browse` | handleAPISSHBrowse | daemon, browser-local |
| POST | `/api/instances/ssh-close` | handleAPISSHSessionDelete | daemon, browser-local |
| GET | `/api/instances/ssh-hosts` | handleAPISSHHosts | daemon, browser-local |
| GET | `/api/instances/ssh-launch-status` | handleAPISSHLaunchStatus | daemon, browser-local |
| POST | `/api/instances/ssh-open` | handleAPISSHOpen | daemon, browser-local |
| GET | `/api/instances/ssh-sessions` | handleAPISSHSessions | daemon, browser-local |
| GET, POST | `/api/workspace` | handleAPIWorkspace | daemon, browser-local |
| GET | `/api/workspace/browse` | handleAPIWorkspaceBrowse | daemon, in-browser WASM agent |
| GET | `/api/workspace/projects` | handleAPIWorkspaceProjects | daemon |
| GET | `/api/workspace/symbols` | handleAPIWorkspaceSymbols | daemon, browser-local |
| POST | `/api/workspace/sync` | handleAPIWorkspaceSync | daemon |
| POST | `/api/workspace/takeover` | handleAPIWorkspaceTakeover | daemon |

Notes:
- `/api/workspace/projects` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)
- `/api/workspace/sync` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)
- `/api/workspace/takeover` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)

## sync/txn

| Method | Path | Handler | Served by |
|---|---|---|---|
| GET, HEAD, POST | `/api/sync` | handleAPISync | daemon |
| POST | `/api/sync/batch` | handleAPISyncBatch | daemon |
| POST | `/api/sync/op` | handleAPISyncOp | daemon |
| GET | `/api/sync/status` | handleAPISyncStatus | daemon |
| POST | `/api/txn/pull` | handleAPITxnPull | daemon |
| POST | `/api/txn/push` | handleAPITxnPush | daemon |
| POST | `/api/txn/run` | handleAPITxnRun | daemon |
| GET, HEAD | `/api/txn/status` | handleAPITxnStatus | daemon |

Notes:
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
| GET, POST | `/api/chat-session/` | handleAPIChatSessionWorktree | daemon, browser-local |
| GET | `/api/chat-sessions` | handleAPIChatSessions | daemon, browser-local |
| POST | `/api/chat-sessions/breakpoints` | handleAPIChatSessionBreakpoints | daemon |
| POST | `/api/chat-sessions/compact` | handleAPIChatSessionsCompact | daemon, browser-local |
| POST | `/api/chat-sessions/create` | handleAPIChatSessionsCreate | daemon, browser-local |
| POST | `/api/chat-sessions/create-in-worktree` | handleAPIChatSessionCreateInWorktree | daemon, browser-local |
| POST | `/api/chat-sessions/delete` | handleAPIChatSessionsDelete | daemon, browser-local |
| POST | `/api/chat-sessions/delete-all` | handleAPIChatSessionsDeleteAll | daemon, browser-local |
| POST | `/api/chat-sessions/fork` | handleAPIChatSessionFork | daemon |
| POST | `/api/chat-sessions/history` | handleAPIChatSessionClearHistory | daemon |
| GET | `/api/chat-sessions/messages` | handleAPIChatSessionMessages | daemon, browser-local |
| POST | `/api/chat-sessions/pin` | handleAPIChatSessionsPin | daemon, browser-local |
| POST | `/api/chat-sessions/rename` | handleAPIChatSessionsRename | daemon, browser-local |
| POST | `/api/chat-sessions/switch` | handleAPIChatSessionsSwitch | daemon, browser-local |
| POST | `/api/chat-sessions/unpin` | handleAPIChatSessionsUnpin | daemon, browser-local |
| GET | `/api/chat-sessions/worktree-mappings` | handleAPIChatSessionWorktreeList | daemon, browser-local |
| GET | `/api/sessions` | handleAPISessions | daemon, browser-local |
| POST | `/api/sessions/restore` | handleAPIRestoreSession | daemon, browser-local |
| GET | `/api/sessions/search` | handleAPISessionsSearch | daemon, browser-local |
| GET | `/api/sessions/{id}/export` | handleAPISessionExport | daemon |

Notes:
- `/api/chat-sessions/breakpoints` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)
- `/api/chat-sessions/fork` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)
- `/api/chat-sessions/history` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)
- `/api/sessions/{id}/export` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)

## search

| Method | Path | Handler | Served by |
|---|---|---|---|
| GET | `/api/search` | handleAPIQuerySearch | daemon, in-browser WASM agent |
| POST | `/api/search/replace` | handleAPIQuerySearchReplace | daemon, in-browser WASM agent |

## diagnostics

| Method | Path | Handler | Served by |
|---|---|---|---|
| POST | `/api/diagnostics` | handleAPIDiagnostics | daemon, browser-local |
| POST | `/api/semantic` | handleAPISemantic | daemon, browser-local |
| GET | `/api/stats` | handleAPIStats | daemon, host |
| GET | `/api/support-bundle` | handleAPISupportBundle | daemon, browser-local |
| GET | `/api/ws-metrics` | handleAPIWSMetrics | daemon |

Notes:
- `/api/ws-metrics` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)

## design

| Method | Path | Handler | Served by |
|---|---|---|---|
| GET | `/api/design/status` | handleAPIDesignStatus | daemon |

Notes:
- `/api/design/status` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)

## starters

| Method | Path | Handler | Served by |
|---|---|---|---|
| GET | `/api/starters` | handleAPIStartersList | daemon |
| POST | `/api/starters/instantiate` | handleAPIStartersInstantiate | daemon |

Notes:
- `/api/starters` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)
- `/api/starters/instantiate` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)

## misc/static

| Method | Path | Handler | Served by |
|---|---|---|---|
| any | `/` | handleIndex | daemon |
| GET | `/api/bootstrap` | handleAPIBootstrap | daemon |
| POST | `/api/open-in-file-browser` | handleAPIOpenInFileBrowser | daemon, browser-local |
| POST | `/api/proxy/chat` | handleAPIProxyChat | daemon |
| GET | `/api/proxy/chat/status` | handleAPIProxyChatStatus | daemon |
| POST | `/api/proxy/chat/stop` | handleAPIProxyChatStop | daemon |
| GET | `/api/proxy/stats` | handleAPIProxyStats | daemon |
| any | `/asset-manifest.json` | handleAssetManifest | daemon |
| any | `/assets/` | handleAssets | daemon |
| any | `/browserconfig.xml` | handleBrowserConfig | daemon |
| any | `/debug/goroutines` | inline closure | daemon |
| any | `/favicon.ico` | handleFavicon | daemon |
| any | `/health` | inline closure | daemon |
| any | `/icon-192.png` | handleIcon192 | daemon |
| any | `/icon-512.png` | handleIcon512 | daemon |
| any | `/logo-mark.svg` | handleLogoMark | daemon |
| any | `/manifest.json` | handleManifest | daemon |
| any | `/ssh/` | handleSSHProxy | daemon |
| any | `/static/` | handleStaticFiles | daemon |
| any | `/sw.js` | handleServiceWorker | daemon |
| any | `/ws` | handleWebSocket | daemon |

Notes:
- `/api/bootstrap` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)
- `/api/proxy/chat` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)
- `/api/proxy/chat/status` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)
- `/api/proxy/chat/stop` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)
- `/api/proxy/stats` — not listed in the endpoint registry or an intercept; proxied to the host backend in cloud mode (gap)
