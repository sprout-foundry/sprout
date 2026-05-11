import { useState, useEffect, useCallback, useRef } from 'react';
import type { ChatTabId, ContextPanelProps, ContextPanelBaseProps } from './types';
import { PANEL_COLLAPSED_KEY, PANEL_TAB_KEY, PANEL_MIN, PANEL_MAX, MOBILE_LAYOUT_MAX_WIDTH } from './types';
import type { MouseEvent as ReactMouseEvent } from 'react';

interface UseContextPanelStateReturn {
  panelCollapsed: boolean;
  setPanelCollapsed: (v: boolean | ((prev: boolean) => boolean)) => void;
  panelWidth: number;
  panelContainerRef: React.RefObject<HTMLDivElement>;
  chatTab: ChatTabId;
  setChatTab: (v: ChatTabId) => void;
  expandedTools: Set<string>;
  expandedQueries: Set<number>;
  expandedSubagents: Set<string>;
  activeToolId: string | null;
  setActiveToolId: (v: string | null) => void;
  setExpandedTools: React.Dispatch<React.SetStateAction<Set<string>>>;
  setExpandedQueries: React.Dispatch<React.SetStateAction<Set<number>>>;
  setExpandedSubagents: React.Dispatch<React.SetStateAction<Set<string>>>;
  toolRefs: React.MutableRefObject<Record<string, HTMLDivElement | null>>;
  startResize: (e: ReactMouseEvent<HTMLDivElement>) => void;
  isResizing: boolean;
  toggleToolExpansion: (toolId: string) => void;
  toggleQueryGroup: (queryId: number) => void;
  toggleSubagentExpansion: (toolId: string) => void;
  isChat: boolean;
}

export function useContextPanelState(props: ContextPanelProps): UseContextPanelStateReturn {
  const base = props as ContextPanelBaseProps;
  const { onPanelWidthChange, onMobileOpenChange, onCollapsedChange, panelWidth: requestedPanelWidth } = base;

  const [panelCollapsed, setPanelCollapsed] = useState(() => {
    if (typeof window !== 'undefined' && window.innerWidth <= MOBILE_LAYOUT_MAX_WIDTH) {
      return true;
    }
    return false;
  });
  const panelWidth = typeof requestedPanelWidth === 'number' ? requestedPanelWidth : 360;
  const panelContainerRef = useRef<HTMLDivElement>(null);

  const [chatTab, setChatTab] = useState<ChatTabId>('subagents');
  const [expandedTools, setExpandedTools] = useState<Set<string>>(new Set());
  const [expandedQueries, setExpandedQueries] = useState<Set<number>>(new Set());
  const [expandedSubagents, setExpandedSubagents] = useState<Set<string>>(new Set());
  const [activeToolId, setActiveToolId] = useState<string | null>(null);
  const [isResizing, setIsResizing] = useState(false);
  const toolRefs = useRef<Record<string, HTMLDivElement | null>>({});

  const isChat = props.context === 'chat';

  // Persistence: read from localStorage
  useEffect(() => {
    if (typeof window === 'undefined') return;
    const storedCollapsed = window.localStorage.getItem(PANEL_COLLAPSED_KEY);
    const storedTab = window.localStorage.getItem(`${PANEL_TAB_KEY}.${props.context}`);

    if (storedCollapsed === '1') {
      setPanelCollapsed(true);
    }
    if (storedTab) {
      if (['subagents', 'tools', 'changes', 'tasks', 'status', 'sessions'].includes(storedTab)) {
        setChatTab(storedTab as ChatTabId);
      }
    }
  }, [props.context]);

  useEffect(() => {
    if (typeof window === 'undefined') return;
    window.localStorage.setItem(PANEL_COLLAPSED_KEY, panelCollapsed ? '1' : '0');
  }, [panelCollapsed]);

  useEffect(() => {
    onCollapsedChange?.(panelCollapsed);
  }, [onCollapsedChange, panelCollapsed]);

  useEffect(() => {
    if (!props.isMobileLayout) {
      return;
    }
    onMobileOpenChange?.(!panelCollapsed);
  }, [panelCollapsed, props.isMobileLayout, onMobileOpenChange]);

  // Persist active tab
  useEffect(() => {
    if (typeof window === 'undefined') return;
    window.localStorage.setItem(`${PANEL_TAB_KEY}.${props.context}`, chatTab);
  }, [props.context, chatTab]);

  // Clear highlight after 3 seconds
  useEffect(() => {
    if (activeToolId) {
      const timer = setTimeout(() => setActiveToolId(null), 3000);
      return () => clearTimeout(timer);
    }
  }, [activeToolId]);

  // Resize handler
  const startResize = useCallback(
    (e: ReactMouseEvent<HTMLDivElement>) => {
      e.preventDefault();
      setPanelCollapsed(false);
      setIsResizing(true);
      const startX = e.clientX;
      const startWidth = panelWidth;

      const onMouseMove = (moveEvent: MouseEvent) => {
        const parentEl = panelContainerRef.current?.parentElement;
        const parentWidth = parentEl ? parentEl.getBoundingClientRect().width : window.innerWidth;
        const rawWidth = startWidth + (startX - moveEvent.clientX);
        const maxByLayout = parentWidth - 260;
        const clamped = Math.max(PANEL_MIN, Math.min(Math.min(PANEL_MAX, maxByLayout), rawWidth));
        onPanelWidthChange?.(clamped);
      };

      const onMouseUp = () => {
        setIsResizing(false);
        document.body.style.userSelect = '';
        document.body.style.cursor = '';
        document.removeEventListener('mousemove', onMouseMove);
        document.removeEventListener('mouseup', onMouseUp);
      };

      document.body.style.userSelect = 'none';
      document.body.style.cursor = 'col-resize';
      document.addEventListener('mousemove', onMouseMove);
      document.addEventListener('mouseup', onMouseUp);
    },
    [onPanelWidthChange, panelWidth],
  );

  const toggleToolExpansion = (toolId: string) => {
    setExpandedTools((prev) => {
      const next = new Set(prev);
      if (next.has(toolId)) next.delete(toolId);
      else next.add(toolId);
      return next;
    });
  };

  const toggleQueryGroup = (queryId: number) => {
    setExpandedQueries((prev) => {
      const next = new Set(prev);
      if (next.has(queryId)) next.delete(queryId);
      else next.add(queryId);
      return next;
    });
  };

  const toggleSubagentExpansion = (toolId: string) => {
    setExpandedSubagents((prev) => {
      const next = new Set(prev);
      if (next.has(toolId)) next.delete(toolId);
      else next.add(toolId);
      return next;
    });
  };

  return {
    panelCollapsed,
    setPanelCollapsed,
    panelWidth,
    panelContainerRef,
    chatTab,
    setChatTab,
    expandedTools,
    expandedQueries,
    expandedSubagents,
    activeToolId,
    setActiveToolId,
    setExpandedTools,
    setExpandedQueries,
    setExpandedSubagents,
    toolRefs,
    startResize,
    isResizing,
    toggleToolExpansion,
    toggleQueryGroup,
    toggleSubagentExpansion,
    isChat,
  };
}
