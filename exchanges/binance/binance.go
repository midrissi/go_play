package binance

import (
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

// BinanceExchangeInfo represents the exchange info response from Binance API
type BinanceExchangeInfo struct {
	Symbols []BinanceSymbol `json:"symbols"`
}

// BinanceSymbol represents a trading symbol from Binance API
type BinanceSymbol struct {
	Symbol                 string        `json:"symbol"`
	Status                 string        `json:"status"`
	BaseAsset              string        `json:"baseAsset"`
	BaseAssetPrecision     int           `json:"baseAssetPrecision"`
	QuoteAsset             string        `json:"quoteAsset"`
	QuotePrecision         int           `json:"quotePrecision"`
	QuoteAssetPrecision    int           `json:"quoteAssetPrecision"`
	OrderTypes             []string      `json:"orderTypes"`
	IcebergAllowed         bool          `json:"icebergAllowed"`
	OcoAllowed             bool          `json:"ocoAllowed"`
	IsSpotTradingAllowed   bool          `json:"isSpotTradingAllowed"`
	IsMarginTradingAllowed bool          `json:"isMarginTradingAllowed"`
	Filters                []interface{} `json:"filters"`
	Permissions            []string      `json:"permissions"`
}

// BinanceExchange implements the Exchange interface for Binance
type BinanceExchange struct {
	baseURL    string
	apiBaseURL string
	ws         *websocket.WebSocketClient
	httpClient *http.Client
	// For multiple connections when streaming all symbols
	wsConnections []*websocket.WebSocketClient
	mu            sync.RWMutex
	// Cache for supported symbols
	symbolsCache     []string
	symbolsCacheTime time.Time
	symbolsCacheMu   sync.RWMutex
}

// New creates a new Binance exchange instance
func New() exchanges.Exchange {
	return &BinanceExchange{
		baseURL:    "wss://stream.binance.com:9443/ws",
		apiBaseURL: "https://api.binance.com",
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// StreamCandles starts streaming candle data from Binance
func (b *BinanceExchange) StreamCandles(ctx context.Context, symbols []string, interval string, handler exchanges.CandleHandler) error {
	// Validate inputs
	if err := b.ValidateInterval(interval); err != nil {
		return fmt.Errorf("invalid interval: %w", err)
	}

	for _, symbol := range symbols {
		if err := b.ValidateSymbol(symbol); err != nil {
			return fmt.Errorf("invalid symbol %s: %w", symbol, err)
		}
	}

	// Create WebSocket configuration
	config := websocket.DefaultConfig(b.baseURL)
	config.ReadTimeout = 30 * time.Second
	config.WriteTimeout = 10 * time.Second
	config.PingInterval = 30 * time.Second
	config.PongTimeout = 10 * time.Second
	config.ReconnectInterval = 2 * time.Second
	config.MaxReconnectDelay = 60 * time.Second
	config.MaxReconnectAttempts = 0 // Infinite retries

	// Create resilient WebSocket
	b.ws = websocket.NewWebSocketClient(config)

	// Set up message handler
	b.ws.SetMessageHandler(func(message []byte) error {
		return b.handleMessage(message, handler)
	})

	// Set up error handler
	b.ws.SetErrorHandler(func(err error) {
		fmt.Printf("⚠️  WebSocket error: %v\n", err)
	})

	// Set up connection handlers
	b.ws.SetConnectHandler(func() {
		fmt.Printf("✅ Connected to Binance WebSocket\n")
		// Send subscription message after connection
		b.subscribeToStreams(symbols, interval)
	})

	b.ws.SetDisconnectHandler(func() {
		fmt.Printf("❌ Disconnected from Binance WebSocket\n")
	})

	// Connect to WebSocket
	if err := b.ws.Connect(); err != nil {
		return fmt.Errorf("failed to connect to Binance WebSocket: %w", err)
	}

	// Wait for context cancellation
	<-ctx.Done()

	// Disconnect WebSocket
	return b.ws.Disconnect()
}

// StreamAllCandles starts streaming candle data for ALL supported symbols and ALL supported intervals
func (b *BinanceExchange) StreamAllCandles(ctx context.Context, handler exchanges.CandleHandler) error {
	// Get all supported symbols
	symbols, err := b.GetSupportedSymbols()
	if err != nil {
		return fmt.Errorf("failed to get supported symbols: %w", err)
	}

	// Get all supported intervals
	intervals := b.GetSupportedIntervals()

	fmt.Printf("Starting streaming for ALL %d symbols from Binance with ALL %d intervals\n", len(symbols), len(intervals))
	fmt.Printf("Supported intervals: %v\n", intervals)

	// Calculate total subscriptions needed
	totalSubscriptions := len(symbols) * len(intervals)
	maxSubsPerConn := b.GetMaxSubscriptionsPerConnection()
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
			if err := b.streamWithConnectionAllIntervals(ctx, connSymbols, connIntervals, handler); err != nil {
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
func (b *BinanceExchange) streamWithConnectionAllIntervals(ctx context.Context, symbols []string, intervals []string, handler exchanges.CandleHandler) error {
	// Create WebSocket configuration
	config := websocket.DefaultConfig(b.baseURL)
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
	b.mu.Lock()
	b.wsConnections = append(b.wsConnections, ws)
	b.mu.Unlock()

	// Set up message handler
	ws.SetMessageHandler(func(message []byte) error {
		return b.handleMessage(message, handler)
	})

	// Set up error handler
	ws.SetErrorHandler(func(err error) {
		fmt.Printf("⚠️  WebSocket error (connection for %d symbol-interval combinations): %v\n", len(symbols), err)
	})

	// Set up connection handlers
	ws.SetConnectHandler(func() {
		fmt.Printf("✅ Connected to Binance WebSocket (streaming %d symbol-interval combinations)\n", len(symbols))
		// Send subscription message after connection
		b.subscribeToStreamsWithConnectionAllIntervals(ws, symbols, intervals)
	})

	ws.SetDisconnectHandler(func() {
		fmt.Printf("❌ Disconnected from Binance WebSocket (connection for %d symbol-interval combinations)\n", len(symbols))
	})

	// Connect to WebSocket
	if err := ws.Connect(); err != nil {
		return fmt.Errorf("failed to connect to Binance WebSocket: %w", err)
	}

	// Wait for context cancellation
	<-ctx.Done()

	// Disconnect
	return ws.Disconnect()
}

// subscribeToStreamsWithConnectionAllIntervals sends batched subscription messages for symbol-interval combinations
func (b *BinanceExchange) subscribeToStreamsWithConnectionAllIntervals(ws *websocket.WebSocketClient, symbols []string, intervals []string) {
	// Create streams list for all symbol-interval combinations
	streams := make([]string, 0, len(symbols))
	for i, symbol := range symbols {
		if i < len(intervals) {
			interval := intervals[i]
			streamName := fmt.Sprintf("%s@kline_%s", strings.ToLower(symbol), interval)
			streams = append(streams, streamName)
		}
	}

	// Binance recommends max 200 streams per subscription message
	// But we'll use a smaller batch size to be safe
	const maxStreamsPerBatch = 100

	// Send batched subscription messages
	for i := 0; i < len(streams); i += maxStreamsPerBatch {
		end := i + maxStreamsPerBatch
		if end > len(streams) {
			end = len(streams)
		}

		batchStreams := streams[i:end]

		subscriptionMsg := map[string]interface{}{
			"method": "SUBSCRIBE",
			"params": batchStreams,
			"id":     time.Now().UnixNano(),
		}

		if err := ws.WriteJSON(subscriptionMsg); err != nil {
			fmt.Printf("⚠️  Failed to subscribe to batch of %d streams: %v\n", len(batchStreams), err)
		} else {
			fmt.Printf("📡 Subscribed to batch of %d streams (total: %d/%d)\n", len(batchStreams), end, len(streams))
		}

		// Small delay between batches to avoid overwhelming the server
		if end < len(streams) {
			time.Sleep(200 * time.Millisecond)
		}
	}
}

// subscribeToStreams sends subscription message to Binance
func (b *BinanceExchange) subscribeToStreams(symbols []string, interval string) {
	// Create subscription message
	streams := make([]string, len(symbols))
	for i, symbol := range symbols {
		streams[i] = fmt.Sprintf("%s@kline_%s", strings.ToLower(symbol), interval)
	}

	subscribeMsg := map[string]interface{}{
		"method": "SUBSCRIBE",
		"params": streams,
		"id":     1,
	}

	// Send subscription message
	if err := b.ws.WriteJSON(subscribeMsg); err != nil {
		fmt.Printf("⚠️  Failed to subscribe to streams: %v\n", err)
	} else {
		fmt.Printf("📡 Subscribed to streams: %v\n", streams)
	}
}

// handleMessage processes incoming WebSocket messages
func (b *BinanceExchange) handleMessage(message []byte, handler exchanges.CandleHandler) error {
	// First, try to parse as subscription response
	var subResponse struct {
		Result interface{} `json:"result"`
		ID     int         `json:"id"`
	}
	if err := json.Unmarshal(message, &subResponse); err == nil && subResponse.ID == 1 {
		return nil // Subscription confirmation received
	}

	// Parse as candle data (direct format from Binance)
	var candleData map[string]interface{}
	if err := json.Unmarshal(message, &candleData); err != nil {
		return nil // Skip invalid messages
	}

	// Check if this is a candle event
	if eventType, ok := candleData["e"].(string); !ok || eventType != "kline" {
		return nil
	}

	// Extract candle data
	candleInfo, ok := candleData["k"].(map[string]interface{})
	if !ok {
		return nil
	}

	// Extract symbol
	symbol, ok := candleData["s"].(string)
	if !ok {
		return nil
	}

	// Extract timestamps
	startTime, ok := candleInfo["t"].(float64)
	if !ok {
		return nil
	}
	endTime, ok := candleInfo["T"].(float64)
	if !ok {
		return nil
	}

	// Extract prices and volume
	openPrice, ok := candleInfo["o"].(string)
	if !ok {
		return nil
	}
	highPrice, ok := candleInfo["h"].(string)
	if !ok {
		return nil
	}
	lowPrice, ok := candleInfo["l"].(string)
	if !ok {
		return nil
	}
	closePrice, ok := candleInfo["c"].(string)
	if !ok {
		return nil
	}
	volume, ok := candleInfo["v"].(string)
	if !ok {
		return nil
	}

	// Parse candle data
	candle := exchanges.Candle{
		Symbol:    symbol,
		OpenTime:  time.Unix(int64(startTime)/1000, 0),
		CloseTime: time.Unix(int64(endTime)/1000, 0),
	}

	// Parse prices and volume
	if open, err := strconv.ParseFloat(openPrice, 64); err == nil {
		candle.Open = open
	}
	if high, err := strconv.ParseFloat(highPrice, 64); err == nil {
		candle.High = high
	}
	if low, err := strconv.ParseFloat(lowPrice, 64); err == nil {
		candle.Low = low
	}
	if close, err := strconv.ParseFloat(closePrice, 64); err == nil {
		candle.Close = close
	}
	if vol, err := strconv.ParseFloat(volume, 64); err == nil {
		candle.Volume = vol
	}

	// Call handler
	handler(candle)
	return nil
}

// GetSupportedIntervals returns supported candle intervals for Binance
func (b *BinanceExchange) GetSupportedIntervals() []string {
	return []string{
		"1m", "3m", "5m", "15m", "30m",
		"1h", "2h", "4h", "6h", "8h", "12h",
		"1d", "3d", "1w", "1M",
	}
}

// GetSupportedSymbols returns supported trading symbols from Binance API
func (b *BinanceExchange) GetSupportedSymbols() ([]string, error) {
	// Check cache first (cache for 1 hour)
	b.symbolsCacheMu.RLock()
	if len(b.symbolsCache) > 0 && time.Since(b.symbolsCacheTime) < time.Hour {
		symbols := make([]string, len(b.symbolsCache))
		copy(symbols, b.symbolsCache)
		b.symbolsCacheMu.RUnlock()
		return symbols, nil
	}
	b.symbolsCacheMu.RUnlock()

	// Fetch from API
	symbols, err := b.fetchSymbolsFromAPI()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch symbols from Binance API: %w", err)
	}

	// Update cache
	b.symbolsCacheMu.Lock()
	b.symbolsCache = symbols
	b.symbolsCacheTime = time.Now()
	b.symbolsCacheMu.Unlock()

	return symbols, nil
}

// fetchSymbolsFromAPI fetches trading symbols from Binance API
func (b *BinanceExchange) fetchSymbolsFromAPI() ([]string, error) {
	url := fmt.Sprintf("%s/api/v3/exchangeInfo", b.apiBaseURL)

	resp, err := b.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to make HTTP request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API request failed with status: %d", resp.StatusCode)
	}

	var exchangeInfo BinanceExchangeInfo
	if err := json.NewDecoder(resp.Body).Decode(&exchangeInfo); err != nil {
		return nil, fmt.Errorf("failed to decode API response: %w", err)
	}

	var symbols []string
	for _, symbol := range exchangeInfo.Symbols {
		// Only include active symbols that are spot trading allowed
		if symbol.Status == "TRADING" && symbol.IsSpotTradingAllowed {
			symbols = append(symbols, symbol.Symbol)
		}
	}

	if len(symbols) == 0 {
		return nil, fmt.Errorf("no active trading symbols found")
	}

	fmt.Printf("📊 Fetched %d active trading symbols from Binance API\n", len(symbols))
	return symbols, nil
}

// ValidateSymbol checks if a symbol is valid for Binance
func (b *BinanceExchange) ValidateSymbol(symbol string) error {
	if symbol == "" {
		return fmt.Errorf("symbol cannot be empty")
	}

	// Get supported symbols from API
	supportedSymbols, err := b.GetSupportedSymbols()
	if err != nil {
		// If API fails, fall back to basic validation
		if !strings.HasSuffix(strings.ToUpper(symbol), "USDT") {
			return fmt.Errorf("symbol must end with USDT (API validation failed: %v)", err)
		}
		return nil
	}

	// Check if symbol is in the supported list
	for _, supported := range supportedSymbols {
		if symbol == supported {
			return nil
		}
	}

	return fmt.Errorf("unsupported symbol: %s. Symbol not found in active trading pairs", symbol)
}

// ValidateInterval checks if an interval is valid for Binance
func (b *BinanceExchange) ValidateInterval(interval string) error {
	supported := b.GetSupportedIntervals()
	for _, supportedInterval := range supported {
		if interval == supportedInterval {
			return nil
		}
	}
	return fmt.Errorf("unsupported interval: %s. Supported intervals: %v", interval, supported)
}

// GetMaxSubscriptionsPerConnection returns the maximum number of subscriptions per websocket connection
// Binance allows up to 200 subscriptions per connection
func (b *BinanceExchange) GetMaxSubscriptionsPerConnection() int {
	return 1024
}
