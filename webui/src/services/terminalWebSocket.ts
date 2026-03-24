import { debugLog } from '../utils/log';

type TerminalEventCallback = (event: any) => void;

class TerminalWebSocketService {
  private static instance: TerminalWebSocketService;
  private ws: WebSocket | null = null;
  private callbacks: TerminalEventCallback[] = [];
  private reconnectAttempts = 0;
  private maxReconnectAttempts = 5;
  private reconnectDelay = 1000;
  private sessionId: string | null = null;
  private isConnected = false;
  private eventHandler: TerminalEventCallback | null = null;
  private pingInterval: NodeJS.Timeout | null = null;
  private intentionalClose = false;
  private reconnectTimeout: NodeJS.Timeout | null = null;

  private constructor() {}

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
    // Got pong response, connection is alive
  }

  static getInstance(): TerminalWebSocketService {
    if (!TerminalWebSocketService.instance) {
      TerminalWebSocketService.instance = new TerminalWebSocketService();
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

    if (this.reconnectTimeout) {
      clearTimeout(this.reconnectTimeout);
      this.reconnectTimeout = null;
    }

    // Use environment variable if provided, otherwise use relative URL
    const wsUrl = process.env.REACT_APP_TERMINAL_WS_URL || (() => {
      const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
      return `${protocol}//${window.location.host}/terminal`;
    })();

    debugLog('Connecting to Terminal WebSocket:', wsUrl);

    this.ws = new WebSocket(wsUrl);

    this.ws.onopen = () => {
      debugLog('Terminal WebSocket connected');
      this.reconnectAttempts = 0;
      this.isConnected = true;
      this.startPingInterval();
      this.notifyCallbacks({ type: 'connection_status', data: { connected: true } });
    };

    this.ws.onclose = (event) => {
      debugLog('Terminal WebSocket disconnected:', event);
      this.stopPingInterval();
      this.isConnected = false;
      this.sessionId = null;
      this.notifyCallbacks({ type: 'connection_status', data: { connected: false } });

      // Only reconnect if not intentionally closed
      if (!this.intentionalClose && this.reconnectAttempts < this.maxReconnectAttempts) {
        this.reconnectAttempts++;
        this.reconnectTimeout = setTimeout(() => {
          debugLog(`Attempting terminal reconnect (${this.reconnectAttempts}/${this.maxReconnectAttempts})`);
          this.reconnectTimeout = null;
          this.connect();
        }, this.reconnectDelay * this.reconnectAttempts);
      }
      this.intentionalClose = false;
    };

    this.ws.onerror = (error) => {
      console.error('Terminal WebSocket error:', error);
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
          // Notify that we're now ready to send commands
          this.notifyCallbacks({ type: 'session_ready', data: { session_id: this.sessionId } });
        }
        
        this.notifyCallbacks(data);
      } catch (error) {
        console.error('Failed to parse Terminal WebSocket message:', error, event.data);
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
    if (this.ws) {
      this.reconnectAttempts = this.maxReconnectAttempts; // Prevent auto-reconnect
      this.ws.close();
      this.ws = null;
    }
    this.isConnected = false;
    this.sessionId = null;
  }

  onEvent(callback: TerminalEventCallback) {
    if (!this.callbacks.includes(callback)) {
      this.callbacks.push(callback);
    }
  }

  removeEvent(callback: TerminalEventCallback) {
    this.callbacks = this.callbacks.filter(cb => cb !== callback);
  }

  private notifyCallbacks(event: any) {
    this.callbacks.forEach(callback => callback(event));
    if (this.eventHandler) {
      this.eventHandler(event);
    }
  }

  sendCommand(command: string) {
    if (!this.isConnected || !this.sessionId) {
      console.warn('Terminal not connected or no session, cannot send command:', command);
      return false;
    }

    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      const message = {
        type: 'input',
        data: {
          session_id: this.sessionId,
          input: command
        }
      };
      this.ws.send(JSON.stringify(message));
      debugLog('Sent terminal command:', command);
      return true;
    } else {
      console.warn('Terminal WebSocket not ready, cannot send command:', command);
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
          input
        }
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
          cols: cols,
          rows: rows
        }
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
          session_id: this.sessionId
        }
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

  getSessionId(): string | null {
    return this.sessionId;
  }
}

export { TerminalWebSocketService };
