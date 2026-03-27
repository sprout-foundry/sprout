import React, { type ReactNode } from 'react';
import { ExternalLink, CheckCircle, Circle, Loader2, Minus, Wrench, Bot, Terminal, BookOpen, Pencil, FileEdit, Search, Eye, FlaskConical, Globe, ArrowDown, ClipboardList, ScrollText, RotateCcw } from 'lucide-react';
import { parseMessageSegments, type MessageSegment } from '../utils/messageSegments';
import { stripAnsiCodes } from '../utils/ansi';
import MessageContent from './MessageContent';

interface MessageSegmentsProps {
  content: string;
  onToolClick?: (toolName: string) => void;
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

const MessageSegments: React.FC<MessageSegmentsProps> = ({ content, onToolClick }) => {
  let segments: MessageSegment[];
  try {
    segments = parseMessageSegments(stripAnsiCodes(content));
  } catch {
    return <MessageContent content={content} />;
  }

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
            return (
              <div
                key={`seg-${idx}`}
                className="segment-tool-call"
                role={onToolClick ? 'button' : undefined}
                tabIndex={onToolClick ? 0 : undefined}
                onClick={() => onToolClick?.(segment.toolName)}
                onKeyDown={(e) => {
                  if (onToolClick && (e.key === 'Enter' || e.key === ' ')) {
                    e.preventDefault();
                    onToolClick(segment.toolName);
                  }
                }}
              >
                <span className="tool-pill-icon">{getToolIcon(segment.toolName.split('(')[0])}</span>
                <span className="tool-pill-name">{segment.summary || segment.toolName}</span>
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
