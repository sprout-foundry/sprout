import type { ToolExecution, LogEntry, SubagentActivity } from '@sprout/ui';
import React from 'react';
import { ApiService } from '../services/api';
import type { QueryProgress, ViewType } from '../types/app';
import ContextPanel, { type ContextPanelHandle } from './ContextPanel';
import ErrorBoundary from './ErrorBoundary';

export interface ContextSidebarProps {
  isMobile: boolean;
  isTablet: boolean;
  showContextSidebar: boolean;
  contextPanelRef: React.RefObject<ContextPanelHandle>;
  currentView: ViewType;
  toolExecutions: ToolExecution[];
  logs: LogEntry[];
  subagentActivities: SubagentActivity[];
  messages: Array<{ type: string; timestamp: Date }>;
  isProcessing: boolean;
  lastError: string | null;
  queryProgress: QueryProgress | null;
}

/**
 * Layout wrapper for the right-hand context panel.
 *
 * Desktop: ALWAYS mounted so the main-content column never reflows when
 * the user moves between chat and file buffers. When `showContextSidebar`
 * is false (file buffer focused, costs view) the panel renders in idle
 * mode — rail visible but disabled, empty body. The user can still
 * collapse it to the 52px rail (persisted) whenever they want the space.
 *
 * Mobile/tablet: the panel is an overlay, so the old behavior of
 * unmounting when no chat is focused is preserved.
 */
const ContextSidebar: React.FC<ContextSidebarProps> = ({
  isMobile,
  isTablet,
  showContextSidebar,
  contextPanelRef,
  currentView,
  toolExecutions,
  logs,
  subagentActivities,
  messages,
  isProcessing,
  lastError,
  queryProgress,
}) => {
  const overlayHidden = !showContextSidebar || currentView === 'costs';
  const panelProps = {
    context: 'chat' as const,
    toolExecutions,
    logs,
    subagentActivities,
    messages,
    isProcessing,
    lastError,
    queryProgress,
    onLoadSessions: () => ApiService.getInstance().getSessions(),
    onRestoreSession: (sessionId: string) => ApiService.getInstance().restoreSession(sessionId),
  };

  // Desktop keeps the panel mounted (idle when no chat is focused);
  // overlay layouts unmount when hidden.
  if (!isMobile && !isTablet && overlayHidden) {
    return (
      <ErrorBoundary panelName="Context Panel">
        <ContextPanel ref={contextPanelRef} {...panelProps} isIdle />
      </ErrorBoundary>
    );
  }

  if (overlayHidden) {
    return null;
  }

  return (
    <ErrorBoundary panelName="Context Panel">
      <ContextPanel
        ref={contextPanelRef}
        {...panelProps}
        isMobileLayout={isMobile}
        isTabletLayout={isTablet}
      />
    </ErrorBoundary>
  );
};

export default ContextSidebar;
