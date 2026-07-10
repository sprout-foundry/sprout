import {
  Trash2,
  Columns2,
  Rows2,
  Plus,
  Check,
  ZoomIn,
  ZoomOut,
  Type,
  Copy,
  ChevronUp,
  ChevronDown,
  X,
  MoreHorizontal,
} from 'lucide-react';
import React, { useState, useEffect, useRef, useCallback } from 'react';
import './Terminal.css';
import { TerminalTabBar, type AttachableSession } from '@sprout/ui';
import { usePersistedBoolean, usePersistedNumber, useOutsideClickDismiss } from '../hooks/usePersistedPref';
import { useTerminalPanes } from '../hooks/useTerminalPanes';
import { ApiService, type ShellInfo } from '../services/api';
import { clientFetch } from '../services/clientSession';
import { notificationBus } from '../services/notificationBus';
import { debugLog } from '../utils/log';
import BackgroundTasks from './BackgroundTasks';
import { FONT_SIZE_DEFAULT, COPY_ON_SELECT_DEFAULT } from './terminalConstants';
import TerminalPane from './TerminalPane';

const TERMINAL_HEIGHT_MIN = 120;
const TERMINAL_HEIGHT_DEFAULT = 400;
const TERMINAL_HEIGHT_MAX_FACTOR = 100;
const TERMINAL_HEIGHT_STORAGE_KEY = 'sprout-terminal-height';

const FONT_SIZE_MIN = 8;
const FONT_SIZE_MAX = 32;
const FONT_SIZE_STORAGE_KEY = 'sprout-terminal-font-size';

const COPY_ON_SELECT_STORAGE_KEY = 'sprout-terminal-copy-on-select';

const parseTerminalHeight = (raw: string | null): number => {
  if (!raw) return TERMINAL_HEIGHT_DEFAULT;
  const n = Number(raw);
  return Number.isFinite(n) ? n : TERMINAL_HEIGHT_DEFAULT;
};

const clampTerminalHeight = (value: number): number => {
  if (!Number.isFinite(value)) return TERMINAL_HEIGHT_DEFAULT;
  if (typeof window === 'undefined') return TERMINAL_HEIGHT_DEFAULT;
  return Math.max(TERMINAL_HEIGHT_MIN, Math.min(window.innerHeight - TERMINAL_HEIGHT_MAX_FACTOR, value));
};

const parseFontSize = (raw: string | null): number => {
  if (!raw) return FONT_SIZE_DEFAULT;
  const n = Number(raw);
  return Number.isFinite(n) ? n : FONT_SIZE_DEFAULT;
};

const clampFontSize = (value: number): number => {
  if (!Number.isFinite(value)) return FONT_SIZE_DEFAULT;
  return Math.max(FONT_SIZE_MIN, Math.min(FONT_SIZE_MAX, value));
};

const parseCopyOnSelect = (raw: string | null, fallback: boolean): boolean => {
  if (raw === null) return fallback;
  return raw === 'true';
};

/** @deprecated Re-exported for backward compatibility with existing tests. */
export { nextActiveAfterClose } from '../hooks/useTerminalPanes';

interface TerminalProps {
  isConnected?: boolean;
  isExpanded?: boolean;
  onToggleExpand?: (expanded: boolean) => void;
}

function Terminal({
  isConnected = true,
  isExpanded: externalIsExpanded = false,
  onToggleExpand,
}: TerminalProps): JSX.Element {
  const getCollapsedHeight = useCallback(() => {
    if (typeof window === 'undefined') return 42;
    return window.innerWidth <= 768 ? 34 : 42;
  }, []);

  const [isExpanded, setIsExpanded] = useState(externalIsExpanded);
  const [hasActivated, setHasActivated] = useState(externalIsExpanded);
  const [terminalHeight, setTerminalHeight] = usePersistedNumber(
    TERMINAL_HEIGHT_STORAGE_KEY,
    TERMINAL_HEIGHT_DEFAULT,
    parseTerminalHeight,
    clampTerminalHeight,
  );
  const [isResizingVertical, setIsResizingVertical] = useState(false);
  const [collapsedHeight, setCollapsedHeight] = useState(getCollapsedHeight);

  // Shell selection state
  const [availableShells, setAvailableShells] = useState<ShellInfo[]>([]);
  const [shellsLoaded, setShellsLoaded] = useState(false);
  const [selectedShell, setSelectedShell] = useState<string | null>(null);
  const [showShellMenu, setShowShellMenu] = useState(false);
  const [showOverflowMenu, setShowOverflowMenu] = useState(false);
  const shellPickerRef = useRef<HTMLDivElement>(null);
  const overflowMenuRef = useRef<HTMLDivElement>(null);

  // Attachable sessions
  const [attachableSessions, setAttachableSessions] = useState<AttachableSession[]>([]);
  const isFetchingSessionsRef = useRef(false);

  // Font size
  const [fontSize, setFontSize] = usePersistedNumber(
    FONT_SIZE_STORAGE_KEY,
    FONT_SIZE_DEFAULT,
    parseFontSize,
    clampFontSize,
  );

  // Copy-on-select
  const [copyOnSelect, setCopyOnSelect] = usePersistedBoolean(
    COPY_ON_SELECT_STORAGE_KEY,
    COPY_ON_SELECT_DEFAULT,
    parseCopyOnSelect,
  );

  /* ---- Attachable session fetching (owns state the hook depends on) ---- */
  const fetchAttachableSessions = useCallback(async () => {
    if (isFetchingSessionsRef.current) return;
    isFetchingSessionsRef.current = true;
    try {
      const response = await clientFetch('/api/terminal/agent-sessions');
      if (!response.ok) {
        throw new Error(`Failed to fetch sessions: ${response.status}`);
      }
      const data = await response.json();
      interface RawSession {
        id: string;
        name?: string;
        status?: string;
      }
      const rawSessions: RawSession[] = data?.sessions || [];
      const sessions: AttachableSession[] = rawSessions.map((s) => ({
        id: s.id,
        name: s.name || s.id,
        status: s.status === 'active' ? 'active' : 'inactive',
      }));
      setAttachableSessions(sessions);
    } catch (err) {
      debugLog('[Terminal] Failed to fetch attachable sessions:', err);
      setAttachableSessions([]);
    } finally {
      isFetchingSessionsRef.current = false;
    }
  }, []);

  /* ---- Terminal panes hook ---- */
  const paneState = useTerminalPanes({
    selectedShell,
    fetchAttachableSessions,
    setAttachableSessions,
    isExpanded,
    terminalHeight,
    setIsResizingVertical,
  });

  const {
    panes,
    focusedPaneId,
    setFocusedPaneId,
    getFocusedPane,
    splitDirection,
    isSplitActive,
    splitStyleForPane,
    handleSplitDividerDragStart,
    addSessionToPane,
    closeSessionInPane,
    removePane,
    renameSessionInPane,
    togglePinInPane,
    switchSessionInPane,
    handleSessionTitleChange,
    handleSessionActivity,
    handlePaneExit,
    handleAttachAgentSession,
    addSplitPane,
    canAddPaneForDirection,
    activitySessionIds,
    paneHandlesRef,
    sessionShellsRef,
    sessionReattachIdsRef,
  } = paneState;

  /* ---- Effects ---- */

  // Expand/collapse sync with external prop
  useEffect(() => {
    setIsExpanded(externalIsExpanded);
    if (externalIsExpanded) {
      setHasActivated(true);
      window.dispatchEvent(new CustomEvent('sprout-terminal-expand'));
    }
  }, [externalIsExpanded]);

  // CSS custom property for reserved height
  useEffect(() => {
    const reservedHeight = isExpanded ? terminalHeight : collapsedHeight;
    document.documentElement.style.setProperty('--sprout-terminal-reserved-height', `${reservedHeight}px`);
    return () => {
      document.documentElement.style.setProperty('--sprout-terminal-reserved-height', `${collapsedHeight}px`);
    };
  }, [collapsedHeight, isExpanded, terminalHeight]);

  // Responsive collapsed height
  useEffect(() => {
    if (typeof window === 'undefined') return undefined;
    const updateCollapsedHeight = () => {
      setCollapsedHeight(getCollapsedHeight());
    };
    updateCollapsedHeight();
    window.addEventListener('resize', updateCollapsedHeight);
    return () => window.removeEventListener('resize', updateCollapsedHeight);
  }, [getCollapsedHeight]);

  // Load available shells
  useEffect(() => {
    let cancelled = false;
    ApiService.getInstance()
      .getAvailableShells()
      .then((res) => {
        if (cancelled) return;
        const shells = res.shells || [];
        setAvailableShells(shells);
        const defaultShell = shells.find((s) => s.default) || shells[0];
        if (defaultShell) {
          setSelectedShell(defaultShell.name);
        }
        setShellsLoaded(true);
      })
      .catch((err) => {
        debugLog('[Terminal] Failed to load available shells:', err);
        notificationBus.notify('warning', 'Terminal', 'Failed to load available shells: ' + String(err));
        setShellsLoaded(true);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  // Poll attachable sessions
  useEffect(() => {
    fetchAttachableSessions();
    const intervalId = setInterval(() => {
      if (isExpanded) {
        fetchAttachableSessions();
      }
    }, 5000);
    return () => {
      clearInterval(intervalId);
    };
  }, [isExpanded, fetchAttachableSessions]);

  // WS events trigger re-fetch
  useEffect(() => {
    const handler = (e: Event) => {
      const detail = (e as CustomEvent).detail;
      if (
        detail?.type === 'terminal_output' ||
        detail?.type === 'pty_exit' ||
        detail?.type === 'agent_session_update'
      ) {
        fetchAttachableSessions();
      }
    };
    window.addEventListener('sprout:wsevent', handler as EventListener);
    return () => window.removeEventListener('sprout:wsevent', handler as EventListener);
  }, [fetchAttachableSessions]);

  // Close menus on outside click / Escape
  useOutsideClickDismiss(showShellMenu, shellPickerRef, () => setShowShellMenu(false));
  useOutsideClickDismiss(showOverflowMenu, overflowMenuRef, () => setShowOverflowMenu(false));

  /* ---- Actions ---- */
  const toggleExpanded = useCallback(() => {
    setIsExpanded((prev) => {
      const next = !prev;
      if (next) {
        setHasActivated(true);
      }
      onToggleExpand?.(next);
      return next;
    });
  }, [onToggleExpand]);

  const zoomIn = useCallback(() => {
    setFontSize((prev) => Math.min(FONT_SIZE_MAX, prev + 1));
  }, [setFontSize]);

  const zoomOut = useCallback(() => {
    setFontSize((prev) => Math.max(FONT_SIZE_MIN, prev - 1));
  }, [setFontSize]);

  const resetFontSize = useCallback(() => {
    setFontSize(FONT_SIZE_DEFAULT);
  }, [setFontSize]);

  const toggleCopyOnSelect = useCallback(() => {
    setCopyOnSelect((prev) => !prev);
  }, [setCopyOnSelect]);

  /* ---- Terminal height resize ---- */
  const handleVerticalResizeStart = useCallback(
    (e: React.MouseEvent) => {
      e.preventDefault();
      const startY = e.clientY;
      const startHeight = terminalHeight;

      const onMove = (ev: MouseEvent) => {
        const delta = startY - ev.clientY;
        const next = clampTerminalHeight(startHeight + delta);
        setTerminalHeight(next);
      };

      const onUp = () => {
        document.removeEventListener('mousemove', onMove);
        document.removeEventListener('mouseup', onUp);
        document.body.style.userSelect = '';
        document.body.style.cursor = '';
        setTerminalHeight((prev) => Math.round(prev));
      };

      document.addEventListener('mousemove', onMove);
      document.addEventListener('mouseup', onUp);
      document.body.style.userSelect = 'none';
      document.body.style.cursor = 'row-resize';
    },
    [terminalHeight],
  );

  /* ---- Initial mount animation guard ---- */
  const hasMountedRef = useRef(false);
  useEffect(() => {
    if (!hasMountedRef.current) {
      hasMountedRef.current = true;
      const timer = setTimeout(() => {
        hasMountedRef.current = false;
      }, 300);
      return () => clearTimeout(timer);
    }
  }, []);

  /* ---- Exit-restart timer (Path 2: last-session restart delay) ---- */
  const exitRestartTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  // Clear any pending restart timer on unmount or when the ref changes
  useEffect(() => {
    return () => {
      if (exitRestartTimerRef.current !== null) {
        clearTimeout(exitRestartTimerRef.current);
        exitRestartTimerRef.current = null;
      }
    };
  }, []);

  /**
   * Wrapper around handlePaneExit that applies a 1.5 s delay ONLY for
   * "Path 2" (last session in the only pane — restart with fresh shell).
   * Paths 1 (split-pane close) and 3 (close tab + switch) fire immediately.
   */
  const handleProcessExit = useCallback(
    (paneId: string, sessionId: string) => {
      const pane = panes.find((p) => p.id === paneId);
      if (!pane) return;

      // Path 2: only pane AND only session → delay 1.5 s so the user sees
      // the "[Process exited]" message before the fresh shell appears.
      if (panes.length === 1 && pane.sessions.length === 1) {
        if (exitRestartTimerRef.current !== null) {
          clearTimeout(exitRestartTimerRef.current);
        }
        exitRestartTimerRef.current = setTimeout(() => {
          exitRestartTimerRef.current = null;
          handlePaneExit(paneId, sessionId);
        }, 1500);
        return;
      }

      // Paths 1 & 3: fire immediately (split-pane close / close-tab-switch)
      handlePaneExit(paneId, sessionId);
    },
    [panes, handlePaneExit],
  );

  /* ---- Computed for rendering ---- */
  const focusedPane = panes.find((p) => p.id === focusedPaneId) ?? panes[0];
  const focusedSession = focusedPane?.sessions.find((s) => s.id === focusedPane.activeSessionId);
  const totalSessions = panes.reduce((acc, p) => acc + p.sessions.length, 0);

  /* ---- Render ---- */
  return (
    <div
      className={[
        'terminal-container',
        isExpanded ? 'expanded' : 'collapsed',
        hasMountedRef.current ? 'initial-mount' : '',
        isResizingVertical ? 'resizing' : '',
      ]
        .filter(Boolean)
        .join(' ')}
      style={{ height: `${isExpanded ? terminalHeight : collapsedHeight}px` }}
      data-testid="terminal-container"
    >
      {isExpanded && (
        <div
          className="terminal-resize-handle"
          onMouseDown={handleVerticalResizeStart}
          title="Drag to resize terminal"
        />
      )}

      {!isExpanded && (
        <button
          type="button"
          className="terminal-collapsed-strip"
          onClick={toggleExpanded}
          title="Expand terminal (Ctrl+`)"
          aria-label="Expand terminal"
          aria-expanded={false}
          data-testid="terminal-toggle"
        >
          <span className="terminal-collapsed-mark" aria-hidden="true">
            $
          </span>
          {focusedSession && (
            <span className="terminal-collapsed-session" title={focusedSession.name}>
              <span className="terminal-collapsed-sep" aria-hidden="true">
                ·
              </span>
              <span className="terminal-collapsed-session-name">{focusedSession.name}</span>
              {totalSessions > 1 && <span className="terminal-collapsed-count">{totalSessions}</span>}
            </span>
          )}
          <span className="terminal-collapsed-spacer" />
          <ChevronUp size={16} aria-hidden="true" data-testid="terminal-collapse" />
        </button>
      )}

      {/* Body */}
      <div className="terminal-body">
        <div className={`terminal-panes-container ${isSplitActive ? `terminal-split-${splitDirection}` : ''}`}>
          {(() => {
            const actionsPaneIdx = splitDirection === 'horizontal' ? 0 : panes.length - 1;
            return panes.map((pane, index) => {
              const isActionsPane = index === actionsPaneIdx;
              return (
                <React.Fragment key={pane.id}>
                  <div
                    className={`terminal-pane-wrapper${isSplitActive && pane.id === focusedPaneId ? ' terminal-pane-wrapper--focused' : ''}`}
                    style={splitStyleForPane(index)}
                    onMouseDown={() => setFocusedPaneId(pane.id)}
                    data-testid="terminal-pane"
                  >
                    <div
                      className={`terminal-pane-tab-bar${isActionsPane ? ' terminal-pane-tab-bar--with-actions' : ''}`}
                      data-testid="terminal-tab-bar"
                    >
                      <div className="terminal-pane-tabs">
                        <TerminalTabBar
                          sessions={pane.sessions}
                          activeSessionId={pane.activeSessionId}
                          onSwitch={(id) => switchSessionInPane(pane.id, id)}
                          onClose={(id) => closeSessionInPane(pane.id, id)}
                          onRename={(id, name) => renameSessionInPane(pane.id, id, name)}
                          onTogglePin={(id) => togglePinInPane(pane.id, id)}
                          attachableSessions={attachableSessions}
                          onAttachSession={handleAttachAgentSession}
                          allowCloseLastTab={panes.length > 1}
                          activitySessionIds={activitySessionIds}
                        />
                      </div>
                      <div className="shell-picker-dropdown" ref={focusedPaneId === pane.id ? shellPickerRef : null}>
                        <button
                          className="terminal-tab-new shell-picker-btn"
                          onClick={() => {
                            if (availableShells.length <= 1) {
                              addSessionToPane(pane.id);
                            } else {
                              setShowShellMenu((prev) => !prev);
                            }
                          }}
                          title="New terminal session"
                          type="button"
                          aria-label="New terminal session"
                          aria-haspopup={availableShells.length > 1}
                          aria-expanded={showShellMenu && focusedPaneId === pane.id}
                        >
                          <Plus size={14} />
                          {shellsLoaded && selectedShell && (
                            <span className="shell-picker-current">{selectedShell}</span>
                          )}
                        </button>
                        {showShellMenu && shellsLoaded && availableShells.length > 1 && focusedPaneId === pane.id && (
                          <div className="shell-picker-menu" role="menu">
                            <div className="shell-picker-header">New Terminal</div>
                            {availableShells.map((shell) => (
                              <button
                                key={shell.name}
                                className="shell-picker-item"
                                onClick={() => {
                                  setSelectedShell(shell.name);
                                  setShowShellMenu(false);
                                  addSessionToPane(pane.id, shell.name);
                                }}
                                type="button"
                                role="menuitem"
                                title={shell.path}
                              >
                                {shell.default && <Check size={12} className="shell-default-indicator" />}
                                {!shell.default && <span className="shell-default-spacer" />}
                                <span className="shell-name">{shell.name}</span>
                                <span className="shell-path">{shell.path}</span>
                              </button>
                            ))}
                          </div>
                        )}
                      </div>
                      {panes.length > 1 && (
                        <button
                          className="terminal-btn pane-close-btn"
                          onClick={() => removePane(pane.id)}
                          title="Close pane"
                          aria-label="Close pane"
                          type="button"
                        >
                          <X size={14} />
                        </button>
                      )}
                      {isActionsPane && (
                        <>
                          <div className="terminal-tab-bar-divider" aria-hidden="true" />
                          <div className="terminal-tab-bar-actions">
                            <BackgroundTasks />
                            <button
                              className="terminal-btn split-btn"
                              onClick={() => addSplitPane('vertical')}
                              disabled={!canAddPaneForDirection('vertical')}
                              title="Split terminal vertically"
                              aria-label="Split terminal vertically"
                              type="button"
                            >
                              <Columns2 size={16} />
                            </button>
                            <button
                              className="terminal-btn split-btn"
                              onClick={() => addSplitPane('horizontal')}
                              disabled={!canAddPaneForDirection('horizontal')}
                              title="Split terminal horizontally"
                              aria-label="Split terminal horizontally"
                              type="button"
                            >
                              <Rows2 size={16} />
                            </button>
                            <button
                              className="terminal-btn clear-btn"
                              onClick={() => {
                                const pane = getFocusedPane();
                                if (pane) {
                                  const handle = paneHandlesRef.current.get(pane.activeSessionId);
                                  handle?.clear();
                                }
                              }}
                              title="Clear terminal"
                              aria-label="Clear terminal"
                              type="button"
                            >
                              <Trash2 size={16} />
                            </button>
                            <div className="terminal-overflow" ref={overflowMenuRef}>
                              <button
                                className="terminal-btn overflow-btn"
                                onClick={() => setShowOverflowMenu((prev) => !prev)}
                                title="More options"
                                aria-label="More options"
                                aria-haspopup="menu"
                                aria-expanded={showOverflowMenu}
                                type="button"
                              >
                                <MoreHorizontal size={16} />
                              </button>
                              {showOverflowMenu && (
                                <div className="terminal-overflow-menu" role="menu">
                                  <div className="terminal-overflow-header">Font size</div>
                                  <button
                                    className="terminal-overflow-item"
                                    onClick={() => {
                                      zoomOut();
                                    }}
                                    type="button"
                                    role="menuitem"
                                  >
                                    <ZoomOut size={14} aria-hidden="true" />
                                    <span className="terminal-overflow-label">Zoom out</span>
                                  </button>
                                  <button
                                    className="terminal-overflow-item"
                                    onClick={() => {
                                      zoomIn();
                                    }}
                                    type="button"
                                    role="menuitem"
                                  >
                                    <ZoomIn size={14} aria-hidden="true" />
                                    <span className="terminal-overflow-label">Zoom in</span>
                                  </button>
                                  <button
                                    className="terminal-overflow-item"
                                    onClick={() => {
                                      resetFontSize();
                                      setShowOverflowMenu(false);
                                    }}
                                    type="button"
                                    role="menuitem"
                                  >
                                    <Type size={14} aria-hidden="true" />
                                    <span className="terminal-overflow-label">Reset to default</span>
                                    <span className="terminal-overflow-meta">{fontSize}px</span>
                                  </button>
                                  <div className="terminal-overflow-divider" role="separator" />
                                  <button
                                    className={`terminal-overflow-item${copyOnSelect ? ' terminal-overflow-item--active' : ''}`}
                                    onClick={() => {
                                      toggleCopyOnSelect();
                                    }}
                                    type="button"
                                    role="menuitemcheckbox"
                                    aria-checked={copyOnSelect}
                                  >
                                    <Copy size={14} aria-hidden="true" />
                                    <span className="terminal-overflow-label">Copy on select</span>
                                    <span className="terminal-overflow-meta">{copyOnSelect ? 'On' : 'Off'}</span>
                                  </button>
                                </div>
                              )}
                            </div>
                            <button
                              className="terminal-btn toggle-btn"
                              onClick={toggleExpanded}
                              title="Collapse terminal (Ctrl+`)"
                              aria-label="Collapse terminal"
                              aria-expanded={isExpanded}
                            >
                              <ChevronDown size={16} />
                            </button>
                          </div>
                        </>
                      )}
                    </div>
                    {pane.sessions.map((session) => {
                      const isActiveSession = session.id === pane.activeSessionId;
                      return (
                        <div
                          key={session.id}
                          style={{
                            display: isActiveSession ? 'flex' : 'none',
                            flex: '1 1 0%',
                            minWidth: 0,
                            minHeight: 0,
                            flexDirection: 'column',
                          }}
                        >
                          <TerminalPane
                            ref={(handle) => {
                              if (handle) {
                                paneHandlesRef.current.set(session.id, handle);
                              } else {
                                paneHandlesRef.current.delete(session.id);
                              }
                            }}
                            isActive={hasActivated || isExpanded}
                            shouldFocus={pane.id === focusedPaneId && isActiveSession}
                            isConnected={isConnected}
                            preferredShell={sessionShellsRef.current.get(session.id) ?? null}
                            reattachSessionId={sessionReattachIdsRef.current.get(session.id) ?? null}
                            fontSize={fontSize}
                            copyOnSelect={copyOnSelect}
                            onProcessExit={() => handleProcessExit(pane.id, session.id)}
                            onTitleChange={(title) => handleSessionTitleChange(pane.id, session.id, title)}
                            onActivity={() => handleSessionActivity(pane.id, session.id)}
                          />
                        </div>
                      );
                    })}
                  </div>
                  {isSplitActive && index < panes.length - 1 && (
                    <div
                      className={`terminal-split-divider terminal-split-divider-${splitDirection}`}
                      onMouseDown={(e) => handleSplitDividerDragStart(e, index)}
                    />
                  )}
                </React.Fragment>
              );
            });
          })()}
        </div>
      </div>
    </div>
  );
}

export default Terminal;
