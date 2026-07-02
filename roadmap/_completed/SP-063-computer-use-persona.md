# SP-063: Real `computer_user` Persona — Mouse/Keyboard/Screenshot Agent

**Status:** ✅ Implemented (2026-06-30; all safety gates shipped including panic key + destructive-app denylist)

The `computer_user` persona was previously a misnomer (shell-and-edit prompt with no actual desktop control). This spec re-introduced it as a real desktop-control persona that drives the user's mouse, keyboard, and screen via an LLM vision loop. Seven tools were added (`take_screenshot`, `mouse_click`, `mouse_drag`, `keyboard_type`, `keyboard_press`, `scroll`, `wait`) backed by platform-specific subprocess implementations (macOS `cliclick`/`screencapture`, Linux X11 `xdotool`/`scrot`). Eleven defense-in-depth safety gates were implemented: master switch (off by default), platform support check, vision capability requirement, top-level-only activation, non-interactive block, per-session opt-in, action-rate cap, audit log, persona allowlist + dispatch guard, panic key (`Ctrl+Shift+Escape`), and destructive-app denylist gate with foreground detection and user override file.

## Key decisions

- Subprocess backends (no CGO) for cross-platform portability and easier installation.
- Wayland explicitly unsupported in v1 (synthetic input blocked by design).
- Text-only providers refused — a blind agent driving the desktop is destructive.
- Destructive-app denylist is hand-curated (`denylist.json`, 43 entries) with per-user override file at `~/.config/sprout/computer_use_denylist_overrides.json`.
- Panic key uses a `PanicableBackend` decorator wrapping the subprocess backend; OS-level chord detection (CGEventTap/XRecord) deferred as best-effort.
- Per-session `computerUseAppAllowlist` short-circuits the denylist gate after first "Allow once".

## Artifacts

- code: `pkg/agent_tools/computer_use/handlers.go` — 7 tool handlers
- code: `pkg/agent_tools/computer_use/backend_subprocess.go` — macOS + Linux X11 subprocess backends
- code: `pkg/agent_tools/computer_use/audit.go` — auditing backend + destructive-app `PreActionHook`
- code: `pkg/agent_tools/computer_use/denylist.go` + `denylist.json` — denylist classifier + 43 curated entries
- code: `pkg/agent_tools/computer_use/panic_key.go` — panic key decorator
- code: `pkg/agent_tools/computer_use/foreground.go` — cross-platform foreground app detection
- code: `pkg/agent/destructive_app_prompter.go` — agent-side prompter for denylist matches
- code: `pkg/agent_tools/computer_use/safety.go` — rate-limited backend
- code: `webui/src/components/settings/ComputerUseSettingsTab.tsx` — settings UI
- code: `pkg/agent/prompts/subagent_prompts/computer_user.md` — persona prompt
- code: `pkg/personas/configs/computer_user.json` — persona config with tool allowlist
- tests: `pkg/agent_tools/computer_use/*_test.go` — unit + mock-backend + roundtrip tests

Full specification archived — see git history for original content.
