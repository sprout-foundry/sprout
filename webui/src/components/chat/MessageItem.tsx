import { MessageBubble, MessageSegments, MessageContent, Collapsible } from '@sprout/ui';
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
  /**
   * SP-071-3: zero-based index of this message in the chat history.
   * Passed through to MessageBubble for data-message-index attribute.
   */
  messageIndex?: number;
  /**
   * SP-076: display verbosity for inter-tool narration filtering.
   * `compact` hides short narration messages between tool calls;
   * `default` shows everything (no filtering);
   * `verbose` shows everything with reasoning expanded inline.
   */
  outputVerbosity?: 'compact' | 'default' | 'verbose';
  /**
   * SP-076: whether this message is followed by another assistant
   * message in the chat history. When true, the message is mid-conversation
   * (narration between tool calls or interim reasoning), not the terminal
   * answer. Used by `compact` mode to hide inter-tool narration.
   */
  hasNextAssistantMessage?: boolean;
}

export const MessageItem = memo(function MessageItem({
  message,
  onToolPillClick,
  findMatchingToolExecution,
  getToolStatus,
  formatTime,
  messageIndex,
  outputVerbosity = 'default',
  hasNextAssistantMessage = false,
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

  // SP-076 compact mode: hide short narration messages that sit between
  // tool calls. These are the "Let me check..." interjections the model
  // emits before each tool invocation — useful in `verbose` for debugging,
  // noisy in `compact`. Heuristic: assistant message with toolRefs AND
  // short prose (< 120 chars) AND not the terminal answer (more
  // assistant messages follow). The terminal answer always renders even
  // if short, because there's no `hasNextAssistantMessage`.
  const isInterToolNarration =
    message.type === 'assistant' &&
    hasToolRefs &&
    hasContent &&
    message.content.length < 120 &&
    hasNextAssistantMessage;
  if (outputVerbosity === 'compact' && isInterToolNarration) {
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
      dataMessageIndex={messageIndex}
    >
      {message.type === 'assistant' ? (
        <>
          {message.reasoning && message.reasoning.trim() && (
            // SP-076: verbose mode expands reasoning inline instead of
            // hiding it behind a <details> toggle. AUDIT-GAP-1: migrated
            // to the shared <Collapsible> primitive. The legacy
            // `reasoning-block` class is preserved on the rendered
            // <details> so existing tests (MessageItem.test.tsx,
            // ChatPanel.test.tsx) keep matching the same DOM node. The
            // match is structural — the legacy summary/icon/content CSS
            // rules were retired; visual styling is now driven by
            // `.collapsible` defaults (bordered card), which is roughly
            // equivalent to the old look.
            <Collapsible
              title="Reasoning"
              icon={<BrainCircuit size={13} />}
              defaultOpen={outputVerbosity === 'verbose'}
              ariaLabel="Reasoning"
              className="reasoning-block"
            >
              <div className="reasoning-content">
                <MessageContent content={message.reasoning} />
              </div>
            </Collapsible>
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
