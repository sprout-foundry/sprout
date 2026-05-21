# Skills

Skills are instruction bundles that can be loaded into agent context to provide domain expertise.

A good skill contains knowledge that models **cannot infer from training data** — tool-specific gotchas, project-specific conventions, or non-obvious workflow patterns.

## Available Skills

| Skill | When to Use |
|-------|-------------|
| `project-planning` | Starting a new project or onboarding to an existing repo |
| `browse-debugging` | Multi-step interactive browser debugging with `browse_url` |

## Creating Custom Skills

Skills live in `pkg/agent/skills/<skill-id>/SKILL.md`. Create your own for project-specific conventions.

### Structure

```markdown
---
name: my-skill
description: When and why to use this skill. Shown in list_skills output.
---

# Skill Title

Concise instructions for the agent. Focus on:
- Tool-specific behaviors not obvious from the schema
- Project-specific patterns that override general conventions
- Common pitfalls discovered through usage
```

### Example: Project-Specific Skill

```markdown
---
name: myproject-conventions
description: Conventions specific to the myproject codebase.
---

# MyProject Conventions

## API Patterns
- All endpoints return `Result<T, ApiError>`
- Use `Uuid` for all IDs, never integers
- Pagination via cursor, not offset

## Common Mistakes
- Don't use `unwrap()` in handlers - use `?`
- Don't skip auth middleware even on public routes
```

## Tips

1. **Keep skills small** — Focus on what's NOT in training data. If a model already knows it, it's not a skill.
2. **Be specific** — Project-specific patterns > generic best practices
3. **Tool gotchas** — Non-obvious tool behaviors (quirks, ordering requirements, side effects) are the highest-value content
