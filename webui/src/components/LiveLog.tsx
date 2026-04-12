import { useRef, useEffect, useCallback } from 'react';
import './LiveLog.css';

// ── Live Log Scroller Component ────────────────────────────────────

export interface LiveLogLine {
  id: string;
  text: string;
  timestamp: Date;
  taskId?: string;
}

export interface LiveLogProps {
  lines: LiveLogLine[];
  maxLines: number;
  className?: string;
}

function LiveLog({ lines, maxLines, className }: LiveLogProps): JSX.Element | null {
  const scrollRef = useRef<HTMLDivElement>(null);
  const userScrolledRef = useRef(false);

  // Combined auto-scroll and user-scroll-reset effect
  useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    const distanceFromBottom = el.scrollHeight - el.scrollTop - el.clientHeight;
    // Reset lock if near bottom, otherwise keep user lock
    if (distanceFromBottom <= 48) {
      userScrolledRef.current = false;
    }
    // Auto-scroll only if not user-locked
    if (!userScrolledRef.current) {
      el.scrollTop = el.scrollHeight;
    }
  }, [lines.length]);

  const handleScroll = useCallback(() => {
    const el = scrollRef.current;
    if (!el) return;
    const distanceFromBottom = el.scrollHeight - el.scrollTop - el.clientHeight;
    userScrolledRef.current = distanceFromBottom > 48;
  }, []);

  const visibleLines = lines.slice(-maxLines);

  if (visibleLines.length === 0) return null;

  return (
    <div className={`subagent-feed-log ${className || ''}`} ref={scrollRef} onScroll={handleScroll}>
      {visibleLines.map((line) => (
        <div key={line.id} className="subagent-feed-log-line">
          {line.taskId && <span className="subagent-feed-log-task">{line.taskId}</span>}
          <span className="subagent-feed-log-text">{line.text}</span>
        </div>
      ))}
    </div>
  );
}

export default LiveLog;
