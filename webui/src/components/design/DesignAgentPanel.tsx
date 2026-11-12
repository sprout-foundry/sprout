/**
 * DesignAgentPanel — the agent chat in Design mode (SP-140).
 *
 * Renders the same Chat component the Code shell mounts, fed by the same
 * `chat.chatProps` payload `WorkspaceShellProps` already passes to every
 * shell — no second query client, no new send path. The chat is where the
 * person asks for a mockup or for a change to the one on screen.
 */

import type { ComponentProps } from 'react';
import Chat from '../ChatView';
import './DesignSurface.css';

export interface DesignAgentPanelProps {
  /** The shell's chat payload — the Code shell's own chat props object. */
  chatProps: ComponentProps<typeof Chat>;
  /**
   * SP-142 §4 (142.5): the design chat's name — when present, the panel
   * renders a header naming the active design-lane chat. Omitted = no
   * header (hosts/tests that mount the bare chat).
   */
  chatName?: string | null;
  /** SP-142 142.5: creates a new chat stamped with the design lane. */
  onCreateDesignChat?: () => void;
}

export default function DesignAgentPanel({ chatProps, chatName, onCreateDesignChat }: DesignAgentPanelProps) {
  return (
    <div className="design-agent-panel" data-testid="design-agent-panel" aria-label="Agent panel">
      {chatName || onCreateDesignChat ? (
        <header className="design-agent-head" data-testid="design-agent-head">
          <span className="design-agent-head-name" data-testid="design-agent-chat-name">
            {chatName ?? 'Design chat'}
          </span>
          {onCreateDesignChat ? (
            <button
              type="button"
              className="design-agent-new-chat"
              data-testid="design-agent-new-chat"
              onClick={onCreateDesignChat}
              title="Start a new design chat"
            >
              + New Chat
            </button>
          ) : null}
        </header>
      ) : null}
      <div className="design-agent-chat" data-testid="design-agent-chat">
        <Chat {...chatProps} inputPlaceholder="Describe the screen you want to see, or a change to this one..." />
      </div>
    </div>
  );
}
