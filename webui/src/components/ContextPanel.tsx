import { Bot, Wrench, History, Clock, PanelRightOpen, PanelRightClose } from 'lucide-react';
import { useState, useEffect, useMemo, useImperativeHandle, forwardRef } from 'react';
import './ContextPanel.css';

import { ActivityTab } from './contextPanel/ActivityTab';
import AgentChangesPanel from './AgentChangesPanel';
import { SessionsTab } from './contextPanel/SessionsTab';
import { SubagentsTab } from './contextPanel/SubagentsTab';
import type {
  ContextPanelProps,
  ContextPanelHandle,
  ChatContextPanelProps,
  ChatTabId,
  ToolExecution,
  PanelTab,
} from './contextPanel/types';
import { PANEL_COLLAPSED_WIDTH } from './contextPanel/types';
import { useContextPanelState } from './contextPanel/useContextPanelState';
import { useSessionManager } from './contextPanel/useSessionManager';
import { useSubagentRuns } from './contextPanel/useSubagentRuns';

const TAB_IDS = ['activity', 'changes', 'sessions'] as const;

const ContextPanel = forwardRef<ContextPanelHandle, ContextPanelProps>((props, ref) => {
  const isChat = props.context === 'chat';
  const chatProps = isChat ? (props as ChatContextPanelProps) : null;
  const isMobileLayout = props.isMobileLayout ?? false;
  const isTabletLayout = props.isTabletLayout ?? false;
  // Idle mode: the panel stays mounted on desktop even when no chat buffer
  // is focused (e.g. the user is reading a file). The rail renders disabled
  // and the body shows an idle note — the layout column never appears or
  // disappears as the user moves between chat and files.
  const isIdle = props.isIdle ?? false;

  // ── Hooks ──────────────────────────────────────────────────────────

  const toolExecutions = useMemo(() => chatProps?.toolExecutions ?? [], [chatProps]);

  const groupedByQuery = useMemo(() => {
    const groups = new Map<number, ToolExecution[]>();
    for (const tool of toolExecutions) {
      const qid = tool.queryId ?? 0;
      if (!groups.has(qid)) groups.set(qid, []);
      const bucket = groups.get(qid);
      if (bucket) bucket.push(tool);
    }
    return groups;
  }, [toolExecutions]);

  const maxQueryId = useMemo(() => {
    if (groupedByQuery.size === 0) return 0;
    return Math.max(...Array.from(groupedByQuery.keys()));
  }, [groupedByQuery]);

  // Panel state (no external deps — avoids circular hook ordering)
  const state = useContextPanelState(props);

  // Session manager (depends on chatTab from state). The "changes" tab
  // is now self-contained in AgentChangesPanel — it loads its own data
  // from /api/changes/* so there's no revision manager to thread here.
  const sessionManager = useSessionManager(chatProps, state.chatTab, chatProps?.isProcessing ?? false);

  const { subagentRuns, resourceCounts } = useSubagentRuns(chatProps);

  // ── Imperative handle ─────────────────────────────────────────────

  const handleTabClick = (tabId: string) => {
    state.setPanelCollapsed(false);
    const id = tabId as ChatTabId;
    state.setChatTab(id);
    // 'changes' tab is self-loading (AgentChangesPanel fetches on mount).
    if (id === 'sessions' && sessionManager.sessionsCount === 0) {
      sessionManager.loadSessions();
    }
  };

  const imperativeHandle = {
    openTab: (tab: string) => {
      if (TAB_IDS.includes(tab as ChatTabId)) {
        handleTabClick(tab);
      }
    },
    highlightTool: (toolId: string) => {
      if (!isChat || !chatProps) return;
      state.setPanelCollapsed(false);
      state.setChatTab('activity');
      state.setActiveToolId(toolId);
      const tool = chatProps.toolExecutions.find((t) => t.id === toolId);
      if (tool) {
        const qid = tool.queryId ?? 0;
        const maxQid = chatProps.toolExecutions.reduce((max, t) => Math.max(max, t.queryId ?? 0), 0);
        state.setExpandedQueries((prev) => {
          // expandedQueries has inverted semantics for the current turn:
          //   current turn: isExpanded = !isInSet (in-set = collapsed)
          //   past turns:   isExpanded = isInSet  (in-set = expanded)
          // To guarantee the target group is visible, we need:
          //   - current turn: REMOVE qid from the set (so it defaults to expanded)
          //   - past turn:    ADD qid to the set (so it expands)
          if (qid === maxQid) {
            if (!prev.has(qid)) return prev; // already expanded
            const next = new Set(prev);
            next.delete(qid);
            return next;
          }
          if (prev.has(qid)) return prev; // already expanded
          const next = new Set(prev);
          next.add(qid);
          return next;
        });
      }
      setTimeout(() => {
        const el = state.toolRefs.current[toolId];
        if (el != null) {
          el.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
        }
      }, 150);
    },
    closePanel: () => {
      state.setPanelCollapsed(true);
    },
    togglePanel: () => {
      state.setPanelCollapsed((prev) => !prev);
    },
  };

  useImperativeHandle(
    ref,
    () => imperativeHandle,
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [isChat, chatProps, sessionManager.sessionsCount],
  );

  // ── Computed counts for tabs ──────────────────────────────────────

  const activeToolCount = toolExecutions.filter((t) => t.status === 'started' || t.status === 'running').length;

  const activeSubagentCount = subagentRuns.filter(
    ({ tool }) => tool.status === 'started' || tool.status === 'running',
  ).length;

  // ── Tab definitions ───────────────────────────────────────────────

  const chatPanelTabs: PanelTab[] = useMemo(
    () => [
      {
        id: 'activity',
        label: 'Activity',
        icon: <Bot size={14} />,
        count:
          activeSubagentCount > 0
            ? `${activeSubagentCount} active`
            : activeToolCount > 0
              ? `${activeToolCount} active`
              : `${toolExecutions.length} total`,
      },
      {
        id: 'changes',
        label: 'Agent Changes',
        icon: <History size={14} />,
      },
      { id: 'sessions', label: 'Sessions', icon: <Clock size={14} />, count: `${sessionManager.sessionsCount}` },
    ],
    [activeSubagentCount, activeToolCount, toolExecutions.length, sessionManager.sessionsCount],
  );

  const activeTab = chatPanelTabs.find((t) => t.id === state.chatTab) || chatPanelTabs[0];

  // ── Render tab content ────────────────────────────────────────────

  const renderTabContent = () => {
    switch (state.chatTab) {
      case 'activity':
        return (
          <ActivityTab
            toolExecutions={toolExecutions}
            subagentRuns={subagentRuns}
            resourceCounts={resourceCounts}
            groupedByQuery={groupedByQuery}
            maxQueryId={maxQueryId}
            expandedQueries={state.expandedQueries}
            expandedTools={state.expandedTools}
            expandedSubagents={state.expandedSubagents}
            activeToolId={state.activeToolId}
            toolRefs={state.toolRefs}
            toggleQueryGroup={state.toggleQueryGroup}
            toggleToolExpansion={state.toggleToolExpansion}
            toggleSubagentExpansion={state.toggleSubagentExpansion}
            setActiveToolId={state.setActiveToolId}
            setExpandedTools={state.setExpandedTools}
            setExpandedQueries={state.setExpandedQueries}
          />
        );
      case 'changes':
        return <AgentChangesPanel />;
      case 'sessions':
        return (
          <SessionsTab
            sessions={sessionManager.sessions}
            currentSessionId={sessionManager.currentSessionId}
            isLoadingSessions={sessionManager.isLoadingSessions}
            sessionRestoreError={sessionManager.sessionRestoreError}
            loadSessions={sessionManager.loadSessions}
            handleRestoreSession={sessionManager.handleRestoreSession}
            sessionSearchQuery={sessionManager.sessionSearchQuery}
            sessionSearchResults={sessionManager.sessionSearchResults}
            sessionSearchLoading={sessionManager.sessionSearchLoading}
            sessionSearchError={sessionManager.sessionSearchError}
            showSessionSearchDropdown={sessionManager.showSessionSearchDropdown}
            handleSessionSearchChange={sessionManager.handleSessionSearchChange}
            handleSessionSearchClear={sessionManager.handleSessionSearchClear}
            handleSessionSearchBlur={sessionManager.handleSessionSearchBlur}
            handleSessionSearchFocus={sessionManager.handleSessionSearchFocus}
            handleSessionSearchResultClick={sessionManager.handleSessionSearchResultClick}
            isExportingAll={sessionManager.isExportingAll}
            exportAllError={sessionManager.exportAllError}
            handleExportAllSessions={sessionManager.handleExportAllSessions}
          />
        );
      default:
        return null;
    }
  };

  // ── Main render ───────────────────────────────────────────────────

  // Tablet overlay needs a backdrop to dismiss; on desktop the panel is a
  // permanent column (idle mode when no chat is focused) — no backdrop.
  const tabletBackdrop =
    isTabletLayout && !state.panelCollapsed ? (
      <div className="context-panel-backdrop" onClick={() => state.setPanelCollapsed(true)} />
    ) : null;

  // Desktop idle: rail rendered (stable layout, visible but disabled) with
  // an inert body. The idle note lives in the body slot so the aside keeps
  // its exact collapsed/expanded geometry.
  const bodyContent = isIdle ? (
    <div className="side-panel-body">
      <div className="context-panel-empty">Chat context — focus a chat to see activity.</div>
    </div>
  ) : (
    <>
      <div className="side-panel-header">
        <div className="side-panel-title">
          {activeTab.icon}
          <h4>{activeTab.label}</h4>
        </div>
        <div className="side-panel-header-actions">
          <span className="tool-count">{activeTab.count}</span>
        </div>
      </div>
      <div className="side-panel-body">{renderTabContent()}</div>
    </>
  );

  return (
    <>
      {tabletBackdrop}
      {!state.panelCollapsed && !isMobileLayout && !isTabletLayout && (
        <div
          className="context-panel-resizer"
          onMouseDown={state.startResize}
          role="separator"
          aria-orientation="vertical"
          aria-label="Resize context panel"
        />
      )}
      {(isMobileLayout && state.panelCollapsed) || (isTabletLayout && state.panelCollapsed) ? null : (
        <aside
          className={`context-panel ${state.panelCollapsed ? 'collapsed' : ''}${state.isResizing ? ' resizing' : ''}${isMobileLayout ? ' context-panel-mobile' : ''}${isTabletLayout && !state.panelCollapsed ? ' context-panel-tablet-overlay' : ''}${isIdle ? ' context-panel-idle' : ''}`}
          aria-label="Context panel"
          style={
            isMobileLayout || isTabletLayout
              ? undefined
              : { width: `${state.panelCollapsed ? PANEL_COLLAPSED_WIDTH : state.panelWidth}px` }
          }
          ref={state.panelContainerRef}
          data-testid="context-panel"
        >
          <div className="side-panel-rail">
            {chatPanelTabs.map((tab) => (
              <button
                key={tab.id}
                className={`side-rail-btn ${state.chatTab === tab.id ? 'active' : ''}`}
                onClick={() => handleTabClick(tab.id)}
                disabled={isIdle}
                title={tab.label}
                aria-label={tab.label}
                aria-pressed={state.chatTab === tab.id}
                data-testid="context-panel-tab"
              >
                {tab.icon}
              </button>
            ))}
            <button
              className="side-collapse-btn"
              onClick={() => state.setPanelCollapsed((prev) => !prev)}
              title={state.panelCollapsed ? 'Expand panel' : 'Collapse panel'}
              data-testid="context-panel-collapse"
            >
              {state.panelCollapsed ? <PanelRightOpen size={14} /> : <PanelRightClose size={14} />}
            </button>
          </div>

          {/* Content — always rendered; CSS handles fade-out on collapse */}
          <div className="side-panel-content" {...(state.panelCollapsed ? { inert: true, 'aria-hidden': true } : {})}>
            {bodyContent}
          </div>
        </aside>
      )}
    </>
  );
});

ContextPanel.displayName = 'ContextPanel';

export default ContextPanel;
export type { ContextPanelHandle, ContextPanelProps, ChatContextPanelProps } from './contextPanel/types';
