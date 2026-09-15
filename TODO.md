# TODO

Active work tracked here. Each item is scoped for a single agent run
via the workflow automation (~1-4 hours of focused work). Only items that are
approved and ready to assign are listed.

---

No active items as of 2026-09-15. The bg-sessions inactivity-expiry item
(quiet watchers killed by the 2h LastPolled expiry) shipped in
`bg-sessions: activity-based expiry`: running sessions are no longer reaped
for being unpolled — cleanup probes pid liveness at TTL boundaries, renews
alive sessions, reaps only dead ones; per-session `TTL` on StartOptions
(agent shell sessions default 8h) and `sprout shell-bg keepalive ID` provide
the explicit escape hatches. Covered by background_expiry_test.go and
TestShellBgKeepalive_*.

---
