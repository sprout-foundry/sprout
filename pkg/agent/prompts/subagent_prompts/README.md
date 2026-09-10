# Subagent Personas - System Prompts

This directory contains the system prompts for each specialized subagent persona.

## Available Personas

1. **[Coder](coder.md)** - Implementation, feature development, debugging, refactoring (aliases: `refactor`, `debugger`)
2. **[Tester](tester.md)** - Unit test writing and test coverage
3. **[Reviewer](reviewer.md)** - Diff-focused code review: correctness, security, quality
4. **[Researcher](researcher.md)** - Local codebase analysis combined with web research (hybrid; alias: `web_scraper`)
5. **[Coordinator](coordinator.md)** - Cross-project coordination and delegation
6. **[General](general.md)** - General-purpose tasks that don't fit specialized categories

## Quick Reference

| Persona | Best For | Primary Tools |
|---------|----------|---------------|
| Coder | Writing production code, debugging, refactoring | read_file, write_file, edit_file, shell_command |
| Tester | Writing unit tests | read_file, write_file, edit_file |
| Reviewer | Diff review: security, correctness, quality | read_file, search, shell_command |
| Researcher | Local + web research, content extraction | read_file, search, web_search, fetch_url, browse_url |
| Coordinator | Cross-project coordination | run_subagent |
| General | Anything not specialized | all defaults |

## Usage

These prompts are loaded automatically when a subagent is spawned with a specific persona. The system prompt is combined with the task-specific instructions to guide the subagent's behavior.

## Persona Selection

- **Implement a feature / fix a bug / refactor** → `coder`
- **Write tests for code** → `tester`
- **Review a diff for real issues** → `reviewer`
- **Investigate codebase / web research / scrape content** → `researcher`
- **Coordinate cross-project work** → `coordinator`
- **Hands-on shell / sysadmin** → `coder` or `general` (use `shell_command` directly)
- **General-purpose task** → `general`

`refactor`, `debugger`, and `web_scraper` were consolidated into `coder` and `researcher` (2026-09); those IDs resolve as aliases.

For complex workflows, use multiple personas in sequence or parallel as appropriate.

## See Also

- [Persona System](../../../docs/PERSONAS.md) - Full persona architecture, risk model, and custom persona guide
