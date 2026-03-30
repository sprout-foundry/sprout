# Configuration

`ledit` is configured via a `config.json` file. It looks for this file first in `./.ledit/config.json` and then in `~/.ledit/config.json`. A default configuration is created on first run.

## API Keys

API keys for services like DeepInfra, OpenAI, Ollama, etc., are stored securely in `~/.ledit/api_keys.json`. If a key is not found, `ledit` will prompt you to enter it. Set environment variables like `DEEPINFRA_API_KEY`, `OPENAI_API_KEY`, `OLLAMA_API_KEY` for convenience.

For Z.AI Coding Plan support, set `ZAI_API_KEY` and select the provider/model:

```bash
export ZAI_API_KEY=your_api_key
ledit agent --provider zai --model GLM-4.6 "implement feature X"
```

## Environment Variables

`ledit` respects the following environment variables:

| Variable | Description | Example |
|----------|-------------|---------|
| `LEDIT_NO_STREAM=1` | Disable streaming mode | `LEDIT_NO_STREAM=1 ledit agent "task"` |
| `LEDIT_NO_SUBAGENTS=1` | Disable subagent tools | `LEDIT_NO_SUBAGENTS=1 ledit agent "task"` |
| `LEDIT_NO_CONNECTION_CHECK=1` | Skip provider connection check | `LEDIT_NO_CONNECTION_CHECK=1 ledit agent "task"` |
| `LEDIT_RESOURCE_DIRECTORY=<dir>` | Store web/vision resources | `LEDIT_RESOURCE_DIRECTORY=captures` |
| `LEDIT_TRACE_DATASET_DIR=<dir>` | Enable dataset tracing | `LEDIT_TRACE_DATASET_DIR=traces` |
| `LEDIT_CONFIG=<dir>` | Custom config directory | `LEDIT_CONFIG=/my/config` |
| `CI=1` or `GITHUB_ACTIONS=1` | CI environment mode | `CI=1 ledit agent "task"` |
| `GITHUB_PERSONAL_ACCESS_TOKEN` | GitHub token for MCP | Auto-discovers GitHub MCP server |
| `OPENAI_API_KEY`, `DEEPINFRA_API_KEY`, etc. | API keys for providers | Set directly or in `api_keys.json` |

## config.json Settings

The configuration uses a flat structure focused on provider and model management. Here's the complete structure with defaults:

```json
{
  "version": "2.0",
  "last_used_provider": "openai",
  "provider_models": {
    "openai": "gpt-5-mini",
    "zai": "GLM-4.6",
    "deepinfra": "meta-llama/Llama-3.3-70B-Instruct",
    "openrouter": "openai/gpt-5",
    "ollama-local": "qwen3-coder:30b",
    "ollama-turbo": "deepseek-v3.1:671b"
  },
  "provider_priority": [
    "openai",
    "zai",
    "openrouter",
    "deepinfra",
    "ollama-turbo",
    "ollama-local"
  ],
  "mcp": {
    "enabled": false,
    "servers": {},
    "auto_start": false,
    "auto_discover": false,
    "timeout": 30000000000
  },
  "file_batch_size": 10,
  "max_concurrent_requests": 5,
  "request_delay_ms": 100,
  "enable_security_checks": true,
  "enable_pre_write_validation": false,
  "api_timeouts": {
    "connection_timeout_sec": 30,
    "first_chunk_timeout_sec": 60,
    "chunk_timeout_sec": 320,
    "overall_timeout_sec": 600
  },
  "preferences": {},
  "custom_providers": {},
  "resource_directory": "",
  "reasoning_effort": "",
  "self_review_gate_mode": "off",
  "subagent_provider": "",
  "subagent_model": "",
  "pdf_ocr_enabled": true,
  "pdf_ocr_provider": "ollama",
  "pdf_ocr_model": "glm-ocr",
  "history_scope": "workspace"
}
```

### Key Sections

#### `provider_models`

Maps each provider to their default model. These models are used when no specific model is specified in commands.

#### `provider_priority`

Defines the order in which providers are tried when multiple are available. The first available provider in this list is used by default.

#### `mcp`

Model Context Protocol configuration:

- `enabled`: Enable MCP servers (default: `false`)
- `servers`: Map of server names to configurations
- `auto_start`: Auto-start MCP servers on launch (default: `false`)
- `auto_discover`: Auto-discover available MCP servers (default: `false`)
- `timeout`: Timeout in nanoseconds for MCP requests (default: 30s)

#### `last_used_provider`

Tracks the most recently used provider for quick switching.

#### `api_timeouts`

Configures timeouts for API requests:

- `connection_timeout_sec`: Timeout for establishing connection (default: 30s)
- `first_chunk_timeout_sec`: Timeout for first response chunk (default: 60s)
- `chunk_timeout_sec`: Timeout between response chunks (default: 320s)
- `overall_timeout_sec`: Maximum total request time (default: 600s)

#### `subagent_provider` and `subagent_model`

Separate provider/model configuration for subagents. Leave empty to use the main provider/model.

#### `pdf_ocr_enabled`, `pdf_ocr_provider`, `pdf_ocr_model`

PDF analysis settings for OCR processing. When enabled, uses the specified provider and model for PDF text extraction.

## Zsh Command Detection

When using zsh as your shell, `ledit` automatically detects commands available in your environment (external commands, builtins, aliases, and functions) and executes them directly instead of sending them to the AI. This feature is **enabled by default** when using zsh.

### Configuration Options

To modify behavior, add to your `~/.ledit/config.json`:

```json
{
  "enable_zsh_command_detection": true,
  "auto_execute_detected_commands": true
}
```

- `enable_zsh_command_detection`: Enable/disable command detection (default: `true`)
- `auto_execute_detected_commands`: Auto-execute detected commands without prompting (default: `true`)

### To disable auto-execution (prompt for confirmation):

```json
{
  "auto_execute_detected_commands": false
}
```

### To disable the feature entirely:

```json
{
  "enable_zsh_command_detection": false
}
```

### How it works:

1. When you type a command that matches an available zsh command (e.g., `git status`, `ls -la`), `ledit` detects it
2. By default, **auto-executes** the command immediately (configurable):
   ```
   [Detected external command: git] [/usr/bin/git]
   [Auto-executing]
   ▶ Executing: git status
   ```
3. If you've disabled auto-execution, it will ask for confirmation first
4. If it's not a clear command, falls through to normal AI processing

### Manual execution with `!`:

Prefix your command with `!` to force auto-execution (overrides config):
```bash
ledit> !git status  # Always executes immediately
```

### Why use this?

- **Faster execution**: Commands run instantly without AI involvement
- **Predictable behavior**: Exact command execution vs AI interpretation
- **Better for routine tasks**: Use shell for simple commands, AI for complex ones
- **Configurable safety**: Choose between auto-execute or confirmation prompts

### Fallback behavior:

If the input is not clearly a command, it will be passed to the AI as normal. This feature only triggers when zsh can confirm the first word is a valid command, builtin, alias, or function.

## CI/CD and Non-Interactive Usage

`ledit` is designed to work seamlessly in CI/CD pipelines and automated environments:

### Automatic CI Detection

`ledit` automatically detects CI environments via `CI` or `GITHUB_ACTIONS` environment variables. When detected:

- Clean, structured output without terminal control sequences
- Progress updates every 5 seconds with token/cost tracking
- Structured summaries at completion with iteration counts and metrics

### Example Usage in CI:

```bash
# Basic CI usage
CI=1 ledit agent "Review changes and generate commit message"

# With specific provider/model
ledit agent --provider deepinfra --model "meta-llama/Llama-3.3-70B-Instruct" "task"

# Skip prompts for automation
ledit agent --skip-prompt "Implement feature"

# Disable streaming for scripts
LEDIT_NO_STREAM=1 ledit agent "task"

# Skip provider connection check (faster in CI)
LEDIT_NO_CONNECTION_CHECK=1 ledit agent "task"
```

### Piped Input Support

For scripted automation, `ledit` supports piped input:

```bash
echo "Analyze main.go for potential issues" | ledit agent --prompt-stdin
```

### Exit Code Handling

`ledit` returns appropriate exit codes for CI integration:

- `0`: Success
- Non-zero: Error or failure

### Example GitHub Actions Workflow:

```yaml
- name: Run ledit agent
  env:
    OPENAI_API_KEY: ${{ secrets.OPENAI_API_KEY }}
  run: |
    CI=1 ledit agent --skip-prompt "Review staged changes"
    ledit commit --skip-prompt
```
