import { Bot, Wrench, History, ListTodo, Clock, Activity, PanelRightOpen, PanelRightClose } from 'lucide-react';
import { useState, useEffect, useMemo, useImperativeHandle, forwardRef } from 'react';
import './ContextPanel.css';

import { SessionsTab } from './contextPanel/SessionsTab';
import { StatusTab } from './contextPanel/StatusTab';
import { SubagentsTab } from './contextPanel/SubagentsTab';
import { ToolsTab } from './contextPanel/ToolsTab';
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
import { useStatusMetrics } from './contextPanel/useStatusMetrics';
import { useSubagentRuns } from './contextPanel/useSubagentRuns';
import AgentChangesPanel from './AgentChangesPanel';
import TodoPanel from './TodoPanel';

const TAB_IDS = ['subagents', 'tools', 'changes', 'tasks', 'status', 'sessions'] as const;

const ContextPanel = forwardRef<ContextPanelHandle, ContextPanelProps>((props, ref) => {
  const isChat = props.context === 'chat';
  const chatProps = isChat ? (props as ChatContextPanelProps) : null;
  const isMobileLayout = props.isMobileLayout ?? false;
  const isTabletLayout = props.isTabletLayout ?? false;

  // ── Hooks ──────────────────────────────────────────────────────────

  const toolExecutions = useMemo(() => chatProps?.toolExecutions ?? [], [chatProps]);
  const currentTodos = chatProps?.currentTodos ?? [];

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

  const statusMetrics = useStatusMetrics(chatProps, toolExecutions, maxQueryId);
  const { subagentRuns, resourceCounts } = useSubagentRuns(chatProps);

  // ── Live duration timer ───────────────────────────────────────────

  const [liveDurationMs, setLiveDurationMs] = useState<number | null>(null);
  const isProcessing = isChat ? (chatProps?.isProcessing ?? false) : false;

  const msgArr: Array<{ timestamp: Date }> = chatProps ? chatProps.messages : [];
  const messageCount = msgArr.length;
  const firstMessageTs =
    messageCount > 0
      ? msgArr[0].timestamp instanceof Date
        ? msgArr[0].timestamp.getTime()
        : new Date(msgArr[0].timestamp).getTime()
      : 0;

  useEffect(() => {
    if (!isProcessing || messageCount === 0) return undefined;
    const tick = () => setLiveDurationMs(Date.now() - firstMessageTs);
    tick();
    const id = setInterval(tick, 1000);
    return () => {
      clearInterval(id);
      setLiveDurationMs(null);
    };
  }, [isProcessing, messageCount, firstMessageTs]);

  // ── Auto-expand latest query group ────────────────────────────────

  useEffect(() => {
    if (maxQueryId > 0) {
      state.setExpandedQueries((prev) => {
        if (prev.size === 1 && prev.has(maxQueryId)) return prev;
        const next = new Set<number>();
        next.add(maxQueryId);
        return next;
      });
    }
  }, [maxQueryId, state]);

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
      state.setChatTab('tools');
      state.setActiveToolId(toolId);
      const tool = chatProps.toolExecutions.find((t) => t.id === toolId);
      if (tool) {
        const qid = tool.queryId ?? 0;
        state.setExpandedQueries((prev) => {
          if (prev.has(qid)) return prev;
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
        id: 'subagents',
        label: 'Subagents',
        icon: <Bot size={14} />,
        count: activeSubagentCount > 0 ? `${activeSubagentCount} active` : `${subagentRuns.length} total`,
      },
      {
        id: 'tools',
        label: 'Tool Executions',
        icon: <Wrench size={14} />,
        count: activeToolCount > 0 ? `${activeToolCount} active` : `${toolExecutions.length} total`,
      },
      {
        id: 'changes',
        label: 'Agent Changes',
        icon: <History size={14} />,
      },
      {
        id: 'tasks',
        label: 'Tasks',
        icon: <ListTodo size={14} />,
        count: `${currentTodos.filter((t) => t.status === 'in_progress').length || 0} active`,
      },
      { id: 'sessions', label: 'Sessions', icon: <Clock size={14} />, count: `${sessionManager.sessionsCount}` },
      {
        id: 'status',
        label: 'Status',
        icon: <Activity size={14} />,
        count: `${statusMetrics.totalMsgs} msgs`,
      },
    ],
    [
      activeSubagentCount,
      subagentRuns.length,
      activeToolCount,
      toolExecutions.length,
      currentTodos,
      sessionManager.sessionsCount,
      statusMetrics.totalMsgs,
    ],
  );

  const activeTab = chatPanelTabs.find((t) => t.id === state.chatTab) || chatPanelTabs[0];

  // ── Render tab content ────────────────────────────────────────────

  const renderTabContent = () => {
    switch (state.chatTab) {
      case 'subagents':
        return (
          <SubagentsTab
            subagentRuns={subagentRuns}
            resourceCounts={resourceCounts}
            expandedSubagents={state.expandedSubagents}
            toolRefs={state.toolRefs}
            expandedTools={state.expandedTools}
            expandedQueries={state.expandedQueries}
            setActiveToolId={state.setActiveToolId}
            setChatTab={state.setChatTab}
            setExpandedTools={state.setExpandedTools}
            setExpandedQueries={state.setExpandedQueries}
            toggleSubagentExpansion={state.toggleSubagentExpansion}
          />
        );
      case 'tools':
        return (
          <ToolsTab
            toolExecutions={toolExecutions}
            groupedByQuery={groupedByQuery}
            maxQueryId={maxQueryId}
            expandedQueries={state.expandedQueries}
            expandedTools={state.expandedTools}
            activeToolId={state.activeToolId}
            toolRefs={state.toolRefs}
            toggleQueryGroup={state.toggleQueryGroup}
            toggleToolExpansion={state.toggleToolExpansion}
          />
        );
      case 'changes':
        return <AgentChangesPanel />;
      case 'tasks':
        return (
          <div className="side-panel-tasks">
            <TodoPanel todos={currentTodos || []} isLoading={isProcessing && currentTodos.length === 0} />
          </div>
        );
      case 'sessions':
        return (
          <SessionsTab
            sessions={sessionManager.sessions}
            currentSessionId={sessionManager.currentSessionId}
            isLoadingSessions={sessionManager.isLoadingSessions}
            sessionRestoreError={sessionManager.sessionRestoreError}
            loadSessions={sessionManager.loadSessions}
            handleRestoreSession={sessionManager.handleRestoreSession}
          />
        );
      case 'status':
        return (
          <StatusTab chatProps={chatProps} statusMetrics={statusMetrics} liveDurationMs={liveDurationMs} />
        );
      default:
        return (
          <SubagentsTab
            subagentRuns={subagentRuns}
            resourceCounts={resourceCounts}
            expandedSubagents={state.expandedSubagents}
            toolRefs={state.toolRefs}
            expandedTools={state.expandedTools}
            expandedQueries={state.expandedQueries}
            setActiveToolId={state.setActiveToolId}
            setChatTab={state.setChatTab}
            setExpandedTools={state.setExpandedTools}
            setExpandedQueries={state.setExpandedQueries}
            toggleSubagentExpansion={state.toggleSubagentExpansion}
          />
        );
    }
  };

  // ── Main render ───────────────────────────────────────────────────

  return (
    <>
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
          className={`context-panel ${state.panelCollapsed ? 'collapsed' : ''}${state.isResizing ? ' resizing' : ''}${isMobileLayout ? ' context-panel-mobile' : ''}${isTabletLayout && !state.panelCollapsed ? ' context-panel-tablet-overlay' : ''}`}
          aria-label="Context panel"
          style={
            isMobileLayout || isTabletLayout
              ? undefined
              : { width: `${state.panelCollapsed ? PANEL_COLLAPSED_WIDTH : state.panelWidth}px` }
          }
          ref={state.panelContainerRef}
        >
          <div className="side-panel-rail">
            {chatPanelTabs.map((tab) => (
              <button
                key={tab.id}
                className={`side-rail-btn ${state.chatTab === tab.id ? 'active' : ''}`}
                onClick={() => handleTabClick(tab.id)}
                title={tab.label}
                aria-label={tab.label}
                aria-pressed={state.chatTab === tab.id}
              >
                {tab.icon}
              </button>
            ))}
            <button
              className="side-collapse-btn"
              onClick={() => state.setPanelCollapsed((prev) => !prev)}
              title={state.panelCollapsed ? 'Expand panel' : 'Collapse panel'}
            >
              {state.panelCollapsed ? <PanelRightOpen size={14} /> : <PanelRightClose size={14} />}
            </button>
          </div>

          {/* Content — always rendered; CSS handles fade-out on collapse */}
          <div className="side-panel-content" {...(state.panelCollapsed ? { inert: true, 'aria-hidden': true } : {})}>
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
          </div>
        </aside>
      )}
    </>
  );
});

ContextPanel.displayName = 'ContextPanel';

export default ContextPanel;
export type { ContextPanelHandle, ContextPanelProps, ChatContextPanelProps } from './contextPanel/types';
