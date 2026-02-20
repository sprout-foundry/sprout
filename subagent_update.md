# Plan for Improving Subagent Context Sharing

## Current State Analysis

Based on my analysis of the ledit codebase, I've identified that the current subagent implementation works but has limitations in sharing context effectively between parent and child agents. The current approach:

1. **Subagent Creation**: Uses `run_subagent` tool which spawns a new ledit process
2. **Context Passing**: Currently passes context through the "context" parameter and "files" parameter
3. **Limitations**: 
   - Limited automatic context propagation
   - No shared state between parent and child agents
   - Manual context management required
   - Difficult to maintain consistent state across agent instances

## Goals for Improvement

1. **Enhanced Context Sharing**: Enable child agents to have access to the parent's context automatically
2. **Shared State Management**: Allow child agents to share state with parent agents
3. **Consistent State Management**: Ensure consistent handling of current state, configuration, and context
4. **Reduced Manual Management**: Minimize the need for manual context passing

## Implementation Plan

### Phase 1: Context Enhancement

**Objective**: Improve how context is passed to subagents to include more comprehensive information

**Changes to make**:
1. **Modify `handleRunSubagent` in `pkg/agent/tool_registry.go`**:
   - Enhance context passing to include current session state
   - Add automatic inclusion of relevant files from parent's context
   - Include configuration and current working directory context

2. **Create Context Propagation Mechanism**:
   - Add a mechanism to automatically determine relevant context files
   - Implement context sharing based on current task scope

### Phase 2: State Management Improvements

**Objective**: Create better state sharing between parent and child agents

**Changes to make**:
1. **Add State Sharing Interface**:
   - Create a shared state manager that can be accessed by both parent and child agents
   - Implement mechanisms for state synchronization

2. **Enhance Subagent Communication**:
   - Add ability for child agents to communicate back state updates to parent
   - Implement a shared memory or file-based state store for cross-agent communication

### Phase 3: Configuration and Environment Improvements

**Objective**: Ensure child agents inherit proper configuration from parent agents

**Changes to make**:
1. **Improve Configuration Propagation**:
   - Automatically pass configuration from parent to child agents
   - Ensure consistent environment variables and settings

### Phase 4: Testing and Validation

**Objective**: Validate that improvements work correctly and don't break existing functionality

**Changes to make**:
1. **Add comprehensive tests** for the new context sharing mechanisms
2. **Ensure backward compatibility** with existing subagent usage
3. **Test edge cases** including complex nested subagents

## Detailed Implementation

### 1. Enhanced Context Passing

In `pkg/agent/tool_registry.go`, update the `handleRunSubagent` function to automatically include:

- Current working directory and relevant project files
- Configuration settings from the parent agent
- Any current task-related context that should be shared
- File system context that's relevant to the current operation

### 2. State Sharing Infrastructure

Create a mechanism to maintain state between parent and child agents:

- **Shared Memory Area**: Implement a temporary file-based or in-memory mechanism for sharing state
- **State Sync Protocol**: Define how state changes in child agents are communicated back to parent
- **Context Keys**: Define standard keys for different types of shared context

### 3. Environment and Configuration Propagation

Ensure that when child agents are spawned, they inherit the configuration:

- Model and provider settings
- Security configurations
- Debug settings
- Any other environment-specific configurations

### 4. Implementation Approach

Use the existing subagent infrastructure but enhance it to:

1. **Automatically determine relevant context** based on current operation
2. **Pass configuration and state** in an organized manner
3. **Maintain consistency** with existing APIs and workflows
4. **Minimize breaking changes** to existing functionality

## Expected Benefits

1. **Better Automation**: Subagents will have better access to necessary context automatically
2. **Reduced Manual Work**: Less need to manually pass context between agents
3. **Improved Consistency**: More uniform handling of context across different operations
4. **Enhanced Collaboration**: Better support for complex multi-agent workflows
5. **Maintained Backward Compatibility**: Existing code will continue to work unchanged

## Implementation Steps

1. **Modify `handleRunSubagent`** to automatically include more context
2. **Create state sharing mechanism** for cross-agent communication
3. **Update documentation** and examples showing the new capabilities
4. **Write comprehensive tests** to validate the new functionality
5. **Validate with existing workflows** to ensure no regressions

This plan will significantly improve the subagent experience by making context sharing more automatic and efficient, reducing the need for manual context management while maintaining full backward compatibility.