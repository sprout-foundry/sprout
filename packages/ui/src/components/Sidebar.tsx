/**
 * Sidebar component for @sprout/ui
 *
 * A composable sidebar that aggregates FileTree, GitPanel, SearchView, and Settings.
 * In the standalone package, the Sidebar is fully props-driven — all data and actions
 * come through props rather than internal API calls.
 */
import { useState, useCallback } from 'react';
import type { ReactNode } from 'react';
import {
  FolderOpen,
  GitBranch,
  Search,
  Settings,
  FileCode,
  ChevronLeft,
} from 'lucide-react';
import FileTree from './FileTree';
import type { FileInfo, FileTreeProps } from './FileTree';
import GitSidebarPanel from './GitPanel';
import type { GitSidebarPanelProps } from './GitPanel';
import './Sidebar.css';

type SectionTab = 'files' | 'git' | 'search' | 'settings';

export interface SidebarProps {
  /** Currently active section tab */
  activeSection?: SectionTab;
  /** Callback when section tab changes */
  onSectionChange?: (section: SectionTab) => void;
  /** Whether sidebar is collapsed */
  collapsed?: boolean;
  /** Callback when sidebar collapse state changes */
  onToggleCollapse?: () => void;
  /** Width of the sidebar in pixels */
  width?: number;
  /** Whether backend is connected */
  isConnected?: boolean;

  // FileTree props (forwarded)
  fileTreeProps?: Partial<FileTreeProps>;

  // GitPanel props (forwarded)
  gitPanelProps?: Partial<GitSidebarPanelProps>;

  // Custom content for each section
  searchContent?: ReactNode;
  settingsContent?: ReactNode;

  // Header branding
  branding?: ReactNode;
}

const SECTIONS: Array<{ id: SectionTab; icon: typeof FolderOpen; label: string }> = [
  { id: 'files', icon: FolderOpen, label: 'Files' },
  { id: 'git', icon: GitBranch, label: 'Git' },
  { id: 'search', icon: Search, label: 'Search' },
  { id: 'settings', icon: Settings, label: 'Settings' },
];

function Sidebar({
  activeSection: externalSection,
  onSectionChange,
  collapsed = false,
  onToggleCollapse,
  width = 260,
  isConnected = true,
  fileTreeProps,
  gitPanelProps,
  searchContent,
  settingsContent,
  branding,
}: SidebarProps): JSX.Element {
  const [internalSection, setInternalSection] = useState<SectionTab>('files');
  const activeSection = externalSection ?? internalSection;

  const handleSectionChange = useCallback(
    (section: SectionTab) => {
      setInternalSection(section);
      onSectionChange?.(section);
    },
    [onSectionChange],
  );

  if (collapsed) {
    return (
      <div className="sidebar-collapsed" style={{ width: 42 }}>
        <button className="sidebar-expand-btn" onClick={onToggleCollapse} title="Expand sidebar">
          <FolderOpen size={18} />
        </button>
      </div>
    );
  }

  return (
    <div className="sidebar" style={{ width }}>
      {/* Branding */}
      {branding && <div className="sidebar-branding">{branding}</div>}

      {/* Section tabs */}
      <div className="sidebar-tabs">
        {SECTIONS.map(({ id, icon: Icon, label }) => (
          <button
            key={id}
            className={`sidebar-tab ${activeSection === id ? 'active' : ''}`}
            onClick={() => handleSectionChange(id)}
            title={label}
          >
            <Icon size={18} />
          </button>
        ))}
        <div className="sidebar-tabs-spacer" />
        <button className="sidebar-collapse-btn" onClick={onToggleCollapse} title="Collapse sidebar">
          <ChevronLeft size={16} />
        </button>
      </div>

      {/* Section content */}
      <div className="sidebar-content">
        {activeSection === 'files' && (
          <FileTree
            onFileSelect={() => {}}
            {...fileTreeProps}
          />
        )}

        {activeSection === 'git' && (
          <GitSidebarPanel
            gitStatus={null}
            gitBranches={{ current: '', branches: [] }}
            selectedFiles={new Set()}
            activeDiffSelectionKey={null}
            commitMessage=""
            isLoading={false}
            isActing={false}
            isGeneratingCommitMessage={false}
            isReviewLoading={false}
            actionError={null}
            actionWarning={null}
            onCommitMessageChange={() => {}}
            onGenerateCommitMessage={() => {}}
            onCommit={() => {}}
            onRunReview={() => {}}
            onCheckoutBranch={() => {}}
            onCreateBranch={() => {}}
            onPull={() => {}}
            onPush={() => {}}
            onRefresh={() => {}}
            onToggleFileSelection={() => {}}
            onToggleSectionSelection={() => {}}
            onClearSelection={() => {}}
            onPreviewFile={() => {}}
            onStageSelected={() => {}}
            onUnstageSelected={() => {}}
            onDiscardSelected={() => {}}
            onStageFile={() => {}}
            onUnstageFile={() => {}}
            onDiscardFile={() => {}}
            onSectionAction={() => {}}
            {...gitPanelProps}
          />
        )}

        {activeSection === 'search' && (
          <div className="sidebar-search">
            {searchContent ?? <div className="sidebar-empty-state">Search not configured</div>}
          </div>
        )}

        {activeSection === 'settings' && (
          <div className="sidebar-settings">
            {settingsContent ?? <div className="sidebar-empty-state">Settings not configured</div>}
          </div>
        )}
      </div>
    </div>
  );
}

export default Sidebar;
