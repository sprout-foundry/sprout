type EventCallback = (event: any) => void;

class WebSocketService {
  private static instance: WebSocketService;
  private ws: WebSocket | null = null;
  private callbacks: EventCallback[] = [];
  private reconnectAttempts = 0;
  private maxReconnectAttempts = 5;
  private reconnectDelay = 2000;
  private pingInterval: NodeJS.Timeout | null = null;
  private lastPongTime = Date.now();
  private intentionalClose = false;

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
      this.lastPongTime = Date.now();
      this.startPingInterval();
      this.notifyCallbacks({ type: 'connection_status', data: { connected: true } });
    };

    this.ws.onclose = (event) => {
      console.log('WebSocket disconnected:', event);
      this.stopPingInterval();
      this.notifyCallbacks({ type: 'connection_status', data: { connected: false } });

      // Only reconnect if not intentionally closed and not already reconnecting
      if (!this.intentionalClose && this.reconnectAttempts < this.maxReconnectAttempts) {
        this.reconnectAttempts++;
        setTimeout(() => {
          console.log(`Attempting to reconnect (${this.reconnectAttempts}/${this.maxReconnectAttempts})`);
          this.connect();
        }, this.reconnectDelay * this.reconnectAttempts);
      }
      // Reset intentional close flag after handling
      this.intentionalClose = false;
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
        console.error('Failed to parse WebSocket message:', error, event.data);
      }
    };
  }

  disconnect() {
    this.intentionalClose = true;
    if (this.ws) {
      this.reconnectAttempts = this.maxReconnectAttempts; // Prevent auto-reconnect
      this.ws.close();
      this.ws = null;
    }
  }

  onEvent(callback: EventCallback) {
    if (!this.callbacks.includes(callback)) {
      this.callbacks.push(callback);
    }
  }

  removeEvent(callback: EventCallback) {
    this.callbacks = this.callbacks.filter(cb => cb !== callback);
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
