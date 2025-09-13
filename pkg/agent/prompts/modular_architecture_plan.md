# Modular Prompt Architecture Plan (Option B)

## OVERVIEW

This plan outlines a modular system where the agent's system prompt is dynamically composed based on:
- Request type detection
- Task complexity analysis  
- Context requirements (debugging, testing, etc.)
- User preferences and workspace patterns

**Goal**: Optimal prompt length and focus for each specific task, reducing cognitive load while maintaining comprehensive coverage.

## ARCHITECTURE DESIGN

### Core Components

**1. Base Prompt (Always Included)**
- Basic agent identity and capabilities
- Fundamental tool usage patterns
- Core success principles
- ~60 lines / 500 tokens

**2. Request Type Modules**
```
modules/
├── exploratory.md          # For understanding/explanation tasks
├── implementation.md       # For coding/building tasks  
├── debugging.md           # For fixing/troubleshooting tasks
├── testing.md             # For test-related work
└── analysis.md            # For code analysis/review tasks
```

**3. Context Modules**  
```
context/
├── circuit_breakers.md    # Infinite loop prevention
├── batch_operations.md    # File reading efficiency  
├── todo_workflow.md       # Complex task management
├── test_debugging.md      # Test failure methodology
└── workspace_context.md   # Codebase understanding
```

**4. Capability Modules**
```
capabilities/
├── web_search.md         # Web search and grounding
├── ui_analysis.md        # Screenshot and UI work
├── multi_file_ops.md     # Complex file operations
└── orchestration.md      # Multi-agent coordination
```

## DYNAMIC COMPOSITION ALGORITHM

### Phase 1: Request Analysis
```go
type RequestClassification struct {
    PrimaryType    string    // exploratory, implementation, debugging, etc.
    Complexity     string    // simple, medium, complex
    RequiredTools  []string  // shell, file_ops, web_search, etc.
    ContextNeeds   []string  // debugging, testing, batch_ops, etc.
}
```

### Phase 2: Module Selection
**Base Logic**:
- Always include Base Prompt
- Include Primary Type module
- Add Context modules based on request patterns
- Add Capability modules based on required tools

**Selection Examples**:
```
Request: "Fix the failing test in user_test.go"
→ Base + Debugging + Testing + CircuitBreakers + TestDebugging

Request: "Tell me about the authentication system"  
→ Base + Exploratory + BatchOperations + WorkspaceContext

Request: "Implement user registration with validation"
→ Base + Implementation + TodoWorkflow + BatchOperations + Testing
```

### Phase 3: Prompt Assembly
```go
func AssemblePrompt(classification RequestClassification) string {
    sections := []string{
        LoadModule("base.md"),
        LoadModule("modules/" + classification.PrimaryType + ".md"),
    }
    
    for _, context := range classification.ContextNeeds {
        sections = append(sections, LoadModule("context/" + context + ".md"))
    }
    
    return strings.Join(sections, "\n\n")
}
```

## IMPLEMENTATION STRATEGY

### Phase 1: Foundation (Week 1)
1. **Create Module Files**: Extract sections from current v3_optimized.md into focused modules
2. **Request Classifier**: Implement keyword-based classification logic
3. **Module Loader**: Simple file-based module loading system
4. **Basic Assembly**: Combine base + primary type module

### Phase 2: Context Intelligence (Week 2)  
1. **Context Detection**: Analyze request for debugging, testing, complexity indicators
2. **Smart Defaults**: Define default module combinations for common scenarios
3. **Module Dependencies**: Implement module dependency resolution (e.g., debugging → circuit_breakers)

### Phase 3: Optimization (Week 3)
1. **Usage Analytics**: Track which module combinations work best
2. **Dynamic Sizing**: Adjust module inclusion based on context window constraints
3. **Performance Testing**: A/B test modular vs monolithic prompts

### Phase 4: Advanced Features (Future)
1. **Learning System**: ML-based module selection based on success patterns
2. **User Preferences**: Allow users to customize default module combinations
3. **Workspace Adaptation**: Adjust modules based on codebase characteristics

## FILE STRUCTURE

```
pkg/agent/prompts/
├── base.md                     # Core prompt (always included)
├── modules/                    # Request type specific
│   ├── exploratory.md
│   ├── implementation.md
│   ├── debugging.md
│   ├── testing.md
│   └── analysis.md
├── context/                    # Context-aware additions
│   ├── circuit_breakers.md
│   ├── batch_operations.md
│   ├── todo_workflow.md
│   ├── test_debugging.md
│   └── workspace_context.md
├── capabilities/               # Tool-specific guidance
│   ├── web_search.md
│   ├── ui_analysis.md
│   ├── multi_file_ops.md
│   └── orchestration.md
└── loader/                     # Assembly system
    ├── classifier.go
    ├── assembler.go
    └── module_loader.go
```

## BENEFITS

### Immediate Benefits
- **Reduced Cognitive Load**: Agent sees only relevant instructions
- **Faster Processing**: Smaller, focused prompts process quicker
- **Better Focus**: Clear priorities without competing guidance
- **Easier Maintenance**: Update specific modules without affecting others

### Long-term Benefits  
- **Adaptive Complexity**: System learns optimal combinations over time
- **Personalization**: Users can customize their agent's behavior patterns
- **Specialized Workflows**: Different modules for different development patterns
- **A/B Testing**: Easy to test prompt variations and measure impact

## MIGRATION STRATEGY

1. **Parallel Development**: Build modular system alongside current v3_optimized
2. **Gradual Rollout**: Start with simple request types (exploratory)
3. **Fallback System**: Use monolithic prompt if module system fails
4. **Performance Monitoring**: Compare success rates between systems
5. **User Opt-in**: Allow users to choose between modular and monolithic

## SUCCESS METRICS

- **Task Completion Rate**: % of tasks completed successfully
- **Iteration Efficiency**: Average tool calls per completed task
- **Error Reduction**: Fewer infinite loops and stuck states  
- **Response Quality**: User satisfaction with agent responses
- **Processing Speed**: Time to first meaningful action

This modular architecture will allow us to optimize the agent experience for each specific use case while maintaining comprehensive capabilities when needed.