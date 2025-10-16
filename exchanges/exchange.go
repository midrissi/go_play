package exchanges

import (
	"context"
	"time"
)

// Candle represents a candlestick data point
type Candle struct {
	Symbol    string    `json:"symbol"`
	OpenTime  time.Time `json:"open_time"`
	CloseTime time.Time `json:"close_time"`
	Open      float64   `json:"open"`
	High      float64   `json:"high"`
	Low       float64   `json:"low"`
	Close     float64   `json:"close"`
	Volume    float64   `json:"volume"`
}

// CandleHandler is a function type for handling incoming candle data
type CandleHandler func(Candle)

// Exchange defines the interface that all exchange implementations must satisfy
type Exchange interface {
	// StreamCandles starts streaming candle data for the given symbols and interval
	// The handler function will be called for each candle update
	StreamCandles(ctx context.Context, symbols []string, interval string, handler CandleHandler) error

	// StreamAllCandles starts streaming candle data for ALL supported symbols and ALL supported intervals
	// Uses multiple websocket connections if necessary to handle subscription limits
	// The handler function will be called for each candle update
	StreamAllCandles(ctx context.Context, handler CandleHandler) error

	// GetSupportedIntervals returns the list of supported candle intervals
	GetSupportedIntervals() []string

	// GetSupportedSymbols returns the list of supported trading symbols
	GetSupportedSymbols() ([]string, error)

	// ValidateSymbol checks if a symbol is valid for this exchange
	ValidateSymbol(symbol string) error

	// ValidateInterval checks if an interval is valid for this exchange
	ValidateInterval(interval string) error

	// GetMaxSubscriptionsPerConnection returns the maximum number of subscriptions per websocket connection
	// Returns 0 if unlimited
	GetMaxSubscriptionsPerConnection() int
}
