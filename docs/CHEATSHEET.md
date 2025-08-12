# Ledit Cheatsheet

This document provides a quick reference for `ledit` commands and concepts.

## Core Commands

| Command | Description | Example |
|---|---|---|
| `ledit code` | Generate or modify code based on instructions. | `ledit code "Add a new function to calculate factorial" -f math.go` |
| `ledit process` | Orchestrate a complex feature implementation. **(Alpha State)** | `ledit process "Implement user authentication with JWT"` |
| `ledit init` | Initialize a specific configuration for `ledit` in your project, separate from the main global configuration. | `ledit init` |
| `ledit log` | View the history of changes made by `ledit` and revert changes by prompt. | `ledit log` |
| `ledit question` | Ask `ledit` a question about your code or general topics in an interactive chat. | `ledit question "Explain the main function in main.go"` |
| `ledit fix` | Attempt to fix a problem in your code by running a command and letting `ledit` attempt to fix the error. | `ledit fix "go build"` |
| `ledit ignore` | Add patterns to `.ledit/leditignore` to exclude files from workspace analysis. | `ledit ignore "temp_files/"` |
| `ledit commit` | Generate a conventional git commit message and complete a git commit for staged changes. | `ledit commit` |

## Common Options

| Option | Description | Example |
|---|---|---|
| `-f, --filename <path>` | Specify the target file for `code` command. If omitted, `ledit` may create a new file. | `ledit code "Add a new route" -f server.go` |
| `-m, --model <provider:model>` | Override the default LLM model for the command. | `ledit code "..." -m openai:gpt-4-turbo` |
| `--skip-prompt` | Bypass all user confirmation prompts (use with caution). | `ledit process "..." --skip-prompt` |

## Context Directives

Use these special directives in your prompts to control the context provided to the LLM.

| Directive | Description | Example |
|---|---|---|
| `#<filepath>` | Include the full content of a specific file. | `ledit code "Refactor based on #./old_code.go" -f new_code.go` |
| `#WORKSPACE` or `#WS` | Automatically select and include relevant files (full content or summary) from your workspace. | `ledit code "Add user roles. #WS"` |
| `#SG "query"` | Perform a web search and ground the LLM's response with relevant snippets. | `ledit code "Use the latest React hook form. #SG \"react hook form latest version\""` |

## Supported LLM Providers

Specify provider and model using `<provider>:<model_name>`.

-   `deepinfra`: `deepinfra:deepseek-ai/DeepSeek-V3-0324`, `deepinfra:mistralai/Mistral-Small-3.2-24B-Instruct-2506`, `deepinfra:meta-llama/Llama-3.3-70B-Instruct-Turbo`, `deepinfra:Qwen/Qwen3-Coder-480B-A35B-Instruct`
-   `gemini`: `gemini:gemini-2.5-flash`, `gemini:gemini-2.5-pro`
-   `openai`: `openai:gpt-4o`
-   `groq`: `groq:llama3-70b-8192`
-   `ollama`: `ollama:qwen2.5-coder` (locally run, cant handle workspace context in most cases)
    -   `cerebras`: `cerebras:qwen-3-coder-480b`
    -   `deepseek`: `deepseek:deepseek-coder`

## Workspace Files

`ledit` maintains these files in your `.ledit/` directory:

-   `.ledit/workspace.json`: Index of your project's files and their summaries and additional workspace context.
-   `.ledit/requirements.json`: Stores the current orchestration plan.
-   `.ledit/config.json`: Project-specific configuration settings.
-   `.ledit/leditignore`: Patterns for files/directories to ignore.
-   `.ledit/setup.sh`: Generated setup script for orchestration.
-   `.ledit/validate.sh`: Generated validation script for orchestration.

## Orchestration Process (using `ledit process`) -- NOT READY FOR REAL WORLD USE!!!

1.  **Analysis**: `ledit` analyzes your prompt and workspace.
2.  **Planning**: An LLM generates a JSON plan of required changes.
3.  **Review**: You review and approve the plan.
4.  **Execution**: `ledit` executes each step: generates code, applies changes, runs setup/validation scripts.
5.  **Self-Correction**: If a step fails, `ledit` analyzes the error, optionally performs web search, re-prompts the LLM for a fix, and retries.

---
[Back to README](../README.md)