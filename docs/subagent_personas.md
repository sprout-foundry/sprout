# Subagent Personas

This document defines the specialized subagent personas available in ledit. Each persona has a specific focus, custom system prompt, and optimal configuration.

## Overview

Subagent personas allow you to delegate tasks to specialized agents with expertise in specific areas. Instead of using a generic "subagent" for everything, you can choose the right persona for the job.

**Available Personas:**
- **Coder** - Implementation and feature development
- **Refactor** - Behavior-preserving refactoring and maintainability improvements
- **Tester** - Test case writing and test coverage
- **QA_Engineer** - Quality assurance, integration testing, test plans
- **Code_Reviewer** - Code review, analysis, and best practices verification
- **Debugger** - Bug investigation, error diagnosis, and fixes
- **Web_Researcher** - Web-only documentation lookup and API research
- **Researcher** - Local codebase analysis combined with web research (hybrid)

## Quick Reference

| Persona | Best For | Example Tasks |
|---------|----------|---------------|
| **Coder** | Writing production code | "Implement user authentication", "Create REST API endpoint" |
| **Refactor** | Low-risk code improvements | "Extract duplicated validation logic", "Split a large file without behavior changes" |
| **Tester** | Writing unit tests | "Write tests for user service", "Add test coverage for payment module" |
| **QA_Engineer** | Test planning and integration tests | "Create test plan for checkout flow", "Design integration tests" |
| **Code_Reviewer** | Security and quality review | "Review auth code for security issues", "Check for bugs in user service" |
| **Debugger** | Bug fixing and troubleshooting | "Fix null pointer exception", "Investigate API 500 errors" |
| **Web_Researcher** | Web-only research | "Look up React documentation", "Find how to configure CORS" |
| **Researcher** | Local + web research | "Investigate auth code AND find best practices", "Understand our caching and research optimal approaches" |

## Usage

### Using Personas with run_subagent

Personas are specified using the `persona` parameter with the `run_subagent` tool:

```json
{
  "tool": "run_subagent",
  "prompt": "Investigate why the API returns 500 errors when user ID is 0",
  "persona": "debugger"
}
```

### Configuration

Personas are configured in `.ledit/config.json` under the `subagent_types` field. Each persona can have:
- Custom provider (uses `subagent_provider` if not set)
- Custom model (uses `subagent_model` if not set)
- Enable/disable flag
- System prompt file path

## Persona Details

### 1. Coder

**Purpose:** Write implementation code and create features

**When to Use:**
- Implementing new features or functionality
- Writing production code
- Creating data structures and algorithms
- Adding API endpoints or handlers
- Database schema changes and migrations
- File creation and code organization

**Strengths:**
- Clean, idiomatic code implementation
- Following language best practices
- Proper error handling and edge cases
- Efficient algorithms and data structures
- Code organization and modularity

**Typical Tasks:**
- "Implement user authentication in auth.go"
- "Create a REST API endpoint for fetching user profiles"
- "Add a new service layer for payment processing"
- "Refactor the user model to include profile fields"

**Configuration Recommendations:**
- **Model:** High-quality coding model (e.g., qwen-coder, deepseek-coder)
- **Provider:** Fast provider for iterative development
- **Timeout:** Longer (3-5 minutes) for complex implementations

**System Prompt Focus:**
- Focus on working, production-ready code
- Emphasize clean code principles
- Prioritize correctness over cleverness
- Include proper error handling
- Write maintainable, readable code

---

### 2. Tester

**Purpose:** Write unit tests and test cases

**When to Use:**
- Writing unit tests for new code
- Adding test coverage for existing code
- Creating test fixtures and mocks
- Writing integration tests
- Test case design and organization

**Strengths:**
- Comprehensive test coverage
- Edge case identification
- Clear test names and organization
- Proper setup/teardown
- Meaningful assertions

**Typical Tasks:**
- "Write unit tests for the user service"
- "Add test cases for the payment validation function"
- "Create tests for the new API endpoint"
- "Generate tests for edge cases in the authentication flow"

**Configuration Recommendations:**
- **Model:** Coding model with testing knowledge
- **Provider:** Standard provider
- **Timeout:** Medium (2-3 minutes)

**System Prompt Focus:**
- Test coverage and completeness
- Clear, descriptive test names
- Proper test organization
- Edge cases and boundary conditions
- Maintainable test code

---

### 3. QA_Engineer

**Purpose:** Quality assurance, test planning, and integration testing

**When to Use:**
- Creating comprehensive test plans
- Integration and end-to-end testing
- Test strategy and coverage analysis
- Acceptance criteria verification
- Quality gates and checks

**Strengths:**
- Systematic test planning
- End-to-end scenario coverage
- User journey testing
- Risk-based testing
- Quality metrics and reporting

**Typical Tasks:**
- "Create a test plan for the checkout flow"
- "Design integration tests for the payment system"
- "Verify acceptance criteria for user registration"
- "Analyze test coverage gaps"
- "Create end-to-end tests for the authentication workflow"

**Configuration Recommendations:**
- **Model:** High-capability model for complex reasoning
- **Provider:** Reliable provider
- **Timeout:** Longer (3-5 minutes) for complex analysis

**System Prompt Focus:**
- Systematic test design
- User perspectives and scenarios
- Risk assessment and coverage
- Integration points and dependencies
- Quality standards and best practices

---

### 4. Code_Reviewer

**Purpose:** Review code for quality, security, and best practices

**When to Use:**
- Reviewing pull requests or code changes
- Checking for security vulnerabilities
- Verifying adherence to coding standards
- Identifying code smells and technical debt
- Suggesting improvements and refactoring

**Strengths:**
- Security vulnerability identification
- Code quality assessment
- Best practices verification
- Performance considerations
- Maintainability and readability analysis

**Typical Tasks:**
- "Review the authentication implementation for security issues"
- "Check the payment processing code for edge cases"
- "Analyze the new API for RESTful best practices"
- "Identify potential bugs in the user service"
- "Review database query efficiency"

**Configuration Recommendations:**
- **Model:** High-quality model with strong reasoning
- **Provider:** Standard provider
- **Timeout:** Medium (2-3 minutes)

**System Prompt Focus:**
- Security vulnerabilities (OWASP Top 10)
- Code quality and readability
- Performance and efficiency
- Error handling and edge cases
- Best practices and design patterns

---

### 5. Debugger

**Purpose:** Investigate and fix bugs, errors, and unexpected behavior

**When to Use:**
- Diagnosing error messages and stack traces
- Investigating unexpected behavior
- Fixing bugs and defects
- Performance troubleshooting
- Log analysis and root cause analysis

**Strengths:**
- Root cause analysis
- Error pattern recognition
- Systematic debugging methodology
- Log and trace analysis
- Hypothesis-driven investigation

**Typical Tasks:**
- "Fix the null pointer exception in user service"
- "Investigate why the API is returning 500 errors"
- "Debug the race condition in the payment processing"
- "Find why tests are failing intermittently"
- "Analyze the memory leak in the background worker"

**Configuration Recommendations:**
- **Model:** High-capability model for complex reasoning
- **Provider:** Fast provider for iterative debugging
- **Timeout:** Medium (2-3 minutes)

**System Prompt Focus:**
- Systematic problem investigation
- Root cause analysis over symptom treatment
- Reproducible test cases
- Log and trace interpretation
- Hypothesis formation and validation

---

### 6. Web_Researcher

**Purpose:** Research documentation, APIs, and solutions online

**When to Use:**
- Looking up library documentation
- Finding API references and examples
- Researching best practices and patterns
- Discovering solutions to technical problems
- Investigating error messages and issues

**Strengths:**
- Efficient web searching
- Documentation synthesis
- Pattern recognition across sources
- Solution evaluation and comparison
- Clear summary of findings

**Typical Tasks:**
- "Research how to implement JWT authentication in Go"
- "Find the best practices for PostgreSQL connection pooling"
- "Look up the documentation for the React useEffect hook"
- "Research solutions for CORS errors in Express.js"
- "Find examples of implementing WebSockets in Python"

**Configuration Recommendations:**
- **Model:** Model with strong web search capabilities
- **Provider:** Provider with web_search tool access
- **Timeout:** Medium (2-3 minutes)

**System Prompt Focus:**
- Efficient search queries
- Source evaluation and credibility
- Clear, actionable summaries
- Code examples and documentation links
- Comparison of alternative approaches

---

## Usage Patterns

### Single Subagent (Delegated Task)

Use when you have ONE focused task that matches a persona's expertise:

```json
{
  "subagent": "Coder",
  "task": "Implement user authentication in auth.go"
}
```

### Parallel Subagents (Multiple Independent Tasks)

Use when you have 2+ independent tasks that can benefit from different personas:

```json
[
  {"subagent": "Coder", "task": "Implement user authentication"},
  {"subagent": "Tester", "task": "Write tests for authentication"},
  {"subagent": "Code_Reviewer", "task": "Review authentication implementation"}
]
```

### Sequential Subagents (Dependent Tasks)

Use when tasks have dependencies or require handoff:

```
1. Web_Researcher: "Research best practices for JWT in Go"
2. Coder: "Implement JWT authentication using research findings"
3. Tester: "Write tests for JWT implementation"
4. Code_Reviewer: "Review JWT implementation for security"
```

## Selection Guide

| Task Type | Best Persona | Alternative |
|-----------|--------------|-------------|
| Write new feature code | **Coder** | - |
| Write unit tests | **Tester** | Coder (for simple tests) |
| Create test plan | **QA_Engineer** | Tester (for simple cases) |
| Review PR/code | **Code_Reviewer** | - |
| Fix bug/error | **Debugger** | Coder (if root cause known) |
| Look up documentation | **Web_Researcher** | - |
| Investigate security issue | **Code_Reviewer** | Debugger (if it's a bug) |
| Performance analysis | **Code_Reviewer** | Debugger (if it's a problem) |
| Integration testing | **QA_Engineer** | Tester (for simple cases) |

## Configuration File Structure

Subagent personas are configured in `.ledit/subagents.json`:

```json
{
  "personas": {
    "Coder": {
      "id": "coder",
      "name": "Coder",
      "provider": "chutes",
      "model": "ai-worker",
      "enabled": true,
      "system_prompt_file": "subagent_prompts/coder.md"
    },
    "Tester": {
      "id": "tester",
      "name": "Tester",
      "provider": "chutes",
      "model": "ai-worker",
      "enabled": true,
      "system_prompt_file": "subagent_prompts/tester.md"
    },
    "QA_Engineer": {
      "id": "qa_engineer",
      "name": "QA Engineer",
      "provider": "deepinfra",
      "model": "deepseek-chat",
      "enabled": true,
      "system_prompt_file": "subagent_prompts/qa_engineer.md"
    },
    "Code_Reviewer": {
      "id": "code_reviewer",
      "name": "Code Reviewer",
      "provider": "deepinfra",
      "model": "deepseek-chat",
      "enabled": true,
      "system_prompt_file": "subagent_prompts/code_reviewer.md"
    },
    "Debugger": {
      "id": "debugger",
      "name": "Debugger",
      "provider": "chutes",
      "model": "ai-worker",
      "enabled": true,
      "system_prompt_file": "subagent_prompts/debugger.md"
    },
    "Web_Researcher": {
      "id": "web_researcher",
      "name": "Web Researcher",
      "provider": "deepinfra",
      "model": "deepseek-chat",
      "enabled": true,
      "system_prompt_file": "subagent_prompts/web_researcher.md"
    }
  },
  "defaults": {
    "provider": "chutes",
    "model": "ai-worker"
  }
}
```

## Slash Commands

```bash
# List all available personas
/subagent-personas

# Show details for a specific persona
/subagent-persona debugger

# Configure a persona (provider/model)
/subagent-persona coder provider openai
/subagent-persona coder model gpt-5-mini

# Enable/disable a persona
/subagent-persona coder enable
/subagent-persona web_researcher disable
```

## Implementation Status

### Completed ✅

1. **Step 1: Define personas** - ✅ Complete
   - Documented 6 personas with clear purposes and use cases
   - Defined strengths, typical tasks, and configuration recommendations

2. **Step 2: Create system prompts** - ✅ Complete
   - Created specialized system prompts for all 6 personas
   - Prompts focus on positive guidance without artificial restrictions
   - Prompts allow agents to exercise judgment and do what's needed

3. **Step 3: Implement configuration structure** - ✅ Complete
   - Added `SubagentType` struct to configuration
   - Added helper methods for retrieving persona configuration
   - Added default personas in `NewConfig()`

4. **Step 4: Update subagent tools** - ✅ Complete
   - Added `persona` parameter to `run_subagent` tool
   - Implemented persona-specific provider/model resolution
   - Integrated with existing `--system-prompt` flag

5. **Step 5: Implement slash commands** - ✅ Complete
   - `/subagent-personas` - List all available personas
   - `/subagent-persona <name>` - Show persona details
   - `/subagent-persona <name> enable/disable` - Enable/disable personas
   - `/subagent-persona <name> provider <provider>` - Set provider for persona
   - `/subagent-persona <name> model <model>` - Set model for persona

6. **Step 6: Add persona selection logic** - ✅ Complete
   - Added persona selection guide to main system prompt
   - Documented when to use each persona
   - Provided examples of persona usage
   - Updated main documentation

### Remaining ⏭️

7. **Step 7: Test and validate each persona**
   - Test each persona with real-world tasks
   - Verify system prompts load correctly
   - Validate persona-specific configurations work as expected

## System Prompts Created

- ✅ `pkg/agent/prompts/subagent_prompts/coder.md` - Implementation focus
- ✅ `pkg/agent/prompts/subagent_prompts/tester.md` - Unit testing focus
- ✅ `pkg/agent/prompts/subagent_prompts/qa_engineer.md` - Test planning and integration testing
- ✅ `pkg/agent/prompts/subagent_prompts/code_reviewer.md` - Security and quality review
- ✅ `pkg/agent/prompts/subagent_prompts/debugger.md` - Bug investigation and fixes
- ✅ `pkg/agent/prompts/subagent_prompts/web_researcher.md` - Documentation and research

## See Also

- [Subagent Prompts README](../pkg/agent/prompts/subagent_prompts/README.md) - Quick reference for all personas
- [Main System Prompt](../pkg/agent/prompts/system_prompt.md) - Includes persona selection guide
