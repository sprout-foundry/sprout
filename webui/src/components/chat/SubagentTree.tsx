import { useState, useMemo, useCallback } from 'react';
import { ChevronRight, ChevronDown, Loader2, CheckCircle2, XCircle, Bot } from 'lucide-react';
import type { SubagentRun } from './types';
import { getPersonaColor, formatCost, formatTokens } from '@sprout/ui';
import './SubagentTree.css';

// ── Duration Formatting ──────────────────────────────────────────────

const formatDuration = (start: Date, end?: Date): string => {
  const ms = (end || new Date()).getTime() - start.getTime();
  if (ms < 1000) return `${ms}ms`;
  if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`;
  return `${(ms / 60000).toFixed(1)}m`;
};

// ── Depth Colors ─────────────────────────────────────────────────────

const DEPTH_COLORS = [
  'var(--subagent-depth-0, #6366f1)',
  'var(--subagent-depth-1, #8b5cf6)',
  'var(--subagent-depth-2, #a855f7)',
  'var(--subagent-depth-3, #c084fc)',
];

const MAX_COMPLETION_MSG_LENGTH = 80;

function getDepthColor(depth: number): string {
  return DEPTH_COLORS[Math.min(depth, DEPTH_COLORS.length - 1)];
}

// ── Tree Building ────────────────────────────────────────────────────

interface TreeNode {
  run: SubagentRun;
  children: TreeNode[];
}

function getRunTimestamp(run: SubagentRun): Date {
  return run.spawnActivity?.timestamp || run.activities[0]?.timestamp || new Date();
}

function buildTree(runs: SubagentRun[]): TreeNode[] {
  if (runs.length === 0) return [];

  // Sort by spawn timestamp
  const sorted = [...runs].sort((a, b) => getRunTimestamp(a).getTime() - getRunTimestamp(b).getTime());

  const roots: TreeNode[] = [];
  const stack: TreeNode[] = [];

  for (const run of sorted) {
    const node: TreeNode = { run, children: [] };
    const depth = run.depth ?? 0;

    // Pop stack until we find the parent level (depth - 1)
    while (stack.length > 0 && (stack[stack.length - 1].run.depth ?? 0) >= depth) {
      stack.pop();
    }

    if (stack.length === 0) {
      roots.push(node);
    } else {
      stack[stack.length - 1].children.push(node);
    }

    stack.push(node);
  }

  return roots;
}

// ── Tree Node Component ──────────────────────────────────────────────

interface SubagentTreeNodeProps {
  node: TreeNode;
  defaultExpanded?: boolean;
}

function SubagentTreeNode({ node, defaultExpanded = true }: SubagentTreeNodeProps): JSX.Element {
  const [expanded, setExpanded] = useState(defaultExpanded);
  const toggle = useCallback(() => setExpanded((e) => !e), []);
  const { run, children } = node;
  const depth = run.depth ?? 0;
  const color = getDepthColor(depth);
  const personaColor = getPersonaColor(run.persona);
  const hasChildren = children.length > 0;

  const isRunning = !run.isComplete;
  const hasFailures =
    run.completionMessage?.toLowerCase().includes('fail') ||
    run.completionMessage?.toLowerCase().includes('error');
  const startTime = run.spawnActivity?.timestamp || run.activities[0]?.timestamp;

  return (
    <div
      className="subagent-tree-node"
      data-depth={depth}
      data-running={isRunning ? 'true' : 'false'}
      style={{ '--st-color': color, '--st-persona-color': personaColor } as React.CSSProperties}
    >
      <button className="subagent-tree-node-header" onClick={hasChildren ? toggle : undefined} type="button" aria-expanded={hasChildren ? expanded : undefined}>
        <span className="subagent-tree-node-left">
          {hasChildren ? (
            expanded ? <ChevronDown size={13} className="subagent-tree-chevron" /> : <ChevronRight size={13} className="subagent-tree-chevron" />
          ) : (
            <span className="subagent-tree-chevron-placeholder" />
          )}
          {isRunning ? (
            <Loader2 size={13} className="subagent-tree-spinner" />
          ) : hasFailures ? (
            <XCircle size={13} className="subagent-tree-status-error" />
          ) : (
            <CheckCircle2 size={13} className="subagent-tree-status-ok" />
          )}
          <Bot size={13} className="subagent-tree-persona-icon" />
          <span className="subagent-tree-persona">{run.persona}</span>
          {depth > 0 && <span className="subagent-tree-depth-badge">D{depth}</span>}
          {run.isParallel && <span className="subagent-tree-parallel-badge">parallel</span>}
        </span>
        <span className="subagent-tree-node-right">
          {startTime && (
            <span className="subagent-tree-duration">
              {formatDuration(startTime, run.completionTimestamp || undefined)}
            </span>
          )}
          {run.tokensUsed > 0 && (
            <span className="subagent-tree-metric">{formatTokens(run.tokensUsed)} tok</span>
          )}
          {run.cost > 0 && (
            <span className="subagent-tree-metric">{formatCost(run.cost)}</span>
          )}
        </span>
      </button>

      {/* Completion message for completed runs */}
      {!isRunning && run.completionMessage && (
        <div className="subagent-tree-completion-msg" title={run.completionMessage}>
          {run.completionMessage.length > MAX_COMPLETION_MSG_LENGTH
            ? `${run.completionMessage.slice(0, MAX_COMPLETION_MSG_LENGTH)}…`
            : run.completionMessage}
        </div>
      )}

      {/* Children with connector lines */}
      {hasChildren && expanded && (
        <div className="subagent-tree-children">
          <div className="subagent-tree-connector" />
          {children.map((child) => (
            <SubagentTreeNode key={child.run.toolCallId} node={child} defaultExpanded={false} />
          ))}
        </div>
      )}
    </div>
  );
}

// ── SubagentTree Component ───────────────────────────────────────────

interface SubagentTreeProps {
  runs: SubagentRun[];
}

export function SubagentTree({ runs }: SubagentTreeProps): JSX.Element | null {
  const tree = useMemo(() => buildTree(runs), [runs]);

  if (tree.length === 0) return null;

  return (
    <div className="subagent-tree">
      {tree.map((node) => (
        <SubagentTreeNode key={node.run.toolCallId} node={node} defaultExpanded />
      ))}
    </div>
  );
}

// Export buildTree for testing
export { buildTree, type TreeNode };
