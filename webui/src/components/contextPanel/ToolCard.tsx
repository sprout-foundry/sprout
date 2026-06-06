import { stripAnsiCodes } from '../../utils/ansi';
import { formatToolDetail } from '../../utils/resultSummary';
import { subagentDepthLabel } from '../chat/SubagentActivityFeed';
import type { ToolExecution } from './types';

/** Shape of tool execution details when result truncation info is present. */
interface TruncationDetails {
  result_truncated?: boolean;
  result_length?: number;
}

const hasTruncation = (d: unknown): d is TruncationDetails =>
  d != null && typeof d === 'object' && 'result_truncated' in d;

/** Depth badge color: amber for deep nesting, purple for orchestrator. */
const getDepthBadgeColor = (depth: number): string => {
  if (depth >= 2) return '#f59e0b';
  return '#a78bfa';
};
import {
  isSubagentTool,
  getSubagentPrompt,
  getToolIcon,
  getPersonaColor,
  getStatusIcon,
  formatDuration,
} from './helpers';
import { FilePathPre } from './FilePathPre';
import {
  Bot,
  ChevronDown,
  ChevronRight,
  FileEdit,
  ClipboardList,
  BarChart3 as BarChart3Icon,
  FileText,
} from 'lucide-react';

interface ToolCardProps {
  tool: ToolExecution;
  expandedTools: Set<string>;
  activeToolId: string | null;
  toolRef: React.MutableRefObject<Record<string, HTMLDivElement | null>>;
  onToggleExpansion: (toolId: string) => void;
}

export function ToolCard({ tool, expandedTools, activeToolId, toolRef, onToggleExpansion }: ToolCardProps) {
  const isSub = isSubagentTool(tool);
  const subagentPrompt = isSub ? getSubagentPrompt(tool) : undefined;

  return (
    <div
      key={tool.id}
      ref={(el) => {
        toolRef.current[tool.id] = el;
      }}
      className={`tool-execution tool-${tool.status} ${isSub ? 'tool-subagent' : ''} ${activeToolId === tool.id ? 'tool-highlighted' : ''}`}
      onClick={() => onToggleExpansion(tool.id)}
    >
      <>
        <div className="tool-summary" style={{ paddingLeft: tool.depth ? `${(tool.depth - 1) * 16}px` : undefined }}>
          <span className="tool-icon">
            {isSub ? (
              <span className="subagent-icon" style={{ color: getPersonaColor(tool.persona) }}>
                <Bot size={14} />
              </span>
            ) : (
              getToolIcon(tool.tool)
            )}
          </span>
          <span className={`tool-name ${isSub ? 'tool-name-subagent' : ''}`}>
            {isSub
              ? tool.persona
                ? `${tool.persona}`
                : tool.subagentType === 'parallel'
                  ? 'parallel subagents'
                  : 'subagent'
              : tool.tool}
            {isSub && tool.subagentType === 'parallel' && ' (parallel)'}
          </span>
          {tool.depth && tool.depth > 0 && (
            <span
              className="tool-depth-badge"
              style={{ backgroundColor: getDepthBadgeColor(tool.depth) }}
              title={subagentDepthLabel(tool.depth)}
              aria-label={subagentDepthLabel(tool.depth)}
            >
              D{tool.depth}
            </span>
          )}
          <span className="tool-status">{getStatusIcon(tool.status)}</span>
          <span className="tool-duration">{formatDuration(tool.startTime, tool.endTime)}</span>
          <span className="tool-expand">
            {expandedTools.has(tool.id) ? <ChevronDown size={12} /> : <ChevronRight size={12} />}
          </span>
        </div>

        {isSub && subagentPrompt && !expandedTools.has(tool.id) && (
          <div className="tool-message tool-subagent-prompt">{stripAnsiCodes(subagentPrompt)}</div>
        )}

        {tool.message && !(isSub && subagentPrompt) && (
          <div className="tool-message">{stripAnsiCodes(tool.message)}</div>
        )}

        {expandedTools.has(tool.id) && (tool.arguments || tool.result || tool.details) && (
          <div className="tool-details">
            {isSub && subagentPrompt && (
              <div className="tool-detail-section">
                <div className="tool-detail-label">
                  <FileEdit size={12} className="inline-icon" /> Task
                </div>
                <pre className="subagent-prompt-detail">{stripAnsiCodes(subagentPrompt)}</pre>
              </div>
            )}
            {tool.arguments && !isSub && (
              <div className="tool-detail-section">
                <div className="tool-detail-label">
                  <ClipboardList size={12} className="inline-icon" /> Call
                </div>
                <FilePathPre text={formatToolDetail(tool.arguments)} />
              </div>
            )}
            {tool.result && (
              <div className="tool-detail-section">
                <div className="tool-detail-label">
                  {isSub ? (
                    <>
                      <BarChart3Icon size={12} className="inline-icon" /> Summary
                    </>
                  ) : (
                    <>
                      <FileText size={12} className="inline-icon" /> Response
                    </>
                  )}
                </div>
                <FilePathPre text={formatToolDetail(tool.result)} />
                {hasTruncation(tool.details) && tool.details.result_truncated && (
                  <div style={{ color: 'var(--text-muted)', fontSize: '0.75rem', marginTop: '0.25rem' }}>
                    {' '}
                    {'⚠'} Truncated — full result was {Number(tool.details.result_length ?? 0)} characters
                  </div>
                )}
              </div>
            )}
          </div>
        )}
      </>
    </div>
  );
}
