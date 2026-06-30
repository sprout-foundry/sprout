/**
 * Lightweight pub-sub for automate WebSocket events.
 *
 * useEventHandler dispatches automate.* events here via emitAutomate().
 * AutomationsPanel and AutomationsSessionDetail subscribe to receive them.
 *
 * This keeps panel-specific knowledge out of the global event handler.
 */

export type AutomateEventType =
  | 'automate.session_started'
  | 'automate.session_ended'
  | 'automate.output_chunk'
  | 'automate.budget_update';

export interface AutomateEventPayload {
  session_id?: string;
  workflow?: string;
  status?: string;
  offset?: number;
  chunk_len?: number;
  spent_usd?: number;
  budget_usd?: number;
  fraction?: number;
  iteration?: number;
  [key: string]: unknown;
}

export type AutomateEventHandler = (eventType: AutomateEventType, payload: AutomateEventPayload) => void;

const handlers = new Set<AutomateEventHandler>();

/** Subscribe to automate events. Returns an unsubscribe function. */
export function subscribeAutomate(handler: AutomateEventHandler): () => void {
  handlers.add(handler);
  return () => {
    handlers.delete(handler);
  };
}

/** Dispatch an automate event to all registered handlers. */
export function emitAutomate(eventType: AutomateEventType, payload: AutomateEventPayload): void {
  for (const handler of handlers) {
    try {
      handler(eventType, payload);
    } catch (err) {
      // Swallow handler errors — one broken subscriber shouldn't block others.
      // Log to console for diagnostic visibility without crashing the panel.
      // eslint-disable-next-line no-console
      console.error('[automateEvents] Handler error (swallowed):', err);
    }
  }
}
