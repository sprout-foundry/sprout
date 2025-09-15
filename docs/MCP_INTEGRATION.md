# MCP (Model Context Protocol) Integration Guide

This guide explains how MCP servers are integrated with the Ledit agent and how to verify they're working.

## Overview

MCP servers extend Ledit's capabilities by providing external tools and services to the agent. The GitHub MCP server is the prototype implementation.

## How MCP Integration Works

### 1. Architecture

```
Ledit Agent
    ├── Base Tools (file operations, shell, etc.)
    └── MCP Manager
        └── MCP Servers (GitHub, etc.)
            └── MCP Tools (list_issues, create_pr, etc.)
```

### 2. Key Components

- **MCP Manager** (`pkg/mcp/`): Manages MCP server lifecycle and tool discovery
- **Agent Integration** (`pkg/agent/agent.go`):
  - Initializes MCP manager in `NewAgent()`
  - Loads MCP config in `initializeMCP()`
  - Discovers tools via `getMCPTools()`
  - Executes tools via `executeMCPTool()`

### 3. Tool Discovery Flow

1. Agent starts → Creates MCP manager
2. Reads MCP config from `~/.ledit/config.json`
3. If enabled and auto-start is true → Starts configured servers
4. When processing messages → Calls `getMCPTools()` to get available tools
5. Tools are prefixed with `mcp_<server>_<tool>` (e.g., `mcp_github_list_issues`)

## Testing MCP Integration

### 1. Check MCP Configuration

```bash
# List configured MCP servers
./ledit mcp list

# Test GitHub MCP server directly
./ledit mcp test github
```

### 2. Enable Debug Mode

The agent checks for `DEBUG` environment variable to show MCP initialization:

```bash
# Run agent with debug output
DEBUG=1 ./ledit agent
```

You should see messages like:
- `🚀 Started N MCP servers`
- `🔧 Executing MCP tool: <tool> on server: <server>`

### 3. Example Prompts to Test GitHub MCP

Try these prompts with the agent to test GitHub functionality:

```bash
# Check notifications
./ledit agent "What are my GitHub notifications?"

# List repositories
./ledit agent "List my starred GitHub repositories"

# Get repository info
./ledit agent "Show me details about the facebook/react repository"

# Search for issues
./ledit agent "Search for open issues in kubernetes/kubernetes with label 'good first issue'"
```

## Adding New MCP Servers

### 1. Configure the Server

```bash
# Interactive setup
./ledit mcp add

# Or manually edit ~/.ledit/config.json
```

Example configuration:
```json
{
  "mcp": {
    "enabled": true,
    "autoStart": true,
    "servers": [
      {
        "name": "github",
        "command": "npx",
        "args": ["-y", "@modelcontextprotocol/server-github"],
        "env": {
          "GITHUB_PERSONAL_ACCESS_TOKEN": "your-token"
        }
      }
    ]
  }
}
```

### 2. Server Requirements

MCP servers must:
- Implement the MCP protocol over stdio
- Provide tool discovery via `tools/list`
- Handle tool execution via `tools/call`

### 3. Tool Naming Convention

Tools from MCP servers are exposed to the agent with the naming pattern:
```
mcp_<server_name>_<tool_name>
```

For example:
- `mcp_github_list_issues`
- `mcp_github_create_pull_request`
- `mcp_slack_send_message` (if you add a Slack server)

## Troubleshooting

### MCP Tools Not Appearing

1. **Check if MCP is enabled:**
   ```bash
   cat ~/.ledit/config.json | jq '.mcp.enabled'
   ```

2. **Verify server is configured:**
   ```bash
   ./ledit mcp list
   ```

3. **Test server directly:**
   ```bash
   ./ledit mcp test <server-name>
   ```

4. **Run with debug mode:**
   ```bash
   DEBUG=1 ./ledit agent "list your tools"
   ```

### Common Issues

- **No MCP config**: Run `./ledit mcp add` to set up
- **Server not starting**: Check command/args in config
- **Missing environment variables**: Ensure tokens are set
- **Tools not executing**: Check server logs with debug mode

## How the Agent Uses MCP Tools

1. **Tool Discovery**: When processing a message, the agent calls `getMCPTools()` which:
   - Queries all running MCP servers for their tools
   - Converts MCP tool definitions to agent tool format
   - Adds them to the available tool list

2. **Tool Selection**: The LLM sees all tools (base + MCP) and can choose any based on the task

3. **Tool Execution**: When an MCP tool is called:
   - Agent extracts server and tool name from the prefixed name
   - Calls `mcpManager.CallTool()` with the arguments
   - Returns the result to the LLM for processing

## Future Enhancements

- Tool caching for faster discovery
- Dynamic server installation
- Tool usage analytics
- Custom tool filtering
- Server health monitoring