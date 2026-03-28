import React, { type ReactNode } from 'react';
import { ExternalLink, CheckCircle, Circle, Loader2, Minus, Wrench, Bot, Terminal, BookOpen, Pencil, FileEdit, Search, Eye, FlaskConical, Globe, ArrowDown, ClipboardList, ScrollText, RotateCcw } from 'lucide-react';
import { parseMessageSegments, type MessageSegment } from '../utils/messageSegments';
import { stripAnsiCodes } from '../utils/ansi';
import MessageContent from './MessageContent';

interface MessageSegmentsProps {
  content: string;
  toolRefs?: Array<{ toolId: string; toolName: string; label: string }>;
  onToolClick?: (toolName: string) => void;
  onToolRefClick?: (toolId: string) => void;
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

const MessageSegments: React.FC<MessageSegmentsProps> = ({ content, toolRefs = [], onToolClick, onToolRefClick }) => {
  let segments: MessageSegment[];
  try {
    segments = parseMessageSegments(stripAnsiCodes(content));
  } catch {
    return <MessageContent content={content} />;
  }

  const remainingRefs = [...toolRefs];

  const claimMatchingToolRef = (segmentToolName: string) => {
    const normalizedSegmentName = segmentToolName.split('(')[0].trim();
    const matchIndex = remainingRefs.findIndex((ref) => ref.toolName === normalizedSegmentName);
    if (matchIndex < 0) {
      return null;
    }
    const [match] = remainingRefs.splice(matchIndex, 1);
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
              </div>
            );

          case 'tool_call':
            const matchingRef = claimMatchingToolRef(segment.toolName);
            const toolLabel = matchingRef?.toolName || segment.summary || segment.toolName;
            return (
              <div
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
                title={matchingRef ? `Open ${matchingRef.toolName} details` : undefined}
              >
                <span className="tool-pill-icon">{getToolIcon(segment.toolName.split('(')[0])}</span>
                <span className="tool-pill-name">{toolLabel}</span>
                <ExternalLink size={10} className="tool-pill-link-icon" />
              </div>
            );

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
                    </span>
                    {todo.content}
                  </span>
                ))}
              </div>
            );

          case 'progress':
          case 'result':
            return null;

          default:
            return null;
        }
      })}
    </div>
  );
};

export default MessageSegments;
