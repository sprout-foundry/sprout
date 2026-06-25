import type { WsEvent } from '@sprout/events';
import { debugLog } from '../utils/log';
import { getAdapter } from './apiAdapter';
import { appendClientIdToUrl, clientFetch, getProxyBase } from './clientSession';
import { notificationBus } from './notificationBus';

export type { WsEvent };

type EventCallback = (event: WsEvent) => void;

class WebSocketService {
  private static instance: WebSocketService;
  private ws: WebSocket | null = null;
  private callbacks: EventCallback[] = [];
  private reconnectAttempts = 0;
  private maxReconnectAttempts = 30;
  private reconnectDelay = 2000;
  private pingInterval: NodeJS.Timeout | null = null;
  private lastPongTime = Date.now();
  private pongWatchdogInterval: NodeJS.Timeout | null = null;
  private maxPongAge = 60000;
  private intentionalClose = false;
  private reconnectTimeout: NodeJS.Timeout | null = null;
  private wasConnectedBefore = false;
  private onReconnectCallback: (() => void) | null = null;
  private pendingQueue: WsEvent[] = [];
  private maxQueueSize = 100;
  private isReplayingQueue = false;
  private activeChatId: string | null = null;
  private chatSeq = new Map<string, number>();
  private connecting = false;

  private constructor() {
    // A real tab close / navigation fires `pagehide` (a background does not).
    // Tell the server to cancel any in-flight query now rather than letting it
    // run out the heartbeat timeout. Best-effort: the send may not flush during
    // unload, in which case the server's heartbeat still cancels it (the client
    // isn't marked paused on a close).
    if (typeof window !== 'undefined') {
      window.addEventListener('pagehide', () => {
        this.sendControl('session_close');
      });
    }
  }

  /** Send a small control frame if the socket is open. Used for lifecycle
   *  signals (pause / session_close) that the server acts on immediately. */
  private sendControl(type: string): void {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      try {
        this.ws.send(JSON.stringify({ type }));
      } catch {
        // best-effort during teardown
      }
    }
  }

  static getInstance(): WebSocketService {
    if (!WebSocketService.instance) {
      WebSocketService.instance = new WebSocketService();
    }
    return WebSocketService.instance;
  }

  private startPingInterval() {
    this.stopPingInterval();
    // Send ping every 30 seconds to keep connection alive
    this.pingInterval = setInterval(() => {
      if (this.ws && this.ws.readyState === WebSocket.OPEN) {
        this.ws.send(JSON.stringify({ type: 'ping' }));
      }
    }, 30000);
  }

  private stopPingInterval() {
    if (this.pingInterval) {
      clearInterval(this.pingInterval);
      this.pingInterval = null;
    }
  }

  private handlePong() {
    this.lastPongTime = Date.now();
    debugLog('Pong received');
  }

  private startPongWatchdog() {
    this.stopPongWatchdog();
    // Check every 30s whether we've received a pong within maxPongAge.
    // If the server stops responding to pings but the TCP connection stays
    // alive (half-open, common during Chrome tab pause), this detects the
    // dead connection and forces a reconnect.
    this.pongWatchdogInterval = setInterval(() => {
      if (Date.now() - this.lastPongTime > this.maxPongAge) {
        debugLog('Pong watchdog: no pong received in', this.maxPongAge, 'ms — connection appears dead, reconnecting');
        if (!this.intentionalClose) {
          this.resetAndReconnect();
        }
      }
    }, 30000);
  }

  private stopPongWatchdog() {
    if (this.pongWatchdogInterval) {
      clearInterval(this.pongWatchdogInterval);
      this.pongWatchdogInterval = null;
    }
  }

  async connect() {
    // Guard against concurrent connect() calls — if we're already in the
    // middle of connecting (e.g., the clientFetch reattach check is still
    // in-flight), skip to avoid creating a second WebSocket.
    if (this.connecting) {
      debugLog('[WebSocket] connect() already in progress, skipping');
      return;
    }
    this.connecting = true;

    // Reset reconnect attempts to allow fresh reconnection cycle.
    // This ensures that calling connect() after a prior disconnect()
    // (which sets reconnectAttempts to maxReconnectAttempts to prevent
    // auto-reconnect) will allow auto-reconnect to work again.
    this.reconnectAttempts = 0;
    // Explicitly reset intentional close flag when the application
    // requests a new connection, so auto-reconnect works after connect().
    this.intentionalClose = false;
    // Reset pong timestamp so the watchdog doesn't immediately trigger
    // during the brief window between connect() and onopen.
    this.lastPongTime = Date.now();

    if (this.ws && (this.ws.readyState === WebSocket.OPEN || this.ws.readyState === WebSocket.CONNECTING)) {
      this.connecting = false;
      return;
    }

    if (this.reconnectTimeout) {
      clearTimeout(this.reconnectTimeout);
      this.reconnectTimeout = null;
    }

    // ── Reattach check on reconnect ──────────────────────────────────────
    // If we're reconnecting during an active chat, check with the backend
    // whether there are missed events. If so, add reattach params so the
    // server replays events from where we left off.
    let reattachChatId: string | null = null;
    let reattachAfterSeq: number | undefined = undefined;

    if (this.wasConnectedBefore && this.activeChatId) {
      const chatId = this.activeChatId;
      const lastSeq = this.chatSeq.get(chatId);
      if (lastSeq !== undefined) {
        try {
          const resp = await clientFetch(
            `/api/query/status?chat_id=${encodeURIComponent(chatId)}`,
          );
          const body: { active?: boolean; chat_id?: string } = await resp.json();
          if (body.active === true) {
            reattachChatId = body.chat_id ?? chatId;
            reattachAfterSeq = lastSeq;
            debugLog('[WebSocket] Reattach reconnect for chat', reattachChatId, 'after_seq', reattachAfterSeq);
          }
        } catch (err) {
          debugLog('[WebSocket] Reattach status check failed:', err);
        }
      }
    }

    // Use environment variable if provided, otherwise use relative URL.
    // When running via the SSH proxy the SPROUT_PROXY_BASE global is injected
    // into the page so WebSocket traffic routes through the same origin.
    // In cloud mode with an adapter, prefer the adapter's WebSocket URL.
    const adapter = getAdapter();
    const adapterWsUrl = adapter?.getWebSocketURL();
    let wsUrl =
      import.meta.env.VITE_WS_URL ||
      adapterWsUrl ||
      (() => {
        const proxyBase = window.SPROUT_PROXY_BASE || '';
        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        return `${protocol}//${window.location.host}${proxyBase}/ws`;
      })();

    // Append reattach query params if needed
    if (reattachChatId) {
      const url = new URL(wsUrl, window.location.origin);
      url.searchParams.set('reattach', reattachChatId);
      if (reattachAfterSeq !== undefined) {
        url.searchParams.set('after_seq', String(reattachAfterSeq));
      }
      if (url.origin === window.location.origin) {
        wsUrl = `${url.pathname}${url.search}${url.hash}`;
      } else {
        wsUrl = url.toString();
      }
    }

    debugLog('Connecting to WebSocket:', wsUrl);

    try {
      this.ws = new WebSocket(appendClientIdToUrl(wsUrl));
    } catch (err) {
      // new WebSocket() can throw synchronously (malformed URL, CSP
      // violation). Without this guard, `connecting` stays true forever,
      // blocking every subsequent connect() attempt at the top-of-method
      // early return — the client can never recover.
      this.connecting = false;
      debugLog('[WebSocket] Failed to construct WebSocket:', err);
      if (!this.intentionalClose && this.reconnectAttempts < this.maxReconnectAttempts) {
        this.reconnectAttempts++;
        const backoffDelay = Math.min(
          this.reconnectDelay * Math.pow(2, this.reconnectAttempts - 1) + Math.random() * 1000,
          30000,
        );
        this.reconnectTimeout = setTimeout(() => {
          this.reconnectTimeout = null;
          this.connect();
        }, backoffDelay);
      }
      return;
    }

    this.ws.onopen = () => {
      this.connecting = false;
      const isReconnect = this.wasConnectedBefore;
      this.wasConnectedBefore = true;
      debugLog('WebSocket connected', isReconnect ? '(reconnect)' : '(initial)');
      this.reconnectAttempts = 0;
      this.lastPongTime = Date.now();
      this.startPingInterval();
      this.startPongWatchdog();

      // Replay queued messages on reconnect (but not on initial connection)
      if (isReconnect && this.pendingQueue.length > 0) {
        this.isReplayingQueue = true;
        try {
          const queuedCount = this.pendingQueue.length;
          debugLog(`Replaying ${queuedCount} queued message(s) after reconnect`);
          this.pendingQueue.forEach((event) => {
            try {
              this.ws?.send(JSON.stringify(event));
            } catch (err) {
              debugLog('[WebSocket] Failed to replay queued message:', err);
            }
          });
          // Clear the queue after replay attempt (best-effort)
          this.pendingQueue = [];
        } finally {
          this.isReplayingQueue = false;
        }
      }

      // Fire the reconnect callback so the application can sync state
      // (e.g., request fresh stats, check for stuck processing state).
      // This MUST fire before the connection_status notification so that
      // any state restoration (like isProcessing) happens before UI updates.
      if (isReconnect && this.onReconnectCallback) {
        this.onReconnectCallback();
      }

      this.notifyCallbacks({
        type: 'connection_status',
        data: { connected: true, reconnected: isReconnect, queuedMessageCount: this.pendingQueue.length },
      });
    };

    this.ws.onclose = (event) => {
      this.connecting = false;
      debugLog('WebSocket disconnected:', event);
      this.stopPingInterval();
      this.stopPongWatchdog();
      const willReconnect = !this.intentionalClose && this.reconnectAttempts < this.maxReconnectAttempts;
      this.notifyCallbacks({
        type: 'connection_status',
        data: { connected: false, reconnecting: willReconnect, queuedMessageCount: this.pendingQueue.length },
      });

      // Only reconnect if not intentionally closed and not already reconnecting
      if (!this.intentionalClose && this.reconnectAttempts < this.maxReconnectAttempts) {
        this.reconnectAttempts++;
        // Use exponential backoff with jitter: base * (2 ^ (attempt - 1)) + random(0-1000ms), capped at 30s
        const backoffDelay = Math.min(
          this.reconnectDelay * Math.pow(2, this.reconnectAttempts - 1) + Math.random() * 1000,
          30000,
        );
        this.reconnectTimeout = setTimeout(() => {
          debugLog(`Attempting to reconnect (${this.reconnectAttempts}/${this.maxReconnectAttempts})`);
          this.reconnectTimeout = null;
          this.connect();
        }, backoffDelay);
      }
    };

    this.ws.onerror = (error) => {
      this.connecting = false;
      // Only notify on the very first connection error. Don't spam toasts during
      // reconnect cycles — onerror can fire transiently and up to 30 times.
      if (!this.wasConnectedBefore) {
        notificationBus.notify('error', 'Connection Error', 'WebSocket error: ' + String(error));
      }
      // Note: onerror does not necessarily mean the connection is dead.
      // It can fire for transient errors. The onclose handler is the proper
      // place to handle reconnection logic.
    };

    this.ws.onmessage = (event) => {
      try {
        const data = JSON.parse(event.data);

        // Handle session_conflict: the backend detected another active WebSocket
        // for this user/client and is waiting for a session_takeover confirmation.
        // Auto-respond so the connection proceeds without a 60s hang.
        if (data.type === 'session_conflict') {
          debugLog('[WebSocket] Session conflict detected, sending takeover confirmation');
          if (this.ws && this.ws.readyState === WebSocket.OPEN) {
            try {
              this.ws.send(JSON.stringify({ type: 'session_takeover' }));
            } catch (err) {
              debugLog('[WebSocket] Failed to send session_takeover:', err);
            }
          }
          // Do NOT notifyCallbacks — this is a transport-level handshake, not an app event.
          return;
        }

        // Handle session_displaced: another session has taken over this connection.
        // Stop reconnecting — the new connection is authoritative.
        if (data.type === 'session_displaced') {
          debugLog('[WebSocket] Session displaced by another connection:', data.data?.message);
          this.intentionalClose = true;
          this.stopPingInterval();
          this.stopPongWatchdog();
          this.notifyCallbacks({
            type: 'session_displaced',
            data: data.data || {},
          });
          return;
        }

        // Handle pong responses from server
        if (data.type === 'pong') {
          this.handlePong();
          return;
        }

        // Handle server ping requests
        if (data.type === 'ping') {
          // Respond to server ping with pong
          if (this.ws && this.ws.readyState === WebSocket.OPEN) {
            this.ws.send(JSON.stringify({ type: 'pong' }));
          }
          return;
        }

        // Track __seq per chat for reattach support
        this.trackSeq(data);

        this.notifyCallbacks(data);
      } catch (error) {
        debugLog('[WebSocket] Failed to parse message:', error);
        notificationBus.notify('error', 'WebSocket Error', 'Failed to parse message: ' + String(error));
      }
    };
  }

  /** Disconnect the WebSocket and clear the outbound message queue.
   *  Use this when the user explicitly disconnects or the application is
   *  shutting down. Clears the message queue since there's no expectation
   *  of reconnecting. */
  disconnect() {
    this.intentionalClose = true;
    this.connecting = false;
    this.wasConnectedBefore = false;
    this.pendingQueue = []; // Clear queue on explicit disconnect
    if (this.reconnectTimeout) {
      clearTimeout(this.reconnectTimeout);
      this.reconnectTimeout = null;
    }
    this.stopPingInterval();
    this.stopPongWatchdog();
    if (this.ws) {
      this.ws.onclose = null;
      this.ws.onerror = null;
      this.reconnectAttempts = this.maxReconnectAttempts; // Prevent auto-reconnect
      this.ws.close();
      this.ws = null;
    }
  }

  /** Proactively disconnect before tab freeze. Sends a clean close frame so the
   *  server can properly detach from backend sessions. Unlike disconnect(), this
   *  does NOT reset wasConnectedBefore so that resume() → resetAndReconnect()
   *  → connect() will still recognise the next open as a reconnection and fire
   *  the onReconnect callback (for state sync, stuck-processing guard, etc.).
   *  The outbound message queue is intentionally preserved so queued messages
   *  can be replayed after resume(). */
  freeze() {
    // Tell the server we're backgrounding (not closing) so it keeps any
    // in-flight query running and reattaches when we return, rather than
    // cancelling it on heartbeat staleness. Sent before the close below.
    this.sendControl('pause');
    this.intentionalClose = true;
    this.connecting = false;
    // Intentionally do NOT reset wasConnectedBefore — see comment above.
    if (this.reconnectTimeout) {
      clearTimeout(this.reconnectTimeout);
      this.reconnectTimeout = null;
    }
    this.stopPingInterval();
    this.stopPongWatchdog();
    if (this.ws) {
      this.ws.onclose = null;
      this.ws.onerror = null;
      this.reconnectAttempts = this.maxReconnectAttempts; // Prevent auto-reconnect
      this.ws.close();
      this.ws = null;
    }
  }

  /** Resume after tab unfreeze. Triggers immediate reconnection. */
  resume() {
    this.resetAndReconnect();
  }

  /** Reset all reconnection state and trigger an immediate reconnect attempt.
   *  Used by visibility change and page freeze handlers to cleanly reconnect
   *  after the browser has throttled/killed WebSocket connections.
   *  Unlike disconnect() + connect(), this does NOT mark the close as
   *  intentional, so auto-reconnect continues to work if the first attempt fails. */
  resetAndReconnect() {
    if (this.reconnectTimeout) {
      clearTimeout(this.reconnectTimeout);
      this.reconnectTimeout = null;
    }
    this.stopPingInterval();
    this.stopPongWatchdog();
    if (this.ws) {
      this.ws.onclose = null; // Neutralize old handler to prevent double-connect
      this.ws.onerror = null;
      this.ws.close();
      this.ws = null;
    }
    // Reset state for fresh reconnection
    this.connecting = false;
    this.reconnectAttempts = 0;
    this.intentionalClose = false;
    // Connect immediately (fire-and-forget — errors are handled by connect()
    // itself and by the reconnect loop)
    this.connect().catch(() => {});
  }

  onEvent(callback: EventCallback) {
    if (!this.callbacks.includes(callback)) {
      this.callbacks.push(callback);
    }
  }

  removeEvent(callback: EventCallback) {
    this.callbacks = this.callbacks.filter((cb) => cb !== callback);
  }

  /** Register a callback that fires when the connection is successfully
   *  restored after a prior disconnect/reconnect cycle (i.e. not the very
   *  first connection). Pass null to unregister. */
  onReconnect(callback: (() => void) | null) {
    this.onReconnectCallback = callback;
  }

  private notifyCallbacks(event: WsEvent) {
    this.callbacks.forEach((callback) => callback(event));
  }

  /** Add a message to the pending queue, dropping the oldest if at capacity. */
  private enqueueMessage(event: WsEvent) {
    if (this.pendingQueue.length >= this.maxQueueSize) {
      this.pendingQueue.shift();
      debugLog(`[WebSocket] Queue full (${this.maxQueueSize} messages). Dropped oldest message.`);
    }
    this.pendingQueue.push(event);
    debugLog(`[WebSocket] Queued message (type: ${event.type}). Queue size: ${this.pendingQueue.length}`);
  }

  sendEvent(event: WsEvent) {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      try {
        this.ws.send(JSON.stringify(event));
      } catch (err) {
        debugLog('[WebSocket] Send failed, queuing message:', err);
        this.enqueueMessage(event);
      }
    } else {
      this.enqueueMessage(event);
    }
  }

  isConnected(): boolean {
    return this.ws !== null && this.ws.readyState === WebSocket.OPEN;
  }

  /** Returns the current number of messages in the outbound queue. */
  getQueuedMessageCount(): number {
    return this.pendingQueue.length;
  }

  /** Force-sends all queued messages if the WebSocket is connected.
   *  Returns the number of messages that were sent. Returns 0 if not connected. */
  flushQueuedMessages(): number {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
      return 0;
    }
    if (this.isReplayingQueue) {
      return 0;
    }
    const count = this.pendingQueue.length;
    if (count > 0) {
      debugLog(`Flushing ${count} queued message(s)`);
      this.pendingQueue.forEach((event) => {
        try {
          this.ws?.send(JSON.stringify(event));
        } catch (err) {
          debugLog('[WebSocket] Failed to flush queued message:', err);
        }
      });
      this.pendingQueue = [];
    }
    return count;
  }

  /** Set the current active chat ID. Used for reattach tracking on reconnect. */
  setActiveChatId(chatId: string | null): void {
    this.activeChatId = chatId;
  }

  /** Get the last seen __seq for a specific chat. */
  getLastSeq(chatId: string): number | undefined {
    return this.chatSeq.get(chatId);
  }

  /** Get the last seen __seq for the currently active chat (if any). */
  getActiveChatSeq(): number | undefined {
    return this.activeChatId ? this.chatSeq.get(this.activeChatId) : undefined;
  }

  /** Track __seq from incoming events for reattach support. */
  private trackSeq(event: WsEvent): void {
    const seq = (event as any).__seq;
    if (typeof seq !== 'number') return;
    const chatId = (event.data as any)?.chat_id || this.activeChatId;
    if (!chatId) return;
    const current = this.chatSeq.get(chatId);
    if (current === undefined || seq > current) {
      this.chatSeq.set(chatId, seq);
    }
  }
}

export { WebSocketService };
