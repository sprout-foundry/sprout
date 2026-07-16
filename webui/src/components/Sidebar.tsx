import React, { useEffect, useState, useMemo, useCallback, useRef } from 'react';
import './Sidebar.css';
import { supportsSettings, supportsGit, supportsWorkspaceSwitching } from '../config/mode';
import { useEditorManager } from '../contexts/EditorManagerContext';
import { useHotkeys } from '../contexts/HotkeyContext';
import { usePlatformNav } from '../contexts/PlatformNavContext';
import { useTheme } from '../contexts/ThemeContext';
import type { WhitespaceRenderingMode } from '../extensions/whitespaceRendering';
import { useSidebarEventHandlers } from '../hooks/useSidebarEventHandlers';
import { useSidebarModel } from '../hooks/useSidebarModel';
import {
  type SectionTab,
  SIDEBAR_DEFAULT_WIDTH,
  SIDEBAR_COLLAPSED_WIDTH,
  clampSidebarWidth,
} from '../hooks/useSidebarState';
import type { ProviderLogEntry } from '../providers/types';
import type { SproutInstance } from '../services/api';
import type { GitCommitSummary, GitCommitDetail } from '../types/git-types';
import { debugLog } from '../utils/log';
import AutomationsPanel from './AutomationsPanel';
import type { GitSidebarPanelProps } from './GitSidebarPanel';
import LocationSwitcher from './LocationSwitcher';
import ResizeHandle from './ResizeHandle';
import {
  ScrollText,
  FolderCog,
  FolderOpen,
  Settings,
  Search,
  GitBranch,
  type LucideIcon,
  // New icons for platform nav items
  CreditCard,
  ListChecks,
  Users,
  LayoutDashboard,
  ExternalLink,
  Zap,
  CircleDollarSign,
} from 'lucide-react';
import SearchView from './SearchView';
import SidebarFilesSection, { type FileTreeHandle } from './SidebarFilesSection';
import SidebarGitSection from './SidebarGitSection';
import SidebarLogsPane from './SidebarLogsPane';
import SidebarSettingsSection from './SidebarSettingsSection';
import SproutLogo from './SproutLogo';

interface SidebarProps {
  isConnected: boolean;
  instances?: SproutInstance[];
  selectedInstancePID?: number;
  isSwitchingInstance?: boolean;
  onInstanceChange?: (pid: number) => void;
  selectedModel?: string;
  onModelChange?: (model: string) => void;
  selectedPersona?: string;
  onPersonaChange?: (persona: string) => void;
  /** Callback to open provider setup / onboarding dialog */
  onRequestProviderSetup?: () => void;
  availableModels?: string[];
  currentView?: 'chat' | 'editor' | 'git' | 'tasks' | 'billing' | 'team' | 'costs' | 'runners';
  onViewChange?: (view: 'chat' | 'editor' | 'git' | 'tasks' | 'billing' | 'team' | 'costs' | 'runners') => void;
  stats?: {
    queryCount: number;
    filesModified: number;
    persona?: string;
  };
  recentFiles?: Array<{ path: string; modified: boolean }>;
  recentLogs?:
    | string[]
    | Array<{
        id: string;
        type: string;
        timestamp: Date;
        data: unknown;
        level: string;
        category: string;
      }>;
  isMobileMenuOpen?: boolean;
  onMobileMenuToggle?: () => void;
  sidebarCollapsed?: boolean;
  onSidebarToggle?: () => void;
  selectedSection?: SectionTab;
  onSectionChange?: (section: SectionTab) => void;
  onFileClick?: (filePath: string, lineNumber?: number) => void;
  sidebarWidth?: number;
  sidebarWidthRef?: React.MutableRefObject<number>;
  onSidebarWidthChange?: (width: number) => void;
  onSidebarWidthPersist?: () => void;
  onSidebarWidthReset?: () => void;
  // Legacy props for backward compatibility
  provider?: string;
  model?: string;
  queryCount?: number;
  logs?: string[];
  files?: Array<{ path: string; modified: boolean }>;
  onProviderChange?: (provider: string) => void;
  isOpen?: boolean;
  onClose?: () => void;
  isMobile?: boolean;
  gitPanel?: GitSidebarPanelProps & {
    openWorkspaceBuffer: (options: {
      kind: 'chat' | 'diff' | 'review' | 'compare';
      path: string;
      title: string;
      content?: string;
      ext?: string;
      isPinned?: boolean;
      isClosable?: boolean;
      metadata?: Record<string, unknown>;
    }) => string;
    // Git history callbacks
    onLoadCommits: (
      limit: number,
      offset: number,
      opts?: { signal?: AbortSignal },
    ) => Promise<{ commits: GitCommitSummary[]; total: number }>;
    onLoadCommitDetail: (hash: string) => Promise<GitCommitDetail>;
    onLoadCommitFileDiff: (
      hash: string,
      filePath: string,
    ) => Promise<{ message: string; hash: string; path: string; diff: string }>;
    onCheckoutCommit: (commitHash: string) => Promise<{ message: string }>;
    onRevertCommit: (commitHash: string) => Promise<{ message: string }>;
  };
}

/**
 * Section tabs rendered in the icon rail. Filtered to only show supported tabs.
 */
const ALL_SECTION_TABS: { id: SectionTab; icon: LucideIcon; label: string }[] = [
  { id: 'git', icon: GitBranch, label: 'Git' },
  { id: 'files', icon: FolderCog, label: 'Files' },
  { id: 'search', icon: Search, label: 'Search' },
  { id: 'automations', icon: Zap, label: 'Automations' },
];

/** Valid platform view IDs for type-safe navigation */
const VALID_PLATFORM_VIEWS = new Set(['tasks', 'billing', 'team', 'costs', 'runners']);

/** Icon name-to-component mapping for platform nav items */
const PLATFORM_ICON_MAP: Record<string, LucideIcon> = {
  'credit-card': CreditCard,
  'list-checks': ListChecks,
  users: Users,
  'layout-dashboard': LayoutDashboard,
  'external-link': ExternalLink,
};

function Sidebar({
  isConnected,
  instances = [],
  selectedInstancePID = 0,
  isSwitchingInstance = false,
  onInstanceChange,
  selectedModel,
  onModelChange,
  selectedPersona,
  onPersonaChange,
  availableModels,
  currentView,
  stats,
  recentFiles: _recentFiles = [],
  recentLogs = [],
  isMobileMenuOpen,
  onMobileMenuToggle,
  sidebarCollapsed,
  onSidebarToggle,
  selectedSection,
  onSectionChange,
  onFileClick,
  sidebarWidth,
  sidebarWidthRef,
  onSidebarWidthChange,
  onSidebarWidthPersist,
  onSidebarWidthReset,
  provider,
  model,
  logs,
  onProviderChange,
  isOpen = true,
  onClose,
  isMobile = false,
  gitPanel,
  onRequestProviderSetup,
  onViewChange,
}: SidebarProps): JSX.Element {
  const { themePack, availableThemePacks, setThemePack, importTheme, removeTheme } = useTheme();
  const { applyPreset } = useHotkeys();
  const {
    isAutoSaveEnabled: autoSaveEnabled,
    setAutoSaveEnabled,
    whitespaceRenderingMode,
    setWhitespaceRenderingMode,
    isFormatOnSaveEnabled: formatOnSaveEnabled,
    setFormatOnSaveEnabled,
  } = useEditorManager();
  const { platformNavItems } = usePlatformNav();
  const sortedPlatformNavItems = useMemo(
    () => [...platformNavItems].sort((a, b) => (a.order ?? Infinity) - (b.order ?? Infinity)),
    [platformNavItems],
  );
  const fileTreeRef = useRef<FileTreeHandle | null>(null);

  const effectiveSidebarCollapsed = !isMobile && !!sidebarCollapsed;
  const effectiveSelectedSection = selectedSection || (supportsGit ? 'git' : 'files');
  // Use props for width or fall back to default
  const effectiveSidebarWidth = sidebarWidth ?? SIDEBAR_DEFAULT_WIDTH;
  // Plain object fallback avoids calling useRef when the prop is not provided
  const effectiveSidebarWidthRef = sidebarWidthRef ?? { current: effectiveSidebarWidth };

  // Use the extracted hook for provider/model/persona state management
  const modelState = useSidebarModel({
    isConnected,
    provider,
    model,
    selectedModel,
    selectedPersona,
    stats,
    onProviderChange,
    onModelChange,
    onPersonaChange,
  });

  // Reset active section if settings tab is selected but settings are not supported
  useEffect(() => {
    if (effectiveSelectedSection === 'settings' && !supportsSettings) {
      onSectionChange?.('files');
    }
  }, [effectiveSelectedSection, supportsSettings, onSectionChange]);

  const finalRecentLogs = useMemo(() => (recentLogs.length > 0 ? recentLogs : logs || []), [recentLogs, logs]);
  const normalizedRecentLogs = useMemo<ProviderLogEntry[]>(
    () =>
      (finalRecentLogs as Array<string | ProviderLogEntry>).filter(
        (entry): entry is ProviderLogEntry => typeof entry !== 'string',
      ),
    [finalRecentLogs],
  );

  const finalIsMobileMenuOpen = isMobileMenuOpen !== undefined ? isMobileMenuOpen : isOpen;
  const finalOnMobileMenuToggle = onMobileMenuToggle || onClose;

  const handleSidebarResize = useCallback(
    (delta: number) => {
      const nextWidth = clampSidebarWidth(effectiveSidebarWidthRef.current + delta);

      // Allow drag-to-expand behavior from collapsed mode.
      if (effectiveSidebarCollapsed) {
        onSidebarWidthChange?.(nextWidth);
        if (delta > 0) {
          onSidebarToggle?.();
        }
        return;
      }

      onSidebarWidthChange?.(nextWidth);
    },
    [effectiveSidebarCollapsed, onSidebarToggle, onSidebarWidthChange],
  );

  const [isResizing, setIsResizing] = useState(false);

  // .resizing class disables CSS transitions during drag to prevent lag
  const handleSidebarResizeStart = useCallback(() => {
    setIsResizing(true);
  }, []);

  const handleSidebarResizeEnd = useCallback(() => {
    setIsResizing(false);
    onSidebarWidthPersist?.();
  }, [onSidebarWidthPersist]);

  const handleSidebarResizeReset = useCallback(() => {
    onSidebarWidthReset?.();
  }, [onSidebarWidthReset]);

  const handleSectionTabClick = (tab: SectionTab) => {
    if (effectiveSidebarCollapsed) onSidebarToggle?.();
    onSectionChange?.(tab);
  };

  // Event handlers: hotkey open_search, reveal-in-explorer, open-settings-focus, settings focus
  useSidebarEventHandlers({
    effectiveSidebarCollapsed,
    isMobile,
    onSidebarToggle,
    onSectionChange,
    finalOnMobileMenuToggle,
    fileTreeRef,
    settingsFocusTarget: modelState.settingsFocusTarget,
    setSettingsFocusTarget: modelState.setSettingsFocusTarget,
  });

  const handleLogoToggle = useCallback(() => {
    if (isMobile) {
      finalOnMobileMenuToggle?.();
      return;
    }
    onSidebarToggle?.();
  }, [finalOnMobileMenuToggle, isMobile, onSidebarToggle]);

  /** Render the content pane based on selected section */
  /** Search section: find and replace panel */
  const renderSearchSection = () => {
    return <SearchView onFileClick={onFileClick} />;
  };

  const renderContentPane = () => {
    switch (effectiveSelectedSection) {
      case 'git':
        if (!supportsGit) {
          return (
            <div className="git-sidebar-panel">
              <div className="empty">No git in browser mode — use repo import instead.</div>
            </div>
          );
        }
        return <SidebarGitSection gitPanel={gitPanel} currentView={currentView} onSectionChange={onSectionChange} />;
      case 'logs':
        return <SidebarLogsPane logs={normalizedRecentLogs} />;
      case 'files':
        return (
          <SidebarFilesSection ref={fileTreeRef} onFileClick={onFileClick} workspaceRoot={gitPanel?.workspaceRoot} />
        );
      case 'search':
        return renderSearchSection();
      case 'automations':
        return (
          <AutomationsPanel
            onNavigateToSession={(id) => {
              debugLog('[Sidebar] Navigate to automation session:', id);
              onSectionChange?.('automations');
            }}
          />
        );
      case 'settings':
        return supportsSettings ? (
          <SidebarSettingsSection
            themePack={themePack}
            availableThemePacks={availableThemePacks}
            setThemePack={setThemePack}
            importTheme={importTheme}
            removeTheme={removeTheme}
            applyPreset={applyPreset}
            autoSaveEnabled={!!autoSaveEnabled}
            whitespaceRenderingMode={whitespaceRenderingMode}
            formatOnSaveEnabled={!!formatOnSaveEnabled}
            setAutoSaveEnabled={setAutoSaveEnabled}
            setWhitespaceRenderingMode={setWhitespaceRenderingMode}
            setFormatOnSaveEnabled={setFormatOnSaveEnabled}
            settings={modelState.settings}
            onSettingsChanged={(s) => modelState.setSettings(s)}
            onRequestProviderSetup={onRequestProviderSetup}
            selectedProvider={modelState.selectedProvider}
            selectedModel={modelState.finalSelectedModel}
            selectedPersona={modelState.selectedPersonaState}
            providers={modelState.providers.map((p) => ({ id: p.id, name: p.name }))}
            availableModels={
              availableModels && availableModels.length > 1 ? availableModels : modelState.finalAvailableModels
            }
            personas={modelState.personas.map((p) => ({ id: p.id, name: p.name }))}
            isLoadingProviders={modelState.isLoadingProviders}
            isLoadingPersonas={modelState.isLoadingPersonas}
            isConnected={isConnected}
            onProviderChange={(val: string) => {
              modelState.setSelectedProvider(val);
              onProviderChange?.(val);
            }}
            onModelChange={(val: string) => {
              if (val !== modelState.finalSelectedModel) {
                modelState.setSelectedModelState(val);
                onModelChange?.(val);
              }
            }}
            onPersonaChange={(val: string) => {
              modelState.setSelectedPersonaState(val);
              onPersonaChange?.(val);
            }}
          />
        ) : null;
      default:
        return null;
    }
  };

  return (
    <div className="sidebar-resize-wrapper" style={{ flexShrink: 0 }} data-testid="sidebar-container">
      <div
        className={`sidebar ${isMobile ? 'mobile' : ''} ${finalIsMobileMenuOpen ? 'open' : 'closed'} ${effectiveSidebarCollapsed ? 'collapsed' : ''} ${isResizing ? 'resizing' : ''}`}
        style={
          isMobile
            ? undefined
            : { width: `${effectiveSidebarCollapsed ? SIDEBAR_COLLAPSED_WIDTH : effectiveSidebarWidth}px` }
        }
      >
        {/* Pinned global header: instance selector */}
        <div className="sidebar-pinned-header">
          <button
            type="button"
            className="sidebar-brand sidebar-brand-button"
            onClick={handleLogoToggle}
            aria-label={isMobile ? 'Close sidebar' : effectiveSidebarCollapsed ? 'Expand sidebar' : 'Collapse sidebar'}
            title={isMobile ? 'Close sidebar' : effectiveSidebarCollapsed ? 'Expand sidebar' : 'Collapse sidebar'}
            data-testid="sidebar-brand"
          >
            <SproutLogo showWordmark={false} compact />
          </button>
          {!effectiveSidebarCollapsed ? (
            <>
              {supportsWorkspaceSwitching ? (
                <LocationSwitcher
                  isConnected={isConnected}
                  instances={instances}
                  selectedInstancePID={selectedInstancePID}
                  isSwitchingInstance={isSwitchingInstance}
                  onInstanceChange={onInstanceChange}
                  sidebarCollapsed={effectiveSidebarCollapsed}
                />
              ) : (
                <div className="sidebar-static-workspace" title="Browser Workspace">
                  <FolderOpen size={14} className="sidebar-static-workspace-icon" />
                  <span className="sidebar-static-workspace-label">Browser Workspace</span>
                </div>
              )}
            </>
          ) : null}
        </div>

        {/* Icon rail (always visible) + Content pane (only when expanded) */}
        <div className="sidebar-body">
          {/* Icon Rail */}
          <div
            className="sidebar-icon-rail"
            role="navigation"
            aria-label="Sidebar navigation"
            data-testid="sidebar-icon-rail"
          >
            {/* Main section tabs: filtered by capability flags */}
            <div role="tablist" aria-orientation="vertical">
              {ALL_SECTION_TABS.filter((tab) => tab.id !== 'git' || supportsGit).map((tab) => (
                <button
                  key={tab.id}
                  role="tab"
                  aria-selected={effectiveSelectedSection === tab.id}
                  aria-controls="sidebar-tabpanel"
                  className={`rail-icon ${effectiveSelectedSection === tab.id ? 'active' : ''}`}
                  onClick={() => handleSectionTabClick(tab.id)}
                  title={tab.label}
                  aria-label={tab.label}
                  data-testid={`sidebar-${tab.id}-tab`}
                >
                  <tab.icon size={18} strokeWidth={1.5} />
                </button>
              ))}
            </div>

            {/* Platform Nav Items (between main sections and settings) */}
            {sortedPlatformNavItems.length > 0 && (
              <>
                <div className="sidebar-icon-rail-divider" role="separator" />
                <nav aria-label="Platform navigation">
                  {sortedPlatformNavItems.map((item) => {
                    const IconComponent = item.icon ? (PLATFORM_ICON_MAP[item.icon] ?? ExternalLink) : ExternalLink;
                    const isActive = currentView === item.id;
                    return (
                      <button
                        key={item.id}
                        role="tab"
                        aria-selected={isActive}
                        className={`rail-icon ${isActive ? 'active' : ''}`}
                        onClick={() => {
                          if (onViewChange && VALID_PLATFORM_VIEWS.has(item.id)) {
                            onViewChange(item.id as 'chat' | 'editor' | 'git' | 'tasks' | 'billing' | 'team' | 'costs' | 'runners');
                          }
                        }}
                        title={item.label}
                        aria-label={item.label}
                      >
                        <IconComponent size={18} strokeWidth={1.5} />
                      </button>
                    );
                  })}
                </nav>
              </>
            )}

            {/* Costs — local feature, always visible */}
            <div role="tablist" aria-orientation="vertical">
              <button
                role="tab"
                aria-selected={currentView === 'costs'}
                className={`rail-icon ${currentView === 'costs' ? 'active' : ''}`}
                onClick={() => onViewChange?.('costs')}
                title="Costs"
                aria-label="Costs"
                data-testid="sidebar-costs-button"
              >
                <CircleDollarSign size={18} strokeWidth={1.5} />
              </button>
            </div>

            {/* Settings & Logs tabs */}
            <div role="tablist" aria-orientation="vertical">
              {supportsSettings && (
                <button
                  role="tab"
                  aria-selected={effectiveSelectedSection === 'settings'}
                  aria-controls="sidebar-tabpanel"
                  className={`rail-icon ${effectiveSelectedSection === 'settings' ? 'active' : ''}`}
                  onClick={() => handleSectionTabClick('settings')}
                  title="Settings"
                  aria-label="Settings"
                  data-testid="sidebar-settings-toggle"
                >
                  <Settings size={18} strokeWidth={1.5} />
                </button>
              )}
              <button
                role="tab"
                aria-selected={effectiveSelectedSection === 'logs'}
                aria-controls="sidebar-tabpanel"
                className={`rail-icon ${effectiveSelectedSection === 'logs' ? 'active' : ''}`}
                onClick={() => handleSectionTabClick('logs')}
                title="Logs"
                aria-label="Logs"
                data-testid="sidebar-logs-tab"
              >
                <ScrollText size={18} strokeWidth={1.5} />
              </button>
            </div>
          </div>

          {/* Content Pane — always rendered; CSS handles fade-out on collapse */}
          <div
            className="sidebar-content-pane"
            role="tabpanel"
            id="sidebar-tabpanel"
            {...(effectiveSidebarCollapsed ? { inert: true, 'aria-hidden': true } : {})}
          >
            <div className="content-pane-scroll">{renderContentPane()}</div>
          </div>
        </div>
      </div>
      {!isMobile && (
        <ResizeHandle
          direction="horizontal"
          onResize={handleSidebarResize}
          onResizeStart={handleSidebarResizeStart}
          onResizeEnd={handleSidebarResizeEnd}
          onDoubleClick={handleSidebarResizeReset}
          className="sidebar-resize-handle"
        />
      )}
    </div>
  );
}

export default React.memo(Sidebar);
