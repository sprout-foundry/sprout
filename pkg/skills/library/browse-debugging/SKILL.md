---
name: Browse Debugging
description: Multi-step interactive browser debugging with persistent sessions for web UI investigation.
---

# Interactive Browser Debugging

Use this skill when debugging web pages, testing local dev servers, investigating UI bugs, or scraping JS-rendered content with `browse_url`.

## Session Lifecycle

Every multi-step debug session follows this pattern:

```
Call 1: persist_session=true  → get back session_id
Call 2+: session_id=X         → reuse same browser, same page state
Final:   close_session=true    → clean up (or sessions leak browser processes)
```

**Always close sessions.** Each persistent session is a full incognito Chromium process. Leaking them consumes memory and file descriptors.

## Critical: URL is Navigated on Every Call

Even with `session_id`, the `url` parameter triggers a navigation. If you want to stay on the current page, pass the `final_url` from the previous result. If you want to go somewhere new, pass the new URL.

Do NOT assume `session_id` means "stay where you are." It means "reuse the same browser context (cookies, localStorage, service workers)."

## When to Use Steps vs Round-Trips

**Batch in `steps`** when you know the full sequence upfront:
```json
{
  "url": "/login",
  "session_id": "abc",
  "steps": [
    {"action": "fill", "selector": "#email", "value": "user@test.com"},
    {"action": "fill", "selector": "#password", "value": "pass123"},
    {"action": "click", "selector": "button[type=submit]"},
    {"action": "wait_for_text", "text": "Dashboard", "millis": 5000}
  ],
  "action": "inspect",
  "capture_text": true
}
```

**Round-trip to the model** only when you need to make a decision based on what's on the page.

## Fill vs Type

- `fill` — sets `.value` directly, dispatches `input` + `change` events. Fast. Works for most forms.
- `type` — sends individual keystrokes (keydown/keyup/keypress). Slower but necessary for **React controlled inputs**, autocomplete dropdowns, and components that listen for native keyboard events.

**Rule of thumb**: If `fill` doesn't seem to register (form validation fails, input appears empty after fill), switch to `type`.

## Wait Strategy

After clicking something that triggers navigation or loads dynamic content:

1. **Best**: `{"action": "wait_for", "selector": ".result-item"}` — waits for a specific element
2. **Good**: `{"action": "wait_for_text", "text": "Results loaded"}` — waits for expected text
3. **Avoid**: `{"action": "sleep", "millis": 3000}` — fixed delays are brittle and slow

Never skip waits. Without them, you'll capture the page mid-transition and get stale content.

## Diagnostic Capture Guide

Choose captures based on what you're debugging:

| Symptom | Captures to Use |
|---------|----------------|
| Blank page / missing content | `capture_text`, `include_console` |
| JS errors / broken interaction | `include_console`, `capture_dom` |
| API not returning data | `capture_network` (shows fetch/XHR status codes) |
| CORS issues | `capture_network` (populates `cors_issues` array) |
| Auth not persisting | `capture_cookies`, `capture_storage` |
| Wrong element state | `capture_selectors: [".btn", ".modal"]` |
| Redirect issues | Check `final_url` in result (always present) |

For initial triage, use `action: "inspect"` with `capture_text: true` and `include_console: true`. Add more captures as you narrow down.

## Common Patterns

### Debug a local dev server page
```
Call 1: url="http://localhost:3000/dashboard", persist_session=true, action="inspect",
        capture_text=true, include_console=true
→ Read text + console errors. Identify issue.

Call 2: url="http://localhost:3000/dashboard", session_id=X,
        steps=[...interact with page...],
        action="inspect", capture_network=true
→ Check network requests after interaction.

Call 3: close_session=true, session_id=X
```

### Screenshot for visual verification
```
Call: url="http://localhost:5173", action="screenshot",
      screenshot_path="/tmp/debug-homepage.png"
→ Capture visual state, then analyze the image or share with user.
```

### Check specific elements
```
Call: url="...", action="inspect",
      capture_selectors: ["#error-banner", ".api-status", "table tbody tr"]
→ Returns text/content of each matched element without full DOM dump.
```

## Things That Trip Up Models

1. **Forgetting `persist_session: true` on the first call** — Without it, the browser closes immediately and `session_id` is never returned.

2. **Using `action: "text"` when you need diagnostics** — `text` only returns visible text. Use `action: "inspect"` for console, network, cookies, and selector captures.

3. **Not waiting after form submission** — Clicking submit then immediately capturing text captures the loading state, not the result.

4. **Ignoring `page_errors` in inspect output** — JavaScript errors are reported separately from console messages. Check both.

5. **Assuming selectors exist** — Dynamic pages may not have elements immediately. Use `wait_for` before interacting, or `wait_for_selector` at the top level.
