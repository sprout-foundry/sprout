# Ledit Prompt Catalog

This document catalogs all the prompts currently used in the ledit project, organized by purpose and priority for optimization.

## 1. Code Generation Prompts

### 1.1 Main Code Generation (HIGH PRIORITY)
**Location**: `pkg/editor/generate.go`
**Purpose**: Primary code generation and editing
**Current Issues**: Parser issues, LLM generates programs instead of doing direct edits
**Goal**: 95%+ accuracy for simple text replacements and code modifications

### 1.2 Agent Code Generation (HIGH PRIORITY) 
**Location**: `pkg/agent/agent.go`
**Purpose**: Agent-driven code editing with rollback support
**Current Issues**: Complex prompt comprehension failures
**Goal**: Handle both simple and complex editing tasks reliably

## 2. Text Replacement Prompts (CRITICAL PRIORITY)

### 2.1 Simple Text Replacement
**Location**: Failed e2e test "Agent v2 - Code smoke (minimal hunk replacement)"
**Current Prompt**: Unknown (inferred from test failure)
**Issue**: LLM generates Go program to perform replacement instead of doing replacement directly
**Target Task**: Replace 'AAA_MARKER' with 'BBB_MARKER' in file
**Goal**: 100% success rate for simple string replacements

Example failing case:
- Input: File with 'AAA_MARKER' 
- Expected: File with 'BBB_MARKER' replacing 'AAA_MARKER'
- Actual: Complete Go program that would perform the replacement

## 3. Analysis and Planning Prompts

### 3.1 Todo Management (MEDIUM PRIORITY)
**Location**: `pkg/agent/todo_management.go`
**Current Prompt**: "You create specific, actionable development todos. Ground todos in workspace context and prefer referencing actual files..."
**Purpose**: Break down user intents into actionable todos
**Goal**: Create accurate, specific todos with proper tool usage

**Fallback Prompt**: "You create development todos. Keep it simple and return JSON only."

### 3.2 Build Validation
**Location**: `pkg/agent/build_validation.go` 
**Purpose**: Validate and suggest fixes for build errors
**Goal**: Accurate diagnosis and actionable fix suggestions

## 4. Interactive Commands

### 4.1 Question/Answer (LOW PRIORITY)
**Location**: `cmd/question.go`
**Current Prompt**: "You are a helpful AI assistant that answers questions about a software project..."
**Purpose**: Interactive Q&A about codebase
**Goal**: Accurate, concise answers with workspace context

### 4.2 Exec Command Generation (MEDIUM PRIORITY)
**Location**: `cmd/exec.go`
**System Prompt**: "You are an expert at generating shell commands from plain text descriptions. You only output the raw command, with no additional text or explanation."
**User Prompt**: "You are an expert in shell commands. Convert the following user input into a single, executable shell command..."
**Goal**: Generate accurate, safe shell commands

## 5. Code Review and Validation

### 5.1 Code Review (MEDIUM PRIORITY)
**Location**: Multiple locations in `pkg/codereview/`
**Purpose**: Automated code review and feedback
**Goal**: Accurate code quality assessment

### 5.2 Validation (LOW PRIORITY)
**Location**: `pkg/agent/validation.go`
**Purpose**: Validate generated code and changes
**Goal**: Catch errors before application

## Priority Rankings for Optimization

### 🔥 CRITICAL (Fix Immediately)
1. **Text Replacement Prompts** - Core functionality failing
   - Target: 100% success rate for simple replacements
   - Current: 0% success rate (generates programs instead of editing)

### 🚨 HIGH (Fix Soon)
2. **Main Code Generation** - Core editing functionality
   - Target: 95% accuracy for code modifications
   - Current: Unknown success rate, but parser issues evident

3. **Agent Code Generation** - Advanced editing with rollback
   - Target: 90% success rate for complex tasks
   - Current: Likely low based on test failures

### ⚠️ MEDIUM (Optimize When Possible)
4. **Todo Management** - Planning and task breakdown
   - Target: 90% accuracy for actionable todos
   - Current: Has fallback, but could be improved

5. **Build Validation** - Error diagnosis and fixes
   - Target: 85% accurate diagnosis
   - Current: Unknown

6. **Exec Command Generation** - Shell command creation
   - Target: 95% safe, accurate commands
   - Current: Unknown

### 📝 LOW (Future Enhancement)
7. **Question/Answer** - Interactive help
8. **Code Review** - Quality assessment
9. **Validation** - Error checking

## Optimization Strategy

1. **Start with Text Replacement** - This is completely broken and is core functionality
2. **Move to Code Generation** - Build on text replacement success
3. **Enhance Todo Management** - Improve planning accuracy
4. **Optimize remaining prompts** - Based on usage frequency and impact

## Test Scenarios Needed

For each prompt type, we need comprehensive test cases covering:
- **Happy path scenarios** - Normal, expected usage
- **Edge cases** - Unusual but valid inputs  
- **Error cases** - Invalid inputs that should be handled gracefully
- **Performance cases** - Large inputs, time constraints
- **Cost optimization** - Achieving goals with minimal token usage