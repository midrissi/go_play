package hyperliquid

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"exchange-relayer/exchanges"
	"exchange-relayer/websocket"
)

// HyperliquidMeta represents the meta response from Hyperliquid API
type HyperliquidMeta struct {
	Universe []HyperliquidAsset `json:"universe"`
}

// HyperliquidAsset represents a trading asset from Hyperliquid API
type HyperliquidAsset struct {
	Name         string `json:"name"`
	OnlyIsolated bool   `json:"onlyIsolated"`
}

// HyperliquidExchange implements the Exchange interface for Hyperliquid
type HyperliquidExchange struct {
	baseURL    string
	wsBaseURL  string
	httpClient *http.Client
	ws         *websocket.WebSocketClient
	// For multiple connections when streaming all symbols
	wsConnections []*websocket.WebSocketClient
	mu            sync.RWMutex
	// Cache for supported symbols
	symbolsCache     []string
	symbolsCacheTime time.Time
	symbolsCacheMu   sync.RWMutex
}

// New creates a new Hyperliquid exchange instance
func New() exchanges.Exchange {
	return &HyperliquidExchange{
		baseURL:   "https://api.hyperliquid.xyz",
		wsBaseURL: "wss://api.hyperliquid.xyz/ws",
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// StreamCandles starts streaming candle data from Hyperliquid
func (h *HyperliquidExchange) StreamCandles(ctx context.Context, symbols []string, interval string, handler exchanges.CandleHandler) error {
	// Validate inputs
	if err := h.ValidateInterval(interval); err != nil {
		return fmt.Errorf("invalid interval: %w", err)
	}

	for _, symbol := range symbols {
		if err := h.ValidateSymbol(symbol); err != nil {
			return fmt.Errorf("invalid symbol %s: %w", symbol, err)
		}
	}

	// Create WebSocket configuration
	config := websocket.DefaultConfig(h.wsBaseURL)
	config.ReadTimeout = 30 * time.Second
	config.WriteTimeout = 10 * time.Second
	config.PingInterval = 30 * time.Second
	config.PongTimeout = 10 * time.Second
	config.ReconnectInterval = 2 * time.Second
	config.MaxReconnectDelay = 60 * time.Second
	config.MaxReconnectAttempts = 0 // Infinite retries

	// Create resilient WebSocket
	h.ws = websocket.NewWebSocketClient(config)

	// Set up message handler
	h.ws.SetMessageHandler(func(message []byte) error {
		return h.handleMessage(message, handler)
	})

	// Set up error handler
	h.ws.SetErrorHandler(func(err error) {
		fmt.Printf("⚠️  WebSocket error: %v\n", err)
	})

	// Set up connection handlers
	h.ws.SetConnectHandler(func() {
		fmt.Printf("✅ Connected to Hyperliquid WebSocket\n")
		// Send subscription messages after connection
		h.subscribeToStreams(symbols, interval)
	})

	h.ws.SetDisconnectHandler(func() {
		fmt.Printf("❌ Disconnected from Hyperliquid WebSocket\n")
	})

	// Connect to WebSocket
	if err := h.ws.Connect(); err != nil {
		return fmt.Errorf("failed to connect to Hyperliquid WebSocket: %w", err)
	}

	// Wait for context cancellation
	<-ctx.Done()

	// Disconnect WebSocket
	return h.ws.Disconnect()
}

// StreamAllCandles starts streaming candle data for ALL supported symbols and ALL supported intervals
func (h *HyperliquidExchange) StreamAllCandles(ctx context.Context, handler exchanges.CandleHandler) error {
	// Get all supported symbols
	symbols, err := h.GetSupportedSymbols()
	if err != nil {
		return fmt.Errorf("failed to get supported symbols: %w", err)
	}

	// Get all supported intervals
	intervals := h.GetSupportedIntervals()

	fmt.Printf("Starting streaming for ALL %d symbols from Hyperliquid with ALL %d intervals\n", len(symbols), len(intervals))
	fmt.Printf("Supported intervals: %v\n", intervals)

	// Calculate total subscriptions needed
	totalSubscriptions := len(symbols) * len(intervals)
	maxSubsPerConn := h.GetMaxSubscriptionsPerConnection()
	if maxSubsPerConn == 0 {
		maxSubsPerConn = totalSubscriptions // Unlimited, use single connection
	}

	numConnections := (totalSubscriptions + maxSubsPerConn - 1) / maxSubsPerConn
	fmt.Printf("Total subscriptions needed: %d\n", totalSubscriptions)
	fmt.Printf("Using %d websocket connections (max %d subscriptions per connection)\n", numConnections, maxSubsPerConn)

	// Create multiple websocket connections
	var wg sync.WaitGroup
	errChan := make(chan error, numConnections)

	// Distribute subscriptions across connections
	subscriptionsPerConn := totalSubscriptions / numConnections
	if totalSubscriptions%numConnections != 0 {
		subscriptionsPerConn++
	}

	subscriptionIndex := 0
	for i := 0; i < numConnections; i++ {
		// Calculate how many subscriptions this connection will handle
		remainingSubs := totalSubscriptions - subscriptionIndex
		connSubs := subscriptionsPerConn
		if connSubs > remainingSubs {
			connSubs = remainingSubs
		}

		// Create subscription list for this connection
		var connSymbols []string
		var connIntervals []string

		// Distribute subscriptions across symbols and intervals
		for connSubs > 0 && subscriptionIndex < totalSubscriptions {
			symbolIdx := subscriptionIndex / len(intervals)
			intervalIdx := subscriptionIndex % len(intervals)

			if symbolIdx < len(symbols) && intervalIdx < len(intervals) {
				connSymbols = append(connSymbols, symbols[symbolIdx])
				connIntervals = append(connIntervals, intervals[intervalIdx])
				subscriptionIndex++
				connSubs--
			} else {
				break
			}
		}

		if len(connSymbols) == 0 {
			continue
		}

		fmt.Printf("Connection %d: streaming %d symbol-interval combinations\n", i+1, len(connSymbols))

		wg.Add(1)
		go func(connIndex int, connSymbols []string, connIntervals []string) {
			defer wg.Done()
			if err := h.streamWithConnectionAllIntervals(ctx, connSymbols, connIntervals, handler); err != nil {
				errChan <- fmt.Errorf("connection %d error: %w", connIndex+1, err)
			}
		}(i, connSymbols, connIntervals)
	}

	// Wait for all connections to complete or error
	go func() {
		wg.Wait()
		close(errChan)
	}()

	// Check for errors
	select {
	case err := <-errChan:
		return err
	case <-ctx.Done():
		return nil
	}
}

// streamWithConnectionAllIntervals creates a single websocket connection for a subset of symbols and intervals
func (h *HyperliquidExchange) streamWithConnectionAllIntervals(ctx context.Context, symbols []string, intervals []string, handler exchanges.CandleHandler) error {
	// Create WebSocket configuration
	config := websocket.DefaultConfig(h.wsBaseURL)
	config.ReadTimeout = 30 * time.Second
	config.WriteTimeout = 10 * time.Second
	config.PingInterval = 30 * time.Second
	config.PongTimeout = 10 * time.Second
	config.ReconnectInterval = 2 * time.Second
	config.MaxReconnectDelay = 60 * time.Second
	config.MaxReconnectAttempts = 0 // Infinite retries

	// Create resilient WebSocket
	ws := websocket.NewWebSocketClient(config)

	// Store connection for cleanup
	h.mu.Lock()
	h.wsConnections = append(h.wsConnections, ws)
	h.mu.Unlock()

	// Set up message handler
	ws.SetMessageHandler(func(message []byte) error {
		return h.handleMessage(message, handler)
	})

	// Set up error handler
	ws.SetErrorHandler(func(err error) {
		fmt.Printf("⚠️  WebSocket error (connection for %d symbol-interval combinations): %v\n", len(symbols), err)
	})

	// Set up connection handlers
	ws.SetConnectHandler(func() {
		fmt.Printf("✅ Connected to Hyperliquid WebSocket (streaming %d symbol-interval combinations)\n", len(symbols))
		// Send subscription message after connection
		h.subscribeToStreamsWithConnectionAllIntervals(ws, symbols, intervals)
	})

	ws.SetDisconnectHandler(func() {
		fmt.Printf("❌ Disconnected from Hyperliquid WebSocket (connection for %d symbol-interval combinations)\n", len(symbols))
	})

	// Connect to WebSocket
	if err := ws.Connect(); err != nil {
		return fmt.Errorf("failed to connect to Hyperliquid WebSocket: %w", err)
	}

	// Wait for context cancellation
	<-ctx.Done()

	// Disconnect
	return ws.Disconnect()
}

// subscribeToStreamsWithConnectionAllIntervals sends subscription messages for multiple symbols and intervals
func (h *HyperliquidExchange) subscribeToStreamsWithConnectionAllIntervals(ws *websocket.WebSocketClient, symbols []string, intervals []string) {
	// Send subscription message for each symbol-interval combination
	// Note: Hyperliquid requires individual subscription messages per symbol-interval pair
	for i, symbol := range symbols {
		if i < len(intervals) {
			interval := intervals[i]
			convertedInterval := h.convertInterval(interval)
			streamName := fmt.Sprintf("candles.%s.%s", symbol, convertedInterval)

			// Convert symbol to Hyperliquid format
			hyperliquidSymbol := h.convertSymbolToHyperliquid(symbol)

			subscriptionMsg := map[string]interface{}{
				"method": "subscribe",
				"subscription": map[string]interface{}{
					"type":     "candle",
					"coin":     hyperliquidSymbol,
					"interval": convertedInterval,
				},
			}

			if err := ws.WriteJSON(subscriptionMsg); err != nil {
				fmt.Printf("⚠️  Failed to subscribe to %s: %v\n", streamName, err)
			} else {
				fmt.Printf("📡 Subscribed to %s\n", streamName)
			}
		}
	}
}

// subscribeToStreams sends subscription messages to Hyperliquid
func (h *HyperliquidExchange) subscribeToStreams(symbols []string, interval string) {
	for _, symbol := range symbols {
		// Convert symbol format (BTCUSD -> BTC)
		hyperliquidSymbol := h.convertSymbolToHyperliquid(symbol)

		subscribeMsg := map[string]interface{}{
			"method": "subscribe",
			"subscription": map[string]interface{}{
				"type":     "candle",
				"coin":     hyperliquidSymbol,
				"interval": h.convertInterval(interval),
			},
		}

		if err := h.ws.WriteJSON(subscribeMsg); err != nil {
			fmt.Printf("⚠️  Failed to subscribe to %s: %v\n", symbol, err)
		} else {
			fmt.Printf("📡 Subscribed to %s (%s) candle stream\n", symbol, hyperliquidSymbol)
		}
	}
}

// handleMessage processes incoming WebSocket messages
func (h *HyperliquidExchange) handleMessage(message []byte, handler exchanges.CandleHandler) error {
	var response map[string]interface{}
	if err := json.Unmarshal(message, &response); err != nil {
		return nil // Skip invalid messages
	}

	// Check if this is candle data
	if channel, ok := response["channel"].(string); ok && channel == "candle" {
		if data, ok := response["data"].(map[string]interface{}); ok {
			candle := h.parseCandle(data)
			if candle.Symbol != "" {
				handler(candle)
			}
		}
	}

	return nil
}

// parseCandle parses candle data from Hyperliquid response
func (h *HyperliquidExchange) parseCandle(data map[string]interface{}) exchanges.Candle {
	candle := exchanges.Candle{}

	// Parse symbol (s field)
	if symbol, ok := data["s"].(string); ok {
		candle.Symbol = symbol
	}

	// Parse timestamps (t = start time, T = end time)
	if startTime, ok := data["t"].(float64); ok {
		candle.OpenTime = time.Unix(int64(startTime)/1000, 0)
	}

	if endTime, ok := data["T"].(float64); ok {
		candle.CloseTime = time.Unix(int64(endTime)/1000, 0)
	}

	// Parse prices (o = open, h = high, l = low, c = close)
	if open, ok := data["o"].(string); ok {
		if val, err := strconv.ParseFloat(open, 64); err == nil {
			candle.Open = val
		}
	}

	if high, ok := data["h"].(string); ok {
		if val, err := strconv.ParseFloat(high, 64); err == nil {
			candle.High = val
		}
	}

	if low, ok := data["l"].(string); ok {
		if val, err := strconv.ParseFloat(low, 64); err == nil {
			candle.Low = val
		}
	}

	if close, ok := data["c"].(string); ok {
		if val, err := strconv.ParseFloat(close, 64); err == nil {
			candle.Close = val
		}
	}

	// Parse volume (v field)
	if volume, ok := data["v"].(string); ok {
		if val, err := strconv.ParseFloat(volume, 64); err == nil {
			candle.Volume = val
		}
	}

	return candle
}

// convertInterval converts standard interval format to Hyperliquid format
func (h *HyperliquidExchange) convertInterval(interval string) string {
	switch interval {
	case "1m":
		return "1m"
	case "5m":
		return "5m"
	case "15m":
		return "15m"
	case "1h":
		return "1h"
	case "4h":
		return "4h"
	case "1d":
		return "1d"
	default:
		return "1m" // Default to 1 minute
	}
}

// GetSupportedIntervals returns supported candle intervals for Hyperliquid
func (h *HyperliquidExchange) GetSupportedIntervals() []string {
	return []string{
		"1m", "5m", "15m", "1h", "4h", "1d",
	}
}

// GetSupportedSymbols returns supported trading symbols from Hyperliquid API
func (h *HyperliquidExchange) GetSupportedSymbols() ([]string, error) {
	// Check cache first (cache for 1 hour)
	h.symbolsCacheMu.RLock()
	if len(h.symbolsCache) > 0 && time.Since(h.symbolsCacheTime) < time.Hour {
		symbols := make([]string, len(h.symbolsCache))
		copy(symbols, h.symbolsCache)
		h.symbolsCacheMu.RUnlock()
		return symbols, nil
	}
	h.symbolsCacheMu.RUnlock()

	// Fetch from API
	symbols, err := h.fetchSymbolsFromAPI()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch symbols from Hyperliquid API: %w", err)
	}

	// Update cache
	h.symbolsCacheMu.Lock()
	h.symbolsCache = symbols
	h.symbolsCacheTime = time.Now()
	h.symbolsCacheMu.Unlock()

	return symbols, nil
}

// fetchSymbolsFromAPI fetches trading symbols from Hyperliquid API
func (h *HyperliquidExchange) fetchSymbolsFromAPI() ([]string, error) {
	url := fmt.Sprintf("%s/info", h.baseURL)

	// Create POST request with JSON body
	requestBody := map[string]string{"type": "meta"}
	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make HTTP request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API request failed with status: %d", resp.StatusCode)
	}

	var meta HyperliquidMeta
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		return nil, fmt.Errorf("failed to decode API response: %w", err)
	}

	var symbols []string
	for _, asset := range meta.Universe {
		// Include all assets (both isolated and cross-margin)
		symbols = append(symbols, asset.Name)
	}

	if len(symbols) == 0 {
		return nil, fmt.Errorf("no trading assets found")
	}

	fmt.Printf("📊 Fetched %d active trading assets from Hyperliquid API\n", len(symbols))
	return symbols, nil
}

// ValidateSymbol checks if a symbol is valid for Hyperliquid
func (h *HyperliquidExchange) ValidateSymbol(symbol string) error {
	if symbol == "" {
		return fmt.Errorf("symbol cannot be empty")
	}

	// Convert to Hyperliquid format
	hyperliquidSymbol := h.convertSymbolToHyperliquid(symbol)

	// Get supported symbols from API
	supportedSymbols, err := h.GetSupportedSymbols()
	if err != nil {
		// If API fails, fall back to basic validation
		return fmt.Errorf("API validation failed: %v", err)
	}

	// Check if symbol is in the supported list
	for _, supported := range supportedSymbols {
		if hyperliquidSymbol == supported {
			return nil
		}
	}

	return fmt.Errorf("unsupported symbol: %s (converted to %s). Symbol not found in active trading assets", symbol, hyperliquidSymbol)
}

// ValidateInterval checks if an interval is valid for Hyperliquid
func (h *HyperliquidExchange) ValidateInterval(interval string) error {
	supported := h.GetSupportedIntervals()
	for _, supportedInterval := range supported {
		if interval == supportedInterval {
			return nil
		}
	}
	return fmt.Errorf("unsupported interval: %s. Supported intervals: %v", interval, supported)
}

// convertSymbolToHyperliquid converts symbol format from BTCUSD to BTC
func (h *HyperliquidExchange) convertSymbolToHyperliquid(symbol string) string {
	// Remove common suffixes
	symbol = strings.TrimSuffix(symbol, "USDT")
	symbol = strings.TrimSuffix(symbol, "USD")
	symbol = strings.TrimSuffix(symbol, "BUSD")

	// Convert to uppercase
	return strings.ToUpper(symbol)
}

// GetMaxSubscriptionsPerConnection returns the maximum number of subscriptions per websocket connection
// Hyperliquid allows up to 50 subscriptions per connection
func (h *HyperliquidExchange) GetMaxSubscriptionsPerConnection() int {
	return 50
}
