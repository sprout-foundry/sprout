# Multi-Agent Orchestration

The `ledit process` command now supports multi-agent orchestration, allowing you to coordinate multiple AI agents with different personas to accomplish complex, multi-step development tasks.

## Overview

Multi-agent orchestration enables you to:
- Define multiple agents with specialized personas (e.g., frontend developer, backend architect, QA engineer)
- Specify steps that each agent should execute
- Set dependencies between steps to ensure proper execution order
- Track progress and agent status throughout the process
- Validate results using build, test, and lint commands

## Usage

### Basic Command

```bash
# Execute a multi-agent process
ledit process process.json

# Create an example process file
ledit process --create-example my_process.json

# Execute with specific model
ledit process --model gpt-4 process.json

# Skip confirmation prompts
ledit process --skip-prompt process.json
```

### Process File Format

Process files are JSON files that define the orchestration plan. Here's the structure:

```json
{
  "version": "1.0",
  "goal": "Overall goal description",
  "description": "Detailed description of what to accomplish",
  "agents": [...],
  "steps": [...],
  "validation": {...},
  "settings": {...}
}
```

#### Agents

Each agent represents a specialized role with specific skills:

```json
{
  "id": "architect",
  "name": "System Architect",
  "persona": "system_architect",
  "description": "Designs the overall system architecture",
  "skills": ["system_design", "database_design"],
  "model": "gpt-4",
  "priority": 1,
  "depends_on": [],
  "config": {"skip_prompt": "false"},
  "budget": {
    "max_tokens": 100000,
    "max_cost": 10.0,
    "token_warning": 80000,
    "cost_warning": 8.0,
    "alert_on_limit": true,
    "stop_on_limit": false
  }
}
```

**Agent Properties:**
- `id`: Unique identifier for the agent
- `name`: Human-readable name
- `persona`: Role/persona (e.g., "frontend_developer", "backend_architect")
- `description`: What this agent is responsible for
- `skills`: List of expertise areas
- `model`: LLM model to use for this agent (overrides base model)
- `priority`: Execution priority (lower = higher priority)
- `depends_on`: Agent IDs this agent depends on
- `config`: Agent-specific configuration
- `budget`: Budget constraints and cost controls

**Budget Controls:**
The `budget` field allows you to control costs and token usage per agent:

```json
"budget": {
  "max_tokens": 100000,      // Maximum tokens this agent can use
  "max_cost": 10.0,          // Maximum cost in USD this agent can incur
  "token_warning": 80000,    // Token threshold for warnings
  "cost_warning": 8.0,       // Cost threshold for warnings
  "alert_on_limit": true,    // Whether to alert when approaching limits
  "stop_on_limit": false     // Whether to stop execution when limit reached
}
```

**Base Model Configuration:**
Set a default model for all agents in the `base_model` field. Individual agents can override this with their own `model` field:

```json
{
  "base_model": "gpt-4",
  "agents": [
    {
      "id": "architect",
      "model": "gpt-4o",     // Overrides base model
      // ... other fields
    },
    {
      "id": "backend_dev",   // Uses base model "gpt-4"
      // ... other fields
    }
  ]
}
```

#### Steps

Steps define the work to be done:

```json
{
  "id": "design_architecture",
  "name": "Design System Architecture",
  "description": "Create the overall system architecture",
  "agent_id": "architect",
  "input": {"requirements": "web application with user management"},
  "expected_output": "System architecture document",
  "status": "pending",
  "depends_on": [],
  "timeout": 600,
  "retries": 2
}
```

**Step Properties:**
- `id`: Unique identifier for the step
- `name`: Human-readable name
- `description`: What this step accomplishes
- `agent_id`: Which agent should execute this step
- `input`: Input data for the agent
- `expected_output`: What output is expected
- `status`: Current status ("pending", "in_progress", "completed", "failed")
- `depends_on`: Step IDs this step depends on
- `timeout`: Timeout in seconds
- `retries`: Number of retries allowed

#### Validation

Define how to validate the final results:

```json
{
  "build_command": "npm run build",
  "test_command": "npm test",
  "lint_command": "npm run lint",
  "custom_checks": ["npm run security-check"],
  "required": true
}
```

#### Settings

Process-wide configuration:

```json
{
  "max_retries": 3,
  "step_timeout": 900,
  "parallel_execution": false,
  "stop_on_failure": true,
  "log_level": "info"
}
```

## Example Process

See `examples/multi_agent_example.json` for a complete example that demonstrates:

1. **System Architect** designs the overall architecture
2. **Backend Developer** implements the API (depends on architect)
3. **Frontend Developer** creates the UI (depends on architect)
4. **QA Engineer** tests everything (depends on backend and frontend)

## How It Works

1. **Process Loading**: The system loads and validates the process file
2. **Agent Initialization**: Each agent is initialized with its configuration
3. **Dependency Resolution**: Steps are sorted by dependencies
4. **Step Execution**: Each step is executed by its assigned agent
5. **Progress Tracking**: Agent status and step progress are monitored
6. **Validation**: Final results are validated using the specified commands

## Agent Personas

The system supports various agent personas:

- **system_architect**: Designs system architecture and data models
- **backend_developer**: Implements APIs and business logic
- **frontend_developer**: Creates user interfaces and frontend components
- **qa_engineer**: Tests applications and ensures quality
- **devops_engineer**: Handles deployment and infrastructure
- **security_engineer**: Reviews security aspects
- **documentation_writer**: Creates documentation and guides

## Best Practices

1. **Clear Dependencies**: Define clear, logical dependencies between steps
2. **Agent Specialization**: Give each agent a focused, specific role
3. **Realistic Timeouts**: Set appropriate timeouts for each step
4. **Input Context**: Provide relevant input context for each step
5. **Expected Outputs**: Clearly define what each step should produce
6. **Validation**: Include comprehensive validation steps

## Troubleshooting

### Common Issues

1. **Circular Dependencies**: Ensure no circular dependencies between steps
2. **Agent Not Found**: Verify all referenced agent IDs exist
3. **Step Dependencies**: Check that all step dependencies are valid
4. **Timeout Issues**: Adjust timeouts based on step complexity

### Debugging

- Use `--skip-prompt` to avoid confirmation prompts during testing
- Check the logs for detailed execution information
- Review agent status and step progress in real-time

## Advanced Features

### Parallel Execution

Set `"parallel_execution": true` in settings to allow independent steps to run simultaneously.

### Custom Validation

Add custom validation commands to the `custom_checks` array in the validation section.

### Agent Configuration

Use the `config` field to pass agent-specific settings like `skip_prompt`, `model_override`, etc.

## Integration

The multi-agent orchestration system integrates with:

- Existing `ledit` commands and workflows
- LLM models (OpenAI, Anthropic, Ollama, etc.)
- Workspace analysis and file management
- Git integration and change tracking
- Build and validation systems
