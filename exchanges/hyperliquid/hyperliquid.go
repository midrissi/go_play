package hyperliquid

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"binance-go/exchanges"
	"binance-go/websocket"
)

// HyperliquidExchange implements the Exchange interface for Hyperliquid
type HyperliquidExchange struct {
	baseURL    string
	wsBaseURL  string
	httpClient *http.Client
	ws         *websocket.WebSocketClient
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

// GetSupportedSymbols returns supported trading symbols
func (h *HyperliquidExchange) GetSupportedSymbols() ([]string, error) {
	// For simplicity, return common symbols
	// In a real implementation, you would fetch this from Hyperliquid API
	return []string{
		"BTC", "ETH", "SOL", "AVAX", "MATIC",
		"ARB", "OP", "SUI", "APT", "SEI",
	}, nil
}

// ValidateSymbol checks if a symbol is valid for Hyperliquid
func (h *HyperliquidExchange) ValidateSymbol(symbol string) error {
	if symbol == "" {
		return fmt.Errorf("symbol cannot be empty")
	}

	// Convert to Hyperliquid format and check if it's supported
	hyperliquidSymbol := h.convertSymbolToHyperliquid(symbol)
	supportedSymbols, _ := h.GetSupportedSymbols()

	for _, supported := range supportedSymbols {
		if hyperliquidSymbol == supported {
			return nil
		}
	}

	return fmt.Errorf("unsupported symbol: %s (converted to %s). Supported symbols: %v", symbol, hyperliquidSymbol, supportedSymbols)
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
