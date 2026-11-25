# Roadmap

## In Progress

| Spec | Title | Description |
|------|-------|-------------|
| SP-016 | Conformance Test Suite | Language-agnostic CLI-based test suite — 99/102 tests passing, 3 known core limitations |

## Completed

All 15 core specs are complete. See [docs/](../docs/) for current implementation details.

| Spec | Title | Docs |
|------|-------|------|
| SP-001 | Event System | [architecture](../docs/architecture.md) |
| SP-002 | Error Handling & Retry | [extensibility](../docs/extensibility.md) |
| SP-003 | Streaming & Output | [conversation-flow](../docs/conversation-flow.md) |
| SP-004 | Output Routing | [architecture](../docs/architecture.md) |
| SP-005 | Context Cancellation | [extensibility](../docs/extensibility.md) |
| SP-006 | Fallback Parsing | [conversation-flow](../docs/conversation-flow.md) |
| SP-007 | Response Validation | [conversation-flow](../docs/conversation-flow.md) |
| SP-008 | Conversation Optimizer | [compaction](../docs/compaction.md) |
| SP-009 | Configuration, Steering & Extensibility | [extensibility](../docs/extensibility.md) |
| SP-010 | Turn Checkpoints | [compaction](../docs/compaction.md) |
| SP-011 | Response Processing Hardening | [conversation-flow](../docs/conversation-flow.md) |
| SP-012 | Library Integrability | [extensibility](../docs/extensibility.md) |
| SP-013 | Tool Registry | [tool-registry](../docs/tool-registry.md) |
| SP-014 | Compaction Hardening | [compaction](../docs/compaction.md) |
| SP-015 | Checkpoint Hooks | [compaction](../docs/compaction.md) |

## Out of Scope

| Area | Reason |
|------|--------|
| Persistence / Sessions | `State.ExportState()` / `ImportState()` are exposed — the consumer handles file I/O |
| Security & Approval | Consumer implements `ToolExecutor` — approval gates belong inside their `Execute()` |
| Circuit Breaker | Consumer's executor controls repetition handling |
| Scripted Client | Test utility, not library functionality |
| Agent Lifecycle / Debug Logger | Logging is an integration concern (consumer wires `UI`) |

## Spec File Convention

Each spec follows this structure:

```
## Summary
One-paragraph description of what this spec accomplishes.

## Current State (if applicable)
What exists today relevant to this spec.

## Requirements
Numbered list of everything that must be true when this spec is done.

## Technical Design
Implementation details, data models, API contracts, code paths.

## Acceptance Criteria
Testable conditions that prove the spec is complete.

## Dependencies
Other specs that must be completed first.
```

## Pipeline

Process roadmap items via `process/roadmap_workflow.json`:

```bash
sprout agent --workflow-config process/roadmap_workflow.json
```
