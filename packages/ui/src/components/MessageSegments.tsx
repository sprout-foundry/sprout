import React, { type ReactNode } from 'react';
import { ExternalLink, CheckCircle, Circle, Loader2, Minus, Wrench, Bot, Terminal, BookOpen, Pencil, FileEdit, Search, Eye, FlaskConical, Globe, ArrowDown, ClipboardList, ScrollText, RotateCcw } from 'lucide-react';
import { parseMessageSegments, type MessageSegment } from '../utils/messageSegments';
import { stripAnsiCodes } from '../utils/ansi';
import MessageContent from './MessageContent';
import type { ToolRef } from '../types/chat';

export interface MessageSegmentsProps {
  content: string;
  toolRefs?: ToolRef[];
  onToolClick?: (toolName: string) => void;
  onToolRefClick?: (toolId: string) => void;
  /** Returns the status of a tool execution by ID, used to show footnote vs pill. */
  getToolStatus?: (toolId: string) => string | undefined;
}

const getToolIcon = (toolName: string): ReactNode => {
  const iconMap: { [key: string]: ReactNode } = {
    'shell_command': <Terminal size={14} />,
    'read_file': <BookOpen size={14} />,
    'write_file': <Pencil size={14} />,
    'edit_file': <FileEdit size={14} />,
    'search_files': <Search size={14} />,
    'analyze_ui_screenshot': <Eye size={14} />,
    'analyze_image_content': <FlaskConical size={14} />,
    'web_search': <Globe size={14} />,
    'fetch_url': <ArrowDown size={14} />,
    'TodoWrite': <ClipboardList size={14} />,
    'TodoRead': <ClipboardList size={14} />,
    'view_history': <ScrollText size={14} />,
    'rollback_changes': <RotateCcw size={14} />,
    'mcp_tools': <Wrench size={14} />,
    'run_subagent': <Bot size={14} />,
    'run_parallel_subagents': <Bot size={14} />,
  };
  return iconMap[toolName] || <Wrench size={14} />;
};

const SHORT_TOOL_NAMES: { [key: string]: string } = {
  'read_file': 'read',
  'write_file': 'write',
  'edit_file': 'edit',
  'shell_command': 'shell',
  'search_files': 'search',
  'analyze_ui_screenshot': 'UI screenshot',
  'analyze_image_content': 'analyze image',
  'web_search': 'web search',
  'fetch_url': 'fetch url',
  'TodoWrite': 'todo write',
  'TodoRead': 'todo read',
  'view_history': 'history',
  'rollback_changes': 'rollback',
  'mcp_tools': 'mcp',
  'run_subagent': 'subagent',
  'run_parallel_subagents': 'subagents',
};

const getShortToolName = (toolName: string): string => {
  if (SHORT_TOOL_NAMES[toolName]) return SHORT_TOOL_NAMES[toolName];
  // Fallback: replace underscores with spaces, max 2 words
  const words = toolName.replace(/_/g, ' ').split(' ');
  return words.slice(0, 2).join(' ');
};

const MessageSegments: React.FC<MessageSegmentsProps> = ({ content, toolRefs = [], onToolClick, onToolRefClick, getToolStatus }) => {
  let segments: MessageSegment[];
  try {
    segments = parseMessageSegments(stripAnsiCodes(content));
  } catch {
    return <MessageContent content={content} />;
  }

  const unclaimedRefs = [...toolRefs];

  const claimMatchingToolRef = (segmentToolName: string) => {
    const normalizedSegmentName = segmentToolName.split('(')[0].trim();
    const matchIndex = unclaimedRefs.findIndex((ref) => ref.toolName === normalizedSegmentName);
    if (matchIndex < 0) {
      return null;
    }
    const [match] = unclaimedRefs.splice(matchIndex, 1);
    return match;
  };

  return (
    <div className="message-segments">
      {segments.map((segment, idx) => {
        switch (segment.type) {
          case 'text':
            return (
              <div key={`seg-${idx}`} className="segment-text">
                <MessageContent content={segment.content} />
              </div >
            );
          case 'tool_call': {
            const matchingRef = claimMatchingToolRef(segment.toolName);
            const baseName = segment.toolName.split('(')[0];
            // When the chat provides getToolStatus AND we have a matching toolRef,
            // a returned `undefined` means the tool ran in a previous turn whose
            // execution state has been cleared (handleQueryStart resets
            // state.toolExecutions to [] on every new query). Treat that as done
            // so the badge stays in compact footnote form instead of regressing
            // to the running pill on every subsequent query — that regression was
            // the visible "tool call badges show then hide every few seconds"
            // flicker. Consumers that don't pass getToolStatus (e.g. unit tests
            // exercising a bare pill) keep the original render path.
            const hasStatusLookup = !!matchingRef && !!getToolStatus;
            const toolStatus = hasStatusLookup ? getToolStatus!(matchingRef!.toolId) : undefined;
            const isDone =
              hasStatusLookup &&
              (toolStatus === undefined || toolStatus === 'completed' || toolStatus === 'error');

            if (isDone && matchingRef) {
              // Completed tool: render a compact footnote superscript link [shortname]
              return (
                <span
                  key={`seg-${idx}`}
                  className={`segment-tool-footnote${toolStatus === 'error' ? ' segment-tool-footnote--error' : ''}`}
                  role="button"
                  tabIndex={0}
                  onClick={() => onToolRefClick?.(matchingRef.toolId)}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter' || e.key === ' ') {
                      e.preventDefault();
                      onToolRefClick?.(matchingRef.toolId);
                    }
                  }}
                  title={matchingRef.label}
                  aria-label={`View tool: ${matchingRef.label}`}
                >
                  [{getShortToolName(baseName)}]
                </span >
              );
            }

            return (
              <span
                key={`seg-${idx}`}
                className="segment-tool-call"
                role={matchingRef || onToolClick ? 'button' : undefined}
                tabIndex={matchingRef || onToolClick ? 0 : undefined}
                onClick={() => {
                  if (matchingRef) {
                    onToolRefClick?.(matchingRef.toolId);
                    return;
                  }
                  onToolClick?.(segment.toolName);
                }}
                onKeyDown={(e) => {
                  if ((matchingRef || onToolClick) && (e.key === 'Enter' || e.key === ' ')) {
                    e.preventDefault();
                    if (matchingRef) {
                      onToolRefClick?.(matchingRef.toolId);
                      return;
                    }
                    onToolClick?.(segment.toolName);
                  }
                }}
                title={matchingRef ? matchingRef.label : segment.summary || segment.toolName}
              >
                <span className="tool-pill-icon">{getToolIcon(baseName)}</span >
                <span className="tool-pill-name">{getShortToolName(baseName)}</span >
                <ExternalLink size={10} className="tool-pill-link-icon" />
              </span >
            );
          }

          case 'todo_update':
            return (
              <div key={`seg-${idx}`} className="segment-todo-summary">
                {segment.todos.map((todo, todoIdx) => (
                  <span key={`todo-${todoIdx}`} className={`inline-todo inline-todo-${todo.status}`}>
                    <span className="inline-todo-icon">
                      {todo.status === 'completed' ? <CheckCircle size={10} /> :
                       todo.status === 'in_progress' ? <Loader2 size={10} /> :
                       todo.status === 'cancelled' ? <Minus size={10} /> :
                       <Circle size={10} />}
                    </span >
                    {todo.content}
                  </span >
                ))}
              </div >
            );

          case 'progress':
          case 'result':
            return null;

          default:
            return null;
        }
      })}
    </div >
  );
};

export default MessageSegments;
