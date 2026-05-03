/**
 * Message Segment Parsing Module
 *
 * This module re-exports from @sprout/ui — the shared canonical source.
 * All message segment types and parsing utilities are defined in packages/ui.
 */

// Re-export from @sprout/ui — shared canonical source
export {
  parseMessageSegments,
  type TextSegment,
  type ToolCallSegment,
  type TodoUpdateSegment,
  type ProgressSegment,
  type ResultSegment,
  type MessageSegment,
} from '@sprout/ui';
