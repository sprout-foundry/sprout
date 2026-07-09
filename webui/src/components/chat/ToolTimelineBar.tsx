import { getPersonaColor, type ToolExecution } from '@sprout/ui';
import { CheckCircle2, Loader2, XCircle } from 'lucide-react';
import { useEffect, useMemo, useRef, useState } from 'react';

/**
 * Live tool timeline rendered above the chat input as a single-row
 * horizontal strip. Shows tools as they run with spinners, then fades
 * them out after completion. Not clickable — this is a status display
 * only. For clickable tool details, see the inline MessageSegments badges.
 */

interface ToolTimelineBarProps {
  toolExecutions: ToolExecution[];
  maxVisible?: number;
}

const FADE_MS = 3000;
const DEFAULT_MAX_VISIBLE = 8;

export function ToolTimelineBar({
  toolExecutions,
  maxVisible = DEFAULT_MAX_VISIBLE,
}: ToolTimelineBarProps): JSX.Element | null {
  const [, forceTick] = useState(0);
  useEffect(() => {
    const hasRunning = toolExecutions.some((t) => t.status === 'started' || t.status === 'running');
    if (!hasRunning) return;
    const id = window.setInterval(() => forceTick((n) => n + 1), 250);
    return () => window.clearInterval(id);
  }, [toolExecutions]);

  const completedAtRef = useRef<Map<string, number>>(new Map());
  const [now, setNow] = useState(() => Date.now());

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

  useEffect(() => {
    if (completedAtRef.current.size === 0) return;
    const tick = window.setInterval(() => setNow(Date.now()), 500);
    return () => window.clearInterval(tick);
  }, [toolExecutions]);

  const visible = useMemo(() => {
    const map = completedAtRef.current;
    const filtered = toolExecutions.filter((t) => {
      if (t.status === 'error') return true;
      if (t.status === 'completed') {
        const seen = map.get(t.id);
        if (seen == null) return true;
        return now - seen < FADE_MS;
      }
      return true;
    });
    return filtered.slice(-maxVisible);
  }, [toolExecutions, now, maxVisible]);

  // Always render the bar — CSS handles empty state via :empty selector
  // with smooth min-height transition so the space collapses gracefully.
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
