---
name: release-preflight
description: Pre-release validation checklist with go/no-go recommendation.
---

# Release Preflight Workflow

Use this skill before shipping or cutting a release.

## Steps

1. Run build and core test suite.
2. Run static checks (lint/vet/type checks as applicable).
3. Verify versioning/changelog and release notes inputs.
4. Call out blockers and rollback considerations.

## Required Output

- Checks run and outcomes
- Blocking issues (if any)
- Residual risk summary
- Clear go/no-go recommendation
