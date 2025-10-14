package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"exchange-relayer/exchanges"
	"exchange-relayer/exchanges/binance"
	"exchange-relayer/exchanges/hyperliquid"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	exchangeName string
	symbols      []string
	interval     string
)

var rootCmd = &cobra.Command{
	Use:   "exchange-relayer",
	Short: "A CLI tool to fetch candle streams from various exchanges",
	Long:  `A flexible CLI tool that supports fetching candle (candlestick) data from multiple exchanges including Binance and Hyperliquid.`,
	Run:   run,
}

func init() {
	rootCmd.Flags().StringVarP(&exchangeName, "exchange", "e", "binance", "Exchange to use (binance, hyperliquid)")
	rootCmd.Flags().StringSliceVarP(&symbols, "symbols", "s", []string{"BTCUSDT"}, "Trading symbols to fetch")
	rootCmd.Flags().StringVarP(&interval, "interval", "i", "1m", "Candle interval (1m, 5m, 15m, 1h, 4h, 1d)")

	viper.BindPFlag("exchange", rootCmd.Flags().Lookup("exchange"))
	viper.BindPFlag("symbols", rootCmd.Flags().Lookup("symbols"))
	viper.BindPFlag("interval", rootCmd.Flags().Lookup("interval"))
}

func run(cmd *cobra.Command, args []string) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Println("\nShutting down gracefully...")
		cancel()
	}()

	// Create exchange client
	var exchange exchanges.Exchange
	switch exchangeName {
	case "binance":
		exchange = binance.New()
	case "hyperliquid":
		exchange = hyperliquid.New()
	default:
		log.Fatalf("Unsupported exchange: %s", exchangeName)
	}

	fmt.Printf("Starting candle stream from %s for symbols: %v with interval: %s\n",
		exchangeName, symbols, interval)

	// Start streaming
	err := exchange.StreamCandles(ctx, symbols, interval, func(candle exchanges.Candle) {
		fmt.Printf("[%s] %s: O=%.8f H=%.8f L=%.8f C=%.8f V=%.8f\n",
			candle.Symbol,
			candle.OpenTime.Format("2006-01-02 15:04:05"),
			candle.Open,
			candle.High,
			candle.Low,
			candle.Close,
			candle.Volume,
		)
	})

	if err != nil {
		log.Fatalf("Error streaming candles: %v", err)
	}
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
