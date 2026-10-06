# SP-156 — Deploy Targets and Ship Mode

> **Status (2026-10-03):** Proposed.
> Depends on SP-147 (Design · Code · Ship stages), SP-153 (build output),
> SP-149 (verification gate when enabled), SP-155 (mode registry).

## Problem

SP-147 names Ship as the third stage of a project, but it does not
exist: `webui/src/workspaces/registry.ts` registers only Code and Design,
and there is no deploy tool, command or skill in this repo. Developers
deploy by hand outside sprout, so the agent cannot check that what it
built is what went live, and deploy history is invisible to the
conversation.

## Design

### 156a. Deploy target interface

A Go interface for deploy targets:

- `Deploy(build) → deployment`, `Status`, `List`, `Rollback(deployment)`,
  and `PreviewURL` where the target supports per-deployment previews.
- Configuration per project (`.sprout/deploy.json`: target, project
  name, build output from the starter manifest).
- Adapters are small packages; others can be contributed.

### 156a-1. First adapter: Cloudflare

The first adapter targets Cloudflare (Pages for static output, Workers
where the starter needs server-side code), deploying into the user's own
Cloudflare account with their own API token (156b). It matches the
reference starters' build output (SP-153b), including starter 3's
Cloudflare storage bindings.

### 156a-2. Build and upload

Builds run in the workspace, using the starter manifest's `build`
command, after SP-149 verification has passed on the same tree. The
adapter uploads that built output; it never rebuilds on the target. What
was verified is what ships.

### 156a-3. Preview vs production

- **Preview deploys** (per-deployment preview URLs) may run automatically
  once verification passes.
- **Production deploys** always require explicit user confirmation, from
  the CLI prompt, the Ship mode action, or a confirmed tool approval. The
  agent cannot promote to production on its own.

### 156b. Credentials

Tokens live in the existing credential store (keyring or encrypted file)
or are supplied by the embedding environment. They are never placed in
model context, tool arguments or logs; adapters run in the runtime.

### 156c. CLI and tools

- `sprout deploy`, `sprout deploy status`, `sprout deploy history`,
  `sprout deploy rollback <id>`.
- Agent tools `deploy_status` and `deploy`. When SP-149 is enabled,
  `deploy` refuses unless the latest verification passed. A production
  `deploy` always goes through user confirmation (156a-3).
- Deploy outcomes emit progress events (SP-151).

### 156d. Ship mode

A Ship mode registered through the SP-155 mode registry:

- Status: live URL, which version is live, last deploy.
- Deploy action.
- History with per-deploy summaries (plan revision and change summaries,
  SP-157), each linkable.
- Roll back.

Per SP-147, Ship is a lens on the same conversation: it advertises the
deploy tools and instructions, not a new chat.

## Acceptance criteria

- [ ] Interface + fake adapter: deploy, status, history, rollback round
      trip in tests.
- [ ] Deploy blocked when SP-149 is enabled and failing (test).
- [ ] No credential appears in model requests or logs (test).
- [ ] Cloudflare adapter deploys each reference starter end to end
      (preview, then confirmed production).
- [ ] Production deploy without user confirmation is refused (test).
- [ ] The uploaded artifact is the one built after verification passed
      (test: changed tree after verification → deploy refused).
- [ ] Ship mode appears in the switcher; Code/Design unaffected.

## Non-goals

- No hosting of applications by sprout itself.
- No domain or DNS management.

## Open questions

- Resolved: first adapter is Cloudflare, user's own account (156a-1).
- Resolved: previews may auto-deploy after verification; production
  always needs confirmation (156a-3).
- Pages vs Workers selection rule per starter (from the manifest).
