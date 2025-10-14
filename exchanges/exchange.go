package exchanges

import (
	"context"
	"time"
)

// Kline represents a candlestick data point
type Kline struct {
	Symbol    string    `json:"symbol"`
	OpenTime  time.Time `json:"open_time"`
	CloseTime time.Time `json:"close_time"`
	Open      float64   `json:"open"`
	High      float64   `json:"high"`
	Low       float64   `json:"low"`
	Close     float64   `json:"close"`
	Volume    float64   `json:"volume"`
}

// KlineHandler is a function type for handling incoming kline data
type KlineHandler func(Kline)

// Exchange defines the interface that all exchange implementations must satisfy
type Exchange interface {
	// StreamKlines starts streaming kline data for the given symbols and interval
	// The handler function will be called for each kline update
	StreamKlines(ctx context.Context, symbols []string, interval string, handler KlineHandler) error

	// GetSupportedIntervals returns the list of supported kline intervals
	GetSupportedIntervals() []string

	// GetSupportedSymbols returns the list of supported trading symbols
	GetSupportedSymbols() ([]string, error)

	// ValidateSymbol checks if a symbol is valid for this exchange
	ValidateSymbol(symbol string) error

	// ValidateInterval checks if an interval is valid for this exchange
	ValidateInterval(interval string) error
}
