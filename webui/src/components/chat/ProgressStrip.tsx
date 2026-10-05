import { memo, useEffect, useState } from 'react';
import { Info } from 'lucide-react';
import type { SproutEvent } from '@sprout/events';
import { useEvents } from '../../contexts/EventsContext';
import { progressEventSummary } from '../../utils/progressSummary';
import './ProgressStrip.css';

/**
 * Compact SP-151 progress strip (item 151.7, SP-151 §151c): a memo'd
 * one-liner in the chat rendering the latest deterministic template
 * summary of the active run's progress events — the same text the CLI
 * renders (item 151.6). No model call, no invented detail.
 *
 * Subscribes to the events transport the way useGitWorkspace does: a
 * stable callback registered via onEvent/removeEvent in a single effect.
 * The latest non-empty summary wins; a `query_started` event (a new
 * run) clears the stored summary so the previous run's state is not
 * shown.
 *
 * Self-hides (renders null) when there is no stored summary — nothing
 * to show yet, or a fresh run.
 */
function ProgressStripInner(): JSX.Element | null {
  const events = useEvents();
  const [summary, setSummary] = useState('');

  useEffect(() => {
    const handleEvent = (e: SproutEvent): void => {
      if (e.type === 'query_started') {
        setSummary('');
        return;
      }
      const next = progressEventSummary(e.type, e.data);
      if (next !== '') setSummary(next);
    };
    events.onEvent(handleEvent);
    return () => {
      events.removeEvent(handleEvent);
    };
  }, [events]);

  if (summary === '') return null;

  return (
    <div className="progress-strip" data-testid="progress-strip" role="status" aria-live="polite">
      <Info size={12} aria-hidden="true" />
      <span className="progress-strip-text">{summary}</span>
    </div>
  );
}

export const ProgressStrip = memo(ProgressStripInner);

export default ProgressStrip;
