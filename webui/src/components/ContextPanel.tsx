import { History, PanelRightOpen, PanelRightClose } from 'lucide-react';
import { useMemo, useImperativeHandle, forwardRef } from 'react';
import './ContextPanel.css';

import AgentChangesPanel from './AgentChangesPanel';
import type {
  ContextPanelProps,
  ContextPanelHandle,
  ChatContextPanelProps,
  ChatTabId,
  PanelTab,
} from './contextPanel/types';
import { PANEL_COLLAPSED_WIDTH } from './contextPanel/types';
import { useContextPanelState } from './contextPanel/useContextPanelState';
import { useHostCapabilities } from '../host';

const TAB_IDS: readonly ChatTabId[] = ['changes'];

const ContextPanel = forwardRef<ContextPanelHandle, ContextPanelProps>((props, ref) => {
  const { agentChanges: supportsAgentChanges } = useHostCapabilities();
  const isChat = props.context === 'chat';
  const chatProps = isChat ? (props as ChatContextPanelProps) : null;
  const isMobileLayout = props.isMobileLayout ?? false;
  const isTabletLayout = props.isTabletLayout ?? false;
  // Idle mode: the panel stays mounted on desktop even when no chat buffer
  // is focused (e.g. the user is reading a file). The rail renders disabled
  // and the body shows an idle note — the layout column never appears or
  // disappears as the user moves between chat and files.
  const isIdle = props.isIdle ?? false;

  // Panel state (no external deps — avoids circular hook ordering)
  const state = useContextPanelState(props);

  // ── Imperative handle ─────────────────────────────────────────────

  const handleTabClick = (tabId: string) => {
    state.setPanelCollapsed(false);
    state.setChatTab(tabId as ChatTabId);
  };

  const imperativeHandle = {
    openTab: (tab: string) => {
      if (TAB_IDS.includes(tab as ChatTabId)) {
        handleTabClick(tab);
      }
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
    [isChat, chatProps],
  );

  // ── Tab definitions ───────────────────────────────────────────────

  const chatPanelTabs: PanelTab[] = useMemo(
    () => [
      ...(supportsAgentChanges
        ? [
            {
              id: 'changes' as const,
              label: 'Agent Changes',
              icon: <History size={14} />,
            },
          ]
        : []),
    ],
    [],
  );

  const activeTab = chatPanelTabs.find((t) => t.id === state.chatTab) || chatPanelTabs[0];
  // Nothing to show (a hosted chat in the main view): no column at all.
  if (!activeTab) return null;

  // ── Render tab content ────────────────────────────────────────────

  const renderTabContent = () => {
    switch (activeTab.id) {
      case 'changes':
        return <AgentChangesPanel />;
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
        {activeTab.count && (
          <div className="side-panel-header-actions">
            <span className="tool-count">{activeTab.count}</span>
          </div>
        )}
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
          onKeyDown={(e) => {
            // The panel is docked right: ArrowLeft widens it, ArrowRight narrows it.
            if (e.key !== 'ArrowLeft' && e.key !== 'ArrowRight') return;
            e.preventDefault();
            const step = e.shiftKey ? 50 : 10;
            state.setPanelWidth(state.panelWidth + (e.key === 'ArrowLeft' ? step : -step));
          }}
          tabIndex={0}
          role="separator"
          aria-orientation="vertical"
          aria-label="Resize context panel"
          aria-valuenow={Math.round(state.panelWidth)}
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
