# ledit Command Cheat Sheet

## Core Commands

| Command             | Description                 | Example                        |
| ------------------- | --------------------------- | ------------------------------ |
| `ledit init`        | Initialize project          | `ledit init`                   |
| `ledit code`        | Generate/edit code          | `ledit code "Add feature"`     |
| `ledit fix`         | Apply suggested fixes       | `ledit fix "Fix linting errors"`|
| `ledit orchestrate` | Plan complex feature        | `ledit orchestrate "Add auth"` |
| `ledit log`         | View change history         | `ledit log`                    |
| `ledit question`    | Interactive chat            | `ledit question`               |
| `ledit ignore`      | Add patterns to ignore list | `ledit ignore "*.log"`         |

## Common Options

| Option          | Description                |
| --------------- | -------------------------- |
| `--filename`    | Target file path           |
| `--model`       | Override default model     |
| `--skip-prompt` | Apply without confirmation |
| `--verbose`     | Show detailed output       |

## Context Directives

| Tag             | Purpose                                         | Example                                |
| --------------- | ----------------------------------------------- | -------------------------------------- |
| `#SG`           | Search grounding                                | `"#SG \"react native\" Start project"` |
| `#WORKSPACE`    | Include project context                         | `"Refactor #WORKSPACE"`                |
| `#WS`           | Alias for #WORKSPACE                            | `"Update #WS"`                         |
| `#<file \ url>` | Include specific file or content from a webpage | `"Use functions from #utils.go"`       |

## Supported LLM Providers

| Provider  | Example Model Specifier               |
| --------- | ------------------------------------- |
| Lambda AI | `lambda-ai:qwen25-coder-32b-instruct` |
| OpenAI    | `openai:gpt-4o`                       |
| Groq      | `groq:llama-3.3-70b`                  |
| Gemini    | `gemini:gemini-1.5-pro`               |
| Ollama    | `ollama:llama3`                       |

## Workspace Files

- `.ledit/workspace.json` - File summaries and exports
- `.ledit/leditignore` - Custom ignore patterns
- `.ledit/changelog.db` - Change history database
- `.ledit/config.json` - Project configuration

## Orchestration Process

1. Analysis of prompt and workspace
2. LLM generates JSON plan
3. User review and approval
4. Step-by-step execution
5. Validation and self-correction
