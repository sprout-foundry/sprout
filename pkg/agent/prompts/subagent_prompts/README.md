# Subagent Personas - System Prompts

This directory contains the system prompts for each specialized subagent persona.

## Available Personas

1. **[Coder](coder.md)** - Implementation and feature development
2. **[Refactor](refactor.md)** - Behavior-preserving refactoring and risk reduction
3. **[Tester](tester.md)** - Unit test writing and test coverage
4. **[QA_Engineer](qa_engineer.md)** - Quality assurance, test planning, integration testing
5. **[Code_Reviewer](code_reviewer.md)** - Code review, security, and best practices
6. **[Debugger](debugger.md)** - Bug investigation, root cause analysis, and fixes
7. **[Web_Researcher](web_researcher.md)** - Documentation lookup, API research, solution discovery

## Quick Reference

| Persona | Best For | Model Suggestion | Primary Tool |
|---------|----------|-------------------|--------------|
| Coder | Writing production code | ai-worker, qwen-coder | read, write, edit |
| Refactor | Low-risk code cleanup | ai-worker, qwen-coder | read, edit, search_files |
| Tester | Writing unit tests | ai-worker, qwen-coder | read, write, edit |
| QA_Engineer | Test planning, integration tests | deepseek-chat, claude | read, write, edit, web_search |
| Code_Reviewer | Security, code quality | claude, deepseek-chat | read, search_files |
| Debugger | Bug fixing, root cause | ai-worker, claude | read, write, edit, search_files, execute_command |
| Web_Researcher | Documentation, APIs, solutions | claude, deepseek-chat | web_search, read |

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
- **Research how to implement X** → `Web_Researcher`

For complex workflows, use multiple personas in sequence or parallel as appropriate.

## See Also

- [Subagent Personas Documentation](../../../docs/subagent_personas.md) - Detailed persona descriptions
- [Subagent Configuration Plan](../../../subagent_plan.md) - Implementation roadmap
