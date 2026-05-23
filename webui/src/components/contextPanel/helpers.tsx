import type { ToolExecution } from '@sprout/ui';
import {
  Wrench,
  Terminal,
  BookOpen,
  Pencil,
  Search,
  Eye,
  FlaskConical,
  Globe,
  ArrowDown,
  ClipboardList,
  ScrollText,
  RotateCcw,
  Bot,
  Rocket,
  Zap,
  CheckCircle2,
  XCircle,
  Hourglass,
} from 'lucide-react';
import type { ReactNode } from 'react';
import { debugLog } from '../../utils/log';

// ── Tool helpers ────────────────────────────────────────────────────

export const isSubagentTool = (tool: ToolExecution) =>
  tool.tool === 'run_subagent' || tool.tool === 'run_parallel_subagents';

export const getSubagentPrompt = (tool: ToolExecution): string | undefined => {
  if (!tool.arguments) return undefined;
  try {
    const args = JSON.parse(tool.arguments);
    return typeof args.prompt === 'string' ? args.prompt : undefined;
  } catch (err) {
    debugLog('Failed to parse subagent prompt arguments:', err);
    return undefined;
  }
};

// ── Icon / status getters ──────────────────────────────────────────

export const getToolIcon = (toolName: string): ReactNode => {
  const iconMap: { [key: string]: ReactNode } = {
    shell_command: <Terminal size={14} />,
    read_file: <BookOpen size={14} />,
    write_file: <Pencil size={14} />,
    edit_file: <Pencil size={14} />,
    search_files: <Search size={14} />,
    analyze_ui_screenshot: <Eye size={14} />,
    analyze_image_content: <FlaskConical size={14} />,
    web_search: <Globe size={14} />,
    fetch_url: <ArrowDown size={14} />,
    TodoWrite: <ClipboardList size={14} />,
    TodoRead: <ClipboardList size={14} />,
    view_history: <ScrollText size={14} />,
    rollback_changes: <RotateCcw size={14} />,
    mcp_tools: <Wrench size={14} />,
    run_subagent: <Bot size={14} />,
    run_parallel_subagents: <Bot size={14} />,
  };
  return iconMap[toolName] || <Wrench size={14} />;
};

// SP-053-1a: re-export from @sprout/ui to keep the single source of truth
// in `packages/ui/src/utils/personaColors.ts`. Existing importers of this
// module keep working without code changes.
export { getPersonaColor } from '@sprout/ui';

export const getStatusIcon = (status: string): ReactNode => {
  switch (status) {
    case 'started':
      return <Rocket size={14} />;
    case 'running':
      return <Zap size={14} />;
    case 'completed':
      return <CheckCircle2 size={14} />;
    case 'error':
      return <XCircle size={14} />;
    default:
      return <Hourglass size={14} />;
  }
};

// ── Formatting utilities ───────────────────────────────────────────

export const formatDuration = (startTime: Date, endTime?: Date) => {
  const end = endTime || new Date();
  const duration = end.getTime() - startTime.getTime();
  if (duration < 1000) return `${duration}ms`;
  if (duration < 60000) return `${(duration / 1000).toFixed(1)}s`;
  return `${(duration / 60000).toFixed(1)}m`;
};

export const formatRelativeTime = (value: string) => {
  const date = new Date(value);
  const diffMs = Date.now() - date.getTime();
  const diffSecs = Math.max(0, Math.floor(diffMs / 1000));
  const diffMins = Math.floor(diffSecs / 60);
  const diffHours = Math.floor(diffMins / 60);
  if (diffSecs < 60) return `${diffSecs}s ago`;
  if (diffMins < 60) return `${diffMins}m ago`;
  if (diffHours < 24) return `${diffHours}h ago`;
  return date.toLocaleString([], {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  });
};

export const formatTime = (value: Date) => {
  return new Date(value).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
};

export const formatDurationMs = (ms: number): string => {
  if (ms < 1000) return `${ms}ms`;
  if (ms < 60000) return `${(ms / 1000).toFixed(0)}s`;
  const mins = Math.floor(ms / 60000);
  const secs = Math.floor((ms % 60000) / 1000);
  return `${mins}m ${secs}s`;
};

export const formatTokens = (tokens: number): string => {
  if (!Number.isFinite(tokens) || tokens < 0) return '—';
  if (tokens >= 1000000) return `${(tokens / 1000000).toFixed(1)}M`;
  if (tokens >= 1000) return `${(tokens / 1000).toFixed(1)}K`;
  return tokens.toString();
};

export const formatCost = (cost: number): string => {
  if (!Number.isFinite(cost)) return '—';
  return `$${cost.toFixed(4)}`;
};
