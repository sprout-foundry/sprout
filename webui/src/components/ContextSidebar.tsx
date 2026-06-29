import type { ToolExecution, LogEntry, SubagentActivity, TodoItem, FileEdit } from '@sprout/ui';
import React, { useEffect } from 'react';
import { ApiService } from '../services/api';
import type { QueryProgress } from '../types/app';
import ContextPanel, { type ContextPanelHandle } from './ContextPanel';
import ErrorBoundary from './ErrorBoundary';

const PLATFORM_VIEWS = new Set(['tasks', 'billing', 'team', 'costs']);
const CONTEXT_PANEL_COLLAPSED_KEY = 'sprout.contextPanel.collapsed';

export interface ContextSidebarProps {
  isMobile: boolean;
  isTablet: boolean;
  showContextSidebar: boolean;
  contextPanelRef: React.RefObject<ContextPanelHandle>;
  currentView: 'chat' | 'editor' | 'git' | 'tasks' | 'billing' | 'team' | 'costs';
  toolExecutions: ToolExecution[];
  fileEdits: FileEdit[];
  logs: LogEntry[];
  subagentActivities: SubagentActivity[];
  currentTodos: TodoItem[];
  messages: Array<{ type: string; timestamp: Date }>;
  isProcessing: boolean;
  lastError: string | null;
  queryProgress: QueryProgress | null;
}

const ContextSidebar: React.FC<ContextSidebarProps> = ({
  isMobile,
  isTablet,
  showContextSidebar,
  contextPanelRef,
  currentView,
  toolExecutions,
  fileEdits,
  logs,
  subagentActivities,
  currentTodos,
  messages,
  isProcessing,
  lastError,
  queryProgress,
}) => {
  const [panelWidth, setPanelWidth] = React.useState(() => {
    if (typeof window === 'undefined') return 360;
    const storedWidth = Number(window.localStorage.getItem('sprout.contextPanel.width'));
    if (Number.isFinite(storedWidth) && storedWidth >= 260 && storedWidth <= 600) {
      return storedWidth;
    }
    return 360;
  });

  const [isContextPanelCollapsed, setIsContextPanelCollapsed] = React.useState(() => {
    if (typeof window === 'undefined') {
      return false;
    }
    return window.localStorage.getItem(CONTEXT_PANEL_COLLAPSED_KEY) === '1';
  });

  const [isContextPanelMobileOpen, setIsContextPanelMobileOpen] = React.useState(false);

  // Persist panel width to localStorage
  React.useEffect(() => {
    if (typeof window === 'undefined') return;
    window.localStorage.setItem('sprout.contextPanel.width', String(Math.round(panelWidth)));
  }, [panelWidth]);

  // Persist collapsed state to localStorage
  React.useEffect(() => {
    if (typeof window === 'undefined') return;
    window.localStorage.setItem(CONTEXT_PANEL_COLLAPSED_KEY, isContextPanelCollapsed ? '1' : '0');
  }, [isContextPanelCollapsed]);

  // Auto-close mobile context panel when not needed
  React.useEffect(() => {
    if (!isMobile || !showContextSidebar) {
      setIsContextPanelMobileOpen(false);
    }
  }, [isMobile, showContextSidebar]);

  const apiService = ApiService.getInstance();

  const handleToggleContextPanel = React.useCallback(() => {
    setIsContextPanelCollapsed((prev) => {
      if (prev) {
        contextPanelRef.current?.openTab('subagents');
      } else {
        contextPanelRef.current?.closePanel();
      }
      return !prev;
    });
  }, [contextPanelRef]);

  // Listen for toggle event from parent/hotkeys
  useEffect(() => {
    const handleToggleEvent = () => handleToggleContextPanel();
    window.addEventListener('toggle-context-panel', handleToggleEvent);
    return () => window.removeEventListener('toggle-context-panel', handleToggleEvent);
  }, [handleToggleContextPanel]);

  if (!showContextSidebar || PLATFORM_VIEWS.has(currentView)) {
    return null;
  }

  return (
    <ErrorBoundary panelName="Context Panel">
      {isTablet && !isContextPanelCollapsed && (
        <div className="context-panel-backdrop" onClick={() => contextPanelRef.current?.closePanel()} />
      )}
      <ContextPanel
        ref={contextPanelRef}
        context="chat"
        toolExecutions={toolExecutions}
        fileEdits={fileEdits}
        logs={logs}
        subagentActivities={subagentActivities}
        currentTodos={currentTodos}
        messages={messages}
        isProcessing={isProcessing}
        lastError={lastError}
        queryProgress={queryProgress}
        isMobileLayout={isMobile}
        isTabletLayout={isTablet}
        onMobileOpenChange={setIsContextPanelMobileOpen}
        onCollapsedChange={setIsContextPanelCollapsed}
        panelWidth={panelWidth}
        onPanelWidthChange={setPanelWidth}
        onLoadSessions={() => apiService.getSessions()}
        onRestoreSession={(sessionId) => apiService.restoreSession(sessionId)}
      />
    </ErrorBoundary>
  );
};

export default ContextSidebar;
