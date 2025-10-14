package binance

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"binance-go/exchanges"
	"binance-go/websocket"
)

// BinanceExchange implements the Exchange interface for Binance
type BinanceExchange struct {
	baseURL string
	ws      *websocket.WebSocketClient
}

// New creates a new Binance exchange instance
func New() exchanges.Exchange {
	return &BinanceExchange{
		baseURL: "wss://stream.binance.com:9443/ws",
	}
}

// StreamKlines starts streaming kline data from Binance
func (b *BinanceExchange) StreamKlines(ctx context.Context, symbols []string, interval string, handler exchanges.KlineHandler) error {
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
func (b *BinanceExchange) handleMessage(message []byte, handler exchanges.KlineHandler) error {
	// First, try to parse as subscription response
	var subResponse struct {
		Result interface{} `json:"result"`
		ID     int         `json:"id"`
	}
	if err := json.Unmarshal(message, &subResponse); err == nil && subResponse.ID == 1 {
		return nil // Subscription confirmation received
	}

	// Parse as kline data (direct format from Binance)
	var klineData map[string]interface{}
	if err := json.Unmarshal(message, &klineData); err != nil {
		return nil // Skip invalid messages
	}

	// Check if this is a kline event
	if eventType, ok := klineData["e"].(string); !ok || eventType != "kline" {
		return nil
	}

	// Extract kline data
	klineInfo, ok := klineData["k"].(map[string]interface{})
	if !ok {
		return nil
	}

	// Extract symbol
	symbol, ok := klineData["s"].(string)
	if !ok {
		return nil
	}

	// Extract timestamps
	startTime, ok := klineInfo["t"].(float64)
	if !ok {
		return nil
	}
	endTime, ok := klineInfo["T"].(float64)
	if !ok {
		return nil
	}

	// Extract prices and volume
	openPrice, ok := klineInfo["o"].(string)
	if !ok {
		return nil
	}
	highPrice, ok := klineInfo["h"].(string)
	if !ok {
		return nil
	}
	lowPrice, ok := klineInfo["l"].(string)
	if !ok {
		return nil
	}
	closePrice, ok := klineInfo["c"].(string)
	if !ok {
		return nil
	}
	volume, ok := klineInfo["v"].(string)
	if !ok {
		return nil
	}

	// Parse kline data
	kline := exchanges.Kline{
		Symbol:    symbol,
		OpenTime:  time.Unix(int64(startTime)/1000, 0),
		CloseTime: time.Unix(int64(endTime)/1000, 0),
	}

	// Parse prices and volume
	if open, err := strconv.ParseFloat(openPrice, 64); err == nil {
		kline.Open = open
	}
	if high, err := strconv.ParseFloat(highPrice, 64); err == nil {
		kline.High = high
	}
	if low, err := strconv.ParseFloat(lowPrice, 64); err == nil {
		kline.Low = low
	}
	if close, err := strconv.ParseFloat(closePrice, 64); err == nil {
		kline.Close = close
	}
	if vol, err := strconv.ParseFloat(volume, 64); err == nil {
		kline.Volume = vol
	}

	// Call handler
	handler(kline)
	return nil
}

// GetSupportedIntervals returns supported kline intervals for Binance
func (b *BinanceExchange) GetSupportedIntervals() []string {
	return []string{
		"1m", "3m", "5m", "15m", "30m",
		"1h", "2h", "4h", "6h", "8h", "12h",
		"1d", "3d", "1w", "1M",
	}
}

// GetSupportedSymbols returns supported trading symbols
func (b *BinanceExchange) GetSupportedSymbols() ([]string, error) {
	// For simplicity, return common symbols
	// In a real implementation, you would fetch this from Binance API
	return []string{
		"BTCUSDT", "ETHUSDT", "BNBUSDT", "ADAUSDT", "XRPUSDT",
		"SOLUSDT", "DOTUSDT", "DOGEUSDT", "AVAXUSDT", "MATICUSDT",
	}, nil
}

// ValidateSymbol checks if a symbol is valid for Binance
func (b *BinanceExchange) ValidateSymbol(symbol string) error {
	if symbol == "" {
		return fmt.Errorf("symbol cannot be empty")
	}

	// Basic validation - symbol should be uppercase and contain USDT
	if !strings.HasSuffix(strings.ToUpper(symbol), "USDT") {
		return fmt.Errorf("symbol must end with USDT")
	}

	return nil
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
