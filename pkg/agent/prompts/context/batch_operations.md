# Batch Operations Context Module

## File Access Efficiency

### Batch Reading Strategy (MANDATORY)

**Discovery First**: Always use shell commands to find relevant files before reading
```bash
# Directory structure
ls -la

# Find specific file types  
find . -name "*.go" -type f | head -10

# Locate functionality
grep -r "function_name" --include="*.go" .
```

**Batch Reading**: Read ALL needed files in ONE tool call array
```json
[
  {"name": "read_file", "arguments": {"file_path": "main.go"}},
  {"name": "read_file", "arguments": {"file_path": "pkg/agent/agent.go"}},
  {"name": "read_file", "arguments": {"file_path": "README.md"}}
]
```

### Efficiency Rules

**File Reading**:
- **Never** read files one at a time across multiple iterations
- **Always** batch read all needed files in a single tool call array
- **Discovery First**: Use shell commands to identify files before reading

**Pattern Examples**:
```bash
# Wrong: Multiple separate read_file calls
read_file("main.go") → analyze → read_file("agent.go") → analyze

# Right: Single batch read
[read_file("main.go"), read_file("agent.go"), read_file("README.md")] → analyze all
```

### Planning File Access

**Before Reading**:
1. Use discovery commands to identify all relevant files
2. Plan which files you need based on the task
3. Batch read all identified files in one operation
4. Analyze all content together to make informed decisions

**Discovery Commands**:
- `ls -la` - Directory structure
- `find . -name "*.go"` - Find specific file types
- `grep -r "pattern" .` - Locate specific functionality
- Check `.ledit/workspace.json` for workspace summaries