# Security Architecture

This document describes Sprout's security model, trust boundaries, and data handling practices.

## Trust Boundaries

> **See also:** [Persona System](PERSONAS.md) for the two-gate risk model and depth-based delegation that enforces trust boundaries at the subagent level.

### Local-First Execution

Sprout runs entirely on the user's machine. The daemon process executes under the installing user's UID with no root privileges. All file operations, command execution, and tool invocations happen within the user's security context.

### Network Trust

| Boundary | Default | Override |
|---|---|---|
| Web UI bind address | `127.0.0.1` (localhost only) | `--bind` flag / `SPROUT_BIND_ADDR` |
| Non-localhost auth | Required (`SPROUT_AUTH_TOKEN`) | Refuses to start without it |
| Provider API calls | Outbound TLS only | N/A |
| LLM provider | User-selected API endpoint | Configured per provider |

### Security Classification

Sprout uses a three-tier security classifier for tool execution (see `pkg/agent_tools/security_classifier.go`):

- **Safe**: Automatic execution (read operations, file creation in workspace)
- **Caution**: Prompts user with preview and reasoning
- **Dangerous**: Blocked by default (system-level operations, files outside workspace)

**Limitations**: The classifier uses heuristic pattern matching. It cannot guarantee perfect classification. Users should review caution-classified operations before approving.

## Data Handling

### Files on Disk

| Path | Contents | Sensitivity |
|---|---|---|
| `~/.sprout/config.json` | Provider settings, preferences | Low |
| `~/.sprout/api_keys.json` | API keys (encrypted at rest via OS keyring when available) | **High** |
| `~/.sprout/service.env` | Environment variables for daemon | **High** |
| `~/.sprout/logs/` | Daemon logs, rotated (10MB, 5 backups) | Medium |
| `~/.sprout/memories/` | Agent memory .md files | Medium |
| `.sprout/workspace.log` | Per-workspace run log | Medium |
| `.sprout/history/` | Change tracker revisions | Medium |
| `.sprout/embeddings/` | Conversation turn embeddings | Medium |

### Credential Redaction

All HTTP request payloads logged to disk are passed through `pkg/redact` which replaces recognised secret patterns (AWS keys, GitHub tokens, API keys, private keys, env-style secrets) with `[REDACTED:<kind>]` tokens. See the redaction package for the full list of patterns.

### Memory Files

Agent memory files (`.sprout/memories/*.md`) are redacted via `pkg/redact` before being written to disk.

## Clearing Persisted Data

### Run logs
```bash
rm -rf .sprout/workspace.log
```

### Embeddings
```bash
rm -rf .sprout/embeddings/
```

### Change history
```bash
rm -rf .sprout/history/
```

### Agent memories
```bash
rm -rf .sprout/memories/
```

### Full reset
```bash
# Per-workspace
rm -rf .sprout/

# Global config and keys
rm -rf ~/.sprout/
```

## Skill Allowlist

Project-local skills (`.sprout/skills/`) are discovered automatically. To control which skills are active:

1. Create `.sprout/allowed_skills` with one skill ID per line
2. Skills not in the allowlist are loaded as disabled
3. Use `--no-project-skills` flag to skip skill discovery entirely

## Authentication for Remote Access

When binding to a non-localhost address:

```bash
export SPROUT_AUTH_TOKEN="your-secret-token"
export SPROUT_BIND_ADDR="0.0.0.0"
sprout agent -d
```

All write endpoints require `Authorization: Bearer <token>` when auth is configured. Read-only endpoints (static assets, health) remain open.
