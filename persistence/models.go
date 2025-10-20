package persistence

import "time"

// CandleModel is the DB representation of a candle
type CandleModel struct {
	ID        uint `gorm:"primaryKey"`
	CreatedAt time.Time
	UpdatedAt time.Time

	Exchange  string    `gorm:"uniqueIndex:idx_exchange_symbol_interval_open_time,priority:1;size:32;not null"`
	Symbol    string    `gorm:"uniqueIndex:idx_exchange_symbol_interval_open_time,priority:2;size:32;not null"`
	Interval  string    `gorm:"uniqueIndex:idx_exchange_symbol_interval_open_time,priority:3;size:8;not null"`
	OpenTime  time.Time `gorm:"uniqueIndex:idx_exchange_symbol_interval_open_time,priority:4;not null"`
	CloseTime time.Time `gorm:"not null"`

	Open   float64 `gorm:"not null"`
	High   float64 `gorm:"not null"`
	Low    float64 `gorm:"not null"`
	Close  float64 `gorm:"not null"`
	Volume float64 `gorm:"not null"`
}
