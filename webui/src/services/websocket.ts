import { debugLog } from '../utils/log';
import { appendClientIdToUrl } from './clientSession';
import { notificationBus } from './notificationBus';

/** Shape of a WebSocket event coming from / going to the server. */
export interface WsEvent {
  type: string;
  data?: unknown;
  [key: string]: unknown;
}

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

  private constructor() {}

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

  connect() {
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
      return;
    }

    if (this.reconnectTimeout) {
      clearTimeout(this.reconnectTimeout);
      this.reconnectTimeout = null;
    }

    // Use environment variable if provided, otherwise use relative URL.
    // When running via the SSH proxy the LEDIT_PROXY_BASE global is injected
    // into the page so WebSocket traffic routes through the same origin.
    const wsUrl =
      process.env.REACT_APP_WS_URL ||
      (() => {
        const proxyBase = (window as unknown as Record<string, string>).LEDIT_PROXY_BASE || '';
        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        return `${protocol}//${window.location.host}${proxyBase}/ws`;
      })();

    debugLog('Connecting to WebSocket:', wsUrl);

    this.ws = new WebSocket(appendClientIdToUrl(wsUrl));

    this.ws.onopen = () => {
      const isReconnect = this.wasConnectedBefore;
      this.wasConnectedBefore = true;
      debugLog('WebSocket connected', isReconnect ? '(reconnect)' : '(initial)');
      this.reconnectAttempts = 0;
      this.lastPongTime = Date.now();
      this.startPingInterval();
      this.startPongWatchdog();
      this.notifyCallbacks({ type: 'connection_status', data: { connected: true } });

      // Fire the reconnect callback so the application can sync state
      // (e.g., request fresh stats, check for stuck processing state).
      if (isReconnect && this.onReconnectCallback) {
        this.onReconnectCallback();
      }
    };

    this.ws.onclose = (event) => {
      debugLog('WebSocket disconnected:', event);
      this.stopPingInterval();
      this.stopPongWatchdog();
      this.notifyCallbacks({ type: 'connection_status', data: { connected: false } });

      // Only reconnect if not intentionally closed and not already reconnecting
      if (!this.intentionalClose && this.reconnectAttempts < this.maxReconnectAttempts) {
        this.reconnectAttempts++;
        this.reconnectTimeout = setTimeout(() => {
          debugLog(`Attempting to reconnect (${this.reconnectAttempts}/${this.maxReconnectAttempts})`);
          this.reconnectTimeout = null;
          this.connect();
        }, this.reconnectDelay * this.reconnectAttempts);
      }
    };

    this.ws.onerror = (error) => {
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

        this.notifyCallbacks(data);
      } catch (error) {
        debugLog('[WebSocket] Failed to parse message:', error);
        notificationBus.notify('error', 'WebSocket Error', 'Failed to parse message: ' + String(error));
      }
    };
  }

  disconnect() {
    this.intentionalClose = true;
    this.wasConnectedBefore = false;
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
   *  the onReconnect callback (for state sync, stuck-processing guard, etc.). */
  freeze() {
    this.intentionalClose = true;
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
    this.reconnectAttempts = 0;
    this.intentionalClose = false;
    // Connect immediately
    this.connect();
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

  sendEvent(event: WsEvent) {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify(event));
    } else {
      notificationBus.notify('warning', 'WebSocket Warning', 'Cannot send event: connection not active');
    }
  }

  isConnected(): boolean {
    return this.ws !== null && this.ws.readyState === WebSocket.OPEN;
  }
}

export { WebSocketService };
