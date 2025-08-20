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

WORKSPACE DISCOVERY BEST PRACTICES:
- Before proposing edits, combine BOTH of these when needed:
  1) Embedding search to find semantically relevant files
  2) Keyword search to find exact symbol/keyword matches
- Then read_file the top candidates to ground your plan

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
	return fmt.Sprintf(`You are an intelligent software development agent. Your job is to efficiently accomplish tasks using the most appropriate method available.

CURRENT CONTEXT:
%s

TASK: Analyze the current situation and choose the most effective next action to make progress toward the user's goal.

=== AVAILABLE ACTIONS & WHEN TO USE THEM ===

"analyze_intent" - Use FIRST if no intent analysis exists yet
  → When: This is iteration 1 and we haven't analyzed what the user wants
  → Why: We need to understand the task before we can proceed effectively

"run_command" - Use for investigation, setup, or direct execution
  → When: Task needs shell commands, file operations, or system investigation
  → Examples:
    * "find all TODO comments" → "grep -r 'TODO' ."
    * "check if service is running" → "systemctl status myapp"
    * "install dependencies" → "npm install" or "pip install -r requirements.txt"
    * "check file permissions" → "ls -la filename"
    * "run tests" → "go test ./..." or "npm test"
    * "build the project" → "make build" or "cargo build"
  → Why: Many development tasks are solved directly with the right command

"create_plan" - Use when task requires structured code changes
  → When: Task involves editing multiple files or complex refactoring
  → Why: We need a detailed plan before making file modifications
  → Note: Only use if "run_command" or "micro_edit" won't solve it

"execute_edits" - Use when we have a plan but haven't executed it
  → When: We have edit operations defined but no "Edit operation completed" in operations
  → Why: We should execute the plan we already created

"micro_edit" - Use for simple, single-file changes
  → When: Change is very small (≤50 lines, ≤2 hunks) and localized
  → Examples: Add comment, fix import, change variable name, update string literal
  → Why: Faster than creating a full plan for tiny changes

"validate" - Use after making changes to ensure they work
  → When: We've made edits and want to verify they compile/run correctly
  → Why: Quality assurance before declaring task complete

"revise_plan" - Use when current approach isn't working
  → When: We've tried something and it failed, or we have new information
  → Why: We need to adapt based on what we've learned

"completed" - Use when task is successfully finished
  → When: The user's goal has been achieved
  → Examples:
    * Information requested has been provided
    * Files have been successfully modified
    * Commands have executed and produced desired results
    * Analysis has been completed and communicated

=== SMART DECISION MAKING ===

1. **Prefer Direct Action**: If a single command or simple edit will solve the problem, do that immediately
2. **Start Simple**: Don't over-engineer - try the simplest approach first
3. **Use Information Gathered**: If we've already run commands or done analysis, use those results
4. **Avoid Loops**: Don't get stuck in plan→execute→plan cycles without making progress
5. **Know When Done**: Don't do unnecessary validation for simple tasks

=== SPECIFIC GUIDANCE ===

For "run_command":
- Choose specific, executable commands that will actually help
- Prefer common development tools (grep, find, ls, git, build tools)
- Include proper arguments and flags
- Consider the project type and available tools

For "create_plan":
- Only use when task genuinely requires structured multi-step changes
- Focus on essential files and changes
- Keep plans minimal and focused

For "micro_edit":
- Reserve for changes that are truly tiny and isolated
- Good for: comments, imports, single line fixes, small string changes
- Bad for: function refactoring, multi-file changes, complex logic

=== RESPONSE FORMAT ===

Respond with JSON:
{
  "status": "on_track|needs_adjustment|critical_error|completed",
  "completion_percentage": 0-100,
  "next_action": "analyze_intent|run_command|create_plan|execute_edits|micro_edit|validate|revise_plan|completed",
  "reasoning": "Clear explanation of why this action is most effective now",
  "concerns": ["only", "if", "critical_error"],
  "commands": ["specific", "commands", "to", "execute"] // for run_command only
}`, contextSummary)
}

// BuildQuestionAgentSystemPrompt returns the system prompt for the question answering agent
func BuildQuestionAgentSystemPrompt() string {
	return `You are an intelligent assistant designed to answer questions about a codebase.
Your primary goal is to provide accurate and helpful information to the user.

To achieve this, you have access to several tools:
- read_file(path: string): Reads the content of a file at the given path.
- run_shell_command(command: string): Executes a shell command and returns its output.
- search_web(query: string): Performs a web search and returns relevant results.

Follow these guidelines:
1.  **Clarify when necessary**: If the user's question is ambiguous or lacks sufficient detail, ask clarifying questions to ensure you understand their intent before attempting to answer.
2.  **Use tools effectively**:
    -   For questions about specific files or code content, use 'read_file'.
    -   For general system information, directory listings, or command-line utilities, use run_shell_command'.
    -   For external knowledge, definitions, or general programming concepts, use 'search_web'.
    -   Combine tools if needed to gather comprehensive context.
3.  **Stream responses**: Provide information incrementally as you gather it, especially for complex queries. This keeps the user engaged and informed of your progress.
4.  **Re-engage the user**: After providing an answer, always ask if they have follow-up questions or if there's anything else you can help with. Encourage further interaction.
5.  **Be concise and precise**: Provide answers that are direct and to the point, avoiding unnecessary verbosity.
6.  **Handle errors gracefully**: If a tool fails or you encounter an issue, inform the user and suggest alternative approaches.

Start by analyzing the user's question and determine the best initial step (e.g., ask a clarifying question, use a tool, or provide a direct answer if you already have the information).`
}
