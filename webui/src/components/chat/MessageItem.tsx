import { MessageBubble, MessageSegments, MessageContent } from '@sprout/ui';
import { BrainCircuit } from 'lucide-react';
import { memo } from 'react';
import type { Message, ToolExecution } from './types';

interface MessageItemProps {
  message: Message;
  onToolPillClick?: (toolId: string) => void;
  findMatchingToolExecution: (toolName: string) => ToolExecution | undefined;
  /**
   * Status lookup that spans ALL tool executions, not just the current
   * query's. Decides whether a tool segment renders as the running pill
   * or the completed footnote — if this falls back to undefined for a
   * past-query tool, the badge regresses to the pill and visibly flickers.
   */
  getToolStatus: (toolId: string) => ToolExecution['status'] | undefined;
  formatTime: (date: Date) => string;
}

export const MessageItem = memo(function MessageItem({
  message,
  onToolPillClick,
  findMatchingToolExecution,
  getToolStatus,
  formatTime,
}: MessageItemProps) {
  // Suppress empty bubbles. Session restore replays the assistant turn
  // boundaries verbatim, including tool-only turns whose persisted
  // `content` is "" — those would otherwise render as a bubble with
  // nothing but a timestamp and a copy button, looking broken. We only
  // drop the row when there is truly nothing to render: no prose, no
  // tool segments (toolRefs), and no reasoning to expand. Tool-only
  // turns with at least one toolRef still render so the user can see
  // the [tool] footnotes that closed the turn. Same guard for user
  // turns — defensive, since the restore path also yields empty user
  // content in rare cases.
  const hasContent = !!message.content && message.content.trim().length > 0;
  const hasReasoning = !!message.reasoning && message.reasoning.trim().length > 0;
  const hasToolRefs = !!message.toolRefs && message.toolRefs.length > 0;
  if (!hasContent && !hasReasoning && !hasToolRefs) {
    return null;
  }
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
            getToolStatus={getToolStatus}
          />
        </>
      ) : (
        <MessageContent content={message.content} />
      )}
    </MessageBubble>
  );
});
