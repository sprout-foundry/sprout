# SP-148 — Structured Plans

> **Status (2026-10-07):** Shipped.
> Consumed by SP-149 (verified done), SP-151 (progress events),
> SP-153 (starters), SP-154 (agent benchmark).

## Problem

`sprout plan` (`cmd/plan.go`, `pkg/agent/prompts/planning_prompt.md`)
produces a free-form markdown plan (`plan.md` by default). It is useful to
read but nothing else can use it:

- No machine-readable acceptance criteria, so nothing can check whether
  the work it describes is finished.
- No link between plan items and the agent's progress; the todo list
  (`todo_update` events, `pkg/events/events_types.go`) is rebuilt from
  scratch each session.
- No record of what was deliberately left out, so scope creeps silently
  between sessions.
- No stable reference to a chosen design (SP-140 `design/` tree), so
  design and implementation drift apart.

## Design

### 148a. The plan file

A versioned plan stored in the project, so it travels with the code and
lands in git history:

- Path: `.sprout/plan.json` (authoritative) plus a rendered
  `.sprout/plan.md` for reading and review. The markdown is regenerated
  from the JSON on every write.
- Fields:
  - `version` (schema version), `revision` (increments on every edit),
    `created`, `updated`.
  - `goal`: what the work is for, in one or two sentences.
  - `scope[]`: features or changes, each `{id, title, description}`.
  - `steps[]`: ordered implementation steps referencing scope IDs.
  - `design`: optional reference to screens or files under `design/`
    (SP-140 directory contract).
  - `starter`: optional starter ID when the project uses one (SP-153).
  - `acceptance[]`: `{id, scope, check, kind}` where `kind` is
    `build` | `test` | `page` (a route renders) | `interaction`
    (scripted browser steps) | `manual` (reported, not checked).
  - `out_of_scope[]`: items discussed and deliberately excluded, with the
    reason.
- A Go package (e.g. `pkg/plancontract`) owns the schema and validator,
  used by every reader and writer.

### 148b. Writing plans

- `sprout plan` gains `--structured` to emit `.sprout/plan.json`
  alongside the markdown; the planning prompt gets a section describing
  the schema and the requirement that every scope item has at least one
  acceptance item.
- The plan can be edited by hand or by the agent; every write validates
  and bumps `revision`.

### 148c. Executing plans

- When `.sprout/plan.json` exists, the agent reads it at the start of a
  turn and works scope item by scope item.
- Todo items carry plan scope IDs, so progress maps back to the plan and
  survives across sessions.
- Changes to scope during execution are written back to the plan (new
  revision) rather than silently diverging.

### 148d. Interaction steps

Acceptance items of kind `interaction` reuse the browse tool's step
format (`parseBrowseSteps` in `pkg/agent/tool_handlers_browse.go`,
`webcontent.BrowseStep`) so there is one step language for browser
automation.

## Acceptance criteria

- [ ] Schema and validator with fixtures: a valid plan passes; duplicate
      IDs, an acceptance item without a kind, and a scope item with no
      acceptance item each fail with a clear message.
- [ ] `sprout plan --structured` writes a valid `.sprout/plan.json` and
      the rendered markdown.
- [ ] A scripted run reads a fixture plan and emits todo items carrying
      scope IDs.
- [ ] Editing the plan bumps `revision`; the markdown view regenerates.
- [ ] `make vet && make lint && make build-all` clean.

## Non-goals

- No plan editor UI beyond the existing file views.
- One active plan per project; history comes from git.

## Open questions

- Whether `.sprout/plan.json` should be committed by default or opt-in
  via `.gitignore` guidance.
- Whether the free-form `plan.md` output of `sprout plan` becomes the
  rendered view by default once `--structured` is stable.
