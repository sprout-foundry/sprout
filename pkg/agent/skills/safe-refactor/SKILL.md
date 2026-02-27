---
name: safe-refactor
description: Behavior-preserving refactor workflow using small steps and verification gates.
---

# Safe Refactor Workflow

Use this skill for maintainability improvements without behavior changes.

## Steps

1. Define invariants that must not change.
2. Split refactor into small checkpoints.
3. Compile and run focused tests at each checkpoint.
4. Stop and correct immediately if behavior drifts.

## Required Output

- Refactor checkpoints completed
- Invariants preserved
- Verification commands and results
- Remaining risks
