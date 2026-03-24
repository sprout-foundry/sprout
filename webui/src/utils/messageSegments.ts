/**
 * Message Segment Parsing Module
 * 
 * This module provides utilities for parsing raw streamed assistant messages
 * into structured segments for rendering in the UI.
 */

// ============================================================================
// Types
// ============================================================================

/**
 * A todo item representing a task with its status
 */
export interface TodoItem {
  id: string;
  content: string;
  status: string; // 'pending', 'in_progress', 'completed', etc.
}

/**
 * A text segment containing plain prose (markdown-ready)
 */
export interface TextSegment {
  type: 'text';
  content: string;
}

/**
 * A tool call segment representing a tool execution
 */
export interface ToolCallSegment {
  type: 'tool_call';
  toolId: string;
  toolName: string;
  summary?: string;
}

/**
 * A todo update segment containing a list of todos
 */
export interface TodoUpdateSegment {
  type: 'todo_update';
  todos: Array<{ id: string; content: string; status: string }>;
}

/**
 * A progress segment showing agent activity
 */
export interface ProgressSegment {
  type: 'progress';
  message: string;
  details?: string;
}

/**
 * A result segment showing a completion or error status
 */
export interface ResultSegment {
  type: 'result';
  label: string;
  content: string;
}

/**
 * Discriminated union type representing all message segment types
 */
export type MessageSegment = 
  | TextSegment
  | ToolCallSegment
  | TodoUpdateSegment
  | ProgressSegment
  | ResultSegment;

// ============================================================================
// Pattern Regexes
// ============================================================================

/**
 * Pattern for tool execution lines: "executing tool [ToolName]"
 */
const TOOL_EXEC_PATTERN = /\[executing tool \[([^\]]+)\]/;

/**
 * Pattern for progress lines with percentage: "[0 - 2%] executing tool"
 */
const PROGRESS_PERCENT_PATTERN = /^\[\d+\s+-\s+\d+%\]/;

/**
 * Pattern for tool result lines: "[OK] Completed in 1.2m" or "[FAIL] Error message"
 */
const TOOL_RESULT_PATTERN = /^\[(OK|FAIL|edit)\]\s*(.*)$/;

/**
 * Pattern for todo lines: "   [x] Task description" or "   [ ] Task description"
 */
const TODO_LINE_PATTERN = /^\s*\[([x ~-])\]\s+(.+)$/;

/**
 * Pattern for subagent progress: "... Processing (elapsed: 3s, tokens: 3924) ..."
 */
const SUBAGENT_PROGRESS_PATTERN = /\.\.\s*Processing\s*\([^)]*\)/;

/**
 * Pattern for simple progress: "Processing..." or "Running..."
 */
const SIMPLE_PROGRESS_PATTERN = /^(Processing|Running|Executing|Generating|Writing|Reading|Creating|Updating|Deleting)\.\.\.?$/i;

/**
 * Pattern to extract tool name and arguments from tool call line
 */
const TOOL_CALL_DETAIL_PATTERN = /executing tool \[([^\]]+)\]/;

// ============================================================================
// Helper Functions
// ============================================================================

/**
 * Extract a human-readable summary from a tool call line
 */
function extractToolSummary(toolLine: string): string {
  const match = toolLine.match(TOOL_CALL_DETAIL_PATTERN);
  if (!match) return 'tool execution';

  const toolPart = match[1];
  const parts = toolPart.split(' ');
  const toolName = parts[0] || 'tool';
  
  // Truncate arguments if present
  const args = parts.slice(1).join(' ');
  const summary = args ? `${toolName}(${args.substring(0, 50)}${args.length > 50 ? '...' : ''})` : toolName;
  
  return summary;
}

/**
 * Parse a todo line into a TodoItem
 */
function parseTodoLine(line: string): TodoItem | null {
  const match = line.match(TODO_LINE_PATTERN);
  if (!match) return null;

  const statusSymbol = match[1];
  const content = match[2].trim();

  let status: string;
  switch (statusSymbol) {
    case 'x':
      status = 'completed';
      break;
    case '~':
      status = 'in_progress';
      break;
    case '-':
      status = 'cancelled';
      break;
    default:
      status = 'pending';
  }

  return {
    id: `todo-${Date.now()}-${Math.random().toString(36).substr(2, 9)}`,
    content,
    status
  };
}

/**
 * Check if a line is a tool execution line
 */
function isToolExecutionLine(line: string): boolean {
  return TOOL_EXEC_PATTERN.test(line);
}

/**
 * Check if a line is a progress percentage line
 */
function isProgressPercentLine(line: string): boolean {
  return PROGRESS_PERCENT_PATTERN.test(line);
}

/**
 * Check if a line is a tool result line
 */
function isToolResultLine(line: string): boolean {
  return TOOL_RESULT_PATTERN.test(line);
}

/**
 * Check if a line is a todo line
 */
function isTodoLine(line: string): boolean {
  return TODO_LINE_PATTERN.test(line);
}

/**
 * Check if a line is a subagent progress line
 */
function isSubagentProgressLine(line: string): boolean {
  return SUBAGENT_PROGRESS_PATTERN.test(line);
}

/**
 * Check if a line is a simple progress line
 */
function isSimpleProgressLine(line: string): boolean {
  return SIMPLE_PROGRESS_PATTERN.test(line);
}

/**
 * Check if a line is a progress-related line (any type)
 */
function isProgressLine(line: string): boolean {
  return isProgressPercentLine(line) || isSubagentProgressLine(line) || isSimpleProgressLine(line);
}

// ============================================================================
// Main Parser
// ============================================================================

/**
 * Parse raw streamed message content into structured segments
 * 
 * This function analyzes the raw text (after ANSI stripping) and categorizes
 * each line into appropriate segments:
 * - Tool execution traces are grouped into tool_call segments
 * - Todo lists are grouped into todo_update segments
 * - Progress indicators become progress segments
 * - Tool results become result segments
 * - Remaining lines are preserved as text segments
 * 
 * @param rawContent - The raw streamed message content (ANSI codes should be stripped first)
 * @returns Array of MessageSegment objects organized by type
 */
export function parseMessageSegments(rawContent: string): MessageSegment[] {
  const lines = rawContent.split('\n');
  const segments: MessageSegment[] = [];
  
  let i = 0;
  while (i < lines.length) {
    const line = lines[i];
    const trimmedLine = line.trim();
    
    // Skip empty lines
    if (!trimmedLine) {
      i++;
      continue;
    }

    // Check for tool execution lines (group consecutive ones)
    if (isToolExecutionLine(line)) {
      const toolLines: string[] = [line];
      i++;
      
      // Group consecutive tool execution lines
      while (i < lines.length && isToolExecutionLine(lines[i])) {
        toolLines.push(lines[i]);
        i++;
      }
      
      // Also include progress lines that are part of the tool execution block
      while (i < lines.length && isProgressPercentLine(lines[i])) {
        toolLines.push(lines[i]);
        i++;
      }
      
      // Extract the main tool name from the first line
      const toolName = extractToolSummary(toolLines[0]);
      
      segments.push({
        type: 'tool_call',
        toolId: `tool-${Date.now()}-${Math.random().toString(36).substr(2, 9)}`,
        toolName,
        summary: toolName
      });
      continue;
    }

    // Check for tool result lines
    if (isToolResultLine(line)) {
      const match = line.match(TOOL_RESULT_PATTERN);
      if (match) {
        const resultType = match[1]; // 'OK', 'FAIL', or 'edit'
        const content = match[2].trim();
        
        segments.push({
          type: 'result',
          label: `[${resultType}]`,
          content
        });
      }
      i++;
      continue;
    }

    // Check for todo lines (group consecutive ones)
    if (isTodoLine(line)) {
      const todos: Array<{ id: string; content: string; status: string }> = [];
      
      while (i < lines.length && isTodoLine(lines[i])) {
        const todo = parseTodoLine(lines[i]);
        if (todo) {
          todos.push(todo);
        }
        i++;
      }
      
      if (todos.length > 0) {
        segments.push({
          type: 'todo_update',
          todos
        });
      }
      continue;
    }

    // Check for progress lines (group consecutive ones)
    if (isProgressLine(line)) {
      const progressLines: string[] = [line];
      i++;
      
      // Group consecutive progress lines
      while (i < lines.length && isProgressLine(lines[i])) {
        progressLines.push(lines[i]);
        i++;
      }
      
      // Extract a summary from the progress lines
      const lastLine = progressLines[progressLines.length - 1];
      const message = lastLine.replace(PROGRESS_PERCENT_PATTERN, '').trim();
      
      segments.push({
        type: 'progress',
        message,
        details: progressLines.length > 1 ? progressLines.join(' ') : undefined
      });
      continue;
    }

    // Everything else is text
    const textLines: string[] = [line];
    i++;
    
    // Group consecutive text lines
    while (i < lines.length) {
      const nextLine = lines[i];
      
      // Stop if we hit another special pattern
      if (
        isToolExecutionLine(nextLine) ||
        isToolResultLine(nextLine) ||
        isTodoLine(nextLine) ||
        isProgressLine(nextLine)
      ) {
        break;
      }
      
      textLines.push(nextLine);
      i++;
    }
    
    const textContent = textLines.join('\n');
    
    // Only add non-empty text segments
    if (textContent.trim()) {
      segments.push({
        type: 'text',
        content: textContent
      });
    }
  }

  // Merge consecutive text segments
  const mergedSegments: MessageSegment[] = [];
  for (const segment of segments) {
    if (segment.type === 'text' && mergedSegments.length > 0 && 
        mergedSegments[mergedSegments.length - 1].type === 'text') {
      const prev = mergedSegments[mergedSegments.length - 1] as TextSegment;
      prev.content += '\n' + segment.content;
    } else {
      mergedSegments.push(segment);
    }
  }

  return mergedSegments;
}

// ============================================================================
// Export
// ============================================================================

// All types are already exported as interfaces above
