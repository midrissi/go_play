package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"exchange-relayer/exchanges"
	"exchange-relayer/exchanges/binance"
	"exchange-relayer/exchanges/hyperliquid"
	"exchange-relayer/persistence"
	pb "exchange-relayer/proto"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// QuoteStream represents a client's quote stream
type QuoteStream struct {
	Symbols  map[string]bool // Set of symbols this client is interested in
	Interval string          // Interval this client wants (e.g., "1m", "5m")
	Stream   pb.MarketData_StreamQuotesServer
	Cancel   context.CancelFunc
	mu       sync.RWMutex
}

// MarketDataServer implements the gRPC MarketData service
type MarketDataServer struct {
	pb.UnimplementedMarketDataServer

	// Server configuration
	trackedSymbols map[string]bool // Symbols the server is tracking
	exchange       exchanges.Exchange
	exchangeName   string

	// Client management
	clients   map[string]*QuoteStream // Map of client ID to stream
	clientsMu sync.RWMutex

	// Quote broadcasting - now per interval
	quoteChans   map[string]chan exchanges.Candle // Map of interval to quote channel
	quoteChansMu sync.RWMutex

	// Exchange connections per interval
	exchangeConnections   map[string]context.CancelFunc // Map of interval to cancel function
	exchangeConnectionsMu sync.RWMutex

	ctx    context.Context
	cancel context.CancelFunc
}

// NewMarketDataServer creates a new MarketData server
func NewMarketDataServer(exchangeName string, symbols []string) (*MarketDataServer, error) {
	// Create exchange instance
	var exchange exchanges.Exchange
	switch exchangeName {
	case "binance":
		exchange = binance.New()
	case "hyperliquid":
		exchange = hyperliquid.New()
	default:
		return nil, fmt.Errorf("unsupported exchange: %s", exchangeName)
	}

	// Validate symbols if provided
	trackedSymbols := make(map[string]bool)
	if len(symbols) > 0 {
		for _, symbol := range symbols {
			if err := exchange.ValidateSymbol(symbol); err != nil {
				return nil, fmt.Errorf("invalid symbol %s: %w", symbol, err)
			}
			trackedSymbols[symbol] = true
		}
	}

	ctx, cancel := context.WithCancel(context.Background())

	server := &MarketDataServer{
		trackedSymbols:      trackedSymbols,
		exchange:            exchange,
		exchangeName:        exchangeName,
		clients:             make(map[string]*QuoteStream),
		quoteChans:          make(map[string]chan exchanges.Candle),
		exchangeConnections: make(map[string]context.CancelFunc),
		ctx:                 ctx,
		cancel:              cancel,
	}

	return server, nil
}

// Start starts the server and begins streaming quotes
func (s *MarketDataServer) Start(ctx context.Context) error {
	// Start quote broadcaster
	go s.broadcastQuotes()

	return nil
}

// Stop stops the server
func (s *MarketDataServer) Stop() {
	s.cancel()

	// Close all quote channels
	s.quoteChansMu.Lock()
	for _, ch := range s.quoteChans {
		close(ch)
	}
	s.quoteChansMu.Unlock()

	// Cancel all exchange connections
	s.exchangeConnectionsMu.Lock()
	for _, cancel := range s.exchangeConnections {
		cancel()
	}
	s.exchangeConnectionsMu.Unlock()
}

// startQuoteStreamingForInterval starts streaming quotes from the exchange for a specific interval
func (s *MarketDataServer) startQuoteStreamingForInterval(ctx context.Context, interval string) error {
	if len(s.trackedSymbols) == 0 {
		log.Println("No symbols to track, skipping quote streaming")
		return nil
	}

	// Validate interval
	if err := s.exchange.ValidateInterval(interval); err != nil {
		return fmt.Errorf("invalid interval %s: %w", interval, err)
	}

	symbols := make([]string, 0, len(s.trackedSymbols))
	for symbol := range s.trackedSymbols {
		symbols = append(symbols, symbol)
	}

	log.Printf("Starting quote streaming for %d symbols with interval %s: %v", len(symbols), interval, symbols)

	// Create quote channel for this interval
	s.quoteChansMu.Lock()
	if s.quoteChans[interval] == nil {
		s.quoteChans[interval] = make(chan exchanges.Candle, 1000)
	}
	quoteChan := s.quoteChans[interval]
	s.quoteChansMu.Unlock()

	// Start streaming in background
	go func() {
		err := s.exchange.StreamCandles(ctx, symbols, interval, func(candle exchanges.Candle) {
			// Persist candle to database
			if err := persistence.SaveCandle(persistence.CandleModel{
				Exchange:  s.exchangeName,
				Symbol:    candle.Symbol,
				Interval:  interval,
				OpenTime:  candle.OpenTime,
				CloseTime: candle.CloseTime,
				Open:      candle.Open,
				High:      candle.High,
				Low:       candle.Low,
				Close:     candle.Close,
				Volume:    candle.Volume,
			}); err != nil {
				log.Printf("Error persisting candle for %s %s: %v", candle.Symbol, interval, err)
			}

			// Send to quote channel for broadcasting
			select {
			case quoteChan <- candle:
			case <-ctx.Done():
				return
			default:
				// Channel is full, skip this quote to avoid blocking
				log.Printf("Warning: quote channel full for interval %s, dropping quote for %s", interval, candle.Symbol)
			}
		})

		if err != nil {
			log.Printf("Error streaming quotes for interval %s: %v", interval, err)
		}
	}()

	return nil
}

// broadcastQuotes broadcasts quotes to all connected clients
func (s *MarketDataServer) broadcastQuotes() {
	for {
		select {
		case <-s.ctx.Done():
			return
		default:
			// Check all quote channels
			s.quoteChansMu.RLock()
			for interval, quoteChan := range s.quoteChans {
				select {
				case candle, ok := <-quoteChan:
					if !ok {
						continue
					}
					s.sendQuoteToClients(candle, interval)
				default:
					// No message available on this channel
				}
			}
			s.quoteChansMu.RUnlock()

			// Small delay to prevent busy waiting
			time.Sleep(10 * time.Millisecond)
		}
	}
}

// sendQuoteToClients sends a quote to all clients interested in the symbol and interval
func (s *MarketDataServer) sendQuoteToClients(candle exchanges.Candle, interval string) {
	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()

	quote := &pb.Quote{
		Symbol:    candle.Symbol,
		Price:     candle.Close, // Use close price as current price
		Volume:    candle.Volume,
		Timestamp: candle.CloseTime.UnixMilli(),
		Exchange:  s.exchangeName,
	}

	for clientID, client := range s.clients {
		client.mu.RLock()
		if client.Symbols[candle.Symbol] && client.Interval == interval {
			client.mu.RUnlock()

			// Send quote to client
			if err := client.Stream.Send(quote); err != nil {
				log.Printf("Error sending quote to client %s: %v", clientID, err)
				// Remove client on error
				go s.removeClient(clientID)
			}
		} else {
			client.mu.RUnlock()
		}
	}
}

// StreamQuotes implements the gRPC StreamQuotes method
func (s *MarketDataServer) StreamQuotes(req *pb.SubscribeRequest, stream pb.MarketData_StreamQuotesServer) error {
	// Validate request
	if len(req.Symbols) == 0 {
		return status.Error(codes.InvalidArgument, "at least one symbol must be specified")
	}

	if req.Interval == "" {
		return status.Error(codes.InvalidArgument, "interval must be specified")
	}

	// Validate interval
	if err := s.exchange.ValidateInterval(req.Interval); err != nil {
		return status.Error(codes.InvalidArgument, fmt.Sprintf("invalid interval %s: %v", req.Interval, err))
	}

	// Filter symbols to only include tracked ones
	var validSymbols []string
	for _, symbol := range req.Symbols {
		if s.trackedSymbols[symbol] {
			validSymbols = append(validSymbols, symbol)
		}
	}

	if len(validSymbols) == 0 {
		return status.Error(codes.InvalidArgument, "none of the requested symbols are being tracked by this server")
	}

	// Start streaming for this interval if not already started
	s.quoteChansMu.Lock()
	if s.quoteChans[req.Interval] == nil {
		s.quoteChansMu.Unlock()
		if err := s.startQuoteStreamingForInterval(s.ctx, req.Interval); err != nil {
			return status.Error(codes.Internal, fmt.Sprintf("failed to start streaming for interval %s: %v", req.Interval, err))
		}
	} else {
		s.quoteChansMu.Unlock()
	}

	// Create client stream
	clientCtx, clientCancel := context.WithCancel(stream.Context())
	clientID := fmt.Sprintf("client_%d", time.Now().UnixNano())

	symbolsSet := make(map[string]bool)
	for _, symbol := range validSymbols {
		symbolsSet[symbol] = true
	}

	client := &QuoteStream{
		Symbols:  symbolsSet,
		Interval: req.Interval,
		Stream:   stream,
		Cancel:   clientCancel,
	}

	// Add client
	s.clientsMu.Lock()
	s.clients[clientID] = client
	s.clientsMu.Unlock()

	log.Printf("Client %s connected, subscribed to symbols: %v with interval: %s", clientID, validSymbols, req.Interval)

	// Wait for client disconnect
	<-clientCtx.Done()

	// Remove client
	s.removeClient(clientID)

	log.Printf("Client %s disconnected", clientID)
	return nil
}

// removeClient removes a client from the server
func (s *MarketDataServer) removeClient(clientID string) {
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()

	if client, exists := s.clients[clientID]; exists {
		client.Cancel()
		delete(s.clients, clientID)
	}
}

func main() {
	// Parse command line flags
	var (
		port     = flag.String("port", "50051", "gRPC server port")
		exchange = flag.String("exchange", "binance", "Exchange to use (binance, hyperliquid)")
		symbols  = flag.String("symbols", "", "Comma-separated list of symbols to track (e.g., BTCUSDT,ETHUSDT)")
		all      = flag.Bool("all", false, "Track all supported symbols")
		dbPath   = flag.String("db_path", "./data/exchange_relayer.db", "Path to SQLite database file")
	)
	flag.Parse()

	// Initialize persistence (SQLite via GORM)
	if err := persistence.Init(*dbPath); err != nil {
		log.Fatalf("failed to initialize database: %v", err)
	}

	// Determine symbols to track
	var symbolsToTrack []string
	if *all {
		// Get all supported symbols
		var exchangeInstance exchanges.Exchange
		switch *exchange {
		case "binance":
			exchangeInstance = binance.New()
		case "hyperliquid":
			exchangeInstance = hyperliquid.New()
		default:
			log.Fatalf("Unsupported exchange: %s", *exchange)
		}

		symbols, err := exchangeInstance.GetSupportedSymbols()
		if err != nil {
			log.Fatalf("Failed to get supported symbols: %v", err)
		}
		symbolsToTrack = symbols
		log.Printf("Tracking all %d supported symbols", len(symbolsToTrack))
	} else if *symbols != "" {
		symbolsToTrack = strings.Split(*symbols, ",")
		// Trim whitespace
		for i, symbol := range symbolsToTrack {
			symbolsToTrack[i] = strings.TrimSpace(symbol)
		}
		log.Printf("Tracking specified symbols: %v", symbolsToTrack)
	} else {
		log.Fatal("Either --all or --symbols must be specified")
	}

	// Create server
	server, err := NewMarketDataServer(*exchange, symbolsToTrack)
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
	}

	// Create gRPC server
	grpcServer := grpc.NewServer()
	pb.RegisterMarketDataServer(grpcServer, server)

	// Start quote streaming
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := server.Start(ctx); err != nil {
		log.Fatalf("Failed to start quote streaming: %v", err)
	}

	// Setup graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		log.Println("Shutting down gracefully...")
		cancel()
		server.Stop()
		grpcServer.GracefulStop()
	}()

	// Start gRPC server
	lis, err := net.Listen("tcp", ":"+*port)
	if err != nil {
		log.Fatalf("Failed to listen on port %s: %v", *port, err)
	}

	log.Printf("gRPC server listening on port %s", *port)
	log.Printf("Exchange: %s", *exchange)
	log.Printf("Tracked symbols: %d", len(symbolsToTrack))

	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}
