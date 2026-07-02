# SP-079: Migrate Stub Tool Handlers off the Legacy `*Agent` Path

**Status:** ✅ Implemented (2026-06-30; all 5 stub handlers rewritten, zero "requires full *Agent refactoring" remain)

Five handlers in the new `pkg/agent_tools/*_handler.go` path were stubs returning `requires full *Agent refactoring` when invoked via the new registry, causing `analyze_image_content`, `analyze_ui_screenshot`, `browse_url`, `activate_skill`, and `web_search` to be intermittently broken. Each stub was replaced with a working handler that calls the same underlying logic as the legacy path but accepts dependencies via `ToolEnv` instead of `*Agent`. `ToolEnv` was extended with four new fields: `VisionProcessor`, `WebBrowser`, `SkillLoader`, and `SearchEngine` — each a simple interface. The dispatch shim in `tool_definitions.go` populates these fields from the agent's subsystems. The "thin wrappers pending refactoring" caveat was removed from `handler.go`.

## Key decisions

- Dependencies expressed as interfaces (`WebBrowser`, `SkillLoader`, `SearchEngine`) rather than concrete types — simplifies testing and keeps `pkg/agent_tools` decoupled from `*Agent`.
- `WebBrowser` is nil-tolerant — returns structured "browser unavailable" error instead of failing fast, allowing non-browser deployments to function.
- `VisionProcessor` is a concrete struct (not interface) — it's already well-defined in `vision_types.go` and doesn't need indirection.
- Conformance tests compare new handler outputs against legacy handler outputs with identical args to verify parity.

## Artifacts

- code: `pkg/agent_tools/handler.go` — `ToolEnv` extended with `VisionProcessor`, `WebBrowser`, `SkillLoader`, `SearchEngine`
- code: `pkg/agent_tools/analyze_image_content_handler.go` — working handler using `env.VisionProcessor`
- code: `pkg/agent_tools/browse_url_handler.go` — working handler using `env.WebBrowser`
- code: `pkg/agent_tools/activate_skill_handler.go` — working handler using `env.SkillLoader`
- code: `pkg/agent_tools/web_search_handler.go` — working handler using `env.SearchEngine`
- tests: `pkg/agent_tools/analyze_handlers_test.go` — conformance tests asserting no "requires full *Agent refactoring" in output

Full specification archived — see git history for original content.
