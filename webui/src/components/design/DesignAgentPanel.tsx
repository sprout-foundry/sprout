/**
 * DesignAgentPanel — agent presence in Design mode (SP-140-6 §6f).
 *
 * A collapsible panel on the design surface that renders the same Chat
 * component the Code shell mounts, fed by the same `chat.chatProps` payload
 * `WorkspaceShellProps` already passes to every shell — no second query
 * client, no new send path. This is what makes Design mode a place where the
 * loop can be *directed*: critique rounds, "build this screen from the
 * brief", and sync adoption become dispatchable without leaving the mode.
 *
 * Prefill ("Ask the designer", §6c remedy rows, §6g adopt) fills the chat
 * input through the controlled `onInputChange` — it never auto-sends.
 */

import { MessageSquare, X } from 'lucide-react';
import { useEffect } from 'react';
import type { ComponentProps } from 'react';
import Chat from '../ChatView';

export interface DesignAgentPanelProps {
  /** The shell's chat payload — the Code shell's own chat props object. */
  chatProps: ComponentProps<typeof Chat>;
  /** Whether the panel is expanded. */
  open: boolean;
  onToggle: () => void;
  /** A prompt to seed the input with. Consumed once, never sent. */
  prefill?: string | null;
  /** Fired after the prefill landed in the input. */
  onPrefillConsumed?: () => void;
  /** Fires when a prompt is actually sent (hosts may close the prefill). */
  showHeader?: boolean;
}

export default function DesignAgentPanel({
  chatProps,
  open,
  onToggle,
  prefill,
  onPrefillConsumed,
  showHeader = true,
}: DesignAgentPanelProps) {
  // Seed the input when a prefill arrives. The chat input is app-owned
  // state; writing it through the same controlled callback the Code shell
  // uses means one input, one draft, no duplicated state.
  useEffect(() => {
    if (!open || !prefill) return;
    chatProps.onInputChange?.(prefill);
    onPrefillConsumed?.();
  }, [open, prefill, chatProps, onPrefillConsumed]);

  if (!open) {
    return (
      <button
        type="button"
        className="design-agent-fab"
        data-testid="design-agent-open"
        onClick={onToggle}
        aria-label="Ask the designer"
        title="Ask the designer"
      >
        <MessageSquare size={16} />
      </button>
    );
  }

  return (
    <aside className="design-agent-panel" data-testid="design-agent-panel" aria-label="Agent panel">
      {showHeader && (
        <div className="design-agent-header">
          <span className="design-agent-title">Ask the designer</span>
          <button
            type="button"
            className="design-agent-close"
            data-testid="design-agent-close"
            onClick={onToggle}
            aria-label="Close agent panel"
          >
            <X size={14} />
          </button>
        </div>
      )}
      <div className="design-agent-chat" data-testid="design-agent-chat">
        <Chat {...chatProps} />
      </div>
    </aside>
  );
}
