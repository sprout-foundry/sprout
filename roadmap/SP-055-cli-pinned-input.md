# SP-055: CLI Pinned Input — Always-On Steering Panel

**Status:** ✅ Shipped — Phases 1/2/3 + 3b (done-queue mode) + 3c (UTF-8) + OPOST fix.
**Date:** 2026-05-24
**Depends on:** SP-048 (CLI Delight — established the status footer pattern this spec borrows from), `pkg/agent/seed_integration.go` steer-bridge (already landed; makes the agent-side mechanism functional), seed v0.x `InjectInput` API (already integrated)
**Priority:** Medium-High — closes the largest remaining CLI ergonomics gap. Users on the CLI today have to wait for a turn to finish before they can redirect the agent; webui users got mid-turn steering via the floating input box. Until this lands the CLI feels "fire and forget" while the webui feels live.
**Effort Estimate:** ~3–5 days end-to-end, split into 3 layered phases

Full specification archived. See git history for original content.
