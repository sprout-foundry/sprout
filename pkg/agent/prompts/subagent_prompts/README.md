# Subagent Personas - System Prompts

This directory contains the system prompts for each specialized subagent persona.

## Available Personas

1. **[Coder](coder.md)** - Implementation, feature development, debugging, refactoring (aliases: `refactor`, `debugger`)
2. **[Tester](tester.md)** - Unit test writing and test coverage
3. **[Reviewer](reviewer.md)** - Diff-focused code review: correctness, security, quality
4. **[Researcher](researcher.md)** - Local codebase analysis combined with web research (hybrid; alias: `web_scraper`)
5. **[Designer](designer.md)** - The clickable mockup: `draw_mockup` composes `design/mockup.html` from the plan, and a change is the person's own sentence (aliases: `ux`, `design`)
6. **[Coordinator](coordinator.md)** - Cross-project coordination and delegation
7. **[Computer User](computer_user.md)** - Desktop automation: screenshots, mouse, and keyboard
8. **[General](general.md)** - General-purpose tasks that don't fit specialized categories
9. **[Worker](worker.md)** - Routine delegated subtasks on a cheaper model (read/search, summarise, mechanical edits, running tests)
10. **[Planner](planner.md)** - Conversation from an idea to an agreed plan and a clickable mockup, before anything is built (confined: writes only the plan and `design/mockup.html`)
11. **[Developer Planner](dev_planner.md)** - Reads the repository and hands a developer a task list in the same turn; no interview, no code changes (confined: writes only the plan)

## Quick Reference

| Persona | Best For | Primary Tools |
|---------|----------|---------------|
| Coder | Writing production code, debugging, refactoring | read_file, write_file, edit_file, shell_command |
| Tester | Writing unit tests | read_file, write_file, edit_file |
| Reviewer | Diff review: security, correctness, quality | read_file, search, shell_command |
| Researcher | Local + web research, content extraction | read_file, search, web_search, fetch_url, browse_url |
| Designer | Mockups and visual ideas: a streamed, kit-composed mockup from a prompt | read_file, edit_file, shell_command, draw_mockup |
| Coordinator | Cross-project coordination | run_subagent |
| Computer User | Desktop automation: mouse, keyboard, screenshots | take_screenshot, mouse_click, keyboard_type |
| General | Anything not specialized | all defaults |
| Worker | Routine subtasks on a cheaper model: read/search, summarise, mechanical edits, run tests | read_file, search, edit_file, shell_command |
| Planner | Idea → agreed plan → clickable mockup, for a host's builder screen | ask_user, write_plan, draw_mockup |
| Developer Planner | A goal or issue → a task list a coding agent can finish one at a time | repo_map, read_file, search, propose_tasks, write_plan, draw_mockup |

## Usage

These prompts are loaded automatically when a subagent is spawned with a specific persona. The system prompt is combined with the task-specific instructions to guide the subagent's behavior.

## Persona Selection

- **Implement a feature / fix a bug / refactor** → `coder`
- **Write tests for code** → `tester`
- **Review a diff for real issues** → call the `review_changes` tool (it runs `reviewer` subagents with pre-built context); spawn `reviewer` directly only for a custom review task
- **Draw or change the clickable mockup** → `designer` (aliases `ux`, `design`)
- **Investigate codebase / web research / scrape content** → `researcher`
- **Coordinate cross-project work** → `coordinator`
- **Drive the desktop (mouse, keyboard, screenshots)** → `computer_user`
- **Delegate a routine, well-scoped subtask on a cheaper model** (read/search, summarise, mechanical edits, run a test) → `worker`
- **Hands-on shell / sysadmin** → `coder` or `general` (use `shell_command` directly)
- **General-purpose task** → `general`
- **Turn an idea into an agreed plan and a mockup, in plain words** → `planner` (not delegatable: a host runs it as the conversation's persona)

`refactor`, `debugger`, and `web_scraper` were consolidated into `coder` and `researcher` (2026-09); those IDs resolve as aliases.

For complex workflows, use multiple personas in sequence or parallel as appropriate.

## See Also

- [Persona System](../../../docs/PERSONAS.md) - Full persona architecture, risk model, and custom persona guide
