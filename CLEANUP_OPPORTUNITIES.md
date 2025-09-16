# Cleanup Opportunities After Code Command Removal

This document outlines cleanup opportunities discovered after removing the `ledit code` command.

## 1. Configuration Cleanup

### EditingModel References
The `editing_model` configuration is still referenced in 74+ places throughout the codebase, but it's primarily used by the agent now:

**Files with heavy usage:**
- `/pkg/config/config.go` - Config struct still has EditingModel field
- `/pkg/config/llm.go` - LLM config struct defines EditingModel
- `/pkg/context/context_builder.go` - Still uses EditingModel for some operations
- `/pkg/changetracker/` - Tracks EditingModel in change history

**Recommended action:** 
- Consider renaming `editing_model` to `agent_model` or just `model` since it's no longer specific to editing
- Update all references accordingly

### Orphaned Configuration Fields
- `QualityLevel` in config.go (line 66) - Was used for code generation quality levels
- Various code generation related config options that might no longer be needed

## 2. Orphaned Code and Interfaces

### `/pkg/interfaces/domain.go`
Contains unused interfaces after code command removal:
- `CodeGenerator` interface (lines 10-22)
- `CodeGenerationRequest` struct (lines 25-33)  
- `CodeGenerationResult` struct (lines 36-41)
- `ValidationResult` struct (lines 44-49)

**Recommended action:** Remove these interfaces entirely

### `/pkg/context/context_builder.go`
Contains orphaned functions:
- `callLLMDirectly()` function (line 364+) - Only used for direct code generation
- `buildQualityAwareCodeMessages()` - Related to quality-enhanced code generation
- References to `code_generation_system.txt` and `code_modification_system.txt`

**Recommended action:** Remove these functions if not used by agent

## 3. Orphaned Prompts

The following prompt files appear to be orphaned:
- `/prompts/code_generation_system.txt` - Direct code generation prompt
- `/prompts/code_modification_system.txt` - Code modification prompt
- `/prompts/base_code_editing.txt` and variants - Multiple code editing prompts
- `/prompts/interactive_code_generation.txt` and variants

**Recommended action:** Audit which prompts are actually used by the agent and remove unused ones

## 4. Code Review Service Cleanup

`/pkg/codereview/service.go` contains many references to code generation workflows:
- `performAutomatedReview()` - Automated review during code generation
- `attemptRetry()` - Retry code generation logic
- Various code generation related constants and messages

**Recommended action:** Simplify to only support agent-based workflows

## 5. Message and String Constants

`/pkg/prompts/messages.go` contains unused functions:
- `ProcessingCodeGeneration()` (line 62)
- `CodeGenerationError()` (line 66)
- `CodeGenerationFinished()` (line 70)
- References to "code generation" in various messages

**Recommended action:** Remove unused message functions

## 6. Simplified Architecture Opportunities

### Workspace Context Builder
The context builder is overly complex for just agent usage:
- Can remove quality-aware code generation logic
- Simplify to just support agent's needs
- Remove direct LLM call paths that bypass agent

### Configuration Simplification
With only the agent command needing models, configuration can be simplified:
- Single model configuration instead of multiple (editing, orchestration, etc.)
- Remove code-generation specific options
- Simplify model selection logic

## 7. Test Infrastructure

### Missing Test Provider
Integration tests expect "test:test" provider which doesn't exist
**Recommended action:** 
- Implement a proper test provider in the agent_api package
- Or update integration tests to use a minimal real provider

## 8. Documentation Updates Needed

- Update architecture documentation to reflect removal of code command
- Update model configuration documentation
- Remove references to code generation workflows
- Simplify getting started guides

## Summary

The removal of the code command leaves significant cleanup opportunities:
1. **~200+ lines** of orphaned interfaces and types
2. **~500+ lines** of code generation specific logic in various services  
3. **10+ orphaned prompt files**
4. **Configuration complexity** that can be significantly reduced
5. **Simplified architecture** opportunity by removing parallel code paths

This cleanup would make the codebase more maintainable and focused on the agent as the primary interface.