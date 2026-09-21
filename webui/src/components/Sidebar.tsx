import React, { type ComponentType, useEffect, useState, useMemo, useCallback, useRef } from 'react';
import './Sidebar.css';
import { supportsSettings, supportsGit, supportsWorkspaceSwitching } from '../config/mode';
import { useEditorManager } from '../contexts/EditorManagerContext';
import { useHotkeys } from '../contexts/HotkeyContext';
import { usePlatformNav } from '../contexts/PlatformNavContext';
import { usePlugins } from '../contexts/PluginContext';
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
import { useUIScale } from '../hooks/useUIScale';
import type { ProviderLogEntry } from '../providers/types';
import type { SproutInstance } from '../services/api';
import { NATIVE_GIT_ENABLED } from '../services/nativeGitStubs/nativeGitFlag';
import type { ViewType } from '../types/app';
import type { GitCommitSummary, GitCommitDetail } from '../types/git-types';
import { debugLog } from '../utils/log';
import ModeSwitcher from '../workspaces/ModeSwitcher';
import type { ModeRailProps } from '../workspaces/rail';
import type { WorkspaceMode, WorkspaceModeId } from '../workspaces/registry';
import AutomationsPanel from './AutomationsPanel';
import { useDesignPresence } from './design/useDesignPresence';
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
  Shield,
  Server,
  Monitor,
  Zap,
  PanelLeft,
  Palette,
} from 'lucide-react';
import SearchView from './SearchView';
import SidebarFilesSection, { type FileTreeHandle } from './SidebarFilesSection';
import SidebarGitSection from './SidebarGitSection';
import SidebarLogsPane from './SidebarLogsPane';
import SidebarSettingsSection from './SidebarSettingsSection';
import SproutLogo from './SproutLogo';
import DesignAssetsPane from './design/DesignAssetsPane';
interface SidebarProps {
  isConnected: boolean;
  instances?: SproutInstance[];
  selectedInstancePID?: number;
  isSwitchingInstance?: boolean;
  onInstanceChange?: (pid: number) => void;
  selectedModel?: string;
  onModelChange?: (model: string) => void;
  /** Callback to open provider setup / onboarding dialog */
  onRequestProviderSetup?: () => void;
  availableModels?: string[];
  currentView?: ViewType;
  onViewChange?: (view: ViewType) => void;
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
  /** Workspace modes offered for the current workspace, in switcher order. */
  modes?: WorkspaceMode[];
  /** The active mode's id. */
  activeModeId?: WorkspaceModeId;
  /** Switch modes (the top-left switcher). */
  onSelectMode?: (id: WorkspaceModeId) => void;
  /** The active mode's rail component, or undefined for the Code default. */
  modeRail?: ComponentType<ModeRailProps>;
  /** Active entry id within the mode's rail. */
  modeSection?: string;
  /** Fired when the user picks an entry in the mode's rail. */
  onModeSectionChange?: (id: string) => void;
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

/** Icon name-to-component mapping for platform nav items */
const PLATFORM_ICON_MAP: Record<string, LucideIcon> = {
  'credit-card': CreditCard,
  'list-checks': ListChecks,
  users: Users,
  'layout-dashboard': LayoutDashboard,
  'external-link': ExternalLink,
  shield: Shield,
  server: Server,
  monitor: Monitor,
};

function Sidebar({
  isConnected,
  instances = [],
  selectedInstancePID = 0,
  isSwitchingInstance = false,
  onInstanceChange,
  selectedModel,
  onModelChange,
  availableModels,
  currentView,
  stats,
  recentFiles: _recentFiles = [],
  recentLogs = [],
  isMobileMenuOpen,
  onMobileMenuToggle,
  sidebarCollapsed,
  onSidebarToggle,
  modes = [],
  activeModeId = 'code',
  onSelectMode,
  modeRail,
  modeSection,
  onModeSectionChange,
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
  // UI Size (P4.5-C): hook mount applies data-ui-scale to <html> on boot
  // (persisted choice, tablet heuristic on first run) and re-applies on change.
  const { uiScale, setUIScale } = useUIScale();
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
  const { pluginViews, pluginPanels } = usePlugins();
  const sortedPlatformNavItems = useMemo(
    () => [...platformNavItems].sort((a, b) => (a.order ?? Infinity) - (b.order ?? Infinity)),
    [platformNavItems],
  );
  const fileTreeRef = useRef<FileTreeHandle | null>(null);
  // SP-140-5: the active mode's rail, rendered in place of the Code section
  // tabs. The prop name starts with a lowercase letter, which JSX would
  // parse as an intrinsic (HTML) element, so it is aliased before use.
  const ModeRailComponent = modeRail;
  // SP-140-3 §3a: the design nav item is visible only when the workspace has
  // a design/ directory. The view route is equally gated (EditorWorkspace),
  // so a workspace without a design tree can never reach the DesignView chunk.
  const { present: designPresent } = useDesignPresence();

  const effectiveSidebarCollapsed = !isMobile && !!sidebarCollapsed;
  // While a mode rail is active the content pane belongs to the mode: Code
  // sections (git/files/search) yield to the mode's section, but the global
  // sections (logs/settings/plugin panels — addressed by the shared rail
  // below the mode rail) keep rendering in every mode. Picking a mode
  // section releases a global pane back to the mode (handleModeSectionChange
  // clears the selection); picking a global entry claims it back.
  const isCodeSection = (section: SectionTab | undefined | null) =>
    section != null && ALL_SECTION_TABS.some((tab) => tab.id === section);
  const isGlobalSection = (section: SectionTab | undefined | null) =>
    !!section && (section === 'logs' || section === 'settings' || pluginPanels.some((panel) => panel.id === section));
  const effectiveSelectedSection = ModeRailComponent
    ? isGlobalSection(selectedSection)
      ? selectedSection
      : (modeSection ?? null)
    : selectedSection || (supportsGit ? 'git' : 'files'); // Use props for width or fall back to default
  const effectiveSidebarWidth = sidebarWidth ?? SIDEBAR_DEFAULT_WIDTH;
  // Plain object fallback avoids calling useRef when the prop is not provided
  const effectiveSidebarWidthRef = sidebarWidthRef ?? { current: effectiveSidebarWidth };

  // Use the extracted hook for provider/model state management
  const modelState = useSidebarModel({
    isConnected,
    provider,
    model,
    selectedModel,
    stats,
    onProviderChange,
    onModelChange,
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

  /**
   * Mode selection from the top-left switcher. Optional because Sidebar is also
   * rendered in hosts that don't own workspace state (tests, storybook-style
   * harnesses); without a handler the switcher simply doesn't render options
   * that do nothing.
   */
  const selectMode = useCallback(
    (id: string) => {
      onSelectMode?.(id);
    },
    [onSelectMode],
  );

  /**
   * Section selection from the active mode's rail. Optional for the same
   * reason as `selectMode`: Sidebar is rendered in hosts without workspace
   * state, and a rail without a handler is a read-only indicator there.
   */
  const handleModeSectionChange = useCallback(
    (id: string) => {
      // A mode-section pick releases a global pane (logs/settings/plugin
      // panel) back to the mode's own content; without this the global pane
      // would stick until manually re-picked.
      if (isGlobalSection(selectedSection)) onSectionChange?.('' as SectionTab);
      onModeSectionChange?.(id);
    },
    [onModeSectionChange, onSectionChange, selectedSection],
  );

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
        // R-4: in a --native-git dist the shell provides git natively (the git
        // client API + boot wiring are hard-excluded), so the git surface
        // renders a clear handoff placeholder instead of the browser-git UI.
        // Dead branch in the default build (flag off → today's exact
        // behavior, byte-identical).
        if (NATIVE_GIT_ENABLED) {
          return (
            <div className="git-sidebar-panel">
              <div className="terminal-status-inline" style={{ margin: '8px 12px' }}>
                Git provided by the native shell
              </div>
            </div>
          );
        }
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
      case 'flows':
      case 'screens':
      case 'tokens':
        // Design mode's sections: the assets browser for the active section,
        // rendered from the shared DesignWorkspaceContext. Outside a provider
        // (hosts without the workspace shell) the pane renders nothing.
        return <DesignAssetsPane />;
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
            uiScale={uiScale}
            setUIScale={setUIScale}
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
            providers={modelState.providers.map((p) => ({ id: p.id, name: p.name }))}
            availableModels={
              availableModels && availableModels.length > 1 ? availableModels : modelState.finalAvailableModels
            }
            isLoadingProviders={modelState.isLoadingProviders}
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
          />
        ) : null;
      default:
        // Check if this is a plugin panel
        const pluginPanel = pluginPanels.find((p) => p.id === effectiveSelectedSection);
        if (pluginPanel) {
          const PanelComponent = pluginPanel.component;
          return <PanelComponent />;
        }
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
        {/* Pinned global header: mode switcher (top-left) + location selector */}
        <div className="sidebar-pinned-header">
          <ModeSwitcher
            modes={modes}
            activeId={activeModeId}
            onSelect={selectMode}
            triggerLabel="Switch mode"
            trigger={({ open }) => (
              <span className={`sidebar-brand-mark${open ? ' is-open' : ''}`}>
                <SproutLogo showWordmark={false} compact />
              </span>
            )}
          />
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
          {/* Collapse/expand now lives beside the switcher: the logo's click
              moved to opening the mode menu, and a control that vanishes when
              the rail is collapsed would strand a collapsed user. */}
          <button
            type="button"
            className="sidebar-collapse-button"
            onClick={handleLogoToggle}
            aria-label={isMobile ? 'Close sidebar' : effectiveSidebarCollapsed ? 'Expand sidebar' : 'Collapse sidebar'}
            title={isMobile ? 'Close sidebar' : effectiveSidebarCollapsed ? 'Expand sidebar' : 'Collapse sidebar'}
            data-testid="sidebar-collapse-toggle"
          >
            <PanelLeft size={16} aria-hidden="true" />
          </button>
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
            {/* Main section tabs: the active mode's rail, or the Code
                defaults filtered by capability flags (SP-140-5). Global
                chrome below (platform nav, plugins, settings, logs)
                is shared by every mode. */}
            {ModeRailComponent ? (
              <div
                className={`sidebar-mode-rail ${effectiveSidebarCollapsed ? 'collapsed' : ''}`}
                data-testid="sidebar-mode-rail"
                data-section={modeSection ?? ''}
              >
                <ModeRailComponent
                  activeId={modeSection ?? ''}
                  onSelect={handleModeSectionChange}
                  collapsed={effectiveSidebarCollapsed}
                />
              </div>
            ) : (
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
            )}

            {/* Platform Nav Items (between main sections and settings) */}
            {sortedPlatformNavItems.length > 0 && (
              <>
                <div className="sidebar-icon-rail-divider" role="separator" />
                <nav aria-label="Platform navigation">
                  {sortedPlatformNavItems.map((item) => {
                    const IconComponent = item.icon ? (PLATFORM_ICON_MAP[item.icon] ?? ExternalLink) : ExternalLink;
                    const hasPluginView = pluginViews.some((v) => v.id === item.id);
                    const isActive = currentView === item.id;
                    return (
                      <button
                        key={item.id}
                        role="tab"
                        aria-selected={isActive}
                        className={`rail-icon ${isActive ? 'active' : ''}`}
                        onClick={() => {
                          if (hasPluginView) {
                            onViewChange?.(item.id);
                          } else {
                            window.location.href = item.href;
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

            {/* Plugin Panels (below platform nav) */}
            {pluginPanels.length > 0 && (
              <>
                <div className="sidebar-icon-rail-divider" role="separator" />
                <nav aria-label="Plugin panels">
                  {pluginPanels.map((panel) => {
                    const IconComponent = panel.icon ? (PLATFORM_ICON_MAP[panel.icon] ?? ExternalLink) : ExternalLink;
                    const isActive = effectiveSelectedSection === panel.id;
                    return (
                      <button
                        key={panel.id}
                        role="tab"
                        aria-selected={isActive}
                        aria-controls="sidebar-tabpanel"
                        className={`rail-icon ${isActive ? 'active' : ''}`}
                        onClick={() => handleSectionTabClick(panel.id)}
                        title={panel.label}
                        aria-label={panel.label}
                      >
                        <IconComponent size={18} strokeWidth={1.5} />
                      </button>
                    );
                  })}
                </nav>
              </>
            )}

            {/* Design — visible only when the workspace has a design/ tree */}
            {designPresent && (
              <div role="tablist" aria-orientation="vertical">
                <button
                  role="tab"
                  aria-selected={currentView === 'design'}
                  className={`rail-icon ${currentView === 'design' ? 'active' : ''}`}
                  onClick={() => onViewChange?.('design')}
                  title="Design"
                  aria-label="Design"
                  data-testid="sidebar-design-button"
                >
                  <Palette size={18} strokeWidth={1.5} />
                </button>
              </div>
            )}

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
