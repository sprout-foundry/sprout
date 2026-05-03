/**
 * Message Segment Types
 *
 * These types define the structure of message segments used for parsing
 * and rendering streamed assistant messages in the chat UI.
 *
 * Import from `@sprout/ui` to use these types across your application.
 */

import type { TodoItem } from './chat';

// ============================================================================
// Message Segment Types
// ============================================================================

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
  todos: TodoItem[];
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
