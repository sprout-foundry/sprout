import { useState, useCallback } from 'react';
import { ChevronRight, ChevronDown, Loader2, CheckCircle2, XCircle, Wrench } from 'lucide-react';
import type { DelegateActivity, DelegateToolCallInfo } from '@sprout/ui';
import './DelegateActivityTree.css';

interface DelegateActivityTreeProps {
  activity: DelegateActivity;
}

const DEPTH_COLORS = [
  'var(--delegate-depth-0, #6366f1)',
  'var(--delegate-depth-1, #8b5cf6)',
  'var(--delegate-depth-2, #a855f7)',
  'var(--delegate-depth-3, #c084fc)',
];

function formatCost(cost: number): string {
  return `$${cost.toFixed(4)}`;
}

function formatTokens(tokens: number): string {
  if (tokens >= 1000) {
    return `${(tokens / 1000).toFixed(1)}k`;
  }
  return String(tokens);
}

function ToolCallItem({ toolCall }: { toolCall: DelegateToolCallInfo }) {
  const [expanded, setExpanded] = useState(false);
  const toggle = useCallback(() => setExpanded((e) => !e), []);

  return (
    <div className="delegate-tool-call">
      <button className="delegate-tool-call-header" onClick={toggle} aria-expanded={expanded}>
        {expanded ? <ChevronDown size={12} /> : <ChevronRight size={12} />}
        <Wrench size={12} />
        <span className="delegate-tool-name">{toolCall.tool_name}</span>
        {toolCall.duration_ms > 0 && (
          <span className="delegate-tool-duration">{toolCall.duration_ms}ms</span>
        )}
        <span className={`delegate-tool-status ${toolCall.success ? 'success' : 'error'}`}>
          {toolCall.success ? '✓' : '✗'}
        </span>
      </button>
      {expanded && (
        <div className="delegate-tool-call-body">
          {toolCall.input && (
            <div className="delegate-tool-section">
              <span className="delegate-tool-label">Input:</span>
              <pre className="delegate-tool-pre">{toolCall.input.slice(0, 500)}</pre>
            </div>
          )}
          {toolCall.output && (
            <div className="delegate-tool-section">
              <span className="delegate-tool-label">Output:</span>
              <pre className="delegate-tool-pre">{toolCall.output.slice(0, 500)}</pre>
            </div>
          )}
        </div>
      )}
    </div>
  );
}

export function DelegateActivityTree({ activity }: DelegateActivityTreeProps) {
  const [expanded, setExpanded] = useState(false);
  const toggle = useCallback(() => setExpanded((e) => !e), []);

  const depthColor = DEPTH_COLORS[Math.min(activity.depth, DEPTH_COLORS.length - 1)];
  const isRunning = activity.status === 'running';
  const isCompleted = activity.status === 'completed';
  const isError = activity.status === 'error';

  return (
    <div
      className={`delegate-activity-tree depth-${Math.min(activity.depth, 3)}`}
      style={{ '--delegate-color': depthColor } as React.CSSProperties}
    >
      <button className="delegate-activity-header" onClick={toggle} aria-expanded={expanded}>
        {expanded ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
        {isRunning && <Loader2 size={14} className="delegate-spinner" />}
        {isCompleted && <CheckCircle2 size={14} className="delegate-status-completed" />}
        {isError && <XCircle size={14} className="delegate-status-error" />}
        <span className="delegate-id">
          {activity.depth > 0 ? `Delegate (depth ${activity.depth})` : 'Delegate'}
        </span>
        <span className="delegate-summary-text">{activity.summary || activity.action}</span>
        <span className="delegate-metrics">
          {activity.tokensUsed > 0 && (
            <span className="delegate-metric">{formatTokens(activity.tokensUsed)} tok</span>
          )}
          {activity.cost > 0 && (
            <span className="delegate-metric">{formatCost(activity.cost)}</span>
          )}
        </span>
      </button>
      {expanded && (
        <div className="delegate-activity-body">
          {activity.toolsCalled.length > 0 && (
            <div className="delegate-tools-list">
              {activity.toolsCalled.map((tc, i) => (
                <ToolCallItem key={`${tc.tool_name}-${i}`} toolCall={tc} />
              ))}
            </div>
          )}
          {activity.toolsCalled.length === 0 && isRunning && (
            <div className="delegate-empty">Waiting for tool calls...</div>
          )}
        </div>
      )}
    </div>
  );
}
