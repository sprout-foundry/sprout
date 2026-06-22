# SP-063: Real `computer_user` Persona — Mouse/Keyboard/Screenshot Agent

**Status:** 🚧 Partially implemented (2026-06-15) — core shipped (backend + tools + persona + safety gates); WebUI settings + interactive opt-in remain
**Date:** 2026-06-03
**Depends on:** SP-050 (orchestrator persona collapse — same persona-system mechanics)
**Priority:** Medium-Low (capability addition, not bug-fix)

## Implementation status (2026-06-15)

The Go core landed and is build-verified (`make build-all`) + unit-tested
(`pkg/agent_tools/computer_use/*_test.go`, 0 GUI dependency — backends are
exercised via an overridable `commandRunner`).

| Phase | Status | Where |
|---|---|---|
| 1 Tool surface (7 tools + Anthropic `computer_20241022` translation) | ✅ done | `handlers.go`, `anthropic.go`, `registry.go` |
| 2 Platform backends (macOS `cliclick`/`screencapture`, Linux-X11 `xdotool`/`scrot`/`import`; Wayland + other OS rejected with a clear reason; region crop in-process) | ✅ done | `backend_subprocess.go`, `backend_select.go` |
| 3 Vision wiring (screenshot returns an image content block; activation refused on text-only providers) | ✅ done | `handlers.go` + `checkComputerUseActivation` (`computer_use_registration.go`, called from `ApplyPersona`) |
| 4 Safety gates | 🟡 mostly done | off-by-default `ComputerUseConfig.Enabled`, audit log (`audit.go`), action-rate cap (`safety.go`), and **activation gates** (enabled flag, platform-supported, top-level-only/no-subagent, vision-capable) **done**; per-session interactive opt-in dialog, global Ctrl+C+Esc panic key, destructive-app denylist heuristic, and precise `--skip-prompt`/daemon block **remain** |
| 5 Persona prompt | ✅ done | `pkg/agent/prompts/subagent_prompts/computer_user.md` |
| 6 Tool allowlist (computer_user only) | ✅ done | persona `allowed_tools` (`pkg/personas/configs/computer_user.json`) + dispatch-layer guard (`computer_use_registration.go`, `tool_security.go`) |
| 7 WebUI settings panel | ❌ remaining | "Computer Use (Experimental)" section + Test-connection button |
| 8 Tests | 🟡 unit done | mock-backend unit tests shipped; Xvfb+tkinter integration smoke **remains** |

Registration is gated four ways (config flag off by default → real backend must
exist → exposed only to `computer_user` persona → dispatch-layer rejection for
any other persona) and wired at agent creation
(`agent_creation.go` → `RegisterComputerUseTools`). The platform backends are
written but cannot be functionally verified in CI/headless — they need a real
macOS/X11 display.

## Background

The previous `computer_user` persona was a misnomer — its prompt was a shell-and-edit persona with no actual computer-use capability. It was deleted in the persona cleanup round (see commit history around the SP-063 introduction).

This spec proposes re-introducing `computer_user` as a *real* desktop-control persona that drives the user's mouse, keyboard, and screen via an LLM loop. The model takes screenshots, interprets them with vision, and emits click/type/scroll actions until a task is complete. This mirrors Anthropic's `computer_20241022` tool and equivalent capabilities in other multimodal providers.

## Problem

There is no in-Sprout way to ask an agent to drive an actual desktop application — clicking through a setup wizard, filling a native-app form, automating a non-CLI tool, etc. `browse_url` only covers headless browsers. The current `coder`/`general` personas can shell out but cannot interact with GUI applications.

## Proposed Solution

A new `computer_user` persona with platform-specific backend, vision wiring, and strict safety gates.

### Phase 1: Tool surface

Add new tool handlers under `pkg/agent_tools/computer_use/`:

- `take_screenshot(region? = {x, y, width, height})` → `{image_base64, width, height, display_id}`
- `mouse_click(x, y, button = "left", double = false)`
- `mouse_drag(from_x, from_y, to_x, to_y, button = "left")`
- `keyboard_type(text)` — sends a string verbatim
- `keyboard_press(key)` — single special key (Enter, Tab, Escape) or chord (`cmd+space`, `ctrl+shift+t`)
- `scroll(direction, amount, x?, y?)` — scroll at coordinates
- `wait(ms)` — short sleep to let UI settle

**Anthropic provider shortcut.** Anthropic's `computer_20241022` tool defines this schema natively. For Claude sessions, register that tool and route its calls to our backend. Saves inventing a schema and gets us free model-side fluency.

### Phase 2: Platform backends

Backend interface in Go:

```go
type ComputerBackend interface {
    Screenshot(region *Rect) (image []byte, dims Size, err error)
    MouseClick(x, y int, button MouseButton, double bool) error
    MouseDrag(from, to Point, button MouseButton) error
    KeyboardType(text string) error
    KeyboardPress(key string) error
    Scroll(dir ScrollDir, amount int, at *Point) error
}
```

Implementations:

| Platform | Approach | Notes |
|---|---|---|
| macOS | `cliclick` + `screencapture` (subprocess), or CGEvent via cgo | First run prompts user to grant Accessibility + Screen Recording permissions |
| Linux X11 | `xdotool` + `scrot` (subprocess) | Works on any X11 session |
| Linux Wayland | Not supported in v1 | Wayland's input model blocks synthetic events. Defer or require X11. |
| Windows | `SendInput` via cgo, or PowerShell subprocess | Works natively |
| WSL2 | Requires WSLg (Win 11+) for display; otherwise X-forward | Document as unsupported on plain WSL2 |

Default to subprocess implementations (no cgo, easier to install). Optionally add a `robotgo`-based path behind a build tag for users who want a single static binary.

### Phase 3: Vision wiring

The model must see the screenshot it just took.

- **Native vision providers** (Anthropic, OpenAI vision, Gemini): send the PNG as an `image` content block. Confirm `pkg/agent_api` already supports per-message image content; add provider-specific encoders if missing.
- **Text-only providers**: refuse to activate the persona with a clear error. Computer use without vision is operating blind — not a useful fallback.

Persona definition gets a new field:

```json
"requires_capabilities": ["vision"]
```

Surfaced via `Definition` and checked in `ApplyPersona` and `tool_handlers_subagent.go` spawn path.

### Phase 4: Safety gates

Computer use is categorically more dangerous than file edits — a click can send an email, empty trash, or submit a payment. Safety is non-negotiable.

Required gates (all on by default; user can relax in settings):

1. **Off by default.** Settings flag `enable_computer_use_tools = false`. Toggling it on triggers a one-time "I understand this lets the agent control my computer" confirmation.
2. **Per-session opt-in.** First computer-use tool call of a session prompts: "Allow agent to control your computer for this session? [Yes once / Yes always for this workspace / No]"
3. **Foreground-only.** Disabled in `--skip-prompt`, daemon, and non-interactive modes. No silent autonomous computer use.
4. **Panic key.** A global handler binds Ctrl+C + Esc to immediate halt; injected before the persona's first tool call.
5. **Audit log.** Every action recorded to `~/.config/sprout/computer_use_log/<session>.jsonl` with timestamp, action, coordinates, and a thumbnail of the screen state at action time.
6. **Destructive-app heuristic.** Before clicking when foreground window matches a denylist (Mail, Banking, Disk Utility, Terminal-with-sudo-history, system Settings), require per-action confirmation.
7. **Action-rate limit.** Hard cap at e.g. 60 actions/minute to prevent runaway loops from causing OS-level damage before the user notices.

### Phase 5: Persona prompt

New `pkg/agent/prompts/subagent_prompts/computer_user.md`:

- Identity: "You operate the user's actual computer. Humans are watching."
- Workflow: always screenshot → describe → propose → act → screenshot to verify.
- Coordinate handling: read coordinates off the screenshot's dimensions, expect (0,0) at top-left.
- Mandatory pause-and-ask before: Send buttons, Submit buttons, Delete/Empty Trash, Pay/Confirm, system password prompts, browser address bar entries (the user might prefer to type themselves).
- Failure modes: stop and ask if a screenshot is ambiguous, if a click doesn't change the screen, or if a permission dialog appears.

### Phase 6: Tool allowlist

The new tools are allowlisted **only** for the new `computer_user` persona. Other personas reject them at the tool-dispatch layer. A user who wants computer use must explicitly switch to `computer_user`.

### Phase 7: Settings UI

In the WebUI settings panel:

- New section: "Computer Use (Experimental)"
- Master toggle: `enable_computer_use_tools` (default off)
- Workspace allowlist: "Auto-approve computer use in these workspaces"
- Audit log location (read-only display + "Open log folder" button)
- "Test connection" button: takes one screenshot, displays it, confirms backend works

### Phase 8: Tests

- Unit: mock backend records "would have clicked at (x,y)"; assert tool handler translates LLM calls correctly.
- Integration: Xvfb + a controlled tkinter app with a known button; assert agent can click it and the app's state changes.
- Manual smoke matrix: macOS (Accessibility granted), Linux X11, Windows. Document expected results.

## Out of Scope

Deferred or rejected:

- **Wayland support.** Wayland blocks synthetic input by design. Possible future workaround via a Sprout-shipped Wayland compositor extension, but not in v1.
- **Mobile (iOS/Android).** No.
- **Remote computer use** (driving a different machine over the network). The audit and consent model is local-only.
- **Visual-element-finding heuristics** (OCR, OmniParser-style element detection). Rely on the multimodal model's own vision for v1; revisit if accuracy is poor.
- **Recording/replay** of computer-use sessions for testing. Useful but separate scope.
- **Automatic permission grant** on macOS. Sprout must direct the user to System Settings; we never automate the grant.

## Success Criteria

- A user on macOS with Accessibility + Screen Recording granted can ask `sprout agent --persona computer_user "open Calculator and compute 1234 × 5678"` and the agent succeeds.
- A user on Linux X11 with `xdotool` + `scrot` installed can do the same.
- Attempting to activate `computer_user` on a text-only provider returns a clear "this persona requires a vision-capable provider" error.
- With `enable_computer_use_tools=false`, the persona fails to activate with a clear "enable in settings first" message.
- Audit log files appear under `~/.config/sprout/computer_use_log/` containing every action of a session.
- Ctrl+C during a computer-use loop halts within 500 ms.
- The destructive-app heuristic prompts before the agent can click "Send" in Mail or "Empty Trash" in Finder.
- `go test ./...` and `make build-all` pass on macOS and Linux with the new backend code.

## Effort Estimate

Rough sizing (not commitments):

- macOS-only v1 with Anthropic-provider-only vision: ~1 week
- Cross-platform (macOS + Linux X11 + Windows) with provider-portable vision: ~3-4 weeks
- Safety gates + settings UI + audit log: +1 week
- Tests + docs + smoke matrix: +1 week

So ~2 weeks for a credible macOS+Claude-only v1, ~5-6 weeks for credible cross-platform.

## Open Questions

1. Does the WSL2-on-Windows-11 path through WSLg work cleanly enough to support, or do we treat WSL as "unsupported, use the Windows-native sprout binary instead"?
2. Anthropic's `computer_20241022` tool comes with a recommended screen resolution (1024x768). Do we downscale screenshots to match, or send native resolution?
3. Per-action confirmation for destructive apps — is the denylist hand-curated, or does the model itself classify the app via screenshot?
4. Should the audit log be encrypted at rest? Screenshots may contain sensitive UI content (passwords mid-entry, financial info).
