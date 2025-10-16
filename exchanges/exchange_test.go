package exchanges

import (
	"context"
	"testing"
	"time"
)

// MockExchange is a mock implementation of the Exchange interface for testing
type MockExchange struct {
	supportedIntervals []string
	supportedSymbols   []string
}

func NewMockExchange() Exchange {
	return &MockExchange{
		supportedIntervals: []string{"1m", "5m", "15m", "1h", "4h", "1d"},
		supportedSymbols:   []string{"BTCUSDT", "ETHUSDT", "BNBUSDT"},
	}
}

func (m *MockExchange) StreamCandles(ctx context.Context, symbols []string, interval string, handler CandleHandler) error {
	// Simulate streaming by sending a few mock candles
	for i := 0; i < 3; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			candle := Candle{
				Symbol:    symbols[0],
				OpenTime:  time.Now().Add(-time.Duration(i) * time.Minute),
				CloseTime: time.Now().Add(-time.Duration(i-1) * time.Minute),
				Open:      50000.0 + float64(i),
				High:      50100.0 + float64(i),
				Low:       49900.0 + float64(i),
				Close:     50050.0 + float64(i),
				Volume:    1000.0 + float64(i)*100,
			}
			handler(candle)
			time.Sleep(100 * time.Millisecond)
		}
	}
	return nil
}

func (m *MockExchange) GetSupportedIntervals() []string {
	return m.supportedIntervals
}

func (m *MockExchange) GetSupportedSymbols() ([]string, error) {
	return m.supportedSymbols, nil
}

func (m *MockExchange) ValidateSymbol(symbol string) error {
	for _, s := range m.supportedSymbols {
		if s == symbol {
			return nil
		}
	}
	return &ValidationError{Field: "symbol", Value: symbol, Message: "unsupported symbol"}
}

func (m *MockExchange) ValidateInterval(interval string) error {
	for _, i := range m.supportedIntervals {
		if i == interval {
			return nil
		}
	}
	return &ValidationError{Field: "interval", Value: interval, Message: "unsupported interval"}
}

func (m *MockExchange) StreamAllCandles(ctx context.Context, handler CandleHandler) error {
	// Simulate streaming all symbols with all intervals by sending mock candles
	for _, symbol := range m.supportedSymbols {
		for range m.supportedIntervals {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
				candle := Candle{
					Symbol:    symbol,
					OpenTime:  time.Now().Add(-time.Minute),
					CloseTime: time.Now(),
					Open:      50000.0,
					High:      50100.0,
					Low:       49900.0,
					Close:     50050.0,
					Volume:    1000.0,
				}
				handler(candle)
				time.Sleep(10 * time.Millisecond) // Faster for testing
			}
		}
	}
	return nil
}

func (m *MockExchange) GetMaxSubscriptionsPerConnection() int {
	return 10 // Mock value for testing
}

// ValidationError represents a validation error
type ValidationError struct {
	Field   string
	Value   string
	Message string
}

func (e *ValidationError) Error() string {
	return e.Message
}

// TestExchangeInterface tests the basic functionality of the Exchange interface
func TestExchangeInterface(t *testing.T) {
	exchange := NewMockExchange()

	// Test GetSupportedIntervals
	intervals := exchange.GetSupportedIntervals()
	if len(intervals) == 0 {
		t.Error("Expected supported intervals, got empty slice")
	}

	// Test GetSupportedSymbols
	symbols, err := exchange.GetSupportedSymbols()
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if len(symbols) == 0 {
		t.Error("Expected supported symbols, got empty slice")
	}

	// Test ValidateSymbol
	err = exchange.ValidateSymbol("BTCUSDT")
	if err != nil {
		t.Errorf("Expected no error for valid symbol, got %v", err)
	}

	err = exchange.ValidateSymbol("INVALID")
	if err == nil {
		t.Error("Expected error for invalid symbol, got nil")
	}

	// Test ValidateInterval
	err = exchange.ValidateInterval("1m")
	if err != nil {
		t.Errorf("Expected no error for valid interval, got %v", err)
	}

	err = exchange.ValidateInterval("invalid")
	if err == nil {
		t.Error("Expected error for invalid interval, got nil")
	}

	// Test StreamCandles
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	candles := make([]Candle, 0)
	handler := func(candle Candle) {
		candles = append(candles, candle)
	}

	err = exchange.StreamCandles(ctx, []string{"BTCUSDT"}, "1m", handler)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if len(candles) == 0 {
		t.Error("Expected candles to be received, got none")
	}
}
