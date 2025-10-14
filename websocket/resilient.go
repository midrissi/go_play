package websocket

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Config holds WebSocket connection configuration
type Config struct {
	URL                  string        // WebSocket URL
	ReadTimeout          time.Duration // Read timeout
	WriteTimeout         time.Duration // Write timeout
	PingInterval         time.Duration // Ping interval for keep-alive
	PongTimeout          time.Duration // Pong timeout
	ReconnectInterval    time.Duration // Base reconnection interval
	MaxReconnectDelay    time.Duration // Maximum reconnection delay
	MaxReconnectAttempts int           // Maximum reconnection attempts (0 = infinite)
	BackoffMultiplier    float64       // Exponential backoff multiplier
}

// DefaultConfig returns a default configuration
func DefaultConfig(url string) *Config {
	return &Config{
		URL:                  url,
		ReadTimeout:          30 * time.Second,
		WriteTimeout:         10 * time.Second,
		PingInterval:         30 * time.Second,
		PongTimeout:          10 * time.Second,
		ReconnectInterval:    1 * time.Second,
		MaxReconnectDelay:    60 * time.Second,
		MaxReconnectAttempts: 0, // Infinite retries
		BackoffMultiplier:    2.0,
	}
}

// ResilientWebSocket wraps gorilla/websocket with reconnection logic
type ResilientWebSocket struct {
	config      *Config
	conn        *websocket.Conn
	connMutex   sync.RWMutex
	ctx         context.Context
	cancel      context.CancelFunc
	reconnectCh chan struct{}
	stopCh      chan struct{}
	wg          sync.WaitGroup

	// Connection state
	isConnected    bool
	reconnectCount int
	lastError      error

	// Message handlers
	messageHandler    func([]byte) error
	errorHandler      func(error)
	connectHandler    func()
	disconnectHandler func()
}

// NewResilientWebSocket creates a new resilient WebSocket connection
func NewResilientWebSocket(config *Config) *ResilientWebSocket {
	ctx, cancel := context.WithCancel(context.Background())

	return &ResilientWebSocket{
		config:      config,
		ctx:         ctx,
		cancel:      cancel,
		reconnectCh: make(chan struct{}, 1),
		stopCh:      make(chan struct{}),
	}
}

// SetMessageHandler sets the message handler function
func (rws *ResilientWebSocket) SetMessageHandler(handler func([]byte) error) {
	rws.messageHandler = handler
}

// SetErrorHandler sets the error handler function
func (rws *ResilientWebSocket) SetErrorHandler(handler func(error)) {
	rws.errorHandler = handler
}

// SetConnectHandler sets the connection handler function
func (rws *ResilientWebSocket) SetConnectHandler(handler func()) {
	rws.connectHandler = handler
}

// SetDisconnectHandler sets the disconnection handler function
func (rws *ResilientWebSocket) SetDisconnectHandler(handler func()) {
	rws.disconnectHandler = handler
}

// Connect establishes the initial WebSocket connection
func (rws *ResilientWebSocket) Connect() error {
	rws.wg.Add(1)
	go rws.connectionManager()

	// Trigger initial connection
	select {
	case rws.reconnectCh <- struct{}{}:
	default:
	}

	return nil
}

// Disconnect closes the WebSocket connection
func (rws *ResilientWebSocket) Disconnect() error {
	rws.cancel()
	close(rws.stopCh)
	rws.wg.Wait()

	rws.connMutex.Lock()
	defer rws.connMutex.Unlock()

	if rws.conn != nil {
		rws.conn.Close()
		rws.conn = nil
		rws.isConnected = false
	}

	return nil
}

// WriteMessage sends a message through the WebSocket connection
func (rws *ResilientWebSocket) WriteMessage(messageType int, data []byte) error {
	rws.connMutex.RLock()
	conn := rws.conn
	isConnected := rws.isConnected
	rws.connMutex.RUnlock()

	if !isConnected || conn == nil {
		return fmt.Errorf("WebSocket not connected")
	}

	// Set write timeout
	conn.SetWriteDeadline(time.Now().Add(rws.config.WriteTimeout))

	return conn.WriteMessage(messageType, data)
}

// WriteJSON sends a JSON message through the WebSocket connection
func (rws *ResilientWebSocket) WriteJSON(v interface{}) error {
	rws.connMutex.RLock()
	conn := rws.conn
	isConnected := rws.isConnected
	rws.connMutex.RUnlock()

	if !isConnected || conn == nil {
		return fmt.Errorf("WebSocket not connected")
	}

	// Set write timeout
	conn.SetWriteDeadline(time.Now().Add(rws.config.WriteTimeout))

	return conn.WriteJSON(v)
}

// IsConnected returns the current connection status
func (rws *ResilientWebSocket) IsConnected() bool {
	rws.connMutex.RLock()
	defer rws.connMutex.RUnlock()
	return rws.isConnected
}

// GetReconnectCount returns the number of reconnection attempts
func (rws *ResilientWebSocket) GetReconnectCount() int {
	rws.connMutex.RLock()
	defer rws.connMutex.RUnlock()
	return rws.reconnectCount
}

// GetLastError returns the last connection error
func (rws *ResilientWebSocket) GetLastError() error {
	rws.connMutex.RLock()
	defer rws.connMutex.RUnlock()
	return rws.lastError
}

// connectionManager manages the WebSocket connection lifecycle
func (rws *ResilientWebSocket) connectionManager() {
	defer rws.wg.Done()

	for {
		select {
		case <-rws.ctx.Done():
			return
		case <-rws.stopCh:
			return
		case <-rws.reconnectCh:
			rws.attemptConnection()
		}
	}
}

// attemptConnection attempts to establish a WebSocket connection
func (rws *ResilientWebSocket) attemptConnection() {
	// Check if we should attempt reconnection
	if rws.config.MaxReconnectAttempts > 0 && rws.reconnectCount >= rws.config.MaxReconnectAttempts {
		log.Printf("Max reconnection attempts (%d) reached", rws.config.MaxReconnectAttempts)
		return
	}

	// Calculate backoff delay
	delay := rws.calculateBackoffDelay()
	if delay > 0 {
		log.Printf("Waiting %v before reconnection attempt %d", delay, rws.reconnectCount+1)
		time.Sleep(delay)
	}

	// Attempt connection
	log.Printf("Attempting WebSocket connection to %s", rws.config.URL)

	dialer := websocket.DefaultDialer
	dialer.HandshakeTimeout = rws.config.WriteTimeout

	conn, _, err := dialer.Dial(rws.config.URL, nil)
	if err != nil {
		rws.handleConnectionError(err)
		return
	}

	// Configure connection
	conn.SetReadDeadline(time.Now().Add(rws.config.ReadTimeout))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(rws.config.ReadTimeout))
		return nil
	})

	// Update connection state
	rws.connMutex.Lock()
	rws.conn = conn
	rws.isConnected = true
	rws.lastError = nil
	rws.connMutex.Unlock()

	log.Printf("WebSocket connected successfully")

	// Reset reconnect count on successful connection
	rws.reconnectCount = 0

	// Start ping/pong and message reading goroutines
	rws.wg.Add(2)
	go rws.pingPongLoop()
	go rws.readLoop()

	// Call connect handler
	if rws.connectHandler != nil {
		rws.connectHandler()
	}
}

// calculateBackoffDelay calculates the delay for exponential backoff
func (rws *ResilientWebSocket) calculateBackoffDelay() time.Duration {
	if rws.reconnectCount == 0 {
		return 0
	}

	delay := time.Duration(float64(rws.config.ReconnectInterval) *
		pow(rws.config.BackoffMultiplier, float64(rws.reconnectCount-1)))

	if delay > rws.config.MaxReconnectDelay {
		delay = rws.config.MaxReconnectDelay
	}

	return delay
}

// pow calculates x^y
func pow(x, y float64) float64 {
	result := 1.0
	for i := 0; i < int(y); i++ {
		result *= x
	}
	return result
}

// handleConnectionError handles connection errors and triggers reconnection
func (rws *ResilientWebSocket) handleConnectionError(err error) {
	rws.connMutex.Lock()
	rws.isConnected = false
	rws.lastError = err
	rws.reconnectCount++
	rws.connMutex.Unlock()

	log.Printf("WebSocket connection error: %v", err)

	// Call error handler
	if rws.errorHandler != nil {
		rws.errorHandler(err)
	}

	// Call disconnect handler
	if rws.disconnectHandler != nil {
		rws.disconnectHandler()
	}

	// Trigger reconnection if not stopped
	select {
	case <-rws.ctx.Done():
		return
	case <-rws.stopCh:
		return
	default:
		select {
		case rws.reconnectCh <- struct{}{}:
		default:
		}
	}
}

// pingPongLoop sends periodic ping messages
func (rws *ResilientWebSocket) pingPongLoop() {
	defer rws.wg.Done()

	ticker := time.NewTicker(rws.config.PingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-rws.ctx.Done():
			return
		case <-rws.stopCh:
			return
		case <-ticker.C:
			rws.connMutex.RLock()
			conn := rws.conn
			isConnected := rws.isConnected
			rws.connMutex.RUnlock()

			if !isConnected || conn == nil {
				return
			}

			conn.SetWriteDeadline(time.Now().Add(rws.config.WriteTimeout))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				log.Printf("Failed to send ping: %v", err)
				rws.handleConnectionError(err)
				return
			}
		}
	}
}

// readLoop reads messages from the WebSocket connection
func (rws *ResilientWebSocket) readLoop() {
	defer rws.wg.Done()

	for {
		select {
		case <-rws.ctx.Done():
			return
		case <-rws.stopCh:
			return
		default:
			rws.connMutex.RLock()
			conn := rws.conn
			isConnected := rws.isConnected
			rws.connMutex.RUnlock()

			if !isConnected || conn == nil {
				return
			}

			conn.SetReadDeadline(time.Now().Add(rws.config.ReadTimeout))
			messageType, message, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					log.Printf("WebSocket read error: %v", err)
					rws.handleConnectionError(err)
				}
				return
			}

			// Handle different message types
			switch messageType {
			case websocket.TextMessage, websocket.BinaryMessage:
				if rws.messageHandler != nil {
					if err := rws.messageHandler(message); err != nil {
						log.Printf("Message handler error: %v", err)
						// Don't disconnect on message handler errors
					}
				}
			case websocket.PongMessage:
				// Pong received, connection is alive
				continue
			case websocket.CloseMessage:
				log.Printf("Received close message")
				rws.handleConnectionError(fmt.Errorf("connection closed by server"))
				return
			}
		}
	}
}
