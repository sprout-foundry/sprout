# Todo Workflow Context Module

## Complex Task Management

### When to Use Todos

**Use Todos For**:
- Tasks with 3+ distinct steps
- Multiple files need modification
- Keywords: "implement", "refactor", "build", "create", "fix multiple"
- Task will take multiple iterations
- Building/testing is required

**Don't Use Todos For**:
- Simple questions or explanations
- Single file reads  
- One-step operations
- Basic information requests

### Todo Workflow Pattern

**Step 1: Task Breakdown**
- Use add_todos to create specific, actionable steps
- Each todo should be a concrete deliverable
- Order todos by dependencies

**Step 2: Execution Tracking**
- Mark todo as "in_progress" IMMEDIATELY when starting work
- Only one todo should be "in_progress" at a time
- Work on todo until completely finished

**Step 3: Completion Tracking**  
- Mark todo as "completed" IMMEDIATELY after finishing
- Don't batch completions - mark each as done when done
- Use list_todos to see what's next

### Example Todo Workflows

**User Request**: "Add user authentication to the API"
```
1. add_todos: ["Create user model", "Add login endpoint", "Add auth middleware", "Write tests"]
2. Mark "Create user model" → in_progress → work → completed
3. Mark "Add login endpoint" → in_progress → work → completed  
4. Continue until all todos completed
```

**Complex Implementation**:
```
Todos for "Implement file upload system":
- Create upload endpoint handler
- Add file validation logic
- Implement storage integration
- Add error handling and logging
- Write unit tests
- Update API documentation
```

### Todo Management Rules

**Progression**: Only mark as completed when work is fully done
**Focus**: Work on one todo at a time (only one "in_progress")
**Tracking**: Use list_todos regularly to see progress
**Completion**: Don't move to next todo until current one is complete