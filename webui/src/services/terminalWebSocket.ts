import { debugLog, warn } from '../utils/log';
import { appendClientIdToUrl, getWebUIClientId } from './clientSession';
import type { WsEvent } from './websocket';
import { notificationBus } from './notificationBus';

type TerminalEventCallback = (event: WsEvent) => void;

class TerminalWebSocketService {
  private static instance: TerminalWebSocketService;
  /** Registry of all live instances (including the singleton). Used by the
   *  visibility change handler to freeze/resume all terminal connections. */
  private static readonly instances = new Set<TerminalWebSocketService>();
  private ws: WebSocket | null = null;
  private callbacks: TerminalEventCallback[] = [];
  private reconnectAttempts = 0;
  private maxReconnectAttempts = 30;
  private reconnectDelay = 1000;
  private sessionId: string | null = null;
  private isConnected = false;
  private eventHandler: TerminalEventCallback | null = null;
  private pingInterval: NodeJS.Timeout | null = null;
  private lastPongTime = Date.now();
  private pongWatchdogInterval: NodeJS.Timeout | null = null;
  private maxPongAge = 60000;
  private intentionalClose = false;
  private reconnectTimeout: NodeJS.Timeout | null = null;
  private preferredShell: string | null = null;

  private constructor() {}

  // ── Static instance registry ──────────────────────────────────────────

  /** Register an instance so it is included in freezeAll / resumeAll calls. */
  static registerInstance(inst: TerminalWebSocketService): void {
    TerminalWebSocketService.instances.add(inst);
  }

  /** Remove an instance from the registry. Called on permanent teardown (disconnect). */
  static unregisterInstance(inst: TerminalWebSocketService): void {
    TerminalWebSocketService.instances.delete(inst);
  }

  /** Call freeze() on every registered instance. Used by the visibility handler. */
  static freezeAll(): void {
    TerminalWebSocketService.instances.forEach((inst) => inst.freeze());
  }

  /** Call resume() on every registered instance. Used by the visibility handler. */
  static resumeAll(): void {
    TerminalWebSocketService.instances.forEach((inst) => inst.resume());
  }

  /** Creates a fresh independent instance (not the singleton). Use for split panes. */
  static createInstance(): TerminalWebSocketService {
    const inst = new TerminalWebSocketService();
    TerminalWebSocketService.registerInstance(inst);
    return inst;
  }

  private startPingInterval() {
    this.stopPingInterval();
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
    debugLog('Terminal pong received');
  }

  private startPongWatchdog() {
    this.stopPongWatchdog();
    // Check every 30s whether we've received a pong within maxPongAge.
    // If the server stops responding to pings but the TCP connection stays
    // alive (half-open, common during Chrome tab pause), this detects the
    // dead connection and forces a reconnect.
    this.pongWatchdogInterval = setInterval(() => {
      if (Date.now() - this.lastPongTime > this.maxPongAge) {
        debugLog(
          '[terminal] Pong watchdog: no pong received in',
          this.maxPongAge,
          'ms — connection appears dead, reconnecting',
        );
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

  static getInstance(): TerminalWebSocketService {
    if (!TerminalWebSocketService.instance) {
      TerminalWebSocketService.instance = new TerminalWebSocketService();
      TerminalWebSocketService.registerInstance(TerminalWebSocketService.instance);
    }
    return TerminalWebSocketService.instance;
  }

  connect() {
    // Don't connect if already connected
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      debugLog('Terminal WebSocket already connected');
      return;
    }

    // Don't connect if connecting
    if (this.ws && this.ws.readyState === WebSocket.CONNECTING) {
      debugLog('Terminal WebSocket already connecting');
      return;
    }

    // Reset intentionalClose flag (e.g. if disconnect() was called but we manually reconnect)
    this.intentionalClose = false;
    // Reset pong timestamp so the watchdog doesn't immediately trigger
    // during the brief window between connect() and onopen.
    this.lastPongTime = Date.now();

    if (this.reconnectTimeout) {
      clearTimeout(this.reconnectTimeout);
      this.reconnectTimeout = null;
    }

    // Use environment variable if provided, otherwise use relative URL.
    // When running via the SSH proxy the LEDIT_PROXY_BASE global is injected
    // into the page so WebSocket traffic routes through the same origin.
    let wsUrl =
      process.env.REACT_APP_TERMINAL_WS_URL ||
      (() => {
        const proxyBase = (window as unknown as Record<string, string>).LEDIT_PROXY_BASE || '';
        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        return `${protocol}//${window.location.host}${proxyBase}/terminal`;
      })();

    // Build query parameters for the WebSocket URL.
    // On reconnect with a sessionId, pass it so server can reattach to the
    // existing tmux session (preserving history). This also covers the case
    // where the sessionId was restored from localStorage after a tab discard.
    // A preferredShell can be set before the first connect to select a specific shell.
    const params = new URLSearchParams();
    if (this.sessionId) {
      params.set('reattach', this.sessionId);
    }
    if (this.preferredShell && !this.sessionId) {
      params.set('shell', this.preferredShell);
    }
    const paramStr = params.toString();
    if (paramStr) {
      const separator = wsUrl.includes('?') ? '&' : '?';
      wsUrl = `${wsUrl}${separator}${paramStr}`;
    }

    debugLog('Connecting to Terminal WebSocket:', wsUrl);

    this.ws = new WebSocket(appendClientIdToUrl(wsUrl));

    this.ws.onopen = () => {
      debugLog('Terminal WebSocket connected');
      this._isReconnecting = false;
      this.reconnectAttempts = 0;
      this.isConnected = true;
      this.lastPongTime = Date.now();
      this.startPingInterval();
      this.startPongWatchdog();
      this.notifyCallbacks({ type: 'connection_status', data: { connected: true } });
    };

    this.ws.onclose = (event) => {
      debugLog('Terminal WebSocket disconnected:', event);
      this.stopPingInterval();
      this.stopPongWatchdog();
      this.isConnected = false;
      // Only clear sessionId on intentional close.
      // On unexpected disconnect, keep it so we can reattach.
      if (!this.intentionalClose) {
        // Keep sessionId for reattach - don't null it
        this.notifyCallbacks({
          type: 'connection_status',
          data: { connected: false, reattach: this.sessionId },
        });
      } else {
        this.sessionId = null;
        this.notifyCallbacks({ type: 'connection_status', data: { connected: false } });
      }

      if (!this.intentionalClose && this.reconnectAttempts < this.maxReconnectAttempts) {
        this.reconnectAttempts++;
        this.reconnectTimeout = setTimeout(() => {
          this.reconnectTimeout = null;
          this.connect();
        }, this.reconnectDelay * this.reconnectAttempts);
      } else {
        this._isReconnecting = false;
      }
    };

    this.ws.onerror = (error) => {
      // Only notify on first connection error to avoid spam during reconnect cycles
      if (this.reconnectAttempts === 0) {
        notificationBus.notify('error', 'Terminal Connection Error', 'WebSocket error: ' + String(error));
      }
      this.notifyCallbacks({ type: 'error', data: { message: 'WebSocket connection error' } });
    };

    this.ws.onmessage = (event) => {
      try {
        const data = JSON.parse(event.data);
        debugLog('Terminal WebSocket message:', data);

        // Handle pong response
        if (data.type === 'pong') {
          this.handlePong();
          return;
        }

        // Handle server ping
        if (data.type === 'ping') {
          if (this.ws && this.ws.readyState === WebSocket.OPEN) {
            this.ws.send(JSON.stringify({ type: 'pong' }));
          }
          return;
        }

        // Handle session creation
        if (data.type === 'session_created') {
          this.sessionId = data.data.session_id;
          debugLog('Terminal session created:', this.sessionId);
          this.persistSessionId();
          // Notify that we're now ready to send commands
          this.notifyCallbacks({ type: 'session_ready', data: { session_id: this.sessionId } });
        }

        this.notifyCallbacks(data);
      } catch (error) {
        debugLog('[TerminalWebSocket] Failed to parse message:', error);
        notificationBus.notify('error', 'Terminal WebSocket Error', 'Failed to parse message: ' + String(error));
      }
    };
  }

  disconnect() {
    this.intentionalClose = true;
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
    this.isConnected = false;
    this._isReconnecting = false;
    this.clearPersistedSessionId();
    this.sessionId = null;
    // Permanent teardown — remove from the freeze/resume registry.
    TerminalWebSocketService.unregisterInstance(this);
  }

  onEvent(callback: TerminalEventCallback) {
    if (!this.callbacks.includes(callback)) {
      this.callbacks.push(callback);
    }
  }

  removeEvent(callback: TerminalEventCallback) {
    this.callbacks = this.callbacks.filter((cb) => cb !== callback);
  }

  private notifyCallbacks(event: WsEvent) {
    this.callbacks.forEach((callback) => callback(event));
    if (this.eventHandler) {
      this.eventHandler(event);
    }
  }

  sendCommand(command: string) {
    if (!this.isConnected || !this.sessionId) {
      notificationBus.notify('warning', 'Terminal Warning', 'Cannot send command: no session');
      return false;
    }

    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      const message = {
        type: 'input',
        data: {
          session_id: this.sessionId,
          input: command,
        },
      };
      this.ws.send(JSON.stringify(message));
      debugLog('Sent terminal command:', command);
      return true;
    } else {
      notificationBus.notify('warning', 'Terminal Warning', 'Cannot send command: connection not ready');
      return false;
    }
  }

  sendRawInput(input: string) {
    if (!this.isConnected || !this.sessionId) {
      return false;
    }

    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      const message = {
        type: 'input_raw',
        data: {
          session_id: this.sessionId,
          input,
        },
      };
      this.ws.send(JSON.stringify(message));
      return true;
    }
    return false;
  }

  sendResize(cols: number, rows: number) {
    if (!this.isConnected || !this.sessionId) {
      return false;
    }

    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      const message = {
        type: 'resize',
        data: {
          session_id: this.sessionId,
          cols,
          rows,
        },
      };
      this.ws.send(JSON.stringify(message));
      return true;
    }
    return false;
  }

  closeSession() {
    if (!this.isConnected || !this.sessionId) {
      return false;
    }

    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      const message = {
        type: 'close',
        data: {
          session_id: this.sessionId,
        },
      };
      this.ws.send(JSON.stringify(message));
      debugLog('Sent terminal close session');
      return true;
    }
    return false;
  }

  isReady(): boolean {
    return this.isConnected && this.sessionId !== null;
  }

  /** Set the preferred shell name (e.g. "bash", "zsh", "fish") to use for the
   *  next connect() call. Must be called before connect(). On reconnect (reattach)
   *  the shell parameter is ignored by the server since the PTY already exists. */
  setPreferredShell(shell: string | null): void {
    this.preferredShell = shell;
  }

  getSessionId(): string | null {
    return this.sessionId;
  }

  /** Returns the preserved sessionId for reattach, or null. */
  getSessionIdForReattach(): string | null {
    return this.sessionId;
  }

  /** Clear persisted session after a successful reattach. */
  clearPersistedSession() {
    this.sessionId = null;
  }

  private getPersistedSessionKey(): string {
    return `ledit.webui.terminalSession.${getWebUIClientId()}`;
  }

  /** Persist the current sessionId to localStorage so it survives tab discard. */
  persistSessionId() {
    if (this.sessionId) {
      try {
        window.localStorage.setItem(this.getPersistedSessionKey(), this.sessionId);
        debugLog('Terminal session ID persisted:', this.sessionId);
      } catch (err) {
        warn(`[persistSessionId] failed to persist session ID: ${err instanceof Error ? err.message : String(err)}`);
        // localStorage may be unavailable
      }
    }
  }

  /** Restore a Previously persisted sessionId from localStorage (for reattach after tab discard). */
  restorePersistedSessionId(): string | null {
    try {
      const saved = window.localStorage.getItem(this.getPersistedSessionKey());
      if (saved) {
        this.sessionId = saved;
        debugLog('Terminal session ID restored from persistence:', saved);
        return saved;
      }
    } catch (err) {
      warn(
        `[restorePersistedSessionId] failed to restore session ID: ${err instanceof Error ? err.message : String(err)}`,
      );
      // localStorage may be unavailable
    }
    return null;
  }

  /** Remove the persisted sessionId from localStorage. */
  clearPersistedSessionId() {
    try {
      window.localStorage.removeItem(this.getPersistedSessionKey());
    } catch (err) {
      warn(`[clearPersistedSessionId] failed to clear session ID: ${err instanceof Error ? err.message : String(err)}`);
      // localStorage may be unavailable
    }
  }

  /** Set sessionId externally before connecting (for restore from localStorage persistence). */
  restoreSessionId(id: string) {
    this.sessionId = id;
    this.persistSessionId();
  }

  /** Proactively disconnect before tab freeze. Sends a clean close frame to
   *  the server so it can properly detach from the backend session (tmux).
   *  Unlike disconnect(), this does NOT clear the persisted sessionId --
   *  resume() will restore it for reattachment.
   *  
   *  IMPORTANT: This method sets ws.onclose = null to prevent the async
   *  close event from firing and triggering unwanted side effects. The
   *  TerminalPane component's useEffect watches isConnected, so we must
   *  ensure that when resume() is called, the pane can reconnect without
   *  tearing down and recreating its xterm instance.
   *  
   *  We use a separate flag to track freeze state so the TerminalPane can
   *  distinguish between a freeze (temporary) and a disconnect (permanent).
   */
  private isFrozen = false;
  /** True while a reconnect is in progress (resetAndReconnect has been called
   *  but the WebSocket has not yet opened or permanently failed). This lets
   *  React components avoid tearing down state during the async gap. */
  private _isReconnecting = false;

  freeze() {
    this.intentionalClose = true;
    this.isFrozen = true;
    if (this.reconnectTimeout) {
      clearTimeout(this.reconnectTimeout);
      this.reconnectTimeout = null;
    }
    this.stopPingInterval();
    this.stopPongWatchdog();
    if (this.ws) {
      // Neutralize handlers so the async onclose (from ws.close()) doesn't
      // fire and null the sessionId that we need to preserve for resume().
      this.ws.onclose = null;
      this.ws.onerror = null;
      this.reconnectAttempts = this.maxReconnectAttempts; // Prevent auto-reconnect
      this.ws.close();
      this.ws = null;
    }
    this.isConnected = false;
    // NOTE: Do NOT clear persisted sessionId or null the sessionId itself so
    // that resume() → resetAndReconnect() can reattach to the tmux session.
  }

  /** Resume after tab unfreeze. Triggers immediate reconnection with session restore.
   *  
   *  IMPORTANT: This method is called when the page becomes visible again.
   *  At this point, the TerminalPane component may still be mounted with
   *  isActive=true. We need to ensure the reconnection doesn't cause the
   *  pane to tear down and recreate its xterm instance.
   */
  resume() {
    this.isFrozen = false;
    this.resetAndReconnect();
  }

  /** Check if the service is currently frozen (waiting to resume). */
  isCurrentlyFrozen(): boolean {
    return this.isFrozen;
  }

  /** Check if the service is currently re-connecting (resetAndReconnect called
   *  but the WebSocket has not yet confirmed open or permanently failed). */
  isReconnecting(): boolean {
    return this._isReconnecting;
  }

  /** Check if the service is currently connected. */
  isConnectedToServer(): boolean {
    return this.isConnected;
  }

  /** Reset all reconnection state and trigger an immediate reconnect attempt.
   *  Unlike disconnect() + connect(), this does NOT mark the close as
   *  intentional, so auto-reconnect continues to work if the first attempt fails.
   *  Also restores any previously persisted sessionId for reattachment. */
  resetAndReconnect() {
    this._isReconnecting = true;
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
    this.reconnectAttempts = 0;
    this.intentionalClose = false;
    // Restore persisted sessionId for reattach after tab discard
    this.restorePersistedSessionId();
    this.connect();
  }
}

export { TerminalWebSocketService };
