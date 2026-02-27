---
name: review-workflow
description: Evidence-first review process to reduce false positives and prioritize high-signal issues.
---

# Review Workflow

Use this skill for deep pass code review.

## Steps

1. Classify changed files by risk (security, correctness, data, concurrency).
2. Read enough context to confirm findings before reporting.
3. Report only evidence-backed issues.
4. Split findings into MUST_FIX vs VERIFY.

## Required Output

- Risk classification per changed area
- MUST_FIX findings with evidence
- VERIFY findings that need additional confirmation
- Overall verdict: approved / needs_revision / rejected
