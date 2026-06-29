import { CheckCircle2, Loader2, XCircle } from 'lucide-react';
import { useEffect, useMemo, useRef, useState } from 'react';
import { getPersonaColor, type ToolExecution } from '@sprout/ui';

/**
 * SP-053-2a: live tool timeline rendered above the chat input. Mirrors
 * the CLI's per-tool spinner / [OK] timeline (SP-048-1c) so users get
 * immediate feedback as each tool runs instead of waiting for the
 * sidebar `ToolsTab` to populate after completion.
 *
 * Rendering rules:
 *   - in-flight tools (status: started/running) → spinner + elapsed time ticking
 *   - completed tools                            → green check + final duration, fades after FADE_MS
 *   - error tools                                → red X + final duration, sticks until the next tool starts
 *
 * The bar is capped at MAX_VISIBLE concurrent cards so a parallel
 * fan-out (e.g. 8-way `run_parallel_subagents`) collapses to the most
 * recent activity rather than overflowing the layout.
 */

interface ToolTimelineBarProps {
  toolExecutions: ToolExecution[];
  maxVisible?: number;
}

const FADE_MS = 3000;
const DEFAULT_MAX_VISIBLE = 4;
// Time the bar stays mounted after `visible` empties. Without this the
// bar unmounts the instant the last visible tool ages out, then
// remounts on the next tool_start — which the user sees as the bar
// flickering on and off. 4s comfortably bridges the gap between
// consecutive tools while still letting the bar disappear on true idle.
const HIDE_GRACE_MS = 4000;

export function ToolTimelineBar({
  toolExecutions,
  maxVisible = DEFAULT_MAX_VISIBLE,
}: ToolTimelineBarProps): JSX.Element | null {
  // Live tick so in-flight elapsed times update without parent re-renders.
  // 250ms is fine-grained enough to feel live without being wasteful.
  const [, forceTick] = useState(0);
  useEffect(() => {
    const hasRunning = toolExecutions.some((t) => t.status === 'started' || t.status === 'running');
    if (!hasRunning) return;
    const id = window.setInterval(() => forceTick((n) => n + 1), 250);
    return () => window.clearInterval(id);
  }, [toolExecutions]);

  // Track when each completed tool was first seen so we can fade it out
  // FADE_MS later. Populate synchronously during render so the card is
  // visible immediately on the first frame it transitions to completed
  // (useEffect-based population produced a 1-frame gap where the card
  // would briefly disappear). The ref is render-side state but reading
  // it is idempotent across re-renders for the same tool.
  const completedAtRef = useRef<Map<string, number>>(new Map());
  const [now, setNow] = useState(() => Date.now());

  // Synchronously stamp first-seen time for any newly-completed tools,
  // and trim ids no longer in the input. Safe to do during render: each
  // tool id is only stamped once, so repeat renders are no-ops.
  {
    const map = completedAtRef.current;
    const nowSync = Date.now();
    for (const t of toolExecutions) {
      if (t.status === 'completed' && !map.has(t.id)) {
        map.set(t.id, nowSync);
      }
    }
    for (const id of Array.from(map.keys())) {
      if (!toolExecutions.some((t) => t.id === id)) {
        map.delete(id);
      }
    }
  }

  // Re-render every 500ms while any completed tool is within its fade
  // window, so it actually disappears. Stops the timer once nothing is fading.
  useEffect(() => {
    if (completedAtRef.current.size === 0) return;
    const tick = window.setInterval(() => setNow(Date.now()), 500);
    return () => window.clearInterval(tick);
  }, [toolExecutions]);

  const visible = useMemo(() => {
    const map = completedAtRef.current;
    const filtered = toolExecutions.filter((t) => {
      if (t.status === 'error') return true; // errors stick
      if (t.status === 'completed') {
        const seen = map.get(t.id);
        // First render that sees a completed tool: map is already populated
        // synchronously above, so seen is non-null here in steady state.
        if (seen == null) return true;
        return now - seen < FADE_MS;
      }
      return true; // started/running always visible
    });
    // Show the most recent N — newer items go to the right (visual reading order).
    return filtered.slice(-maxVisible);
  }, [toolExecutions, now, maxVisible]);

  // Hide-grace gate. Stays true for HIDE_GRACE_MS after `visible`
  // last had entries, so a brief gap between consecutive tools doesn't
  // unmount the bar (DOM mount/unmount is what the user reads as
  // flicker). On true idle the timer fires and the bar disappears.
  const [shouldRender, setShouldRender] = useState(false);
  useEffect(() => {
    if (visible.length > 0) {
      setShouldRender(true);
      return undefined;
    }
    const id = window.setTimeout(() => setShouldRender(false), HIDE_GRACE_MS);
    return () => window.clearTimeout(id);
  }, [visible.length]);

  if (!shouldRender) return null;

  return (
    <div className="tool-timeline-bar" role="status" aria-label="Active tools" data-testid="chat-tool-timeline">
      {visible.map((tool) => (
        <ToolTimelineCard key={tool.id} tool={tool} now={now} />
      ))}
    </div>
  );
}

interface ToolTimelineCardProps {
  tool: ToolExecution;
  now: number;
}

function ToolTimelineCard({ tool, now }: ToolTimelineCardProps): JSX.Element {
  const isRunning = tool.status === 'started' || tool.status === 'running';
  const isError = tool.status === 'error';

  const start = tool.startTime.getTime();
  const end = tool.endTime ? tool.endTime.getTime() : now;
  const elapsed = Math.max(0, end - start);

  const personaColor = tool.persona ? getPersonaColor(tool.persona) : undefined;

  return (
    <div
      className={`tool-timeline-card tool-timeline-card--${tool.status}`}
      data-tool-name={tool.tool}
      data-persona={tool.persona || ''}
    >
      <span className="tool-timeline-status" aria-hidden="true">
        {isRunning ? (
          <Loader2 size={12} className="tool-timeline-spinner" />
        ) : isError ? (
          <XCircle size={12} className="tool-timeline-icon-error" />
        ) : (
          <CheckCircle2 size={12} className="tool-timeline-icon-ok" />
        )}
      </span>
      {tool.persona ? (
        <span className="tool-timeline-persona" style={{ color: personaColor }} title={`Persona: ${tool.persona}`}>
          {tool.persona}
        </span>
      ) : null}
      <span className="tool-timeline-name">{tool.tool}</span>
      {tool.arguments ? (
        <span className="tool-timeline-args" title={tool.arguments}>
          {formatArgPreview(tool.tool, tool.arguments)}
        </span>
      ) : null}
      <span className="tool-timeline-duration">{formatElapsed(elapsed)}</span>
    </div>
  );
}

/** Compact, single-line preview for a tool's args. Mirrors the CLI's
 * `formatToolArgPreview` in `cmd/agent_modes.go` so both surfaces show
 * the same hint for the same tool call. */
function formatArgPreview(toolName: string, argsJson: string): string {
  let parsed: Record<string, unknown> = {};
  try {
    parsed = JSON.parse(argsJson) as Record<string, unknown>;
  } catch {
    return '';
  }
  const pickStr = (k: string): string | undefined => {
    const v = parsed[k];
    return typeof v === 'string' ? v : undefined;
  };

  let preview: string | undefined;
  switch (toolName) {
    case 'read_file':
    case 'write_file':
    case 'edit_file':
    case 'write_structured_file':
    case 'patch_structured_file':
      preview = pickStr('path') || pickStr('file_path');
      break;
    case 'shell_command':
    case 'exec':
      preview = pickStr('command');
      break;
    case 'search_files':
    case 'grep':
      preview = pickStr('pattern') || pickStr('query');
      break;
    case 'fetch_url':
    case 'browse_url':
      preview = pickStr('url');
      break;
    default:
      // Generic fallback: first short string field
      for (const v of Object.values(parsed)) {
        if (typeof v === 'string' && v.length > 0 && v.length < 120) {
          preview = v;
          break;
        }
      }
  }
  if (!preview) return '';
  const oneLine = preview.replace(/\s+/g, ' ').trim();
  return oneLine.length > 60 ? `(${oneLine.slice(0, 59)}…)` : `(${oneLine})`;
}

function formatElapsed(ms: number): string {
  if (ms < 1000) return `${ms}ms`;
  if (ms < 60_000) return `${(ms / 1000).toFixed(1)}s`;
  return `${(ms / 60_000).toFixed(1)}m`;
}
