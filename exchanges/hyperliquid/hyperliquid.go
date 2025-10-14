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

// StreamKlines starts streaming kline data from Hyperliquid
func (h *HyperliquidExchange) StreamKlines(ctx context.Context, symbols []string, interval string, handler exchanges.KlineHandler) error {
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
		subscribeMsg := map[string]interface{}{
			"method": "subscribe",
			"subscription": map[string]interface{}{
				"type":     "candle",
				"coin":     symbol,
				"interval": h.convertInterval(interval),
			},
		}

		if err := h.ws.WriteJSON(subscribeMsg); err != nil {
			fmt.Printf("⚠️  Failed to subscribe to %s: %v\n", symbol, err)
		} else {
			fmt.Printf("📡 Subscribed to %s kline stream\n", symbol)
		}
	}
}

// handleMessage processes incoming WebSocket messages
func (h *HyperliquidExchange) handleMessage(message []byte, handler exchanges.KlineHandler) error {
	var response map[string]interface{}
	if err := json.Unmarshal(message, &response); err != nil {
		return nil // Skip invalid messages
	}

	// Check if this is kline data
	if data, ok := response["data"].(map[string]interface{}); ok {
		if candleData, ok := data["candle"].(map[string]interface{}); ok {
			kline := h.parseKline(candleData)
			if kline.Symbol != "" {
				handler(kline)
			}
		}
	}

	return nil
}

// parseKline parses kline data from Hyperliquid response
func (h *HyperliquidExchange) parseKline(data map[string]interface{}) exchanges.Kline {
	kline := exchanges.Kline{}

	if symbol, ok := data["coin"].(string); ok {
		kline.Symbol = symbol
	}

	if startTime, ok := data["startTime"].(float64); ok {
		kline.OpenTime = time.Unix(int64(startTime)/1000, 0)
	}

	if endTime, ok := data["endTime"].(float64); ok {
		kline.CloseTime = time.Unix(int64(endTime)/1000, 0)
	}

	if open, ok := data["open"].(string); ok {
		if val, err := strconv.ParseFloat(open, 64); err == nil {
			kline.Open = val
		}
	}

	if high, ok := data["high"].(string); ok {
		if val, err := strconv.ParseFloat(high, 64); err == nil {
			kline.High = val
		}
	}

	if low, ok := data["low"].(string); ok {
		if val, err := strconv.ParseFloat(low, 64); err == nil {
			kline.Low = val
		}
	}

	if close, ok := data["close"].(string); ok {
		if val, err := strconv.ParseFloat(close, 64); err == nil {
			kline.Close = val
		}
	}

	if volume, ok := data["volume"].(string); ok {
		if val, err := strconv.ParseFloat(volume, 64); err == nil {
			kline.Volume = val
		}
	}

	return kline
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

// GetSupportedIntervals returns supported kline intervals for Hyperliquid
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

	// Basic validation - symbol should be uppercase and not contain USDT
	if strings.Contains(strings.ToUpper(symbol), "USDT") {
		return fmt.Errorf("Hyperliquid symbols should not contain USDT")
	}

	return nil
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
