---
name: Fixture Starter
description: Test-only stack skill for the "fixture" starter (SP-153 §153c fixture): the fixture app's conventions and the commands declared in .sprout/starter.json.
---

# Fixture Starter Stack

Test-only skill backing the SP-153 §153c auto-activation test: the starter
manifest's `starter.id` ("fixture") has a skill to activate. Keep it small —
its job is to prove the mechanism, not to teach a real stack.

- Commands come from `.sprout/starter.json` — never guess them.
- Keep one test per feature; run the manifest's `test` command after changes.

## Upgrade note

No upgrades yet. When the starter version bumps, an upgrade note lands here
and the agent may propose — but never silently apply — the upgrade
(SP-153 §153d).
