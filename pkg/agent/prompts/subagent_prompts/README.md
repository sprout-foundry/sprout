# Subagent Personas - System Prompts

This directory contains the system prompts for each specialized subagent persona.

## Available Personas

1. **[Coder](coder.md)** - Implementation and feature development
2. **[Refactor](refactor.md)** - Behavior-preserving refactoring and risk reduction
3. **[Tester](tester.md)** - Unit test writing and test coverage
4. **[Reviewer](reviewer.md)** - Code review, security, and best practices
5. **[Debugger](debugger.md)** - Bug investigation, root cause analysis, and fixes
6. **[Researcher](researcher.md)** - Local codebase analysis combined with web research (hybrid)
7. **[Web_Scraper](web_scraper.md)** - Web scraping and content extraction
8. **[Coordinator](coordinator.md)** - Cross-project coordination, task queue management, delegation
9. **[General](general.md)** - General-purpose tasks that don't fit specialized categories

## Quick Reference

| Persona | Best For | Primary Tools |
|---------|----------|---------------|
| Coder | Writing production code | read_file, write_file, edit_file |
| Refactor | Low-risk code cleanup | read_file, edit_file, search_files |
| Tester | Writing unit tests | read_file, write_file, edit_file |
| Reviewer | Security, code quality | read_file, search_files |
| Debugger | Bug fixing, root cause | read_file, write_file, edit_file, search_files, shell_command |
| Researcher | Local + web research | read_file, search_files, web_search, fetch_url |
| Web_Scraper | Scraping web content | web_search, fetch_url, browse_url |
| Coordinator | Cross-project coordination | run_subagent, task_queue_* |
| General | Anything not specialized | all defaults |

## Usage

These prompts are loaded automatically when a subagent is spawned with a specific persona. The system prompt is combined with the task-specific instructions to guide the subagent's behavior.

## Persona Selection

When delegating tasks to subagents, choose the persona that best matches the task:

- **Implement a feature** → `coder`
- **Refactor with minimal risk** → `refactor`
- **Write tests for code** → `tester`
- **Review PR for security** → `reviewer`
- **Fix a bug** → `debugger`
- **Investigate codebase + find best practices** → `researcher`
- **Scrape web content** → `web_scraper`
- **Coordinate cross-project / manage task queue** → `coordinator`
- **Hands-on shell / sysadmin** → `coder` or `general` (use `shell_command` directly)
- **General-purpose task** → `general`

For complex workflows, use multiple personas in sequence or parallel as appropriate.

## See Also

- [Persona System](../../../docs/PERSONAS.md) - Full persona architecture, risk model, and custom persona guide
