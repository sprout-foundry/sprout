import React, { useEffect, useState, useMemo, useCallback, useRef } from 'react';
import './Sidebar.css';
import { supportsSettings } from '../config/mode';
import { useEditorManager } from '../contexts/EditorManagerContext';
import { useHotkeys } from '../contexts/HotkeyContext';
import { usePlatformNav } from '../contexts/PlatformNavContext';
import { useTheme } from '../contexts/ThemeContext';
import type { WhitespaceRenderingMode } from '../extensions/whitespaceRendering';
import { debugLog } from '../utils/log';
import { ApiService, type SessionSearchResult } from '../services/api';
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
import type { GitSidebarPanelProps } from './GitSidebarPanel';
import LocationSwitcher from './LocationSwitcher';
import ResizeHandle from './ResizeHandle';
import {
  ScrollText,
  FolderCog,
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
  X,
  Loader2,
  Download,
  CircleDollarSign,
} from 'lucide-react';
import AutomationsPanel from './AutomationsPanel';
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
  currentView?: 'chat' | 'editor' | 'git' | 'tasks' | 'billing' | 'team' | 'costs';
  onViewChange?: (view: 'chat' | 'editor' | 'git' | 'tasks' | 'billing' | 'team' | 'costs') => void;
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
  /** Called when a session search result is clicked to restore that session */
  onSessionSearchRestore?: (sessionId: string) => void;
}

/**
 * Section tabs rendered in the icon rail. Main section tabs (git, files, search)
 * are rendered first, followed by platform nav items, then settings (conditional)
 * and logs.
 */
const MAIN_SECTION_TABS: { id: SectionTab; icon: LucideIcon; label: string }[] = [
  { id: 'git', icon: GitBranch, label: 'Git' },
  { id: 'files', icon: FolderCog, label: 'Files' },
  { id: 'search', icon: Search, label: 'Search' },
  { id: 'automations', icon: Zap, label: 'Automations' },
];

/** Valid platform view IDs for type-safe navigation */
const VALID_PLATFORM_VIEWS = new Set(['tasks', 'billing', 'team', 'costs']);

/** Icon name-to-component mapping for platform nav items */
const PLATFORM_ICON_MAP: Record<string, LucideIcon> = {
  'credit-card': CreditCard,
  'list-checks': ListChecks,
  users: Users,
  'layout-dashboard': LayoutDashboard,
  'external-link': ExternalLink,
};

/** Format a relative time string for session search results */
function formatSessionDate(isoString: string): string {
  const date = new Date(isoString);
  const now = new Date();
  const diffMs = now.getTime() - date.getTime();
  const diffSec = Math.floor(diffMs / 1000);
  const diffMin = Math.floor(diffSec / 60);
  const diffHour = Math.floor(diffMin / 60);
  const diffDay = Math.floor(diffHour / 24);
  const diffMonth = Math.floor(diffDay / 30);
  const diffYear = Math.floor(diffDay / 365);

  if (diffSec < 60) return 'just now';
  if (diffMin < 60) return `${diffMin}m ago`;
  if (diffHour < 24) return `${diffHour}h ago`;
  if (diffDay < 30) return `${diffDay}d ago`;
  if (diffMonth < 12) return `${diffMonth}mo ago`;
  return `${diffYear}y ago`;
}

/** Render an excerpt string, converting [matched] terms to highlighted spans */
function renderSessionExcerpt(excerpt: string): React.ReactNode {
  const parts = excerpt.split(/(\[.+?\])/g);
  return parts.map((part, i) => {
    if (part.startsWith('[') && part.endsWith(']')) {
      const text = part.slice(1, -1);
      return (
        <span key={i} className="sidebar-session-search-match">
          {text}
        </span>
      );
    }
    return <span key={i}>{part}</span>;
  });
}

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
  onSessionSearchRestore,
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
  const effectiveSelectedSection = selectedSection || 'git';
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

  // ── Session search state ─────────────────────────────────────────
  const [sessionSearchQuery, setSessionSearchQuery] = useState('');
  const [sessionSearchResults, setSessionSearchResults] = useState<SessionSearchResult[]>([]);
  const [sessionSearchLoading, setSessionSearchLoading] = useState(false);
  const [sessionSearchError, setSessionSearchError] = useState<string | null>(null);
  const [sessionSearchFocused, setSessionSearchFocused] = useState(false);

  // ── Export all sessions state ──────────────────────────────────────
  const [isExportingAll, setIsExportingAll] = useState(false);
  const [exportAllError, setExportAllError] = useState<string | null>(null);

  // Debounced search execution
  const searchTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const executeSessionSearch = useCallback(
    async (q: string) => {
      if (!q.trim()) {
        setSessionSearchResults([]);
        setSessionSearchError(null);
        return;
      }
      setSessionSearchLoading(true);
      setSessionSearchError(null);
      try {
        const resp = await ApiService.getInstance().searchSessions(q.trim(), { limit: 20 });
        setSessionSearchResults(resp.results || []);
      } catch (err) {
        setSessionSearchError(err instanceof Error ? err.message : 'Search failed');
        setSessionSearchResults([]);
      } finally {
        setSessionSearchLoading(false);
      }
    },
    [],
  );

  const handleSessionSearchChange = useCallback(
    (e: React.ChangeEvent<HTMLInputElement>) => {
      const value = e.target.value;
      setSessionSearchQuery(value);

      // Clear previous timer
      if (searchTimerRef.current) {
        clearTimeout(searchTimerRef.current);
      }

      if (!value.trim()) {
        setSessionSearchResults([]);
        setSessionSearchError(null);
        setSessionSearchLoading(false);
        return;
      }

      // Debounce by 300ms
      searchTimerRef.current = setTimeout(() => {
        executeSessionSearch(value);
      }, 300);
    },
    [executeSessionSearch],
  );

  const handleSessionSearchClear = useCallback(() => {
    setSessionSearchQuery('');
    setSessionSearchResults([]);
    setSessionSearchError(null);
    setSessionSearchLoading(false);
    if (searchTimerRef.current) {
      clearTimeout(searchTimerRef.current);
    }
  }, []);

  const handleSessionSearchBlur = useCallback(() => {
    // Delay to allow clicking results before closing
    setTimeout(() => setSessionSearchFocused(false), 150);
  }, []);

  const handleSessionSearchFocus = useCallback(() => {
    setSessionSearchFocused(true);
  }, []);

  const handleSessionSearchResultClick = useCallback(
    (sessionId: string) => {
      onSessionSearchRestore?.(sessionId);
      setSessionSearchQuery('');
      setSessionSearchResults([]);
      setSessionSearchError(null);
      setSessionSearchLoading(false);
      setSessionSearchFocused(false);
    },
    [onSessionSearchRestore],
  );

  // ── Export all sessions handler ────────────────────────────────────
  const handleExportAllSessions = useCallback(async () => {
    if (isExportingAll) return;
    setExportAllError(null);
    setIsExportingAll(true);

    try {
      const response = await ApiService.getInstance().getSessions('current');
      const sessions = Array.isArray(response?.sessions) ? response.sessions : [];

      // Filter to only sessions with messages
      const sessionsToExport = sessions.filter((s) => s.message_count > 0);

      for (const session of sessionsToExport) {
        // Bulk export defaults to safe/redacted (no no_secret_redaction parameter).
        // Users who need unredacted exports should use the per-session ExportDialog.
        const url = `/api/sessions/${encodeURIComponent(session.session_id)}/export?format=markdown&include_tool_calls=false&include_cost=true`;

        // HEAD pre-check: skip silently if the session was deleted between getSessions() and now.
        try {
          const headResp = await fetch(url, { method: 'HEAD' });
          if (!headResp.ok) {
            console.warn(`[export-all] Skipping session ${session.session_id}: HEAD ${headResp.status}`);
            continue;
          }
        } catch {
          console.warn(`[export-all] Skipping session ${session.session_id}: HEAD request failed`);
          continue;
        }

        // Trigger download for each session sequentially with a small delay
        const anchor = document.createElement('a');
        anchor.href = url;
        anchor.download = '';
        document.body.appendChild(anchor);
        anchor.click();
        document.body.removeChild(anchor);

        // Small delay to avoid browser blocking multiple downloads
        await new Promise((resolve) => setTimeout(resolve, 300));
      }
    } catch (err) {
      setExportAllError(`Failed to export sessions: ${err instanceof Error ? err.message : 'Unknown error'}`);
    } finally {
      setIsExportingAll(false);
    }
  }, [isExportingAll]);

  // Show dropdown when: focused + has query, or has active results with query
  const showSessionSearchDropdown =
    !effectiveSidebarCollapsed &&
    sessionSearchFocused &&
    (sessionSearchQuery.trim().length > 0 || sessionSearchLoading || sessionSearchResults.length > 0 || sessionSearchError !== null);

  // Clean up timer on unmount
  useEffect(() => {
    return () => {
      if (searchTimerRef.current) {
        clearTimeout(searchTimerRef.current);
      }
    };
  }, []);

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
    <div className="sidebar-resize-wrapper" style={{ flexShrink: 0 }}>
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
          >
            <SproutLogo showWordmark={false} compact />
          </button>
          {!effectiveSidebarCollapsed ? (
            <>
              <LocationSwitcher
                isConnected={isConnected}
                instances={instances}
                selectedInstancePID={selectedInstancePID}
                isSwitchingInstance={isSwitchingInstance}
                onInstanceChange={onInstanceChange}
                sidebarCollapsed={effectiveSidebarCollapsed}
              />
              {/* Session search input */}
              <div className="sidebar-session-search">
                <Search size={14} className="sidebar-session-search-icon" strokeWidth={2} />
                <input
                  type="text"
                  className="sidebar-session-search-input"
                  placeholder="Search sessions..."
                  value={sessionSearchQuery}
                  onChange={handleSessionSearchChange}
                  onFocus={handleSessionSearchFocus}
                  onBlur={handleSessionSearchBlur}
                  data-testid="sidebar-session-search-input"
                  aria-label="Search sessions"
                />
                {sessionSearchQuery && (
                  <button
                    type="button"
                    className="sidebar-session-search-clear"
                    onClick={handleSessionSearchClear}
                    aria-label="Clear search"
                    data-testid="sidebar-session-search-clear"
                  >
                    <X size={12} strokeWidth={2} />
                  </button>
                )}
              </div>

              {/* Export all sessions button */}
              <div className="sidebar-export-all-wrapper">
                <button
                  type="button"
                  className="sidebar-export-all-btn"
                  onClick={handleExportAllSessions}
                  disabled={isExportingAll}
                  data-testid="sidebar-export-all"
                  aria-label="Export all sessions"
                  title="Export all sessions"
                >
                  {isExportingAll ? (
                    <>
                      <Loader2 size={14} className="sidebar-export-all-spinner" strokeWidth={2} />
                      Exporting...
                    </>
                  ) : (
                    <>
                      <Download size={14} strokeWidth={2} />
                      Export all
                    </>
                  )}
                </button>
                {exportAllError && (
                  <div className="sidebar-export-all-error" role="alert">
                    {exportAllError}
                  </div>
                )}
              </div>
            </>
          ) : null}
        </div>

        {/* Session search dropdown (overlay below pinned header) */}
        {showSessionSearchDropdown && (
          <div className="sidebar-session-search-dropdown" data-testid="sidebar-session-search-dropdown">
            {sessionSearchLoading ? (
              <div className="sidebar-session-search-loading" data-testid="sidebar-session-search-loading">
                <Loader2 size={14} className="sidebar-session-search-spinner" />
                <span>Searching...</span>
              </div>
            ) : sessionSearchError ? (
              <div className="sidebar-session-search-error" data-testid="sidebar-session-search-error">
                {sessionSearchError}
              </div>
            ) : sessionSearchResults.length === 0 && sessionSearchQuery.trim().length > 0 ? (
              <div className="sidebar-session-search-no-results" data-testid="sidebar-session-search-no-results">
                No matching sessions
              </div>
            ) : (
              sessionSearchResults.map((result) => (
                <button
                  key={result.session_id}
                  type="button"
                  className="sidebar-session-search-result"
                  onClick={() => handleSessionSearchResultClick(result.session_id)}
                  data-testid="sidebar-session-search-result"
                  data-session-id={result.session_id}
                >
                  <div className="sidebar-session-search-result-header">
                    <span className="sidebar-session-search-result-name" title={result.name || result.session_id}>
                      {result.name || result.session_id}
                    </span>
                    <span className="sidebar-session-search-result-date" title={result.last_updated}>
                      {formatSessionDate(result.last_updated)}
                    </span>
                  </div>
                  {result.excerpt && (
                    <div className="sidebar-session-search-result-excerpt">
                      {renderSessionExcerpt(result.excerpt)}
                    </div>
                  )}
                  {result.match_score >= 2 && (
                    <span className="sidebar-session-search-result-score" title={`Match score: ${result.match_score}`}>
                      {result.match_score === 3 ? '★' : '☆'}
                    </span>
                  )}
                </button>
              ))
            )}
          </div>
        )}

        {/* Icon rail (always visible) + Content pane (only when expanded) */}
        <div className="sidebar-body">
          {/* Icon Rail */}
          <div className="sidebar-icon-rail" role="navigation" aria-label="Sidebar navigation">
            {/* Main section tabs: git, files, search */}
            <div role="tablist" aria-orientation="vertical">
              {MAIN_SECTION_TABS.map((tab) => (
                <button
                  key={tab.id}
                  role="tab"
                  aria-selected={effectiveSelectedSection === tab.id}
                  aria-controls="sidebar-tabpanel"
                  className={`rail-icon ${effectiveSelectedSection === tab.id ? 'active' : ''}`}
                  onClick={() => handleSectionTabClick(tab.id)}
                  title={tab.label}
                  aria-label={tab.label}
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
                            onViewChange(item.id as 'chat' | 'editor' | 'git' | 'tasks' | 'billing' | 'team' | 'costs');
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
