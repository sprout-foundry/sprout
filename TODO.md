# TODO

Active work tracked here. Each item is scoped for a single agent run
via the workflow automation (~1-4 hours of focused work). Only items that are
approved and ready to assign are listed.

---

## SP-136: Daemon-First Architecture — Shared Process Model

Status: ✅ All 5 phases shipped 2026-08-07 (P0 file-lock index writes,
P1 multi-workspace hardening, P2 lazy daemon auto-start, P3 embedding
service via Unix socket, P4 full CLI-on-daemon). See `git log --grep=SP-136`
for the per-phase commits; spec body deleted from `roadmap/` per index
policy.

The daemon deduplicates model loads, inference permits, and index writers
across CLI processes; the CLI routes embedding + one-shot agent work through
it with in-process fallback and `SPROUT_DAEMON=0` / `SPROUT_DAEMON_AGENT=0`
escape hatches. Follow-on workstream (open): `roadmap/SP-136-cross-platform-local-llm.md`.
