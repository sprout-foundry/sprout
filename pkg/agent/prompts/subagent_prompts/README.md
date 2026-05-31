# Subagent Personas - System Prompts

This directory contains the system prompts for each specialized subagent persona.

## Available Personas

1. **[Coder](coder.md)** - Implementation and feature development
2. **[Refactor](refactor.md)** - Behavior-preserving refactoring and risk reduction
3. **[Tester](tester.md)** - Unit test writing and test coverage
4. **[QA_Engineer](qa_engineer.md)** - Quality assurance, test planning, integration testing
5. **[Code_Reviewer](code_reviewer.md)** - Code review, security, and best practices
6. **[Debugger](debugger.md)** - Bug investigation, root cause analysis, and fixes
7. **[Researcher](researcher.md)** - Local codebase analysis combined with web research (hybrid)
8. **[Web_Researcher](web_researcher.md)** - Web-only documentation lookup and API research
9. **[Web_Scraper](web_scraper.md)** - Web scraping and content extraction
10. **[Executive_Assistant](executive_assistant.md)** - Cross-project coordination, task queue management, delegation
11. **[Computer_User](computer_user.md)** - Desktop GUI interaction and screenshot-driven automation
12. **[General](general.md)** - General-purpose tasks that don't fit specialized categories

## Quick Reference

| Persona | Best For | Model Suggestion | Primary Tool |
|---------|----------|-------------------|--------------|
| Coder | Writing production code | ai-worker, qwen-coder | read_file, write_file, edit_file |
| Refactor | Low-risk code cleanup | ai-worker, qwen-coder | read_file, edit_file, search_files |
| Tester | Writing unit tests | ai-worker, qwen-coder | read_file, write_file, edit_file |
| QA_Engineer | Test planning, integration tests | deepseek-chat, claude | read_file, write_file, edit_file, web_search |
| Code_Reviewer | Security, code quality | claude, deepseek-chat | read_file, search_files |
| Debugger | Bug fixing, root cause | ai-worker, claude | read_file, write_file, edit_file, search_files, shell_command |
| Researcher | Local + web research | claude, deepseek-chat | read_file, search_files, web_search |
| Web_Researcher | Web-only research | claude, deepseek-chat | web_search, read_file |
| Web_Scraper | Scraping web content | ai-worker | web_search, fetch_url |
| Executive_Assistant | Task queue, delegation | claude, deepseek-chat | run_subagent, task_queue_* |
| Computer_User | GUI automation | claude, gemini-pro-vision | browse_url, analyze_image |
| General | Anything not specialized | any | all |

## Usage

These prompts are loaded automatically when a subagent is spawned with a specific persona. The system prompt is combined with the task-specific instructions to guide the subagent's behavior.

## Persona Selection

When delegating tasks to subagents, choose the persona that best matches the task:

- **Implement a feature** → `Coder`
- **Refactor with minimal risk** → `Refactor`
- **Write tests for code** → `Tester`
- **Create test plan for workflow** → `QA_Engineer`
- **Review PR for security** → `Code_Reviewer`
- **Fix a bug** → `Debugger`
- **Investigate codebase + find best practices** → `Researcher`
- **Look up external docs / APIs** → `Web_Researcher`
- **Scrape web content** → `Web_Scraper`
- **Manage task queue / cross-project** → `Executive_Assistant`
- **Interact with desktop GUI** → `Computer_User`
- **General-purpose task** → `General`

For complex workflows, use multiple personas in sequence or parallel as appropriate.

## See Also

- [Persona System](../../../docs/PERSONAS.md) - Full persona architecture, risk model, and custom persona guide
