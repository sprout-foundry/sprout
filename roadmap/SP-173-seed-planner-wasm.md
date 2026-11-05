# SP-173 — A Planner-Sized WASM Agent on Seed

> **Status (2026-11-01):** Proposed. Not started.
> Related: SP-112 (platform parity), SP-148 (structured plans), SP-160
> (integration API: the headless agent a host drives), the `planner`
> persona.

## Problem

A host that runs Sprout's planner in a web page (the `planner` persona,
driven headless through the integration API) pays for the whole agent on
first load. Measured on the current tree (stripped, brotli -q 11,
headless Chromium, emulated network):

| Build | brotli | Cold load, 50 Mbps | Cold load, 10 Mbps | Warm |
|---|---|---|---|---|
| Today's `sprout.wasm` | 13.8 MB | 2.7 s | 11.5 s | 0.33 s |
| `pkg/agent` with only the planner's tools, no grammars, no mermaid | 6.3 MB | 1.2 s | 5.3 s | 0.18 s |
| `seed/core` + an OpenAI-compatible client + 4 tools | 2.5 MB | 0.48 s | 2.1 s | 0.06 s |
| `seed/core` floor (provider over JS `fetch`) | 1.2 MB | 0.25 s | 1.1 s | 0.03 s |

A planner conversation should show its first words in under a second. On
a phone on a slow connection, only the seed-based builds get there.

The trimmed `pkg/agent` build cannot shrink much further. `pkg/agent`
imports about 45 Sprout packages (mcp, automate, codereview, git, verify,
search, starters, skills, deploy, computer_use, …), and even with the
planner's tools alone the build keeps 22.5 MB of code plus about 10 MB of
runtime metadata that grows with it. An import-graph refactor of
`pkg/agent` might reach 4–5 MB; it does not reach 2–3 MB.

The planner's own tools are light. Their handlers import little: `ask_user`
(credentials), `read_file` / `list_directory` (filesystem, events),
`edit_file` (errors, filesystem), `write_plan` (plancontract, planstore),
`design_render` (errors, filesystem), `analyze_image_content` (the vision
processor). What makes them heavy is where they live:

- **One package.** Every handler is in `pkg/agent_tools`, which imports 30
  Sprout packages directly (49 transitively): ast, codegraph, automate,
  deploy, git, pythonruntime, webcontent, factory, agent_providers, and so
  on. Importing one tool imports all of them.
- **One flat list.** `AllTools()` (`pkg/agent_tools/all.go`) names every
  handler, so every `Execute` stays linked. Only the `all_*_wasm.go` /
  `*_js.go` stubs remove anything today.
- **A wide environment.** `ToolEnv` (`pkg/agent_tools/handler.go`) carries
  `*configuration.Manager`, `*events.EventBus`, `*TodoManager`,
  `*VisionProcessor`, `WebBrowser`, `SkillLoader`, the search engine and
  more. A tool that reads one file still links everything those types
  reach.

## Goal

A planner WASM build on `seed/core` of **2–3 MB brotli, 0.3–0.5 s cold at
50 Mbps**, behaving the same as the `planner` persona on the full agent:
the same prompt, tools, confinement and events. To get there, Sprout's
tools are reorganised so any tool can be built on its own. That also
benefits every other small build (the CLI's startup, future embedded
agents).

## Design

### 173a. A leaf tool kit

A new package, `pkg/toolkit`, holds what a tool needs to exist and nothing
else: `ToolDefinition`, `ParameterDef`, `ToolResult`, the argument
helpers (`extractString…`), the error kinds, and the handler interface. It
imports only the standard library, `pkg/errors` and `seed/core`.
`pkg/agent_tools` re-exports these (type aliases) so no caller changes in
the first step.

### 173b. A narrow environment

`ToolEnv` stays as the full environment for the full agent, but a handler
declares what it needs through small interfaces in `pkg/toolkit`
(`Workspace` for path resolution and confinement, `Events` for publishing,
`Asker` for questions, `Vision` for image analysis, `Renderer` for
rendering a page). `ToolEnv` implements them all; a small build supplies
only the ones its tools use. A handler that needs `*configuration.Manager`
says so and cannot join a small build until it stops needing it.

### 173c. Tools in their own packages

The planner's tools move first, each into a package that imports only
`pkg/toolkit` and what the tool itself needs:

| Tool | New home | Notes |
|---|---|---|
| `ask_user` | `pkg/tools/ask` | Drop the `credentials` import (only used for masking a secret answer; move that to an option) |
| `read_file`, `list_directory`, `edit_file`, `write_file` | `pkg/tools/files` | Over `pkg/filesystem`; confinement through `toolkit.Workspace` |
| `write_plan` | `pkg/tools/plan` | `plancontract` + `planstore` are already light |
| `analyze_image_content` | `pkg/tools/vision` | Over `toolkit.Vision`; the processor is supplied by the host build |
| `design_render` | `pkg/tools/render` | In the browser build the host page renders (as today) |
| `draw_mockup` | `pkg/tools/mockup` | Its model call goes through `toolkit` to the agent's provider |

`pkg/agent_tools` keeps registering them in `AllTools()`, so the full agent
is unchanged. Other tools move later on the same pattern; nothing requires
moving them all.

### 173d. A provider over `fetch`

`seed/core` needs a `Provider`. Under js/wasm, a provider that calls an
OpenAI-compatible endpoint through the browser's `fetch` (via
`syscall/js`) avoids linking `net/http`, TLS and x509 (about 2.3 MB of
code). It streams server-sent events and parses text, reasoning and tool
calls. The host supplies the endpoint and the model's capabilities, so the
build makes no model-listing or backend-detection requests (the full
agent's per-turn metadata requests cost about 0.9 s per turn when each
answers in 100 ms).

### 173e. The planner build

`cmd/planner-wasm`: a `seed/core` agent with the planner prompt, the
173c tools, the 173d provider, and the integration API's headless bridge
(the same events and host-tool calls the full agent's headless mode
offers, so a host switches builds without changing its code). Persona
confinement (allowed tools, writes only under `design/`, `browse_url`
reading only) moves to `pkg/toolkit` so both builds enforce the same
rules.

`browse_url` is not in this build: there is no browser in a browser
build. The host offers page fetching as a host tool, run in a sandbox.

### 173f. Holding the size

A CI check builds `cmd/planner-wasm` and fails if its brotli size grows
past a budget (start at the first measured size plus 10%). A dependency
check fails if any package under `pkg/tools/` or `pkg/toolkit` imports
`pkg/agent`, `pkg/agent_tools`, `pkg/configuration` or `net/http`.

## Ship-anywhere fixes (independent of this spec)

Found while measuring; each helps today's full build:

- `pkg/ast/grammars_embed.go` embeds 19 grammar blobs, though its comment
  names 5; `swift.bin` alone is 5.0 MB. Embedding the 5 saves 4.2 MB
  brotli.
- Serve the WASM brotli-compressed: 23% smaller than gzip, about 0.4 s
  faster cold at 50 Mbps.
- Under js/wasm, skip or cache (negative results included) the backend
  detection and model-list requests made before every turn
  (`pkg/agent_providers/backend_detect.go`, `backend_context.go`,
  `generic_provider_models.go`, `pkg/modelcontract/deepinfra.go`,
  `pkg/providerregistry/registry_providers.go`).

## Order and size

1. 173a and 173b: the kit and the narrow environment, with aliases. Small;
   no behaviour change.
2. 173c for the planner's tools, one package at a time, each with its
   tests moved alongside. Medium.
3. 173d: the `fetch` provider, tested against a stub endpoint under Node.
   Small to medium.
4. 173e: the build, its headless bridge, confinement in the kit. Medium.
5. 173f: the size and dependency gates.

Steps 1–2 are useful on their own and can land before anyone needs the
small build.

## Acceptance

- `cmd/planner-wasm` builds at 3 MB brotli or less.
- Cold load at 50 Mbps under 0.5 s, warm under 0.1 s, measured with the
  same harness as the table above.
- A planner turn on the small build and on the full agent's headless mode
  gives the same events for the same scripted model replies (a shared
  conformance test).
- The full agent's behaviour and test suite are unchanged.

## Non-goals

- Moving every tool out of `pkg/agent_tools`.
- A small build of anything but the planner.
- TinyGo or `wasm-opt` (neither tried yet; worth a measurement once 173e
  exists).

## Open questions

- Does `analyze_image_content` need a separate vision call in the small
  build, or does it attach the image to the primary model when the model
  takes images (as `PrimaryAcceptsImages` already allows)? Attaching avoids
  a second call and code.
- Should the full agent's headless mode and `cmd/planner-wasm` share one
  bridge package, so the event contract has one implementation?
