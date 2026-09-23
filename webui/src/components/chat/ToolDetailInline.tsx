import { Bot } from 'lucide-react';
import { useEffect, useRef } from 'react';
import { getToolIcon, getStatusIcon, formatDuration, isSubagentTool, getPersonaColor } from '../contextPanel/helpers';
import { ToolDetailBody } from '../contextPanel/ToolCard';
import type { ToolExecution } from '../contextPanel/types';

export interface ToolDetailInlineProps {
  tool: ToolExecution;
  /** Collapse callback (re-press). Also used to return focus on Escape. */
  onToggle: (toolId: string) => void;
  /** Stable region id (the pill's aria-controls points at this). */
  id?: string;
}

/**
 * Inline tool-detail block. Rendered directly below the pill row
 * in a chat message — it replaces the old "pill click -> ContextPanel
 * highlight" deep-link. Shows the tool's args / status / output / duration /
 * subagent prompt, reusing the shared ToolDetailBody so it never diverges from
 * the ContextPanel's ToolCard.
 *
 * Collapse: re-press (via onToggle), blur-outside (focusout), or Escape (which
 * returns focus to the controlling pill). At most one is open at a time — that
 * invariant is held by the owning ChatView's single activeToolDetail state.
 */
export function ToolDetailInline({ tool, onToggle, id = `tool-detail-${tool.id}` }: ToolDetailInlineProps) {
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

  return (
    <div id={id} ref={blockRef} className="tool-detail-inline" role="region" aria-label={`Tool details: ${label}`}>
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
        <span className="tool-name">{label}</span>
        <span className="tool-status">{getStatusIcon(tool.status)}</span>
        <span className="tool-duration">{formatDuration(tool.startTime, tool.endTime)}</span>
      </div>
      <ToolDetailBody tool={tool} />
    </div>
  );
}
