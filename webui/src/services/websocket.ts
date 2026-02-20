type EventCallback = (event: any) => void;

class WebSocketService {
  private static instance: WebSocketService;
  private ws: WebSocket | null = null;
  private callbacks: EventCallback[] = [];
  private reconnectAttempts = 0;
  private maxReconnectAttempts = 2;
  private reconnectDelay = 2000;

  private constructor() {}

  static getInstance(): WebSocketService {
    if (!WebSocketService.instance) {
      WebSocketService.instance = new WebSocketService();
    }
    return WebSocketService.instance;
  }

  connect() {
    // Use environment variable if provided, otherwise use relative URL
    const wsUrl = process.env.REACT_APP_WS_URL || (() => {
      const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
      return `${protocol}//${window.location.host}/ws`;
    })();

    console.log('Connecting to WebSocket:', wsUrl);

    this.ws = new WebSocket(wsUrl);

    this.ws.onopen = () => {
      console.log('WebSocket connected');
      this.reconnectAttempts = 0;
      this.notifyCallbacks({ type: 'connection_status', data: { connected: true } });
    };

    this.ws.onclose = (event) => {
      console.log('WebSocket disconnected:', event);
      this.notifyCallbacks({ type: 'connection_status', data: { connected: false } });

      // Try to reconnect
      if (this.reconnectAttempts < this.maxReconnectAttempts) {
        this.reconnectAttempts++;
        setTimeout(() => {
          console.log(`Attempting to reconnect (${this.reconnectAttempts}/${this.maxReconnectAttempts})`);
          this.connect();
        }, this.reconnectDelay * this.reconnectAttempts);
      }
    };

    this.ws.onerror = (error) => {
      console.error('WebSocket error:', error);
      // If connection fails immediately, stop trying to reconnect
      if (this.reconnectAttempts === 0) {
        console.log('WebSocket failed to connect, will not retry');
        this.reconnectAttempts = this.maxReconnectAttempts;
      }
    };

    this.ws.onmessage = (event) => {
      try {
        const data = JSON.parse(event.data);
        this.notifyCallbacks(data);
      } catch (error) {
        console.error('Failed to parse WebSocket message:', error, event.data);
      }
    };
  }

  disconnect() {
    if (this.ws) {
      this.reconnectAttempts = this.maxReconnectAttempts; // Prevent auto-reconnect
      this.ws.close();
      this.ws = null;
    }
  }

  onEvent(callback: EventCallback) {
    this.callbacks.push(callback);
  }

  private notifyCallbacks(event: any) {
    this.callbacks.forEach(callback => callback(event));
  }

  sendEvent(event: any) {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify(event));
    } else {
      console.warn('WebSocket not connected, cannot send event:', event);
    }
  }

  isConnected(): boolean {
    return this.ws !== null && this.ws.readyState === WebSocket.OPEN;
  }
}

export { WebSocketService };