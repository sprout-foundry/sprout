import React from 'react';
import { PanelRightClose } from 'lucide-react';
import MenuBar from './MenuBar';
import WorkspaceBar from './WorkspaceBar';

export interface HeaderBarProps {
  isMobile: boolean;
  isSidebarOpen: boolean;
  showContextSidebar: boolean;
  isConnected: boolean;
  onToggleSidebar: () => void;
  onToggleContextPanel: () => void;
}

const HeaderBar: React.FC<HeaderBarProps> = ({
  isMobile,
  isSidebarOpen,
  showContextSidebar,
  isConnected,
  onToggleSidebar,
  onToggleContextPanel,
}) => {
  return (
    <div className="header-bar">
      <MenuBar />
      <div className="header-bar-actions">
        {!isMobile && showContextSidebar && (
          <button
            className="header-context-toggle-btn"
            onClick={onToggleContextPanel}
            aria-label="Toggle context panel"
            title="Toggle context panel"
          >
            <PanelRightClose size={14} />
          </button>
        )}
        <WorkspaceBar
          isConnected={isConnected}
          isMobile={isMobile}
          isMobileMenuOpen={isSidebarOpen}
        />
      </div>
    </div>
  );
};

export default HeaderBar;
