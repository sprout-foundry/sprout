# TODO

Active work for the backend-contract lane (branch `feat/backend-contract`).
Each item is a small, independently committable unit for the workflow
automation (~30 min – 2 h) and cites its spec section — read
`roadmap/SP-160-integration-api.md` §160c before starting. Completed work
lives in git history; finished sections are removed.

Validation gate for every item: `make vet && make fmt-check && make lint &&
make lint-go-new && make build-all`, plus bounded tests for the packages the
item touched (`go test -p 1 -parallel 2 ./<pkg>/...`, never a bare
`go test ./...`). Webui items additionally: `cd webui && npx prettier --check`
on changed files and only the specific vitest files the item touched.

**This machine runs other automation in parallel; keep tests light.** Never
run the full Go or vitest suites, never start browsers or containers, and
run one test command at a time.

Items are ordered so nothing precedes what it depends on. Work that needs a
human decision is listed under **Not automatable** at the end, without
checkboxes.

---

## SP-160 §160c — Backend contract (`roadmap/SP-160-integration-api.md`)

- [x] **contract.1** Route inventory: a small generator (e.g.
      `cmd/api_inventory`) lists every route registered in
      `pkg/webui/routes.go` (method, path, handler) and writes
      `docs/api/endpoints.md`, grouped by family, with a "served by" column
      (daemon / in-browser WASM agent / browser-local / host) taken from the
      web UI's routing (`webui/src/services/cloudAdapter.ts`,
      `webui/src/services/cloudEndpointRegistry/`). A Go test fails when the
      generated file is stale. Spec: SP-160 §160c.
- [x] **contract.2** OpenAPI skeleton: `docs/api/openapi.yaml` (OpenAPI
      3.1) with `info.version` as the contract version (start at `1.0.0`),
      one tag per family, shared components (errors, pagination). A Go test
      in `pkg/webui` fails if a registered route is neither documented in the
      OpenAPI file nor listed in `docs/api/undocumented.txt`; the allowlist
      may only shrink. Spec: SP-160 §160c.
- [ ] **contract.3** Document the conversation family: `/api/query*`,
      `/api/chat-sessions*`, `/api/sessions*`, `/api/subagent*`,
      `/api/edits*`, `/api/shell-approvals*`, `/api/completion`. Schemas
      come from the handlers' request/response types; remove these routes
      from `undocumented.txt`. Spec: SP-160 §160c.
- [ ] **contract.4** Document the files family: `/api/files`, `/api/file*`,
      `/api/search*`, `/api/create`, `/api/delete`, `/api/rename`,
      `/api/upload`, `/api/diagnostics`, `/api/lsp*`, `/api/semantic`.
      Spec: SP-160 §160c.
- [ ] **contract.5** Document the git family, read endpoints
      (`/api/git/*` status, log, diff, branches and other GETs).
      Spec: SP-160 §160c.
- [ ] **contract.6** Document the git family, write endpoints (commit,
      stage, branch create/switch, push/pull and other mutating routes).
      Spec: SP-160 §160c.
- [ ] **contract.7** Document settings and configuration: `/api/settings*`,
      `/api/providers*`, `/api/onboarding*`, `/api/config`, `/api/hotkeys*`,
      `/api/skills*`, `/api/local-llm*`, `/api/password`. Spec: SP-160 §160c.
- [ ] **contract.8** Document the remaining families: `/api/workspace*`,
      `/api/instances*`, `/api/terminal*`, `/api/txn*`, `/api/sync*`,
      `/api/command*`, `/api/proxy*`, `/api/stats`, `/api/ws-metrics`,
      `/api/support-bundle`, `/api/open-in-file-browser`,
      `/api/computer-use`, `/api/design`, `/api/starters*`. When done,
      `undocumented.txt` lists only routes deliberately kept internal, each
      with a one-line reason. Spec: SP-160 §160c.
- [ ] **contract.9** Event schema: `docs/api/events.schema.json` (JSON
      Schema) generated from the `@sprout/events` types
      (`packages/events/src/types.ts`) by a script in `packages/events`; a Go
      test marshals representative Go event payloads and validates them
      against the schema, so Go and TypeScript cannot drift.
      Spec: SP-160 §160c.
- [ ] **contract.10** Conformance suite package (`pkg/apiconformance`):
      given a base URL, it checks each documented endpoint that has a safe
      read-only probe (status code and response shape validated against
      `openapi.yaml`), and reports per family. Mutating endpoints are
      checked only against an explicitly disposable workspace. Pick a
      maintained, permissively licensed OpenAPI validator and record the
      choice in the package doc. Spec: SP-160 §160c.
- [ ] **contract.11** Run the conformance suite against the local daemon in
      a bounded Go test (in-process server on a temp workspace, no
      browsers, no containers), and fix or document every failure it
      finds. Spec: SP-160 §160c.
- [ ] **contract.12** Command for hosts: `sprout api conformance
      --base-url <url> [--families …]` runs the suite against any
      implementation and exits non-zero on failure; usage documented in
      `docs/api/README.md` (how a host runs it in CI). Spec: SP-160 §160c.
- [ ] **contract.13** Contract version negotiation: `/api/bootstrap`
      returns `contractVersion` (from `openapi.yaml`); the web UI refuses to
      start on an incompatible major version with a clear message, and
      warns on a newer minor. Tests on both sides. Spec: SP-160 §160c.

## Not automatable

- Decision: how the conformance suite runs against the in-browser WASM
  agent (Go `js/wasm` harness under Node, or a browser-driven run). Until
  decided, the suite covers the daemon and hosts only.
- Decision: contract version policy (package semver, a separate contract
  version, or both) — SP-160 open question; contract.13 starts with the
  OpenAPI `info.version`.
