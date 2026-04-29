/**
 * Events transport types for Sprout.
 *
 * Shared between webui and @sprout/ui. Canonical source —
 * do not duplicate; consume via `@sprout/events`.
 */

/**
 * A single event from the transport layer.
 * Compatible with the WsEvent shape used by the webui WebSocketService.
 */
export interface SproutEvent {
  type: string;
  data?: unknown;
  [key: string]: unknown;
}

/** Callback invoked for each incoming event */
export type SproutEventCallback = (event: SproutEvent) => void;

/**
 * EventsProvider — abstraction over the real-time event transport.
 *
 * In local mode this wraps a WebSocket connection to the Go backend.
 * In cloud mode this could wrap Server-Sent Events, a cloud WebSocket,
 * or any other streaming transport.
 *
 * Components consume this via the `useEvents()` hook from EventsContext.
 */
export interface EventsProvider {
  /** Establish the underlying connection. Idempotent if already connected. */
  connect(): void;

  /** Gracefully tear down the connection and clear any outbound queue. */
  disconnect(): void;

  /** Register a callback for incoming events. No-op if already registered. */
  onEvent(callback: SproutEventCallback): void;

  /** Remove a previously registered callback. */
  removeEvent(callback: SproutEventCallback): void;

  /** Send an outbound event to the server. Implementations may queue if disconnected. */
  sendEvent(event: SproutEvent): void;

  /** Whether the underlying transport is currently open. */
  isConnected(): boolean;

  /** Register a one-shot callback that fires on the next successful reconnect (not initial connect). Pass null to unregister. */
  onReconnect(callback: (() => void) | null): void;

  /** Proactively disconnect before tab freeze. Should preserve outbound message queue for replay after resume(). */
  freeze(): void;

  /** Resume after tab freeze/unfreeze. Should trigger immediate reconnection. */
  resume(): void;

  /** Force a clean reconnection, resetting backoff state. */
  resetAndReconnect(): void;

  /** Number of outbound messages currently queued awaiting connection. */
  getQueuedMessageCount(): number;

  /** Manually flush all queued messages if connected. Returns count flushed, or 0 if not connected. */
  flushQueuedMessages(): number;
}
