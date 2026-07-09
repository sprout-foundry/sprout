import { PanelRightClose } from 'lucide-react';
import React, { useState, useEffect } from 'react';
import { isCloud } from '../config/mode';
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
  // Detect ?repo= param to pass along to the Build workspace flow.
  const [repoURL, setRepoURL] = useState<string | null>(null);

  useEffect(() => {
    if (!isCloud) return;
    const params = new URLSearchParams(window.location.search);
    const repo = params.get('repo');
    if (repo) setRepoURL(repo);
  }, []);

  const handleStartBuilding = async () => {
    // Derive the repo URL from the query param or prompt the user.
    const url = repoURL || window.prompt('Enter a GitHub repo URL to build (e.g. https://github.com/owner/repo):');
    if (!url) return;

    // Call the Fly workspace creation endpoint.
    try {
      const response = await fetch('/workspace/fly', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ repo_url: url }),
        credentials: 'include',
      });

      if (!response.ok) {
        const errData = await response.json().catch(() => ({ error: `HTTP ${response.status}` }));
        window.alert(`Failed to start workspace: ${errData.error}`);
        return;
      }

      const data = await response.json();
      if (data.url) {
        window.location.href = data.url;
      } else {
        window.alert(`Workspace status: ${data.status}`);
      }
    } catch (e) {
      window.alert(`Error: ${e instanceof Error ? e.message : String(e)}`);
    }
  };

  return (
    <div className="header-bar">
      <MenuBar />
      <div className="header-bar-actions">
        {isCloud && (
          <button
            className="btn btn-sm btn-accent start-building-btn"
            onClick={handleStartBuilding}
            title="Upgrade to a full workspace with real compute, persistent storage, and git push."
          >
            Start Building
          </button>
        )}
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
        <WorkspaceBar isConnected={isConnected} isMobile={isMobile} isMobileMenuOpen={isSidebarOpen} />
      </div>
    </div>
  );
};

export default HeaderBar;
