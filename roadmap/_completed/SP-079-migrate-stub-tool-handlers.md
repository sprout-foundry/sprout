# SP-079: Migrate Stub Tool Handlers off the Legacy `*Agent` Path

**Status:** ✅ Implemented (2026-06-30)
**Date:** 2026-06-27
**Depends on:** SP-074 (Tool-Registry Migration)
**Priority:** Medium-High (LLM usability — these tools currently error out when invoked via the new registry)
**Effort Estimate:** ~1–2 weeks (5 handlers + dispatch plumbing)

## Problem

SP-074 deliberately finished *dispatch/env plumbing* but left "every handler's rewrite" out of scope (`pkg/agent_tools/handler.go:43`):

> Some current tools (e.g., browseURLHandler, runSubagentHandler) are thin wrappers around legacy agent methods, pending full refactoring.

In practice, 5 handlers in the new `pkg/agent_tools/*_handler.go` path are **stubs** that return `requires full *Agent refactoring` the moment the LLM invokes them. The legacy func-style handlers in `pkg/agent/tool_handlers_*.go` work correctly, but anyone whose persona or dispatch path routes through the new registry hits the error path. The result is that `analyze_image_content`, `analyze_ui_screenshot`, `browse_url`, `activate_skill`, and `web_search` are intermittently broken depending on how the agent happens to dispatch them.

## Concrete evidence

| Tool | Stub file (errors) | Working impl | Failure mode |
|------|---|---|---|
| `analyze_image_content` | `pkg/agent_tools/analyze_image_content_handler.go:47` | `pkg/agent/conversation.go:374` (`NewVisionProcessorWithProvider` → `AnalyzeImage`) | Always returns `requires full *Agent refactoring` |
| `analyze_ui_screenshot` | `pkg/agent_tools/analyze_ui_screenshot_handler.go:48` | `pkg/agent/tool_handlers_browse.go:screenshot` | Same |
| `browse_url` | `pkg/agent_tools/browse_url_handler.go:64` | `pkg/agent/tool_handlers_browse.go:14::handleBrowseURL` (`webcontent.BrowseURL`) | Same |
| `activate_skill` | `pkg/agent_tools/activate_skill_handler.go:47` | `pkg/agent/skills.go:174::handleActivateSkill` (`LoadSkill`) | Same; breaks `cmd/automate.go` and `cmd/skill.go` UX |
| `web_search` | `pkg/agent_tools/web_search_handler.go:45` | `pkg/agent/tool_handlers_search.go:215::handleWebSearch` (`WebSearch`) | Same |

All 5 are registered in `pkg/agent_tools/all.go:69,71,72,75,76`.

## Goals

1. Replace each stub with a working handler that calls the same underlying logic as the legacy path, but accepts its dependencies via `ToolEnv` (no `*Agent`).
2. Extend `ToolEnv` with the minimum set of new fields/interfaces needed to express those dependencies.
3. Confirm parity with the legacy handler outputs for each tool.
4. Once all 5 are migrated, remove the "thin wrappers … pending full refactoring" caveat from `pkg/agent_tools/handler.go:43` and retire the dual-dispatch shim for these tools.

## Design

### `ToolEnv` extensions

Add three fields to `pkg/agent_tools/handler.go:86::ToolEnv`:

```go
// VisionProcessor, when set, lets vision-dependent tools analyze
// images and UI screenshots without holding a *Agent reference.
VisionProcessor *VisionProcessor

// WebBrowser runs headless browser navigation (Playwright wrapper).
// Nil means the tool must report "browser unavailable".
WebBrowser WebBrowser

// SkillLoader resolves skill IDs to their on-disk instructions.
// Implementations: agent's skill manager, or a stub for tests.
SkillLoader SkillLoader

// SearchEngine performs Google Custom Search API queries.
// Reads its API key from configuration.
SearchEngine SearchEngine
```

Each interface is one or two methods. Concrete types live alongside their existing definitions (`pkg/agent_tools/vision_analyze.go`, `pkg/agent_tools/web_search.go`, `pkg/agent/skills.go`).

### Dispatch shim updates

`pkg/agent/tool_definitions.go` builds the `ToolEnv` from `*Agent` context. Populate the new fields:

```go
env := ToolEnv{
    // existing fields...
    VisionProcessor: a.GetVisionProcessor(),
    WebBrowser:      a.GetPlaywrightBrowserManager().Browser(), // adapt if shape differs
    SkillLoader:     a.skillManager,
    SearchEngine:    a.GetSearchEngine(),
}
```

### Per-handler rewrites

Each handler's `Execute` body becomes a thin call to the legacy logic via `ToolEnv`:

- **`analyze_image_content`** — extract `image_path` (and optional `prompt`) from args; call `env.VisionProcessor.AnalyzeImage(ctx, path, prompt)`; return the analysis string.
- **`analyze_ui_screenshot`** — same as above with a UI-specific prompt; if `processor.LooksLikeUI(...)` is false, the handler emits a warning so the LLM knows the input wasn't a screenshot.
- **`browse_url`** — call `env.WebBrowser.BrowseURL(ctx, url, opts)`; return the rendered text.
- **`activate_skill`** — call `env.SkillLoader.Load(ctx, skillID)`; format the skill instructions as a tool result.
- **`web_search`** — call `env.SearchEngine.Search(ctx, query)`; return the result list.

### Testing

`pkg/agent_tools/new_tools_conformance_test.go` already exists with a `TestWebSearchHandlerConformance` slot. Add 4 more tests (one per migrated handler). Each test:

1. Constructs a `ToolEnv` with a real or stub dependency (vision processor, browser, skill loader, search engine).
2. Invokes the handler via the new registry with fixed args.
3. Captures the output.
4. Invokes the legacy handler with the same args.
5. Asserts the outputs are equivalent (string equality or JSON-equivalent for structured results).

### Phase plan

| Phase | Scope | Acceptance |
|-------|-------|------------|
| 1 | Extend `ToolEnv` with the 4 new fields/interfaces; populate them in the dispatch shim. | `go build ./...` clean; existing tests still pass. |
| 2 | Migrate `analyze_image_content` + `analyze_ui_screenshot` (shared `VisionProcessor` dependency). | Conformance tests for both; legacy tests still green. |
| 3 | Migrate `browse_url`. | Conformance test; legacy test still green. |
| 4 | Migrate `activate_skill`. | Conformance test; `cmd/automate.go` and `cmd/skill.go` smoke-tested. |
| 5 | Migrate `web_search`. | Conformance test; legacy test still green. |
| 6 | Remove "thin wrappers" caveat from `pkg/agent_tools/handler.go:43`; retire dual-dispatch shim entries for these 5 tools. | `grep -rn "requires full.*Agent refactoring" pkg/agent_tools/` returns nothing. |

## Success Criteria

- All 5 handlers pass conformance tests against legacy equivalents.
- `grep -rn "requires full.*Agent refactoring" pkg/agent_tools/` returns zero matches.
- `make build-all` clean.
- `go test ./pkg/agent_tools/... ./pkg/agent/... ./cmd/...` all green.
- The "thin wrappers" caveat in `pkg/agent_tools/handler.go:43` is removed or updated to note that all tools now use the new path.

## Risks

- **Breaking dispatch shim for legacy agents.** If any non-migrated consumer still expects the legacy call path, removing the shim entry would break them. Mitigation: keep the shim's *lookup* logic intact; only the per-tool "dispatch here if found in new registry" entries get retired when their handlers are migrated.
- **Hidden behavior differences.** Legacy handlers may emit specific event types, status messages, or apply persona-specific transformations that the new handlers must replicate. Mitigation: conformance tests compare outputs as the contract; manual smoke test for any persona with non-default behavior.

## Open Questions

1. Should `VisionProcessor` etc. be interfaces or concrete types? Interfaces simplify testing but add a layer of indirection. **Recommendation:** interfaces — the conformance tests already mock dependencies, and `pkg/agent_tools` shouldn't reach back into `*Agent` for concrete types.
2. Should `WebBrowser` be `nil`-tolerant (return a structured "browser unavailable" error) or fail-fast? **Recommendation:** nil-tolerant — a non-browser-enabled deployment should still parse URLs without crashing.
