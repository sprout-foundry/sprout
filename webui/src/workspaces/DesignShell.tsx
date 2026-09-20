/**
 * The Design mode's shell.
 *
 * Owns the same `<main>` column the Code shell owns, but composes only what
 * Design needs: its surface (DesignSurface) and — since SP-140-6 §6f — the
 * agent panel, so the loop can be directed from Design mode without a mode
 * switch. The panel renders the same Chat the Code shell mounts, fed by the
 * same chat payload this shell already receives; the shell owns only the
 * panel's open state and the prefill handoff (prefill fills the input, never
 * auto-sends).
 *
 * No menubar, no status bar, no context panel — those are the Code surface's
 * chrome and belong to CodeShell.
 */

import { Menu, MessageSquare } from 'lucide-react';
import React, { useCallback, useState } from 'react';
import DesignAgentPanel from '../components/design/DesignAgentPanel';
import DesignSurface from '../components/design/DesignSurface';
import ErrorBoundary from '../components/ErrorBoundary';
import type { WorkspaceShellProps } from './shell';

const DesignShell: React.FC<WorkspaceShellProps> = ({
  isMobile,
  isSidebarOpen,
  supportsLocalTerminal,
  isTerminalExpanded,
  onToggleSidebar,
  chat,
  design,
}) => {
  // §6f agent presence: the chat is a docked column of the Design surface
  // and is OPEN BY DEFAULT — directing the design loop from chat is the
  // point of the mode, so the panel is part of the layout, not an overlay
  // the user has to summon. Collapse is still available (remembers its
  // state per mount), and on mobile the panel keeps its overlay behavior.
  const [agentOpen, setAgentOpen] = useState(true);
  const [prefill, setPrefill] = useState<string | null>(null);

  const askAgent = useCallback((prompt: string) => {
    setPrefill(prompt);
    setAgentOpen(true);
  }, []);

  const toggleAgent = useCallback(() => setAgentOpen((open) => !open), []);

  return (
    <main
      className={`main-content design-shell ${isMobile && isSidebarOpen ? 'sidebar-open' : ''} ${supportsLocalTerminal && isTerminalExpanded ? 'terminal-expanded' : ''}`}
    >
      {isMobile && (
        <div className="pane-controls pane-controls-mobile">
          <button
            className="top-mobile-menu-btn"
            onClick={onToggleSidebar}
            aria-label={isSidebarOpen ? 'Close sidebar' : 'Open sidebar'}
            title={isSidebarOpen ? 'Close sidebar' : 'Open sidebar'}
          >
            <Menu size={16} />
          </button>
          <button
            className="top-mobile-chat-btn"
            onClick={toggleAgent}
            aria-label={agentOpen ? 'Close agent panel' : 'Ask the designer'}
            title="Ask the designer"
          >
            <MessageSquare size={16} />
          </button>
        </div>
      )}
      <div className="design-shell-body">
        <ErrorBoundary panelName="Design">
          <DesignSurface
            loading={design.loading}
            present={design.present}
            tab={design.tab}
            onTabChange={design.onTabChange}
            onOpenFile={design.onOpenFile}
            onAskAgent={askAgent}
          />
        </ErrorBoundary>
        <ErrorBoundary panelName="Design agent panel">
          <DesignAgentPanel
            chatProps={chat.chatProps}
            open={agentOpen}
            onToggle={toggleAgent}
            prefill={prefill}
            onPrefillConsumed={() => setPrefill(null)}
          />
        </ErrorBoundary>
      </div>
    </main>
  );
};

export default DesignShell;
