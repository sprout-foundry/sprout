# SP-063-4h: Destructive-App Denylist — Pre-Click Gate for Computer-Use Actions

## TL;DR

Add a per-action confirmation gate that fires **before** any `mouse_click` /
`keyboard_press` whose target app is on a curated destructive-app denylist
(Mail, Disk Utility, banking apps, password managers, system Settings, etc.).
The gate has three layers:

1. **Detection** — `osascript` on macOS (`System Events` → `name` and
   `bundle identifier` of the frontmost process); `xdotool` + optional
   `wmctrl` on Linux X11 (window title + window class). Headless / Wayland
   → no-op (denylist gate skipped, per-session opt-in still applies).
2. **Classification** — hand-curated JSON list of bundle IDs (macOS) /
   window-class regexes (Linux) per **category** (`financial`, `system`,
   `destructive`, `password_manager`). User overrides via
   `~/.config/sprout/computer_use_denylist_overrides.json` (override wins
   on conflict; can also add an app to the denylist).
3. **Prompt placement** — before any `mouse_click` / `keyboard_press`
   that targets a denylist app, run detection + classification, then call
   the existing per-session opt-in approval manager with a
   `destructive_app` context. Per-session "always allow this app" persists
   to the override file.

**Chosen approach summary**: hand-curated JSON, override file,
pre-click gate in the existing audit decorator chain (no new decorator —
we add a `PreActionHook` function that the `auditingBackend` calls before
delegation). WebUI/CLI reuse the same approval-broker pattern as
`checkComputerUseSessionOptIn`. macOS detection via `osascript -e '...'`;
Linux X11 via `xdotool getactivewindow` + `wmctrl -lx` (when available);
headless / Wayland → no-op gate. **Tradeoff accepted**: hand-curated list
rots over time — we mitigate by category-level overrides (one entry blocks
all apps in a category) and by logging a `"destructive_app_classified"`
audit event with the detected app name + category so users can easily add
new entries.

---

## Glossary

| Term | Definition |
|------|-----------|
| **Destructive app** | An app whose visible UI affords a destructive or sensitive action reachable by a single click (Send in Mail, Empty Trash in Finder, Erase in Disk Utility, Transfer in a banking app) |
| **Foreground app** | The application process whose window is currently focused; on macOS detectable via `osascript` System Events, on X11 via `_NET_ACTIVE_WINDOW` |
| **Denylist** | A hand-curated JSON list (`pkg/agent_tools/computer_use/denylist.json`) of `{bundle_id}` (macOS) and `{window_class_regex, window_title_regex}` (Linux) entries per **category** |
| **Override file** | Per-user JSON file at `~/.config/sprout/computer_use_denylist_overrides.json` with the same schema; entries here REPLACE the default entry for the same `bundle_id`/`window_class`/`category` (override wins) |
| **PreActionHook** | A function injected into the `auditingBackend` decorator at `pkg/agent_tools/computer_use/audit.go:25` that runs *before* the inner `ComputerBackend` method is called; returns `ErrDestructiveAppBlocked` to short-circuit the action and prompt the user |
| **Approval broker** | The existing per-session opt-in mechanism at `pkg/agent/computer_use_registration.go` (`checkComputerUseSessionOptIn`); reused for the destructive-app prompt with `destructive_app` context |
| **Per-app allowlist** | A session-scoped map `app → "allowed"` stored alongside `computerUseSessionApproved`; entries persist for the session lifetime; "always allow" appends to the override file with `"allow": true` |

---

## (a) How do we detect the foreground app?

### macOS — `osascript` via System Events

We shell out to `osascript -e 'tell application "System Events" to ...'` to
query the frontmost process. Two pieces of information are needed: the
human-readable app name (for logging) and the bundle identifier (for
classification).

```bash
# Get the frontmost app name
osascript -e 'tell application "System Events" to get name of (first application process whose frontmost is true)'

# Get the frontmost app's bundle identifier
osascript -e 'tell application "System Events" to get bundle identifier of (first application process whose frontmost is true)'
```

Both calls take ~50-150ms each. We combine them into a single invocation
that returns both values as a tab-separated pair, e.g.:

```bash
osascript -e 'tell application "System Events" to set out to (name of (first application process whose frontmost is true)) & "\t" & (bundle identifier of (first application process whose frontmost is true))' -e 'return out'
```

This returns e.g. `Safari\tcom.apple.Safari`. Total latency: 100-200ms,
acceptable for a per-action gate (the agent's tool round-trip already
takes 1-3s).

**Permissions**: `osascript` querying System Events requires the user to
have granted Sprout **Accessibility** permission (already required for
the existing `cliclick` backend). No additional prompt.

**Failure modes**:
- `osascript` not on `$PATH` → return error, gate degrades to "skip"
- AppleScript error (e.g., System Events not running) → return error,
  gate degrades to "skip" with a one-time warning logged
- Bundle identifier unavailable (rare; happens for some non-bundled
  apps) → fall back to name-based classification only

### Linux X11 — `xdotool` + `wmctrl`

On X11, we use two queries to assemble the foreground-app tuple:

```bash
# Get the active window ID (hex)
xdotool getactivewindow

# Get the window title (via xdotool)
xdotool getactivewindow getwindowname

# Get the window class + name (via wmctrl, when available)
wmctrl -lx | awk '{print $1, $3, $4}'   # wid, WM_CLASS[Name], title
```

`xdotool` is already a hard dependency of the X11 backend, so it's
guaranteed to be present. `wmctrl` is optional — if absent, we fall back
to title-only classification (the denylist still matches by title regex).

**Failure modes**:
- No DISPLAY → gate skipped (X11 not running; user isn't on a graphical
  session)
- `xdotool` returns nothing (no focused window, e.g., desktop is focused)
  → fall back to the window the agent most recently interacted with
- `wmctrl` missing → title-only classification

### Headless / Wayland / other

On Wayland, synthetic input is blocked by design and there's no
standardized way to query the foreground window from an unprivileged
process (would need a `wlr-foreign-toplevel-management` portal). We
gate this out:

- `runtime.GOOS == "windows"` → no-op (Windows desktop-control support
  is out of scope for v1)
- `os.Getenv("WAYLAND_DISPLAY") != ""` → log once at startup and skip
  the denylist gate (the per-session opt-in still applies)
- `runtime.GOOS == "linux"` and no `$DISPLAY` → no-op (headless)

In all no-op paths, the gate silently passes — the existing safety stack
(rate limit + audit + opt-in) still applies.

### Interface sketch

```go
// pkg/agent_tools/computer_use/foreground.go

// ForegroundInfo describes the app currently in the foreground.
type ForegroundInfo struct {
    // AppName is the human-readable application name (e.g., "Safari",
    // "Mail", "Disk Utility"). Always populated when no error.
    AppName string

    // BundleID is the macOS bundle identifier (e.g., "com.apple.Safari").
    // Empty on Linux and on platforms where detection is unavailable.
    BundleID string

    // WindowClass is the X11 WM_CLASS[Name] field (e.g., "Navigator",
    // "Mail"). Empty on macOS and on platforms where detection is
    // unavailable.
    WindowClass string

    // WindowTitle is the X11 window title or the macOS window name.
    // Empty if not retrievable.
    WindowTitle string
}

// GetForegroundApp returns the foreground-app tuple for the current
// platform. Returns an error when detection fails (caller should log
// and proceed without the gate). Implementation is platform-selected
// via build tags (foreground_darwin.go, foreground_linux.go,
// foreground_other.go).
func GetForegroundApp() (ForegroundInfo, error)
```

---

## (b) How do we classify the foreground app?

### Hand-curated JSON, category-level overrides

We embed a default denylist at `pkg/agent_tools/computer_use/denylist.json`
(versioned with the binary; users can override via the per-user override
file). Each entry has a category so users can override at category
granularity ("always allow all financial apps").

```json
{
  "version": 1,
  "description": "Default destructive-app denylist for SP-063-4h. Categories: financial, system, destructive, password_manager. Override at ~/.config/sprout/computer_use_denylist_overrides.json.",
  "macos": [
    {
      "bundle_id": "com.apple.mail",
      "category": "destructive",
      "reason": "Send button in Mail sends email to recipients."
    },
    {
      "bundle_id": "com.apple.diskutility",
      "category": "destructive",
      "reason": "Erase button formats a disk."
    },
    {
      "bundle_id": "com.apple.systempreferences",
      "category": "system",
      "reason": "System Settings can change security / network / privacy settings."
    },
    {
      "bundle_id": "com.apple.Terminal",
      "window_title_regex": "(?i)\\bsudo\\b",
      "category": "destructive",
      "reason": "Terminal with sudo in title can run privileged commands."
    },
    {
      "bundle_id": "com.agilebits.onepassword7",
      "category": "password_manager",
      "reason": "Password manager — auto-fill could leak credentials."
    }
  ],
  "linux": [
    {
      "window_class_regex": "Thunderbird",
      "window_title_regex": "(?i)\\b(Compose|Send)\\b",
      "category": "destructive",
      "reason": "Thunderbird compose window — Send button sends email."
    },
    {
      "window_class_regex": "Disk Utility",
      "window_title_regex": "(?i)Erase",
      "category": "destructive",
      "reason": "Disk Utility erase dialog."
    },
    {
      "window_class_regex": "polkit-gnome-authentication-agent|pam-tally",
      "category": "destructive",
      "reason": "Polkit / sudo password prompt — type into a password field."
    },
    {
      "window_class_regex": ".*",
      "window_title_regex": "(?i)\\b(Authenticate|sudo password|Password:)\\b",
      "category": "destructive",
      "reason": "Generic sudo / auth prompt — even non-classified apps become destructive when prompting for password."
    }
  ]
}
```

**Why a hand-curated list?** A model-classified approach (screenshot +
LLM classification on every click) would add 500ms-2s of latency to every
click and require a model call for every action — unacceptable. The
hand-curated list covers 95% of common destructive apps and is trivial to
extend by editing JSON.

**Mitigation for the list rotting**: users can append to the override
file. The CLI exposes a `sprout computer-use denylist add` subcommand for
this; the WebUI exposes a settings panel.

### Override file

`~/.config/sprout/computer_use_denylist_overrides.json` has the same
schema. Merge semantics:

- Same `bundle_id` / `window_class_regex` → override entry **replaces**
  the default entry entirely (override wins).
- New `bundle_id` / `window_class_regex` → override entry is **added**
  to the denylist.
- An override entry with `"allow": true` in its `args` → the entry is
  **removed** from the effective denylist for this user. (This is the
  "always allow this app" path; persisted when the user clicks "always
  allow" on the prompt.)

Example override file:

```json
{
  "version": 1,
  "macos": [
    {
      "bundle_id": "com.apple.mail",
      "allow": true,
      "reason": "User explicitly allowed Mail on 2026-07-01."
    }
  ],
  "linux": []
}
```

### Interface sketch

```go
// pkg/agent_tools/computer_use/denylist.go

// Category describes why an app is on the denylist.
type Category string

const (
    CategoryFinancial        Category = "financial"
    CategorySystem           Category = "system"
    CategoryDestructive      Category = "destructive"
    CategoryPasswordManager  Category = "password_manager"
)

// Classification is the result of matching a ForegroundInfo against the
// effective denylist (default + user overrides).
type Classification struct {
    // Category is the matched denylist category, or "" when no match.
    Category Category

    // Reason is the human-readable reason from the matched entry.
    Reason string

    // MatchedEntry is the denylist entry that matched (default or override).
    MatchedEntry DenylistEntry

    // FromOverride is true when the matched entry came from the user's
    // override file (not the bundled default).
    FromOverride bool
}

// IsDestructiveApp classifies a foreground-app tuple against the effective
// denylist. Returns Classification{Category: ""} when no match.
func IsDestructiveApp(fg ForegroundInfo) Classification
```

---

## (c) Where does the gate live in the action loop?

### Pre-action hook in the auditing decorator

The existing decorator chain is
`real → panicable → rateLimited → auditing`. We add a `PreActionHook`
function to `auditingBackend` (or, alternatively, a new
`denylistBackend` decorator between `rateLimited` and `auditing`).
The hook fires before any ComputerBackend method is invoked.

**Chosen approach**: add a `PreActionHook` field to `auditingBackend`
(struct at `pkg/agent_tools/computer_use/audit.go:25`). The hook is
configured at registration time by
`RegisterComputerUseTools` (`pkg/agent/computer_use_registration.go:42`)
to call `GetForegroundApp` + `IsDestructiveApp` and prompt when
destructive.

**Why not a new decorator?** Adding a new layer in the chain means the
reviewer has to verify ordering doesn't break anything, and the
rate-limit decorator would need to count "blocked by denylist" actions
correctly. Hooking into the existing auditing decorator keeps the chain
unchanged and reuses the existing audit event machinery.

**Hook flow**:

1. User calls `mouse_click(x, y)`.
2. `auditingBackend.MouseClick(...)` runs:
   a. Start audit timer.
   b. **Run PreActionHook (NEW).** Returns one of:
      - `nil` → proceed to inner.
      - `ErrDestructiveAppBlocked` → log audit event with
        `destructive_app_classified` action, return the error.
      - `prompt.request` → call approval broker, wait, then either
        proceed or return `ErrDestructiveAppBlocked`.
   c. Delegate to `panicableBackend.MouseClick(...)`.
   d. Record audit event.
3. If `ErrDestructiveAppBlocked`, the tool handler returns the error
   to the LLM; the LLM sees a refusal and adjusts its plan.

**Prompt UI**: reuses the existing approval broker pattern from
`checkComputerUseSessionOptIn`. The WebUI shows a dialog:
"The agent wants to click on Mail (category: destructive). Reason: Send
button in Mail sends email to recipients. [Allow once] [Always allow
this app] [Deny]". The CLI shows an equivalent terminal prompt.

### Per-session allowlist

A session-scoped map `app → "allowed"` stored alongside
`computerUseSessionApproved` (in the agent struct or a package-level
variable) prevents re-prompting within the same session. Cleared on
`ClearSessionOverrides` like the existing workspace allowlist.

"Always allow this app" writes a `"allow": true` entry to the user's
override file (path: `~/.config/sprout/computer_use_denylist_overrides.json`).

---

## (d) Edge cases and open questions

### Race: foreground changes between detection and click

There's a race between `GetForegroundApp()` (detection) and the
subsequent `mouse_click` (action). The user could Alt-Tab to a
different app in the gap. We mitigate by:

- **Short detection window**: <300ms total (osascript 200ms +
  classification <1ms).
- **Re-check on click**: optionally re-call `GetForegroundApp` just
  before the click. Adds 200ms latency. **Chosen approach for v1**:
  re-check on click for high-risk categories (financial, destructive),
  single-check for lower-risk categories (system, password_manager).

### Browser incognito / private windows

Browsers in incognito mode have destructive potential (typed URLs
aren't logged, so users can't audit them). Detection: window title
contains "Incognito" / "Private" / "InPrivate". We add a special-case
denylist entry for browsers when in incognito. Implementation:
window-title regex match against `(Chrome|Edge|Firefox|Safari).*(Incognito|Private|InPrivate)`.

### Window title-only apps

Some destructive dialogs come from apps that aren't on the denylist
(e.g., a one-off shell script's GUI). The "any app with `sudo password`
in title" regex catches this. We accept that 100% coverage is
impossible and document the limit.

### User opts out entirely

A power user who wants zero friction can set
`ComputerUseConfig.DestructiveAppGate = false` to disable the gate.
This is a configuration option, defaulting to `true`. The audit log
records the disable action.

### Open questions for follow-up

- **Wayland support**: requires DBus portal (`wlr-foreign-toplevel-management`).
  Out of scope for v1.
- **Windows**: native `GetForegroundWindow` + `GetWindowText` via cgo,
  but Windows computer-use is out of scope (no backend today).
- **Model-classified fallback**: if we observe users repeatedly
  dismissing the prompt for unclassified apps, add a screenshot-based
  fallback ("agent is about to click on `X` — is this app sensitive?").
  Defer until usage data justifies it.

---

## Implementation Notes

### Files created/modified

| File | Change |
|------|--------|
| `roadmap/SP-063-destructive-denylist-design.md` | **New** — this document |
| `pkg/agent_tools/computer_use/foreground.go` | **New** — `ForegroundInfo`, `GetForegroundApp()` interface |
| `pkg/agent_tools/computer_use/foreground_darwin.go` | **New** — `osascript` impl |
| `pkg/agent_tools/computer_use/foreground_linux.go` | **New** — `xdotool` + optional `wmctrl` impl |
| `pkg/agent_tools/computer_use/foreground_other.go` | **New** — no-op stub |
| `pkg/agent_tools/computer_use/denylist.go` | **New** — `DenylistEntry`, `Classification`, `IsDestructiveApp()` |
| `pkg/agent_tools/computer_use/denylist.json` | **New** — hand-curated list (this doc's example) |
| `pkg/agent_tools/computer_use/audit.go` | **Modified** — add `PreActionHook func(method string, args ...any) error` field on `auditingBackend` |
| `pkg/agent_tools/computer_use/handlers.go` | **Modified** — call `PreActionHook` before delegation in `mouse_click` / `keyboard_press` paths (or wherever the hook is invoked) |
| `pkg/agent/computer_use_registration.go` | **Modified** — register the PreActionHook at setup time |
| `pkg/configuration/config_domain.go` | **Modified** — add `ComputerUseConfig.DestructiveAppGate bool` (default true), `OverrideFilePath string` (default `~/.config/sprout/computer_use_denylist_overrides.json`) |
| `cmd/computer_use_denylist.go` | **New** — `sprout computer-use denylist add/remove/list` CLI |

### Testing

Key scenarios: hand-curated list match (positive cases per category),
override-merge semantics (override wins; `allow:true` removes),
foreground detection mocks per platform (darwin/linux/other), no-op on
Wayland/headless, prompt integration (approval broker receives
`destructive_app` context), per-session allowlist (re-prompt blocked
after first allow within session), audit event emission.

### Rollout

Phase 1: detection + classification + default denylist (no prompt — just
log audit events). Phase 2: prompt integration + per-session allowlist.
Phase 3: override file + CLI subcommand + WebUI settings panel.
