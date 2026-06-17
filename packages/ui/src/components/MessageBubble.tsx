import type { CSSProperties, ReactNode } from 'react';
import { Copy } from 'lucide-react';
import { copyToClipboard } from '../utils/clipboard';
import { getPersonaColor } from '../utils/personaColors';
import { formatCost, formatTokens } from '../utils/formatResourceUsage';

interface MessageBubbleProps {
  type?: 'user' | 'assistant';
  ariaLabel: string;
  copyText?: string;
  timestamp?: string;
  /**
   * SP-053-1d: persona ID of the agent that produced this message
   * (e.g. "coder", "tester"). When set, a colored badge is rendered in
   * the bubble header. Absent for primary-agent messages.
   */
  persona?: string;
  /**
   * SP-053-1d: nesting depth (0=primary, 1=orchestrator, 2=specialist).
   * Depth > 0 indents the bubble container by `depth * 12px` so a
   * delegation chain reads as a visible hierarchy. Default 0.
   */
  depth?: number;
  /**
   * SP-071-3: zero-based index of this message in the chat history.
   * Rendered as data-message-index for the context menu to identify
   * which message was right-clicked.
   */
  dataMessageIndex?: number;
  tokensUsed?: number;
  cost?: number;
  model?: string;
  children: ReactNode;
}

const DEPTH_INDENT_PX = 12;
const MAX_DEPTH_INDENT = 3;

function MessageBubble({
  type = 'assistant',
  ariaLabel,
  copyText,
  timestamp,
  persona,
  depth = 0,
  dataMessageIndex,
  tokensUsed,
  cost,
  model,
  children,
}: MessageBubbleProps): JSX.Element {
  const handleCopy = async () => {
    if (copyText) {
      await copyToClipboard(copyText);
    }
  };

  // Cap the visual indent so a runaway nesting (depth=5+) doesn't crush
  // the bubble width to nothing — still record the true depth on the data
  // attribute for anyone who wants to style off it.
  const indentSteps = Math.min(Math.max(depth, 0), MAX_DEPTH_INDENT);
  const personaColor = persona ? getPersonaColor(persona) : undefined;

  const containerStyle: CSSProperties = {};
  if (indentSteps > 0) {
    containerStyle.marginLeft = `${indentSteps * DEPTH_INDENT_PX}px`;
  }
  if (personaColor) {
    // CSS custom property — drives the colored left rail on the bubble
    // via `.message[data-subagent-depth] .message-bubble`.
    (containerStyle as Record<string, string>)['--persona-color'] = personaColor;
  }
  const hasStyle = Object.keys(containerStyle).length > 0;

  return (
    <div
      className={`message ${type}`}
      // role="user-message" / "assistant-message" aren't valid ARIA roles —
      // Lighthouse flagged them. The parent chat container carries
      // role="log" already; individual messages don't need a role.
      data-message-type={type}
      data-message-index={dataMessageIndex}
      aria-label={ariaLabel}
      style={hasStyle ? containerStyle : undefined}
      data-subagent-depth={depth > 0 ? depth : undefined}
    >
      <div className="message-bubble" data-message-content={copyText || ''}>
        {persona ? (
          <span
            className="message-persona-badge"
            aria-label={`From ${persona}`}
          >
            {persona}
          </span>
        ) : null}
        {copyText ? (
          <button className="copy-button" onClick={handleCopy} title="Copy message" aria-label="Copy message">
            <Copy size={14} />
          </button>
        ) : null}
        <div className="message-content">{children}</div>
        {timestamp ? <div className="message-timestamp">{timestamp}</div> : null}
        {tokensUsed != null || cost != null ? (
          <div className="message-turn-cost" aria-hidden="true">
            {tokensUsed != null && <span>{formatTokens(tokensUsed)} tokens</span>}
            {tokensUsed != null && cost != null && <span className="message-turn-cost-sep"> · </span>}
            {cost != null && <span>{formatCost(cost)}</span>}
            {model && (
              <>
                <span className="message-turn-cost-sep"> · </span>
                <span className="message-turn-cost-model">{model}</span>
              </>
            )}
          </div>
        ) : null}
      </div>
    </div>
  );
}

export default MessageBubble;
