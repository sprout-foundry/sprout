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

  private constructor() {}

  static getInstance(): TerminalWebSocketService {
    if (!TerminalWebSocketService.instance) {
      TerminalWebSocketService.instance = new TerminalWebSocketService();
    }
    return TerminalWebSocketService.instance;
  }

  connect() {
    // Don't connect if already connected
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      console.log('Terminal WebSocket already connected');
      return;
    }
    
    // Don't connect if connecting
    if (this.ws && this.ws.readyState === WebSocket.CONNECTING) {
      console.log('Terminal WebSocket already connecting');
      return;
    }

    // Use environment variable if provided, otherwise use relative URL
    const wsUrl = process.env.REACT_APP_TERMINAL_WS_URL || (() => {
      const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
      return `${protocol}//${window.location.host}/terminal`;
    })();

    console.log('Connecting to Terminal WebSocket:', wsUrl);

    this.ws = new WebSocket(wsUrl);

    this.ws.onopen = () => {
      console.log('Terminal WebSocket connected');
      this.reconnectAttempts = 0;
      this.isConnected = true;
      this.notifyCallbacks({ type: 'connection_status', data: { connected: true } });
    };

    this.ws.onclose = (event) => {
      console.log('Terminal WebSocket disconnected:', event);
      this.isConnected = false;
      this.sessionId = null;
      this.notifyCallbacks({ type: 'connection_status', data: { connected: false } });

      // Try to reconnect
      if (this.reconnectAttempts < this.maxReconnectAttempts) {
        this.reconnectAttempts++;
        setTimeout(() => {
          console.log(`Attempting terminal reconnect (${this.reconnectAttempts}/${this.maxReconnectAttempts})`);
          this.connect();
        }, this.reconnectDelay * this.reconnectAttempts);
      }
    };

    this.ws.onerror = (error) => {
      console.error('Terminal WebSocket error:', error);
      this.notifyCallbacks({ type: 'error', data: { message: 'WebSocket connection error' } });
    };

    this.ws.onmessage = (event) => {
      try {
        const data = JSON.parse(event.data);
        console.log('Terminal WebSocket message:', data);
        
        // Handle session creation
        if (data.type === 'session_created') {
          this.sessionId = data.data.session_id;
          console.log('Terminal session created:', this.sessionId);
        }
        
        this.notifyCallbacks(data);
      } catch (error) {
        console.error('Failed to parse Terminal WebSocket message:', error, event.data);
      }
    };
  }

  disconnect() {
    if (this.ws) {
      this.reconnectAttempts = this.maxReconnectAttempts; // Prevent auto-reconnect
      this.ws.close();
      this.ws = null;
    }
    this.isConnected = false;
    this.sessionId = null;
  }

  onEvent(callback: TerminalEventCallback) {
    this.callbacks.push(callback);
  }

  removeEvent(callback: TerminalEventCallback) {
    const index = this.callbacks.indexOf(callback);
    if (index > -1) {
      this.callbacks.splice(index, 1);
    }
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
      console.log('Sent terminal command:', command);
      return true;
    } else {
      console.warn('Terminal WebSocket not ready, cannot send command:', command);
      return false;
    }
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
      console.log('Sent terminal close session');
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