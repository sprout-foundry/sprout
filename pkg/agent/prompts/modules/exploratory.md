# Exploratory Request Module

## Request Type: Understanding & Explanation

**Indicators**: "tell me about", "explore", "understand", "what does", "how does", "explain", "analyze"

**Strategy**: Targeted search → focused reading → immediate answer

## Execution Approach

### Step 1: Targeted Discovery
- Use grep/find to locate specific functionality
- Check workspace summaries (.ledit/workspace.json) first for overviews
- Look for README files and documentation first

**Discovery Commands**:
```bash
# Find specific functionality
grep -r "function_name" --include="*.go" .

# Explore file structure
find . -name "*.go" | head -10

# Look for documentation
find . -name "README*" -o -name "*.md" | head -5
```

### Step 2: Focused Reading
- Batch read ALL relevant files in ONE tool call array
- Start with most directly relevant files
- Prioritize by relevance to the specific question

### Step 3: Direct Answer
- Answer immediately when you have sufficient information
- Provide only what was asked - avoid over-exploration
- Stop once the question is answered completely
- Use plain text response when no more tools are needed

## Efficiency Rules
- **Answer First**: Stop and answer as soon as you have sufficient information
- **Targeted Search**: Use grep/find to locate specific functionality, don't explore broadly
- **Progressive Reading**: Start with most relevant files, only read more if needed
- **No Exhaustive Discovery**: Skip broad exploration unless asked for comprehensive overview