import { MessageBubble, MessageSegments, MessageContent } from '@sprout/ui';
import { BrainCircuit } from 'lucide-react';
import { memo } from 'react';
import type { Message, ToolExecution } from './types';

interface MessageItemProps {
  message: Message;
  onToolPillClick?: (toolId: string) => void;
  findMatchingToolExecution: (toolName: string) => ToolExecution | undefined;
  filteredToolExecutions: ToolExecution[];
  formatTime: (date: Date) => string;
}

export const MessageItem = memo(function MessageItem({
  message,
  onToolPillClick,
  findMatchingToolExecution,
  filteredToolExecutions,
  formatTime,
}: MessageItemProps) {
  return (
    <MessageBubble
      type={message.type}
      ariaLabel={`${message.type} message`}
      copyText={message.content}
      timestamp={formatTime(message.timestamp)}
      persona={message.persona}
      depth={message.subagentDepth}
    >
      {message.type === 'assistant' ? (
        <>
          {message.reasoning && message.reasoning.trim() && (
            <details className="reasoning-block" open={false}>
              <summary className="reasoning-summary">
                <BrainCircuit size={13} className="reasoning-icon" />
                <span>Reasoning</span>
                <span className="reasoning-toggle">▶</span>
              </summary>
              <div className="reasoning-content">
                <MessageContent content={message.reasoning} />
              </div>
            </details>
          )}
          <MessageSegments
            content={message.content}
            toolRefs={message.toolRefs}
            onToolRefClick={onToolPillClick}
            onToolClick={(toolName) => {
              const matchingTool = findMatchingToolExecution(toolName);
              if (matchingTool) {
                onToolPillClick?.(matchingTool.id);
              }
            }}
            getToolStatus={(toolId) => {
              const te = filteredToolExecutions.find((t) => t.id === toolId);
              return te?.status;
            }}
          />
        </>
      ) : (
        <MessageContent content={message.content} />
      )}
    </MessageBubble>
  );
});
