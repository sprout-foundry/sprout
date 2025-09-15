#!/bin/bash
set -e

echo "Configuring MCP for GitHub integration..."

# Create MCP config directory
mkdir -p ~/.ledit/mcp

# Create MCP configuration
cat > ~/.ledit/mcp_config.json << EOF
{
  "mcpServers": {
    "github": {
      "command": "node",
      "args": ["/usr/local/lib/node_modules/@modelcontextprotocol/server-github/dist/index.js"],
      "env": {
        "GITHUB_PERSONAL_ACCESS_TOKEN": "$GITHUB_TOKEN"
      }
    }
  }
}
EOF

echo "MCP configured for GitHub integration"