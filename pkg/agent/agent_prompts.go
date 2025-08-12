package agent

import "fmt"

func joinLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	s := ""
	for i, ln := range lines {
		if i > 0 {
			s += "\n"
		}
		s += ln
	}
	return s
}

// BuildIntentAnalysisPrompt returns the prompt for intent analysis
func BuildIntentAnalysisPrompt(userIntent string, projectType string, relevantFiles []string) string {
	return fmt.Sprintf(`Analyze this user intent and classify it for optimal execution:

User Intent: %s

WORKSPACE ANALYSIS:
Project Type: %s
Total Files: %d
Available Source Files in Workspace:
%s

CRITICAL WORKSPACE CONSTRAINTS:
- This is a %s project - do NOT suggest files with mismatched extensions
- All file paths must be relative to project root
- Only suggest modifications to EXISTING files shown above
- Do NOT create new files unless explicitly requested
- Verify file extensions match project type

IMMEDIATE EXECUTION OPTIMIZATION:
IMPORTANT: Be VERY conservative with immediate execution. Only use for tasks that are:
1. Pure information gathering (no code modification)
2. Can be completed with a single shell command
3. Don't require any code analysis or understanding

ONLY set "CanExecuteNow": true for these VERY LIMITED cases:
- Explicit file listing requests: "list source files in directory" → "find . -name '*.ext' -type f"
- Direct search queries: "find all TODO comments" → "grep -r -i -n 'TODO' ."
- Simple directory structure: "show directory structure" → "ls -R"
- Count queries: "how many files" → "find . -name '*.ext' | wc -l"
- Function listing: "show functions in file.go" → "grep -n '^func ' file.go"
- Import viewing: "show imports in main.go" → "grep -A 10 '^import' main.go"
- Basic file inspection: "check if go.mod exists" → "ls -la go.mod"

DO NOT use immediate execution for:
- ANY code modification tasks
- ANY analysis tasks ("analyze", "review", "check", "fix")
- ANY tasks requiring understanding of code content
- ANY tasks that might need file editing
- Any ambiguous requests that could involve code changes

Respond with JSON:
{
  "Category": "code|fix|docs|test|review",
  "Complexity": "simple|moderate|complex",
  "EstimatedFiles": ["file1.ext", "file2.ext"],
  "RequiresContext": true|false,
  "CanExecuteNow": false,
  "ImmediateCommand": ""
}

CRITICAL: Default to "CanExecuteNow": false unless the task is clearly a simple shell command for information gathering.


Classification Guidelines:
- "simple": Single file edit, clear target, specific change
- "moderate": 2-5 files, some analysis needed, well-defined scope
- "complex": Multiple files, requires planning, unclear scope

Only include files in estimated_files that are highly likely to be modified.
ALL files must be existing source files from the workspace above.`,
		userIntent,
		projectType,
		len(relevantFiles),
		joinLines(relevantFiles),
		projectType)
}

// BuildProgressEvaluationPrompt returns the prompt for progress evaluation
func BuildProgressEvaluationPrompt(contextSummary string) string {
	return fmt.Sprintf(`You are an intelligent agent evaluating progress on a software development task.


%s

TASK: Evaluate the current progress and decide the next action.

CRITICAL FOR REFACTORING TASKS:
- Creating skeleton files is only 20%% of the work
- A refactoring is NOT complete until the source file is significantly reduced

ANALYSIS REQUIREMENTS:
1. **Progress Assessment**: What percentage of the original task is complete?
2. **Current Status**: Is the agent on track, needs adjustment, has critical errors, or is completed?
3. **Next Action Decision**: Based on the current state, what should happen next?
4. **Reasoning**: Why is this the best next action?

NOTE: Only report concerns for critical_error status - avoid unnecessary warnings about normal progress.

AVAILABLE NEXT ACTIONS:
- "continue": Proceed with the current plan (if we have one and no major issues)
- "analyze_intent": Start with intent analysis (if no analysis has been done)
- "create_plan": Create or recreate an edit plan (if no plan or plan needs revision)
- "execute_edits": Execute the planned edit operations (if plan exists but edits not started)
- "run_command": Execute shell commands for investigation or validation (specify commands)
- "validate": Run validation checks on completed work
- "escalate": Hand off to full orchestration for complex issues
- "revise_plan": Create a new plan based on learnings (if current plan is inadequate)
- "completed": Task is successfully completed

DECISION LOGIC:
- **EARLY TERMINATION**: For "review"/"analysis" tasks that don't modify code: if investigation completed successfully, choose "completed"
- **EDIT EXECUTION PRIORITY**: If plan exists AND no edits executed (no "Edit operation completed" in operations): MUST return "execute_edits"
- **STOP PLANNING LOOPS**: If plan exists AND multiple "Plan created/revised" operations but no actual edits: MUST return "execute_edits"
- **PREVENT VALIDATION LOOPS**: If edits completed but validation failed: consider "completed" if main task accomplished
- If iteration 1 and no intent analysis: "analyze_intent"
- REFACTORING TASKS: If intent contains "refactor", "extract", "move", "split", "reorganize" AND intent analysis done: proceed to "create_plan"
- If investigation/search/analysis task WITHOUT refactoring intent AND commands already executed: "completed"
- If intent analysis done but no plan AND task requires code changes: "create_plan"  
- If edits done and simple task: "completed" (skip validation for simple changes)
- If errors occurred: assess if they can be handled or need "revise_plan"
- If task appears complete: "completed"
- If current approach isn't working after 5+ iterations of analysis: "create_plan" to force progress
- If stuck in planning loop (3+ revise_plan actions): "execute_edits" to force execution
- If current approach isn't working: "run_command" for investigation or "revise_plan"

Respond with JSON:
{
  "status": "on_track|needs_adjustment|critical_error|completed",
  "completion_percentage": 0-100,
  "next_action": "continue|analyze_intent|create_plan|execute_edits|run_command|validate|revise_plan|completed",

  "reasoning": "detailed explanation of why this action is best",
  "concerns": [], // only include for critical_error status
  "commands": ["command1", "command2"] // only if next_action is "run_command"
}`, contextSummary)
}
