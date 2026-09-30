import { Bot, X } from 'lucide-react';
import { useEffect, useRef } from 'react';
import { stripAnsiCodes } from '../../utils/ansi';
import { formatToolDetail } from '../../utils/resultSummary';
import { FilePathPre } from '../contextPanel/FilePathPre';
import {
  getToolIcon,
  getStatusIcon,
  formatDuration,
  isSubagentTool,
  getPersonaColor,
  getSubagentPrompt,
} from '../contextPanel/helpers';
import type { ContextSubagentRun, ToolExecution } from '../contextPanel/types';
import { SubagentRunBody } from './SubagentRunBody';
import { describeToolCall } from './toolCallView';

function truncationLength(details: unknown): number | null {
  if (!details || typeof details !== 'object') return null;
  const d = details as { result_truncated?: unknown; result_length?: unknown };
  return d.result_truncated === true ? Number(d.result_length ?? 0) : null;
}

export interface ToolDetailInlineProps {
  tool: ToolExecution;
  /** Collapse callback (re-press). Also used to return focus on Escape. */
  onToggle: (toolId: string) => void;
  /** Stable region id (the pill's aria-controls points at this). */
  id?: string;
  /** Live state of a subagent call: its updates, tasks and Stop control. */
  subagentRun?: ContextSubagentRun;
}

/**
 * Inline tool-detail block, rendered below the pill row in a chat message:
 * what the call acted on (the command line for shells), its status and
 * duration, its output, and any remaining arguments behind a disclosure.
 * Subagent calls show their task and summary instead.
 *
 * Collapse: re-press (via onToggle), blur-outside (focusout), or Escape (which
 * returns focus to the controlling pill). At most one is open at a time — that
 * invariant is held by the owning ChatView's single activeToolDetail state.
 */
export function ToolDetailInline({
  tool,
  onToggle,
  id = `tool-detail-${tool.id}`,
  subagentRun,
}: ToolDetailInlineProps) {
  const blockRef = useRef<HTMLDivElement>(null);
  const isSub = isSubagentTool(tool);

  const label = isSub ? (tool.persona ? `Subagent: ${tool.persona}` : 'Subagent') : tool.tool || 'Tool';

  useEffect(() => {
    // Capture the node for the life of this effect — the ref may point at a
    // different node (or null) by the time the cleanup runs.
    const block = blockRef.current;
    const closeAndReturnFocus = () => {
      onToggle(tool.id);
      // Return focus to the controlling pill (the element whose aria-controls
      // points at this region).
      const pill = document.querySelector<HTMLElement>(`[aria-controls="${id}"]`);
      pill?.focus();
    };
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') closeAndReturnFocus();
    };
    const onFocusOut = (e: FocusEvent) => {
      const related = e.relatedTarget as HTMLElement | null;
      if (!related || !block) return;
      // Moving focus to the controlling pill is an intentional re-press; let
      // the pill's own toggle handle it (do not double-collapse here).
      if (related.hasAttribute('aria-controls') && related.getAttribute('aria-controls') === id) return;
      if (!block.contains(related)) onToggle(tool.id);
    };
    document.addEventListener('keydown', onKeyDown);
    block?.addEventListener('focusout', onFocusOut);
    return () => {
      document.removeEventListener('keydown', onKeyDown);
      block?.removeEventListener('focusout', onFocusOut);
    };
  }, [tool.id, id, onToggle]);

  const view = isSub ? null : describeToolCall(tool);
  const failed = tool.status === 'error';
  const running = tool.status === 'started' || tool.status === 'running';
  const truncatedLength = truncationLength(tool.details);

  return (
    <div
      id={id}
      ref={blockRef}
      className={`tool-detail-inline${failed ? ' tool-detail-inline--error' : ''}`}
      role="region"
      aria-label={`Tool details: ${label}`}
    >
      <div className="tool-detail-inline-header">
        <span className="tool-icon">
          {isSub ? (
            <span className="subagent-icon" style={{ color: getPersonaColor(tool.persona) }}>
              <Bot size={14} />
            </span>
          ) : (
            getToolIcon(tool.tool)
          )}
        </span>
        <span className="tool-name" title={tool.tool}>
          {view?.label ?? label}
        </span>
        {view?.target && !view.command && (
          <span className="tool-target" title={view.target}>
            {view.target}
          </span>
        )}
        <span className="tool-status">{getStatusIcon(tool.status)}</span>
        <span className="tool-duration">{formatDuration(tool.startTime, tool.endTime)}</span>
        <button
          type="button"
          className="tool-detail-close"
          aria-label="Close tool details"
          onClick={() => onToggle(tool.id)}
        >
          <X size={13} />
        </button>
      </div>
      {isSub ? (
        <SubagentRunBody
          run={subagentRun ?? { tool, prompt: getSubagentPrompt(tool), activities: [], orderedTaskGroups: [] }}
        />
      ) : (
        <>
          {view?.command && <pre className="tool-detail-command">{stripAnsiCodes(view.command)}</pre>}
          {tool.result ? (
            <div className="tool-detail-output" aria-label={failed ? 'Error' : 'Output'}>
              <FilePathPre text={formatToolDetail(tool.result)} />
            </div>
          ) : (
            <div className="tool-detail-empty">{running ? 'Running…' : 'No output.'}</div>
          )}
          {truncatedLength !== null && (
            <div className="tool-detail-note">
              Truncated — the full result was {truncatedLength.toLocaleString()} characters.
            </div>
          )}
          {view?.otherArgs && (
            <details className="tool-detail-args">
              <summary>Arguments</summary>
              <pre>{view.otherArgs}</pre>
            </details>
          )}
        </>
      )}
    </div>
  );
}
