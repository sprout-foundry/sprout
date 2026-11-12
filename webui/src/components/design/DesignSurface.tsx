/**
 * Design mode's surface (SP-140).
 *
 * Design mode is entered via the top-left mode switcher; when it is active
 * this is what the shell puts in the content area. The surface is the mockup
 * and the chat, and nothing else: the chat is where the person asks for a
 * screen or a change, and the mockup is what they react to.
 *
 * `draw_mockup` streams `design/mockup.html` into view while it is drawn, so
 * the document is rendered as it arrives (the mockup_* events carry the
 * pieces) rather than read from disk once the tool finishes.
 */

import { SkeletonText } from '@sprout/ui';
import React from 'react';
import ErrorBoundary from '../ErrorBoundary';
import DesignAgentPanel, { type DesignAgentPanelProps } from './DesignAgentPanel';
import MockupPane from './MockupPane';

const SurfaceFallback: React.FC = () => (
  <div className="editor-workspace-route-fallback" data-testid="design-surface-fallback">
    <SkeletonText lines={6} />
  </div>
);

export interface DesignSurfaceProps {
  /** True while the design-presence probe is in flight. */
  loading: boolean;
  /** True when the workspace has a mockup to show. */
  present: boolean;
  /** The shell's chat payload. */
  chatProps?: DesignAgentPanelProps['chatProps'];
  /** SP-142 142.5: the design chat's name (the agent panel header). */
  agentChatName?: string | null;
  /** SP-142 142.5: New Chat scoped to the design lane. */
  onAgentCreateChat?: () => void;
}

const DesignSurface: React.FC<DesignSurfaceProps> = ({
  loading,
  present,
  chatProps,
  agentChatName,
  onAgentCreateChat,
}) => {
  if (loading) return <SurfaceFallback />;

  return (
    <div className="design-surface" data-testid="design-surface">
      <ErrorBoundary panelName="Mockup">
        <MockupPane present={present} />
      </ErrorBoundary>
      <aside className="design-agent-column" aria-label="Agent chat" data-testid="design-agent-column">
        {chatProps ? (
          <DesignAgentPanel chatProps={chatProps} chatName={agentChatName} onCreateDesignChat={onAgentCreateChat} />
        ) : (
          <div className="design-agent-absent">Agent chat is not available in this host.</div>
        )}
      </aside>
    </div>
  );
};

export default DesignSurface;
