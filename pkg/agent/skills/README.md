# Skills

Skills are instruction bundles that can be loaded into agent context to provide domain expertise.

## Available Skills

| Skill | When to Use |
|-------|-------------|
| `go-conventions` | Writing/reviewing Go code |
| `python-conventions` | Writing/reviewing Python code |
| `typescript-conventions` | Writing/reviewing TypeScript/JavaScript |
| `rust-conventions` | Writing/reviewing Rust code |
| `test-writing` | Creating tests |
| `commit-msg` | Writing commit messages |
| `repo-onboarding` | Mapping new project structure |
| `bug-triage` | Debugging workflow |
| `safe-refactor` | Refactoring with low risk |
| `review-workflow` | Code review process |

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
- Project-specific patterns
- Tool preferences (formatters, linters)
- Key decisions not in general docs
- Common pitfalls in this codebase
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

## Database
- Use sqlx with compile-time checked queries
- Migrations in `migrations/` directory

## Testing
- Integration tests use testcontainers
- Run with `cargo test-all` (includes db tests)

## Common Mistakes
- Don't use `unwrap()` in handlers - use `?`
- Don't skip auth middleware even on public routes
```

### Registering the Skill

Add to your `~/.ledit/config.json`:

```json
{
  "Skills": {
    "myproject-conventions": {
      "id": "myproject-conventions",
      "name": "MyProject Conventions",
      "description": "Conventions specific to the myproject codebase",
      "path": "pkg/agent/skills/myproject-conventions",
      "enabled": true
    }
  }
}
```

Or place in project's `.ledit/skills/` directory (project-specific skills).

## Tips

1. **Keep skills small** - 1-2KB max. Focus on what's NOT in training data.
2. **Be specific** - Project-specific patterns > generic best practices
3. **Show examples** - Code snippets > explanations
4. **List tools** - Which formatters, linters, test runners to use
