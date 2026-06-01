# SP-006: Delegate Tool — In-Process Agent Delegation

**Status:** ⚰️ Superseded by [SP-059](SP-059-subagent-interaction.md)
**Date:** Original 2026-Q1 · Superseded 2026-05-31

The delegate tool shipped as a parallel implementation to `run_subagent`
with weaker operational tooling (no persona enforcement, weaker
ChangeTracker integration, no parallel execution, no fleet token
budget). SP-059 consolidates the two systems onto subagents and
removes the delegate tool entirely.

See git history for the original specification.
