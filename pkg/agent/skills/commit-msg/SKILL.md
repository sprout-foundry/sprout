---
name: commit-msg
description: Conventional commits format and best practices for writing clear commit messages.
---

# Commit Message Guidelines

Follow these conventions for clear, useful commit messages.

## Format

```
<type>(<scope>): <description>

[optional body]

[optional footer]
```

## Types

- `feat`: New feature
- `fix`: Bug fix
- `docs`: Documentation changes
- `style`: Code style (formatting, no logic change)
- `refactor`: Code refactoring
- `perf`: Performance improvement
- `test`: Adding/updating tests
- `chore`: Maintenance, dependencies, tooling
- `ci`: CI configuration changes

## Rules

1. **Use imperative mood**: "add feature" not "added feature"
2. **Lowercase type**: `feat:`, not `Feat:`
3. **No period at end** of subject line
4. **Max 50 chars** for subject line
5. **Max 72 chars** for each body line

## Examples

### Good

```
feat(auth): add JWT token refresh endpoint

Implements token refresh flow to allow users to obtain new
access tokens without re-authenticating.

Closes #123
```

```
fix(api): handle nil response from user service

Returns empty slice instead of nil when no users found.
```

### Bad

```
fix: fixed the thing
```

```
Added new feature for user authentication and authorization
with JWT tokens and refresh token support and role-based
access control which allows administrators to manage permissions.
```

## Body

- Explain **what** and **why**, not how
- Leave blank line between subject and body
- Wrap at 72 characters

## Footer

- Use `Closes #123` or `Fixes #456` to link issues
- Multiple issues: `Closes #123, #456`
- Breaking changes: `BREAKING CHANGE: description`

## Breaking Changes

```
feat(api)!: change user response format

The user response now includes additional fields. Clients
must be updated to handle the new format.

BREAKING CHANGE: /api/users now returns full User object
instead of just ID and name.
```

## Tips

- Make atomic commits (one change per commit)
- Commit early, commit often
- Review before committing
- Use `git add -p` for selective staging
