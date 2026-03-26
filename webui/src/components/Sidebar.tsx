import React, { useEffect, useState, useMemo, useRef, useCallback } from 'react';
import './Sidebar.css';
import { ApiService, ProviderOption, LeditSettings, LeditInstance } from '../services/api';
import SettingsPanel from './SettingsPanel';
import { ProviderLogEntry } from '../providers';
import { useTheme } from '../contexts/ThemeContext';
import { useHotkeys } from '../contexts/HotkeyContext';
import ResizeHandle from './ResizeHandle';
import {
  ScrollText,
  FolderCog,
  Settings,
  ChevronLeft,
  ChevronRight,
  X,
  Keyboard,
  Upload,
  Trash2,
  Search,
  GitBranch,
  History,
  type LucideIcon,
} from 'lucide-react';
import FileTree from './FileTree';
import SearchView from './SearchView';
import GitSidebarPanel, { GitStatusData } from './GitSidebarPanel';
import RevisionListPanel from './RevisionListPanel';

type SectionTab = 'git' | 'history' | 'logs' | 'files' | 'settings' | 'search';

interface SidebarProps {
  isConnected: boolean;
  instances?: LeditInstance[];
  selectedInstancePID?: number;
  isSwitchingInstance?: boolean;
  onInstanceChange?: (e: React.ChangeEvent<HTMLSelectElement>) => void;
  selectedModel?: string;
  onModelChange?: (model: string) => void;
  availableModels?: string[];
  currentView?: 'chat' | 'editor' | 'git';
  onViewChange?: (view: 'chat' | 'editor' | 'git') => void;
  stats?: {
    queryCount: number;
    filesModified: number;
  };
  recentFiles?: Array<{ path: string; modified: boolean }>;
  recentLogs?: string[] | Array<{ id: string; type: string; timestamp: Date; data: any; level: string; category: string }>;
  isMobileMenuOpen?: boolean;
  onMobileMenuToggle?: () => void;
  sidebarCollapsed?: boolean;
  onSidebarToggle?: () => void;
  onFileClick?: (filePath: string, lineNumber?: number) => void;
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
  gitPanel?: {
    gitStatus: GitStatusData | null;
    selectedFiles: Set<string>;
    activeDiffSelectionKey: string | null;
    commitMessage: string;
    isLoading: boolean;
    isActing: boolean;
    isGeneratingCommitMessage: boolean;
    isReviewLoading: boolean;
    actionError: string | null;
    onCommitMessageChange: (value: string) => void;
    onGenerateCommitMessage: () => void;
    onCommit: () => void;
    onRunReview: () => void;
    onToggleFileSelection: (section: 'staged' | 'modified' | 'untracked' | 'deleted', path: string) => void;
    onToggleSectionSelection: (section: 'staged' | 'modified' | 'untracked' | 'deleted') => void;
    onPreviewFile: (section: 'staged' | 'modified' | 'untracked' | 'deleted', path: string) => void;
    onStageFile: (path: string) => void;
    onUnstageFile: (path: string) => void;
    onDiscardFile: (path: string) => void;
    onSectionAction: (section: 'staged' | 'modified' | 'untracked' | 'deleted') => void;
  };
  onOpenRevisionDiff?: (options: { path: string; diff: string; title: string }) => void;
}

/** Section tab definitions */
const SECTION_TABS: { id: SectionTab; icon: LucideIcon; label: string }[] = [
  { id: 'git', icon: GitBranch, label: 'Git' },
  { id: 'history', icon: History, label: 'History' },
  { id: 'files', icon: FolderCog, label: 'Files' },
  { id: 'search', icon: Search, label: 'Search' },
  { id: 'settings', icon: Settings, label: 'Settings' },
  { id: 'logs', icon: ScrollText, label: 'Logs' },
];

const SIDEBAR_MIN_WIDTH = 200;
const SIDEBAR_MAX_WIDTH = 600;
const SIDEBAR_DEFAULT_WIDTH = 288;

const clampSidebarWidth = (value: number): number =>
  Math.max(SIDEBAR_MIN_WIDTH, Math.min(SIDEBAR_MAX_WIDTH, value));

const Sidebar: React.FC<SidebarProps> = ({
  isConnected,
  instances = [],
  selectedInstancePID = 0,
  isSwitchingInstance = false,
  onInstanceChange,
  selectedModel,
  onModelChange,
  availableModels,
  currentView,
  recentFiles = [],
  recentLogs = [],
  isMobileMenuOpen,
  onMobileMenuToggle,
  sidebarCollapsed,
  onSidebarToggle,
  onFileClick,
  provider,
  model,
  logs,
  onProviderChange,
  isOpen = true,
  onClose,
  isMobile = false,
  gitPanel,
  onOpenRevisionDiff
}) => {
  const { themePack, availableThemePacks, setThemePack, importTheme, removeTheme } = useTheme();
  const { applyPreset } = useHotkeys();
  const fileInputRef = useRef<HTMLInputElement>(null);
  const fileTreeRef = useRef<{ refresh: () => void } | null>(null);
  const [importError, setImportError] = useState<string | null>(null);
  const [sidebarWidth, setSidebarWidth] = useState<number>(() => {
    const stored = localStorage.getItem('ledit-sidebar-width');
    return stored ? clampSidebarWidth(Number(stored)) : SIDEBAR_DEFAULT_WIDTH;
  });
  const sidebarWidthRef = useRef(sidebarWidth);
  sidebarWidthRef.current = sidebarWidth;
  const [selectedProvider, setSelectedProvider] = useState(provider || '');
  const [selectedModelState, setSelectedModelState] = useState(model || selectedModel || '');
  const [providers, setProviders] = useState<ProviderOption[]>([]);
  const [isLoadingProviders, setIsLoadingProviders] = useState(false);
  const [selectedSection, setSelectedSection] = useState<SectionTab>('git');
  const [settings, setSettings] = useState<LeditSettings | null>(null);
  const apiService = ApiService.getInstance();

  // Load settings on mount / connection
  useEffect(() => {
    if (!isConnected) return;
    let cancelled = false;
    apiService.getSettings().then((s) => {
      if (!cancelled) setSettings(s);
    }).catch(() => { /* silent */ });
    return () => { cancelled = true; };
  }, [isConnected, apiService]);

  const finalSelectedModel = selectedModel || selectedModelState;
  // Compute available models from providers and selectedProvider
  const availableModelsState = useMemo(() => {
    const providerData = providers.find((p) => p.id === selectedProvider);
    return providerData?.models || [];
  }, [providers, selectedProvider]);
  const finalAvailableModels = availableModels && availableModels.length > 1
    ? availableModels
    : availableModelsState;

  const finalRecentLogs = useMemo(() =>
    recentLogs.length > 0 ? recentLogs : (logs || []),
    [recentLogs, logs]
  );
  const normalizedRecentLogs = useMemo<ProviderLogEntry[]>(
    () => (finalRecentLogs as Array<string | ProviderLogEntry>)
      .filter((log): log is ProviderLogEntry => typeof log !== 'string'),
    [finalRecentLogs]
  );

  const finalIsMobileMenuOpen = isMobileMenuOpen !== undefined ? isMobileMenuOpen : isOpen;
  const finalOnMobileMenuToggle = onMobileMenuToggle || onClose;

  useEffect(() => {
    const fetchProviders = async () => {
      setIsLoadingProviders(true);
      try {
        const data = await apiService.getProviders();
        if (data.providers && data.providers.length > 0) {
          setProviders(data.providers);
          if (data.current_provider) {
            setSelectedProvider(data.current_provider);
          }
          if (data.current_model) {
            setSelectedModelState(data.current_model);
          }
        }
      } catch (error) {
        console.error('Failed to fetch providers:', error);
      } finally {
        setIsLoadingProviders(false);
      }
    };

    fetchProviders();
  }, [apiService, isConnected]);

  useEffect(() => {
    if (!provider || provider === 'unknown') {
      return;
    }
    setSelectedProvider(provider);
  }, [provider, providers]);

  useEffect(() => {
    if (!model || model === 'unknown') {
      return;
    }
    setSelectedModelState(model);
  }, [model, providers, selectedProvider]);

  useEffect(() => {
    if (!selectedProvider) {
      if (providers.length > 0) {
        setSelectedProvider(providers[0].id);
      }
      return;
    }

    const providerExists = providers.some((item) => item.id === selectedProvider);
    if (!providerExists && providers.length > 0) {
      setSelectedProvider(providers[0].id);
    }
  }, [providers, selectedProvider]);

  useEffect(() => {
    if (!selectedProvider) {
      return;
    }

    const providerData = providers.find((item) => item.id === selectedProvider);
    if (!providerData || providerData.models.length === 0) {
      return;
    }

    if (!providerData.models.includes(finalSelectedModel)) {
      setSelectedModelState(providerData.models[0]);
    }
  }, [providers, selectedProvider, finalSelectedModel]);

  const handleProviderChange = (e: React.ChangeEvent<HTMLSelectElement>) => {
    const newProvider = e.target.value;
    setSelectedProvider(newProvider);
    onProviderChange?.(newProvider);
  };

  const handleModelChange = (e: React.ChangeEvent<HTMLSelectElement>) => {
    const newModel = e.target.value;
    // Only update if the model actually changed
    if (newModel !== finalSelectedModel) {
      setSelectedModelState(newModel);
      onModelChange?.(newModel);
    }
  };

  const handleHotkeyPresetChange = async (e: React.ChangeEvent<HTMLSelectElement>) => {
    try {
      await applyPreset(e.target.value);
    } catch (err) {
      console.error('Failed to apply hotkey preset:', err);
    }
  };

  const handleSidebarResize = useCallback((delta: number) => {
    const nextWidth = clampSidebarWidth(sidebarWidthRef.current + delta);

    // Allow drag-to-expand behavior from collapsed mode.
    if (sidebarCollapsed) {
      setSidebarWidth(nextWidth);
      if (delta > 0) {
        onSidebarToggle?.();
      }
      return;
    }

    setSidebarWidth(nextWidth);
  }, [onSidebarToggle, sidebarCollapsed]);

  const handleSidebarResizeEnd = useCallback(() => {
    setSidebarWidth(prev => {
      localStorage.setItem('ledit-sidebar-width', String(prev));
      return prev;
    });
  }, []);

  const handleSidebarResizeReset = useCallback(() => {
    setSidebarWidth(SIDEBAR_DEFAULT_WIDTH);
    localStorage.setItem('ledit-sidebar-width', String(SIDEBAR_DEFAULT_WIDTH));
  }, []);

  const handleSectionTabClick = (tab: SectionTab) => {
    if (sidebarCollapsed) {
      // If collapsed, expand sidebar and switch to the section
      setSelectedSection(tab);
      onSidebarToggle?.();
    } else {
      setSelectedSection(tab);
    }
  };

  // Open search tab on hotkey command
  useEffect(() => {
    const handleHotkey = (e: Event) => {
      const detail = (e as CustomEvent).detail;
      if (detail?.commandId === 'open_search') {
        if (sidebarCollapsed) {
          setSelectedSection('search');
          onSidebarToggle?.();
        } else {
          setSelectedSection('search');
        }
      }
    };
    window.addEventListener('ledit:hotkey', handleHotkey);
    return () => window.removeEventListener('ledit:hotkey', handleHotkey);
  }, [sidebarCollapsed, onSidebarToggle]);

  useEffect(() => {
    if (currentView === 'git') {
      setSelectedSection('git');
    }
  }, [currentView]);

  const handleImportTheme = useCallback((e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;
    setImportError(null);
    const reader = new FileReader();
    reader.onload = (ev) => {
      const text = ev.target?.result;
      if (typeof text !== 'string') return;
      const result = importTheme(text);
      if (!result.success) {
        setImportError(result.warnings?.join('; ') || 'Import failed');
      }
    };
    reader.onerror = () => setImportError('Failed to read file');
    reader.readAsText(file);
    // Reset input so same file can be re-imported
    e.target.value = '';
  }, [importTheme]);

  // ─── Section Renderers ───────────────────────────────────────────────

  /** Logs section: full event/log stream */
  // Terminal-style log formatting helper
  const formatLogLine = (log: ProviderLogEntry): string => {
    const d = log.data as any;
    switch (log.type) {
      case 'query_started': return `Query: ${d?.query?.substring(0, 80) || 'No query'}`;
      case 'tool_start': return `${d?.display_name || d?.tool_name || 'tool'} started`;
      case 'tool_end': return `${d?.display_name || d?.tool_name || 'tool'} ${d?.status === 'failed' ? 'FAILED' : 'done'}`;
      case 'tool_execution': return `${d?.tool || 'tool'}: ${d?.status || 'running'}`;
      case 'file_changed': {
        const p = d?.path || d?.file_path || 'file';
        return `${d?.action || 'changed'}: ${p.split('/').pop() || p}`;
      }
      case 'stream_chunk': return `stream: ${(d?.chunk || '').substring(0, 100)}`;
      case 'error': return `Error: ${d?.message || 'unknown'}`;
      case 'connection_status': return d?.connected ? 'Connected' : 'Disconnected';
      case 'query_completed': return 'Query completed';
      case 'query_progress': return `Step: ${d?.step || '?'}`;
      case 'todo_update': {
        const todos = d?.todos;
        if (!Array.isArray(todos)) return 'todos updated';
        const summary = todos.map((t: any) => `${t.status === 'completed' ? '✓' : t.status === 'in_progress' ? '→' : '○'} ${t.content}`).join('\n  ');
        return `Todos (${todos.filter((t: any) => t.status === 'completed').length}/${todos.length}): ${summary}`;
      }
      case 'agent_message': {
        const msg = String(d?.message || '');
        if (!msg.trim()) return '';
        return `[agent] ${msg.replace(new RegExp(String.fromCharCode(27) + '\\[[0-9;]*[mGKHJABCD]', 'g'), '').substring(0, 120)}`;
      }
      case 'metrics_update': return `Model: ${d?.model || '?'} | Provider: ${d?.provider || '?'}`;
      default: return `${log.type}: ${JSON.stringify(d || {}).substring(0, 80)}`;
    }
  };

  const logsContainerRef = useRef<HTMLDivElement>(null);
  const logsEndRef = useRef<HTMLDivElement>(null);

  // Auto-scroll to bottom when logs change
  useEffect(() => {
    if (logsEndRef.current) {
      logsEndRef.current.scrollIntoView({ behavior: 'smooth' });
    }
  }, [normalizedRecentLogs.length]);

  /** Logs section: terminal-style log output */
  const renderLogsSection = () => {
    // Cap at last 500 logs
    const displayLogs = normalizedRecentLogs.slice(-500);

    if (displayLogs.length === 0) {
      return <div className="empty">No logs yet</div>;
    }

    return (
      <div className="terminal-logs" ref={logsContainerRef}>
        {displayLogs.map((log) => {
          const message = formatLogLine(log);
          // Skip empty log lines
          if (!message) return null;

          const timestamp = new Date(log.timestamp).toLocaleTimeString('en-US', {
            hour12: false,
            hour: '2-digit',
            minute: '2-digit',
            second: '2-digit'
          }) + '.' + new Date(log.timestamp).getMilliseconds().toString().padStart(3, '0');

          return (
            <div key={log.id} className={`term-log-line term-log-${log.level}`}>
              <span className="term-log-time">{timestamp}</span>
              <span className="term-log-type">[{log.type}]</span>
              <span className="term-log-msg">{message}</span>
            </div>
          );
        })}
        <div ref={logsEndRef} />
      </div>
    );
  };

  /** Files section: unified file tree across all views */
  const renderFilesSection = () => {
    return (
      <FileTree
        ref={fileTreeRef as any}
        rootPath="."
        onFileSelect={(file) => onFileClick?.(file.path)}
        onItemCreated={() => {
          fileTreeRef.current?.refresh();
        }}
        onDeleteItem={(path) => {
          fileTreeRef.current?.refresh();
        }}
      />
    );
  };

  /** Settings section: appearance + agent config */
  const renderSettingsSection = () => {
    return (
      <>
        <div className="section">
          <h4>Appearance</h4>
          <div className="config-item">
            <label htmlFor="theme-select">Theme Pack:</label>
            <div style={{ display: 'flex', gap: '4px', alignItems: 'center' }}>
              <select
                id="theme-select"
                value={themePack.id}
                onChange={(e) => setThemePack(e.target.value)}
                className="styled-select"
                style={{ flex: 1 }}
              >
                {availableThemePacks.map((pack) => (
                  <option key={pack.id} value={pack.id}>
                    {pack.name}
                  </option>
                ))}
              </select>
              <button
                type="button"
                className="config-btn"
                onClick={() => fileInputRef.current?.click()}
                title="Import VSCode theme (.json)"
                style={{
                  background: 'var(--bg-tertiary)',
                  border: '1px solid var(--border-default)',
                  borderRadius: 'var(--radius-sm)',
                  padding: '4px 8px',
                  cursor: 'pointer',
                  color: 'var(--text-primary)',
                  display: 'flex',
                  alignItems: 'center',
                  flexShrink: 0,
                }}
              >
                <Upload size={14} />
              </button>
              {themePack.id.startsWith('imported-') && (
                <button
                  type="button"
                  className="config-btn"
                  onClick={() => removeTheme(themePack.id)}
                  title="Remove this imported theme"
                  style={{
                    background: 'var(--color-error-bg)',
                    border: '1px solid var(--accent-error)',
                    borderRadius: 'var(--radius-sm)',
                    padding: '4px 8px',
                    cursor: 'pointer',
                    color: 'var(--accent-error)',
                    display: 'flex',
                    alignItems: 'center',
                    flexShrink: 0,
                  }}
                >
                  <Trash2 size={14} />
                </button>
              )}
            </div>
            <input
              ref={fileInputRef}
              type="file"
              accept=".json"
              style={{ display: 'none' }}
              onChange={handleImportTheme}
            />
            {importError && (
              <div style={{ color: 'var(--accent-error)', fontSize: '12px', marginTop: '2px' }}>
                {importError}
              </div>
            )}
          </div>
          <div className="config-item">
            <label htmlFor="hotkey-preset-select">Apply Hotkey Preset:</label>
            <select
              id="hotkey-preset-select"
              defaultValue=""
              onChange={handleHotkeyPresetChange}
              className="styled-select"
            >
              <option value="" disabled>Choose a preset…</option>
              <option value="vscode">VS Code</option>
              <option value="webstorm">WebStorm</option>
              <option value="ledit">Ledit (Legacy)</option>
            </select>
          </div>
          <div className="config-item" style={{ marginTop: 'var(--space-4, 8px)' }}>
            <button
              type="button"
              className="settings-link-btn"
              onClick={() => {
                // Dispatch event to open hotkeys config
                window.dispatchEvent(new CustomEvent('ledit:open-hotkeys-config'));
              }}
              style={{
                display: 'flex',
                alignItems: 'center',
                gap: '8px',
                padding: '6px 12px',
                background: 'var(--bg-secondary, #2a2a2a)',
                border: '1px solid var(--border-color, #3c3c3c)',
                borderRadius: '4px',
                color: 'var(--text-primary, #fff)',
                cursor: 'pointer',
                fontSize: '13px',
                width: '100%',
              }}
            >
              <Keyboard size={14} />
              Edit Keyboard Shortcuts (JSON)
            </button>
          </div>
        </div>

        <div className="section">
          <h4>Agent Config</h4>
          <div className="config-item">
            <label htmlFor="provider-select">Provider:</label>
            <select
              id="provider-select"
              value={selectedProvider}
              onChange={handleProviderChange}
              disabled={!isConnected || isLoadingProviders}
              className="styled-select"
            >
              {providers.length === 0 && (
                <option value="">
                  {isLoadingProviders ? 'Loading providers...' : 'No providers available'}
                </option>
              )}
              {providers.map(p => (
                <option key={p.id} value={p.id}>{p.name}</option>
              ))}
            </select>
          </div>
          <div className="config-item">
            <label htmlFor="model-select">Model:</label>
            <select
              id="model-select"
              value={finalSelectedModel}
              onChange={handleModelChange}
              disabled={!isConnected || finalAvailableModels.length === 0}
              className="styled-select"
            >
              {finalAvailableModels.map(m => (
                <option key={m} value={m}>{m}</option>
              ))}
            </select>
          </div>
        </div>

        <SettingsPanel
          settings={settings}
          onSettingsChanged={(s) => setSettings(s)}
        />
      </>
    );
  };

  /** Render the content pane based on selected section */
  const renderContentPane = () => {
    switch (selectedSection) {
      case 'git':
        return gitPanel ? <GitSidebarPanel {...gitPanel} /> : <div className="empty">Git unavailable</div>;
      case 'history':
        return onOpenRevisionDiff ? (
          <RevisionListPanel mode="global" allowRollback={true} onOpenDiff={onOpenRevisionDiff} />
        ) : <div className="empty">History unavailable</div>;
      case 'logs':
        return renderLogsSection();
      case 'files':
        return renderFilesSection();
      case 'search':
        return renderSearchSection();
      case 'settings':
        return renderSettingsSection();
      default:
        return null;
    }
  };

  /** Search section: find and replace panel */
  const renderSearchSection = () => {
    return (
      <SearchView onFileClick={onFileClick} />
    );
  };

  return (
    <div className="sidebar-resize-wrapper" style={{ flexShrink: 0 }}>
      <div className={`sidebar ${isMobile ? 'mobile' : ''} ${finalIsMobileMenuOpen ? 'open' : 'closed'} ${sidebarCollapsed ? 'collapsed' : ''}`} style={sidebarCollapsed ? undefined : { width: `${sidebarWidth}px` }}>
        {/* Desktop collapse button */}
      {!isMobile && (
        <button
          className="desktop-collapse-btn"
          onClick={onSidebarToggle}
          aria-label="Toggle sidebar"
        >
          {sidebarCollapsed ? <ChevronRight size={14} /> : <ChevronLeft size={14} />}
        </button>
      )}

      {isMobile && (
        <button
          className="mobile-close-btn"
          onClick={finalOnMobileMenuToggle}
          aria-label="Close sidebar"
        >
          <X size={14} />
        </button>
      )}

      {/* Pinned global header: instance selector */}
      <div className="sidebar-pinned-header">
        {!sidebarCollapsed && (
          <div className="instance-selector">
            <select
              id="sidebar-instance-select"
              value={selectedInstancePID || ''}
              onChange={onInstanceChange}
              disabled={!isConnected || instances.length === 0 || isSwitchingInstance}
              className="instance-select"
              title={instances.find((instance) => instance.pid === selectedInstancePID)?.working_dir || ''}
            >
              {instances.length === 0 && (
                <option value="">No instances</option>
              )}
              {instances.map((instance) => {
                const suffix = [
                  instance.is_host ? 'host' : '',
                  instance.is_current ? 'this' : '',
                ].filter(Boolean).join(', ');
                const name = instance.working_dir.split('/').filter(Boolean).slice(-2).join('/');
                const fullLabel = isMobile
                  ? `pid:${instance.pid}${suffix ? ` (${suffix})` : ''}`
                  : `${name} · pid:${instance.pid}${suffix ? ` (${suffix})` : ''}`;

                return (
                  <option key={instance.id} value={instance.pid}>
                    {fullLabel}
                  </option>
                );
              })}
            </select>
          </div>
        )}
      </div>

      {/* Icon rail (always visible) + Content pane (only when expanded) */}
      <div className="sidebar-body">
        {/* Icon Rail */}
        <div className="sidebar-icon-rail" role="tablist" aria-orientation="vertical">
          {SECTION_TABS.map((tab) => (
            <button
              key={tab.id}
              role="tab"
              aria-selected={selectedSection === tab.id}
              aria-controls="sidebar-tabpanel"
              className={`rail-icon ${selectedSection === tab.id ? 'active' : ''}`}
              onClick={() => handleSectionTabClick(tab.id)}
              title={tab.label}
              aria-label={tab.label}
            >
              <tab.icon size={18} strokeWidth={1.5} />
            </button>
          ))}
        </div>

        {/* Content Pane (only when expanded) */}
        {!sidebarCollapsed && (
          <div className="sidebar-content-pane" role="tabpanel" id="sidebar-tabpanel">
                  <div className="content-pane-scroll">
              {renderContentPane()}
            </div>
          </div>
        )}
      </div>
      </div>
      {!isMobile && (
        <ResizeHandle
          direction="horizontal"
          onResize={handleSidebarResize}
          onResizeEnd={handleSidebarResizeEnd}
          onDoubleClick={handleSidebarResizeReset}
          className="sidebar-resize-handle"
        />
      )}
    </div>
  );
};

export default Sidebar;
