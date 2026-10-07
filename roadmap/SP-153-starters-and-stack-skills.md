# SP-153 — Starters and Stack Skills

> **Status (2026-10-07):** Partially shipped — manifest, store, `sprout new --starter` and stack-skill auto-activation ship; the three reference starters, their stack skills and their CI job remain (framework and local storage-emulation choices pending).
> Consumed by SP-149 (commands), SP-154 (benchmark), SP-155 (preview
> command), SP-156 (build output for deploys).

## Problem

New projects start from nothing and the agent picks a stack ad hoc,
differently each time:

- There are no application starters. The only scaffold is the design tree
  (`pkg/design/scaffold.go`).
- "New project" in the web UI creates an empty repository
  (`webui/src/components/layered/NewProjectDialog.tsx`).
- Skills exist as a mechanism (`pkg/skills/library/`, `registry.json`;
  e.g. `project-planning`, `design-system`) but none teaches a specific
  application stack.
- There is no standard place where a project declares how to build, test
  and run it, so tools that need those commands (verification, preview,
  deploy) have to guess.

## Design

### 153a. Starter manifest

`.sprout/starter.json` declares how a project is built and run:

- `starter` (ID and version), `build`, `test`, `dev`, `preview` commands,
  the dev server port, the routes to check, and the build output
  directory.
- Schema and validator in Go. Any project can add the file by hand; it is
  not limited to projects created from a starter.
- The single source of commands for SP-149 verification, SP-155 preview
  and SP-156 deploys.

### 153b. Reference starters

Three starters ship together in this repo, embedded and versioned:

1. **Static site:** pages, content, forms posting to an external endpoint.
2. **Web app:** client-side app with routing and state.
3. **Web app with data:** the web app plus a data layer.

The reference deploy target is Cloudflare (Pages/Workers, SP-156), so
each starter's build output and manifest fit it. Starter 3's data layer
uses Cloudflare's own storage (D1 for relational data, KV for simple
key-value state, R2 for files) as each need arises, with a local
emulation for development and tests.

Each starter is a minimal working app (renders, has one test) with
quality defaults baked in: formatter and linter config, test runner,
plain README, the starter manifest, and the design tree scaffold so
design mode works from the first turn. `sprout new --starter <id>` and
the web UI's new-project dialog can instantiate one.

### 153c. Stack skills

One skill per starter under `pkg/skills/library/<starter>/`:

- Layout conventions and where things go.
- Patterns to use and avoid on this stack.
- How to add a page, a feature and a test.
- Known failure modes for open models on this stack (fed by SP-154).

The skill activates automatically when `.sprout/starter.json` names the
starter.

### 153d. Upgrades

A starter version bump ships with an upgrade note in its skill. The agent
proposes upgrades for existing projects; it never applies them silently.

## Acceptance criteria

- [ ] Each starter instantiates into an empty directory, builds, passes
      its own test, and serves its routes (CI job).
- [ ] Manifest schema + validator; SP-149 reads commands only from it.
- [ ] Skill auto-activation test when the manifest exists.
- [ ] `sprout new --starter` and the web UI dialog both instantiate.

## Non-goals

- No starter marketplace or third-party starter registry.
- No restriction on stacks: projects without a starter work as today.

## Open questions

- Resolved: three starters ship together (153b).
- Resolved (2026-10-07): static site = Astro; web app = React + Vite +
  React Router; web app with data = the web app plus a Hono API on
  Cloudflare Workers with D1 through Drizzle.
- Resolved (2026-10-07): local Cloudflare storage uses Wrangler/Miniflare
  (local D1/KV/R2 state, tests through `@cloudflare/vitest-pool-workers`).
