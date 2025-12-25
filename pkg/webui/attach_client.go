package webui

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
)

// AttachModeClient handles WebSocket connection as a client (attaching to running instance)
type AttachModeClient struct {
	conn         *websocket.Conn
	port         int
	connID       atomic.Int32
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	onOutput     func(eventType string, data map[string]interface{})
	onDisconnect func(error)
	onReconnect  func()
	mu           sync.RWMutex
}

// NewAttachModeClient creates a new WebSocket client for attaching to a running instance
func NewAttachModeClient(port int, outputHandler func(eventType string, data map[string]interface{})) (*AttachModeClient, error) {
	client := &AttachModeClient{
		port:     port,
		onOutput: outputHandler,
	}

	client.ctx, client.cancel = context.WithCancel(context.Background())

	return client, nil
}

// Connect establishes WebSocket connection to the running instance
func (c *AttachModeClient) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil {
		return fmt.Errorf("already connected")
	}

	// WebSocket URL
	wsURL := fmt.Sprintf("ws://localhost:%d/ws", c.port)

	// Configure dialer with timeouts
	dialer := websocket.DefaultDialer
	dialer.HandshakeTimeout = 5 * time.Second

	// Connect to WebSocket
	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		return fmt.Errorf("failed to connect to %s: %w", wsURL, err)
	}

	c.conn = conn
	c.connID.Store(int32(time.Now().UnixNano()))

	log.Printf("🔗 Connected to ledit instance on port %d", c.port)

	// Start read and write loops
	c.wg.Add(2)
	go c.readLoop()
	go c.pingLoop()

	// Start reconnection monitor if callback provided
	if c.onDisconnect != nil {
		c.wg.Add(1)
		go c.reconnectMonitor()
	}

	return nil
}

// Send sends a message to the server
func (c *AttachModeClient) Send(eventType string, data map[string]interface{}) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.conn == nil {
		return fmt.Errorf("not connected")
	}

	message := map[string]interface{}{
		"type":    eventType,
		"data":    data,
		"conn_id": c.connID.Load(),
	}

	if err := c.conn.WriteJSON(message); err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}

	return nil
}

// SendQuery sends a query request to the server
func (c *AttachModeClient) SendQuery(query string) error {
	return c.Send("query", map[string]interface{}{"query": query})
}

// SendInput sends input text to the server (for interaction)
func (c *AttachModeClient) SendInput(input string) error {
	return c.Send("input", map[string]interface{}{"input": input})
}

// readLoop reads messages from WebSocket
func (c *AttachModeClient) readLoop() {
	defer c.wg.Done()

	for {
		select {
		case <-c.ctx.Done():
			return
		default:
			c.mu.RLock()
			conn := c.conn
			c.mu.RUnlock()

			if conn == nil {
				time.Sleep(100 * time.Millisecond)
				continue
			}

			// Set read deadline for heartbeat
			conn.SetReadDeadline(time.Now().Add(60 * time.Second))

			var msg map[string]interface{}
			if err := conn.ReadJSON(&msg); err != nil {
				if c.isWebSocketCloseError(err) {
					log.Printf("WebSocket closed: %v", err)
				} else if isTimeoutError(err) {
					// Heartbeat timeout, continue
					continue
				} else {
					log.Printf("WebSocket read error: %v", err)
				}

				// Notify disconnect
				if c.onDisconnect != nil {
					c.onDisconnect(err)
				}

				c.closeConnection()
				return
			}

			// Parse message type
			eventType, ok := msg["type"].(string)
			if !ok {
				continue
			}

			// Extract data
			data, _ := msg["data"].(map[string]interface{})

			// Handle message types
			switch eventType {
			case "output", "query_started", "query_completed", "error", "metrics_update", "stream_chunk", "connection_status", "file_list", "search_result":
				if c.onOutput != nil {
					c.onOutput(eventType, data)
				}

			case "pong":
				// Ping response, ignore

			default:
				log.Printf("Unknown message type: %s", eventType)
			}
		}
	}
}

// pingLoop periodically sends ping to keep connection alive
func (c *AttachModeClient) pingLoop() {
	defer c.wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.mu.RLock()
			conn := c.conn
			c.mu.RUnlock()

			if conn != nil {
				if err := conn.WriteJSON(map[string]interface{}{"type": "ping"}); err != nil {
					log.Printf("Ping failed: %v", err)
					c.closeConnection()
					return
				}
			}
		}
	}
}

// reconnectMonitor monitors for disconnects and attempts reconnection
func (c *AttachModeClient) reconnectMonitor() {
	defer c.wg.Done()

	// Check connection health every 2 seconds
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			// Check if connection is still alive
			c.mu.RLock()
			isConnected := c.conn != nil
			c.mu.RUnlock()

			if !isConnected {
				// Try to reconnect
				log.Printf("Attempting to reconnect to port %d...", c.port)
				if err := c.Connect(); err != nil {
					log.Printf("Reconnect failed: %v", err)

					// Check if server is actually down
					if !c.isServerRunning() {
						log.Printf("Server on port %d is not reachable", c.port)
						if c.onReconnect != nil {
							c.onReconnect()
						}
						return
					}
				}
			}
		}
	}
}

// isServerRunning checks if the server on the port is actually running
func (c *AttachModeClient) isServerRunning() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	url := fmt.Sprintf("http://localhost:%d/health", c.port)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return false
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}

// isWebSocketCloseError checks if this is a normal close error
func (c *AttachModeClient) isWebSocketCloseError(err error) bool {
	if strings.Contains(err.Error(), "use of closed network connection") {
		return true
	}
	if strings.Contains(err.Error(), "unexpected close") {
		return true
	}
	return false
}

// isTimeoutError checks if error is a timeout
func isTimeoutError(err error) bool {
	return strings.Contains(err.Error(), "timeout")
}

// closeConnection closes the WebSocket connection
func (c *AttachModeClient) closeConnection() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
}

// Disconnect closes the connection
func (c *AttachModeClient) Disconnect() {
	c.cancel()
	c.closeConnection()
	c.wg.Wait()
	log.Printf("🔌 Disconnected from ledit instance on port %d", c.port)
}

// SetDisconnectHandler sets a callback for disconnection events
func (c *AttachModeClient) SetDisconnectHandler(handler func(error)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onDisconnect = handler
}

// SetReconnectHandler sets a callback for when server goes down and reconnection isn't possible
func (c *AttachModeClient) SetReconnectHandler(handler func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onReconnect = handler
}

// CheckPortAvailable checks if a port is available to bind to
func CheckPortAvailable(port int) bool {
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return false // Port is in use
	}
	listener.Close()
	return true // Port is free
}

// FindAvailablePort finds an available port starting from a base port
func FindAvailablePort(basePort int) int {
	port := basePort
	for port < basePort+100 {
		if CheckPortAvailable(port) {
			return port
		}
		port++
	}
	return basePort + 100 // Return last attempt even if not available
}

// RunAttachMode runs in attach mode with automatic failover
// Returns failoverNeeded = true if server went down and new server should be started
func RunAttachMode(port int, outputHandler func(string, map[string]interface{})) (failoverNeeded bool, err error) {
	// Check if server is actually running on the port
	if !checkServerHealth(port) {
		return true, fmt.Errorf("server on port %d is not responding - failover needed", port)
	}

	// Create and connect WebSocket client
	client, err := NewAttachModeClient(port, outputHandler)
	if err != nil {
		return true, fmt.Errorf("failed to create WebSocket client: %w", err)
	}

	failoverChan := make(chan bool, 1)
	failoverDone := make(chan struct{})

	// Set handlers
	client.SetDisconnectHandler(func(err error) {
		// On disconnect, check if server is down
		if !checkServerHealth(port) {
			select {
			case failoverChan <- true:
			default:
			}
		}
	})
	client.SetReconnectHandler(func() {
		select {
		case failoverChan <- true:
		default:
		}
	})

	// Connect to server
	if err := client.Connect(); err != nil {
		return true, fmt.Errorf("failed to connect to server: %w", err)
	}

	// Handle signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Wait for failover or exit
	select {
	case <-sigCh:
		client.Disconnect()
		return false, nil

	case failover := <-failoverChan:
		client.Disconnect()
		return failover, nil

	case <-failoverDone:
		client.Disconnect()
		return false, nil
	}
}

// checkServerHealth checks if a server is responding on the specified port
func checkServerHealth(port int) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	url := fmt.Sprintf("http://localhost:%d/health", port)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return false
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false
	}

	// Check response body is valid JSON
	var health map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		return false
	}

	return true
}
