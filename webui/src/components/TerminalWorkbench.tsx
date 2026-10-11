import { TerminalTabBar } from '@sprout/ui';
import { Trash2, Columns2, Rows2, Plus, Check, ZoomIn, ZoomOut, Type, Copy, X, MoreHorizontal } from 'lucide-react';
import React, { useCallback, useRef, useState } from 'react';
import type { ReactNode } from 'react';
import { usePersistedBoolean, usePersistedNumber, useOutsideClickDismiss } from '../hooks/usePersistedPref';
import type { TerminalWorkbench as TerminalWorkbenchState } from '../hooks/useTerminalWorkbench';
import { useHostCapabilities } from '../host';
import BackgroundTasks from './BackgroundTasks';
import { FONT_SIZE_DEFAULT, COPY_ON_SELECT_DEFAULT } from './terminalConstants';
import TerminalPane from './TerminalPane';
import {
  FONT_SIZE_MIN,
  FONT_SIZE_MAX,
  FONT_SIZE_STORAGE_KEY,
  parseFontSize,
  clampFontSize,
  COPY_ON_SELECT_STORAGE_KEY,
  parseCopyOnSelect,
} from './terminalPref';

export interface TerminalWorkbenchProps {
  /** Session state from `useTerminalWorkbench`. */
  workbench: TerminalWorkbenchState;
  /**
   * Whether the backend PTY transport is available. `false` runs the panes on
   * the in-browser WASM shell instead.
   */
  isConnected: boolean;
  /** Whether the terminal is showing (the browser-shell notice shows only then). */
  isExpanded: boolean;
  /** Whether the panes have started their sessions. */
  isActive: boolean;
  /** Controls appended to the action bar (the docked terminal's collapse button). */
  trailingActions?: ReactNode;
}

/**
 * The terminal's working surface: the pane tabs, shell picker, split and
 * clear actions, the font and copy-on-select menu, and the panes themselves.
 * Placement-free: the app's docked terminal wraps it in its bottom-panel
 * chrome, and the workspace's terminal view renders it in place.
 */
export function TerminalWorkbench({
  workbench,
  isConnected,
  isExpanded,
  isActive,
  trailingActions,
}: TerminalWorkbenchProps): JSX.Element {
  const { localTerminal } = useHostCapabilities();
  const {
    availableShells,
    shellsLoaded,
    selectedShell,
    setSelectedShell,
    attachableSessions,
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
    handleProcessExit,
    handleAttachAgentSession,
    addSplitPane,
    canAddPaneForDirection,
    activitySessionIds,
    paneHandlesRef,
    sessionShellsRef,
    sessionReattachIdsRef,
  } = workbench;

  const [showShellMenu, setShowShellMenu] = useState(false);
  const [showOverflowMenu, setShowOverflowMenu] = useState(false);
  const shellPickerRef = useRef<HTMLDivElement>(null);
  const overflowMenuRef = useRef<HTMLDivElement>(null);
  useOutsideClickDismiss(showShellMenu, shellPickerRef, () => setShowShellMenu(false));
  useOutsideClickDismiss(showOverflowMenu, overflowMenuRef, () => setShowOverflowMenu(false));

  const [fontSize, setFontSize] = usePersistedNumber(
    FONT_SIZE_STORAGE_KEY,
    FONT_SIZE_DEFAULT,
    parseFontSize,
    clampFontSize,
  );
  const [copyOnSelect, setCopyOnSelect] = usePersistedBoolean(
    COPY_ON_SELECT_STORAGE_KEY,
    COPY_ON_SELECT_DEFAULT,
    parseCopyOnSelect,
  );
  const zoomIn = useCallback(() => setFontSize((prev) => Math.min(FONT_SIZE_MAX, prev + 1)), [setFontSize]);
  const zoomOut = useCallback(() => setFontSize((prev) => Math.max(FONT_SIZE_MIN, prev - 1)), [setFontSize]);
  const resetFontSize = useCallback(() => setFontSize(FONT_SIZE_DEFAULT), [setFontSize]);
  const toggleCopyOnSelect = useCallback(() => setCopyOnSelect((prev) => !prev), [setCopyOnSelect]);

  return (
    <div className="terminal-body">
      {!localTerminal && isExpanded && (
        <div
          className="terminal-cloud-notice"
          title="The browser terminal runs commands in a WASM sandbox. Process-spawning commands may not work."
        >
          Browser terminal — limited command support
        </div>
      )}
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
                        {shellsLoaded && selectedShell && <span className="shell-picker-current">{selectedShell}</span>}
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
                          {trailingActions}
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
                          isActive={isActive}
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
  );
}

export default TerminalWorkbench;
