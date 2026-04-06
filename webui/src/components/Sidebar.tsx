import { useEffect, useState, useMemo, useRef, useCallback } from 'react';
import type { ChangeEvent, KeyboardEvent as ReactKeyboardEvent } from 'react';
import './Sidebar.css';
import { ApiService, type ProviderOption, type LeditSettings, type LeditInstance } from '../services/api';
import SettingsPanel from './SettingsPanel';
import type { ProviderLogEntry } from '../providers/types';
import { useTheme } from '../contexts/ThemeContext';
import { useHotkeys } from '../contexts/HotkeyContext';
import { useLog, debugLog } from '../utils/log';
import ResizeHandle from './ResizeHandle';
import {
  ScrollText,
  FolderCog,
  Settings,
  Keyboard,
  Download,
  Upload,
  Trash2,
  Search,
  GitBranch,
  History,
  type LucideIcon,
} from 'lucide-react';
import FileTree from './FileTree';
import SearchView from './SearchView';
import GitSidebarPanel from './GitSidebarPanel';
import type { GitSidebarPanelProps } from './GitSidebarPanel';
import GitHistoryPanel from './GitHistoryPanel';
import LeditLogo from './LeditLogo';
import LocationSwitcher from './LocationSwitcher';
import WorktreePanel from './WorktreePanel';

type SectionTab = 'git' | 'logs' | 'files' | 'settings' | 'search' | 'worktrees';

interface SidebarProps {
  isConnected: boolean;
  instances?: LeditInstance[];
  selectedInstancePID?: number;
  isSwitchingInstance?: boolean;
  onInstanceChange?: (pid: number) => void;
  selectedModel?: string;
  onModelChange?: (model: string) => void;
  selectedPersona?: string;
  onPersonaChange?: (persona: string) => void;
  availableModels?: string[];
  currentView?: 'chat' | 'editor' | 'git';
  onViewChange?: (view: 'chat' | 'editor' | 'git') => void;
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
  gitPanel?: GitSidebarPanelProps & {
    apiService?: ApiService;
    openWorkspaceBuffer?: (options: {
      kind: 'chat' | 'diff' | 'review';
      path: string;
      title: string;
      content?: string;
      ext?: string;
      isPinned?: boolean;
      isClosable?: boolean;
      metadata?: Record<string, unknown>;
    }) => string;
  };
}

/** Section tab definitions */
const SECTION_TABS: { id: SectionTab; icon: LucideIcon; label: string }[] = [
  { id: 'git', icon: GitBranch, label: 'Git' },
  { id: 'files', icon: FolderCog, label: 'Files' },
  { id: 'search', icon: Search, label: 'Search' },
  { id: 'settings', icon: Settings, label: 'Settings' },
  { id: 'logs', icon: ScrollText, label: 'Logs' },
  { id: 'worktrees', icon: GitBranch, label: 'Worktrees' },
];

const SIDEBAR_MIN_WIDTH = 200;
const SIDEBAR_MAX_WIDTH = 600;
const SIDEBAR_DEFAULT_WIDTH = 288;
const MAX_LOG_ROWS = 1000;

const clampSidebarWidth = (value: number): number => Math.max(SIDEBAR_MIN_WIDTH, Math.min(SIDEBAR_MAX_WIDTH, value));

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
  onFileClick,
  provider,
  model,
  logs,
  onProviderChange,
  isOpen = true,
  onClose,
  isMobile = false,
  gitPanel,
}: SidebarProps): JSX.Element {
  const log = useLog();
  const { themePack, availableThemePacks, setThemePack, importTheme, removeTheme } = useTheme();
  const { applyPreset } = useHotkeys();
  const fileInputRef = useRef<HTMLInputElement>(null);
  const fileTreeRef = useRef<{
    refresh: () => void;
    revealFile: (filePath: string) => void;
  } | null>(null);
  const [importError, setImportError] = useState<string | null>(null);
  const [sidebarWidth, setSidebarWidth] = useState<number>(() => {
    const stored = localStorage.getItem('ledit-sidebar-width');
    return stored ? clampSidebarWidth(Number(stored)) : SIDEBAR_DEFAULT_WIDTH;
  });
  const sidebarWidthRef = useRef(sidebarWidth);
  sidebarWidthRef.current = sidebarWidth;
  const [selectedProvider, setSelectedProvider] = useState(provider || '');
  const [selectedModelState, setSelectedModelState] = useState(model || selectedModel || '');
  const [selectedPersonaState, setSelectedPersonaState] = useState<string>(
    selectedPersona || stats?.persona || 'orchestrator',
  );
  const [personas, setPersonas] = useState<{ id: string; name: string; enabled: boolean }[]>([]);
  const [isLoadingPersonas, setIsLoadingPersonas] = useState(false);
  const [providers, setProviders] = useState<ProviderOption[]>([]);
  const [isLoadingProviders, setIsLoadingProviders] = useState(false);
  const hasHydratedProviderStateRef = useRef(false);
  const [selectedSection, setSelectedSection] = useState<SectionTab>('git');
  const [gitSubTab, setGitSubTab] = useState<'changes' | 'history'>('changes');
  const [worktreePanelOpen, setWorktreePanelOpen] = useState(false);
  const [settings, setSettings] = useState<LeditSettings | null>(null);
  const apiService = ApiService.getInstance();
  const effectiveSidebarCollapsed = !isMobile && !!sidebarCollapsed;

  // Sync persona state when stats change (e.g., from another client's persona change)
  useEffect(() => {
    if (stats?.persona && stats.persona !== selectedPersonaState) {
      setSelectedPersonaState(stats.persona);
    }
  }, [stats?.persona, selectedPersonaState]);

  // Load settings on mount / connection
  useEffect(() => {
    if (!isConnected) return;
    let cancelled = false;
    apiService
      .getSettings()
      .then((s) => {
        if (!cancelled) setSettings(s);
      })
      .catch((err) => {
        debugLog('Failed to load settings:', err);
      });
    return () => {
      cancelled = true;
    };
  }, [isConnected, apiService]);

  const finalSelectedModel = selectedModel || selectedModelState;
  // Compute available models from providers and selectedProvider
  const availableModelsState = useMemo(() => {
    const providerData = providers.find((p) => p.id === selectedProvider);
    return providerData?.models || [];
  }, [providers, selectedProvider]);
  const finalAvailableModels = availableModels && availableModels.length > 1 ? availableModels : availableModelsState;

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

  useEffect(() => {
    const fetchProviders = async () => {
      setIsLoadingProviders(true);
      try {
        const data = await apiService.getProviders();
        if (data.providers && data.providers.length > 0) {
          setProviders(data.providers);
          if (!hasHydratedProviderStateRef.current) {
            const initialProvider =
              provider && provider !== 'unknown' ? provider : data.current_provider || data.providers[0]?.id || '';
            if (initialProvider) {
              setSelectedProvider(initialProvider);
            }

            const initialModel =
              model && model !== 'unknown'
                ? model
                : selectedModel && selectedModel !== 'unknown'
                  ? selectedModel
                  : data.current_model || '';
            if (initialModel) {
              setSelectedModelState(initialModel);
            }

            hasHydratedProviderStateRef.current = true;
          }
        }
      } catch (error) {
        log.error(`Failed to fetch providers: ${error instanceof Error ? error.message : String(error)}`, {
          title: 'Provider Load Error',
        });
      } finally {
        setIsLoadingProviders(false);
      }
    };

    fetchProviders();
  }, [apiService, isConnected, model, provider, selectedModel, log]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    if (!provider || provider === 'unknown') {
      return;
    }
    setSelectedProvider(provider);
  }, [provider]);

  useEffect(() => {
    if (!model || model === 'unknown') {
      return;
    }
    setSelectedModelState(model);
  }, [model]);

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

  const handleProviderChange = (e: ChangeEvent<HTMLSelectElement>) => {
    const newProvider = e.target.value;
    setSelectedProvider(newProvider);
    onProviderChange?.(newProvider);
  };

  const handleModelChange = (e: ChangeEvent<HTMLSelectElement>) => {
    const newModel = e.target.value;
    // Only update if the model actually changed
    if (newModel !== finalSelectedModel) {
      setSelectedModelState(newModel);
      onModelChange?.(newModel);
    }
  };

  const handlePersonaChange = (e: ChangeEvent<HTMLSelectElement>) => {
    const newPersona = e.target.value;
    setSelectedPersonaState(newPersona);
    onPersonaChange?.(newPersona);
  };

  // Load personas from the backend
  useEffect(() => {
    if (!isConnected) return;

    const fetchPersonas = async () => {
      setIsLoadingPersonas(true);
      try {
        const data = await apiService.getSubagentTypes();
        const enabledPersonas = Object.values(data.subagent_types)
          .filter((p) => p.enabled && p.id && p.name) // Skip empty/corrupted entries
          .map((p) => ({
            id: p.id,
            name: p.name || p.id,
            enabled: p.enabled,
          }));

        // Always add orchestrator as an option (it's the default)
        const allPersonas = [
          { id: 'orchestrator', name: 'Orchestrator', enabled: true },
          ...enabledPersonas.filter((p) => p.id !== 'orchestrator'),
        ];

        setPersonas(allPersonas);
      } catch (error) {
        log.error(`Failed to fetch personas: ${error instanceof Error ? error.message : String(error)}`, {
          title: 'Persona Load Error',
        });
        // Fallback to just orchestrator
        setPersonas([{ id: 'orchestrator', name: 'Orchestrator', enabled: true }]);
      } finally {
        setIsLoadingPersonas(false);
      }
    };

    fetchPersonas();
  }, [apiService, isConnected, log]); // eslint-disable-line react-hooks/exhaustive-deps

  const handleHotkeyPresetChange = async (e: ChangeEvent<HTMLSelectElement>) => {
    try {
      await applyPreset(e.target.value);
    } catch (err) {
      log.error(`Failed to apply hotkey preset: ${err instanceof Error ? err.message : String(err)}`, {
        title: 'Hotkey Error',
      });
    }
  };

  const handleSidebarResize = useCallback(
    (delta: number) => {
      const nextWidth = clampSidebarWidth(sidebarWidthRef.current + delta);

      // Allow drag-to-expand behavior from collapsed mode.
      if (effectiveSidebarCollapsed) {
        setSidebarWidth(nextWidth);
        if (delta > 0) {
          onSidebarToggle?.();
        }
        return;
      }

      setSidebarWidth(nextWidth);
    },
    [effectiveSidebarCollapsed, onSidebarToggle],
  );

  const handleSidebarResizeEnd = useCallback(() => {
    setSidebarWidth((prev) => {
      localStorage.setItem('ledit-sidebar-width', String(prev));
      return prev;
    });
  }, []);

  const handleSidebarResizeReset = useCallback(() => {
    setSidebarWidth(SIDEBAR_DEFAULT_WIDTH);
    localStorage.setItem('ledit-sidebar-width', String(SIDEBAR_DEFAULT_WIDTH));
  }, []);

  const handleSectionTabClick = (tab: SectionTab) => {
    if (effectiveSidebarCollapsed) {
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
        if (effectiveSidebarCollapsed) {
          setSelectedSection('search');
          onSidebarToggle?.();
        } else {
          setSelectedSection('search');
        }
      }
    };
    window.addEventListener('ledit:hotkey', handleHotkey);
    return () => window.removeEventListener('ledit:hotkey', handleHotkey);
  }, [effectiveSidebarCollapsed, onSidebarToggle]);

  // Handle reveal-in-explorer event
  useEffect(() => {
    const handleReveal = (e: Event) => {
      const detail = (e as CustomEvent).detail;
      const filePath = detail?.path;
      if (!filePath) return;

      // Switch to files tab
      if (effectiveSidebarCollapsed) {
        setSelectedSection('files');
        onSidebarToggle?.();
      } else {
        setSelectedSection('files');
      }

      // Give the section switch time to render, then reveal
      setTimeout(() => {
        fileTreeRef.current?.revealFile(filePath);
      }, 100);
    };

    window.addEventListener('ledit:reveal-in-explorer', handleReveal);
    return () => window.removeEventListener('ledit:reveal-in-explorer', handleReveal);
  }, [effectiveSidebarCollapsed, onSidebarToggle]);

  const handleLogoToggle = useCallback(() => {
    if (isMobile) {
      finalOnMobileMenuToggle?.();
      return;
    }
    onSidebarToggle?.();
  }, [finalOnMobileMenuToggle, isMobile, onSidebarToggle]);

  useEffect(() => {
    if (currentView === 'git') {
      setSelectedSection('git');
      setGitSubTab('changes');
    }
  }, [currentView]);

  const handleImportTheme = useCallback(
    (e: ChangeEvent<HTMLInputElement>) => {
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
    },
    [importTheme],
  );

  // ─── Section Renderers ───────────────────────────────────────────────

  /** Logs section: full event/log stream */
  // Terminal-style log formatting helper
  const formatLogLine = (logEntry: ProviderLogEntry): string => {
    const d = logEntry.data as Record<string, unknown> | null | undefined;
    switch (logEntry.type) {
      case 'query_started':
        return `Query: ${String(d?.query ?? '').substring(0, 80) || 'No query'}`;
      case 'tool_start':
        return `${String(d?.display_name || d?.tool_name || 'tool')} started`;
      case 'tool_end':
        return `${String(d?.display_name || d?.tool_name || 'tool')} ${d?.status === 'failed' ? 'FAILED' : 'done'}`;
      case 'tool_execution':
        return `${String(d?.tool || 'tool')}: ${String(d?.status || 'running')}`;
      case 'file_changed': {
        const p = String(d?.path || d?.file_path || 'file');
        return `${String(d?.action || 'changed')}: ${p.split('/').pop() || p}`;
      }
      case 'stream_chunk':
        return `stream: ${String(d?.chunk || '').substring(0, 100)}`;
      case 'error':
        return `Error: ${String(d?.message || 'unknown')}`;
      case 'connection_status':
        return d?.connected ? 'Connected' : 'Disconnected';
      case 'query_completed':
        return 'Query completed';
      case 'query_progress':
        return `Step: ${d?.step ?? '?'}`;
      case 'todo_update': {
        const todos = d?.todos;
        if (!Array.isArray(todos)) return 'todos updated';
        const summary = todos
          .map((t: Record<string, unknown>) => {
            const status = String(t.status);
            const icon = status === 'completed' ? '✓' : status === 'in_progress' ? '→' : '○';
            return `${icon} ${String(t.content)}`;
          })
          .join('\n  ');
        const completedCount = todos.filter((t: Record<string, unknown>) => t.status === 'completed').length;
        return `Todos (${completedCount}/${todos.length}): ${summary}`;
      }
      case 'agent_message': {
        const msg = String(d?.message || '');
        if (!msg.trim()) return '';
        return `[agent] ${msg.replace(new RegExp(`${String.fromCharCode(27)}\\[[0-9;]*[mGKHJABCD]`, 'g'), '').substring(0, 120)}`;
      }
      case 'metrics_update':
        return `Model: ${String(d?.model || '?')} | Provider: ${String(d?.provider || '?')}`;
      default:
        return `${logEntry.type}: ${JSON.stringify(d || {}).substring(0, 80)}`;
    }
  };

  const logsContainerRef = useRef<HTMLDivElement>(null);
  const logsEndRef = useRef<HTMLDivElement>(null);
  const shouldAutoScrollLogsRef = useRef(true);

  const buildLogTimestamp = useCallback((value: Date | string) => {
    const date = new Date(value);
    return `${date.toLocaleTimeString('en-US', {
      hour12: false,
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
    })}.${date.getMilliseconds().toString().padStart(3, '0')}`;
  }, []);

  const getRenderedLogLines = useCallback(
    (entries: typeof normalizedRecentLogs) => {
      return entries
        .map((logEntry) => {
          const message = formatLogLine(logEntry);
          if (!message) {
            return null;
          }

          return `${buildLogTimestamp(logEntry.timestamp)} [${logEntry.type}] ${message}`;
        })
        .filter((line): line is string => Boolean(line));
    },
    [buildLogTimestamp],
  );

  // Auto-scroll to bottom when logs change
  useEffect(() => {
    if (shouldAutoScrollLogsRef.current && logsEndRef.current) {
      logsEndRef.current.scrollIntoView({ behavior: 'smooth' });
    }
  }, [normalizedRecentLogs.length]);

  /** Logs section: terminal-style log output */
  const renderLogsSection = () => {
    const displayLogs = normalizedRecentLogs.slice(-MAX_LOG_ROWS);
    // eslint-disable-next-line testing-library/render-result-naming-convention
    const formattedLines = getRenderedLogLines(displayLogs);

    const handleLogsScroll = () => {
      const container = logsContainerRef.current;
      if (!container) {
        return;
      }
      const distanceFromBottom = container.scrollHeight - container.scrollTop - container.clientHeight;
      shouldAutoScrollLogsRef.current = distanceFromBottom < 24;
    };

    const downloadLogs = (format: 'txt' | 'json') => {
      const content = format === 'json' ? JSON.stringify(displayLogs, null, 2) : formattedLines.join('\n');
      const blob = new Blob([content], {
        type: format === 'json' ? 'application/json' : 'text/plain;charset=utf-8',
      });
      const url = URL.createObjectURL(blob);
      const anchor = document.createElement('a');
      const timestamp = new Date().toISOString().replace(/[:.]/g, '-');
      anchor.href = url;
      anchor.download = `ledit-logs-${timestamp}.${format}`;
      document.body.appendChild(anchor);
      anchor.click();
      document.body.removeChild(anchor);
      URL.revokeObjectURL(url);
    };

    if (formattedLines.length === 0) {
      return <div className="empty">No logs yet</div>;
    }

    return (
      <div className="logs-pane">
        <div className="logs-toolbar">
          <div className="logs-toolbar-summary">
            <span>{formattedLines.length} rows</span>
            <span>buffered up to {MAX_LOG_ROWS}</span>
          </div>
          <div className="logs-toolbar-actions">
            <button
              className="logs-toolbar-btn"
              onClick={() => downloadLogs('txt')}
              title="Download visible logs as text"
            >
              <Download size={13} />
              TXT
            </button>
            <button
              className="logs-toolbar-btn"
              onClick={() => downloadLogs('json')}
              title="Download visible logs as JSON"
            >
              <Download size={13} />
              JSON
            </button>
          </div>
        </div>
        <div className="terminal-logs" ref={logsContainerRef} onScroll={handleLogsScroll}>
          {displayLogs.map((logEntry) => {
            const message = formatLogLine(logEntry);
            // Skip empty log lines
            if (!message) return null;

            const timestamp = buildLogTimestamp(logEntry.timestamp);

            return (
              <div key={logEntry.id} className={`term-log-line term-log-${logEntry.level}`}>
                <span className="term-log-time">{timestamp}</span>
                <span className="term-log-type">[{logEntry.type}]</span>
                <span className="term-log-msg">{message}</span>
              </div>
            );
          })}
          <div ref={logsEndRef} />
        </div>
      </div>
    );
  };

  /** Files section: unified file tree across all views */
  const renderFilesSection = () => {
    return (
      <FileTree
        ref={fileTreeRef as React.RefObject<{ refresh: () => void; revealFile: (filePath: string) => void }>}
        rootPath="."
        workspaceRoot={gitPanel?.workspaceRoot}
        onFileSelect={(file) => onFileClick?.(file.path)}
        onItemCreated={() => {
          fileTreeRef.current?.refresh();
        }}
        onDeleteItem={(_path) => {
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
              <div style={{ color: 'var(--accent-error)', fontSize: '12px', marginTop: '2px' }}>{importError}</div>
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
              <option value="" disabled>
                Choose a preset…
              </option>
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
                <option value="">{isLoadingProviders ? 'Loading providers...' : 'No providers available'}</option>
              )}
              {providers.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name}
                </option>
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
              {finalAvailableModels.map((m) => (
                <option key={m} value={m}>
                  {m}
                </option>
              ))}
            </select>
          </div>
          <div className="config-item">
            <label htmlFor="persona-select">Persona:</label>
            <select
              id="persona-select"
              value={selectedPersonaState}
              onChange={handlePersonaChange}
              disabled={!isConnected || isLoadingPersonas}
              className="styled-select"
            >
              {isLoadingPersonas ? (
                <option value="">Loading personas...</option>
              ) : (
                personas.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.name}
                  </option>
                ))
              )}
            </select>
          </div>
        </div>

        <SettingsPanel settings={settings} onSettingsChanged={(s) => setSettings(s)} />
      </>
    );
  };

  // Keyboard navigation for tab bars (arrow keys + Home/End)
  const handleTabBarKeyDown = (e: ReactKeyboardEvent<HTMLDivElement>) => {
    const tabs = Array.from(e.currentTarget.querySelectorAll<HTMLButtonElement>('[role="tab"]:not([disabled])'));
    const currentIndex = tabs.indexOf(document.activeElement as HTMLButtonElement);
    if (currentIndex === -1) return;
    let nextIndex = currentIndex;
    if (e.key === 'ArrowRight' || e.key === 'ArrowDown') nextIndex = (currentIndex + 1) % tabs.length;
    else if (e.key === 'ArrowLeft' || e.key === 'ArrowUp') nextIndex = (currentIndex - 1 + tabs.length) % tabs.length;
    else if (e.key === 'Home') nextIndex = 0;
    else if (e.key === 'End') nextIndex = tabs.length - 1;
    else return;
    e.preventDefault();
    tabs[nextIndex].focus();
  };

  /** Render the content pane based on selected section */
  /** Search section: find and replace panel */
  const renderSearchSection = () => {
    return <SearchView onFileClick={onFileClick} />;
  };

  /** Worktrees section: git worktree management */
  const renderWorktreesSection = () => {
    if (!worktreePanelOpen) {
      return (
        <div className="worktree-panel-trigger" onClick={() => setWorktreePanelOpen(true)}>
          <GitBranch size={20} />
          <span>Git Worktrees</span>
        </div>
      );
    }

    return (
      <div className="worktree-panel-wrapper">
        <WorktreePanel onClose={() => setWorktreePanelOpen(false)} />
      </div>
    );
  };

  const renderContentPane = () => {
    switch (selectedSection) {
      case 'git': {
        return (
          <>
            {/* Sub-tab bar: Current Changes / Commit History */}
            {gitPanel && (
              <div
                className="git-sidebar-tab-bar"
                role="tablist"
                aria-label="Git sub-sections"
                onKeyDown={handleTabBarKeyDown}
              >
                <button
                  type="button"
                  role="tab"
                  id="git-tab-current-changes"
                  aria-controls="git-panel-current-changes"
                  aria-selected={gitSubTab === 'changes'}
                  className={`git-sidebar-tab ${gitSubTab === 'changes' ? 'active' : ''}`}
                  onClick={() => setGitSubTab('changes')}
                >
                  <GitBranch size={14} />
                  <span>Current Changes</span>
                </button>
                <button
                  type="button"
                  role="tab"
                  id="git-tab-commit-history"
                  aria-controls="git-panel-commit-history"
                  aria-selected={gitSubTab === 'history'}
                  className={`git-sidebar-tab ${gitSubTab === 'history' ? 'active' : ''}`}
                  onClick={() => setGitSubTab('history')}
                >
                  <History size={14} />
                  <span>Commit History</span>
                </button>
              </div>
            )}

            {/* Current Changes sub-tab: working tree panel */}
            {gitSubTab === 'changes' ? (
              <div id="git-panel-current-changes" role="tabpanel" aria-labelledby="git-tab-current-changes">
                {gitPanel ? <GitSidebarPanel {...gitPanel} /> : <div className="empty">Git unavailable</div>}
              </div>
            ) : (
              /* Commit History sub-tab: GitHistoryPanel */
              <div
                id="git-panel-commit-history"
                role="tabpanel"
                aria-labelledby="git-tab-commit-history"
                className="history-pane"
              >
                {gitPanel ? (
                  <GitHistoryPanel
                    apiService={gitPanel.apiService ?? ApiService.getInstance()}
                    isActing={gitPanel.isActing}
                    openWorkspaceBuffer={gitPanel.openWorkspaceBuffer ?? ((_options) => '')}
                  />
                ) : (
                  <div className="empty">Git unavailable</div>
                )}
              </div>
            )}
          </>
        );
      }
      case 'logs':
        return renderLogsSection();
      case 'files':
        return renderFilesSection();
      case 'search':
        return renderSearchSection();
      case 'worktrees':
        return renderWorktreesSection();
      case 'settings':
        return renderSettingsSection();
      default:
        return null;
    }
  };

  return (
    <div className="sidebar-resize-wrapper" style={{ flexShrink: 0 }}>
      <div
        className={`sidebar ${isMobile ? 'mobile' : ''} ${finalIsMobileMenuOpen ? 'open' : 'closed'} ${effectiveSidebarCollapsed ? 'collapsed' : ''}`}
        style={effectiveSidebarCollapsed ? undefined : isMobile ? undefined : { width: `${sidebarWidth}px` }}
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
            <LeditLogo showWordmark={false} compact />
          </button>
          {!effectiveSidebarCollapsed ? (
            <LocationSwitcher
              isConnected={isConnected}
              instances={instances}
              selectedInstancePID={selectedInstancePID}
              isSwitchingInstance={isSwitchingInstance}
              onInstanceChange={onInstanceChange}
              sidebarCollapsed={effectiveSidebarCollapsed}
            />
          ) : null}
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
          {!effectiveSidebarCollapsed && (
            <div className="sidebar-content-pane" role="tabpanel" id="sidebar-tabpanel">
              <div className="content-pane-scroll">{renderContentPane()}</div>
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
}

export default Sidebar;
