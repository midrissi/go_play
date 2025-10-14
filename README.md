# Exchange Relayer - Multi-Exchange Candle Streamer

A flexible Go CLI tool for fetching real-time candle (candlestick) data from multiple cryptocurrency exchanges.

## Features

- **Multi-Exchange Support**: Currently supports Binance and Hyperliquid
- **Real-time Streaming**: WebSocket-based streaming for live candle data
- **Resilient Connections**: Automatic reconnection with exponential backoff
- **Connection Health Monitoring**: Ping/pong keep-alive and timeout handling
- **Flexible Architecture**: Easy to add new exchanges
- **CLI Interface**: Simple command-line interface with configurable options
- **Graceful Shutdown**: Proper handling of interrupts and cleanup

## Supported Exchanges

- **Binance**: Full support for all major trading pairs
- **Hyperliquid**: Support for major cryptocurrencies

## Installation

### Option 1: Docker (Recommended)

1. Clone the repository:
```bash
git clone <repository-url>
cd exchange-relayer
```

2. Build and run with Docker:
```bash
# Build Docker image
make docker-build

# Run interactively
make docker-run

# Run with specific exchange
make docker-run-binance
make docker-run-hyperliquid
```

### Option 2: Local Build

1. Clone the repository:
```bash
git clone <repository-url>
cd exchange-relayer
```

2. Install dependencies:
```bash
make deps
# or manually: go mod tidy
```

3. Build the application:
```bash
make build
# or manually: go build -o exchange-relayer
```

## Usage

### Using Make Commands (Recommended)

The project includes convenient Make targets for common operations:

```bash
# Local development
make run                    # Run with default settings
make run-binance           # Run with Binance example
make run-hyperliquid       # Run with Hyperliquid example

# Docker usage
make docker-run            # Run Docker container interactively
make docker-run-binance    # Run Docker with Binance config
make docker-run-hyperliquid # Run Docker with Hyperliquid config
make docker-run-detached   # Run Docker container in background
make docker-logs           # View Docker container logs
make docker-stop           # Stop Docker container
make docker-clean          # Clean up Docker resources

# Development
make test                  # Run tests
make test-coverage         # Run tests with coverage
make fmt                   # Format code
make lint                  # Lint code
make clean                 # Clean build artifacts

# Cross-platform builds
make build-linux           # Build for Linux
make build-windows         # Build for Windows
make build-macos           # Build for macOS

# Show all available targets
make help
```

### Direct Binary Usage

```bash
# Stream BTCUSDT 1-minute candles from Binance
./exchange-relayer --exchange binance --symbols BTCUSDT --interval 1m

# Stream multiple symbols
./exchange-relayer --exchange binance --symbols BTCUSDT,ETHUSDT,BNBUSDT --interval 5m

# Use Hyperliquid exchange
./exchange-relayer --exchange hyperliquid --symbols BTC,ETH,SOL --interval 1h
```

### Command Line Options

- `--exchange, -e`: Exchange to use (binance, hyperliquid)
- `--symbols, -s`: Trading symbols to fetch (comma-separated)
- `--interval, -i`: Candle interval (1m, 5m, 15m, 1h, 4h, 1d)

### Examples

```bash
# Binance examples
./exchange-relayer -e binance -s BTCUSDT -i 1m
./exchange-relayer -e binance -s BTCUSDT,ETHUSDT -i 5m
./exchange-relayer -e binance -s BTCUSDT -i 1h

# Hyperliquid examples
./exchange-relayer -e hyperliquid -s BTC -i 1m
./exchange-relayer -e hyperliquid -s BTC,ETH,SOL -i 15m
./exchange-relayer -e hyperliquid -s BTC -i 1d
```

## Architecture

The project follows a clean architecture pattern with the following structure:

```
├── exchanges/
│   ├── exchange.go          # Exchange interface definition
│   ├── binance/
│   │   └── binance.go       # Binance implementation
│   └── hyperliquid/
│       └── hyperliquid.go   # Hyperliquid implementation
├── websocket/
│   └── resilient.go         # Resilient WebSocket wrapper
├── main.go                  # CLI application
└── go.mod                   # Go module definition
```

### Exchange Interface

All exchanges implement the `exchanges.Exchange` interface:

```go
type Exchange interface {
    StreamCandles(ctx context.Context, symbols []string, interval string, handler CandleHandler) error
    GetSupportedIntervals() []string
    GetSupportedSymbols() ([]string, error)
    ValidateSymbol(symbol string) error
    ValidateInterval(interval string) error
}
```

### Adding New Exchanges

To add a new exchange:

1. Create a new package under `exchanges/`
2. Implement the `Exchange` interface
3. Add the exchange to the main application's switch statement
4. Update the CLI help text

Example for a new exchange:

```go
package newexchange

import "exchange-relayer/exchanges"

type NewExchange struct {
    // exchange-specific fields
}

func New() exchanges.Exchange {
    return &NewExchange{}
}

// Implement all Exchange interface methods...
```

### Resilient WebSocket Module

The `websocket` package provides a robust WebSocket wrapper with:

- **Automatic Reconnection**: Handles connection drops gracefully
- **Exponential Backoff**: Smart retry timing to avoid overwhelming servers
- **Health Monitoring**: Ping/pong keep-alive mechanism
- **Event Handlers**: Customizable callbacks for connection events
- **Thread Safety**: Concurrent access protection

Example usage:

```go
config := websocket.DefaultConfig("wss://api.example.com/ws")
config.ReconnectInterval = 2 * time.Second
config.MaxReconnectDelay = 60 * time.Second

ws := websocket.NewWebSocketClient(config)
ws.SetMessageHandler(func(data []byte) error {
    // Handle incoming messages
    return nil
})
ws.SetConnectHandler(func() {
    // Connection established
})
ws.Connect()
```

## Configuration

The application supports configuration via:

- Command line flags (highest priority)
- Environment variables
- Configuration files (future enhancement)

## Error Handling

The application includes comprehensive error handling and resilient connections:

- **Automatic Reconnection**: Exponential backoff with configurable retry limits
- **Connection Health Monitoring**: Ping/pong keep-alive mechanism
- **Timeout Handling**: Configurable read/write timeouts
- **Connection State Management**: Real-time connection status tracking
- **Graceful Degradation**: Continues operation during temporary network issues
- **Invalid Data Handling**: Skips malformed messages without crashing
- **Graceful Shutdown**: Proper cleanup on interrupt signals

### Resilient WebSocket Features

- **Exponential Backoff**: Reconnection delays increase progressively (1s, 2s, 4s, 8s, etc.)
- **Maximum Retry Delay**: Configurable upper limit for backoff delays
- **Infinite Retries**: Option to retry indefinitely or set maximum attempts
- **Connection Monitoring**: Automatic ping/pong to detect dead connections
- **Event Handlers**: Customizable handlers for connect/disconnect/error events
- **Thread-Safe**: Concurrent access protection with mutexes

## Docker

The project includes comprehensive Docker support with a multi-stage build for optimal image size and security.

### Docker Features

- **Multi-stage Build**: Optimized for size and security
- **Non-root User**: Runs with minimal privileges
- **Alpine Linux**: Minimal base image for security
- **Health Checks**: Built-in container health monitoring
- **Configurable**: Supports all command-line options

### Docker Usage

```bash
# Build the Docker image
make docker-build

# Run interactively (shows help by default)
make docker-run

# Run with specific configurations
make docker-run-binance
make docker-run-hyperliquid

# Run in background
make docker-run-detached

# View logs
make docker-logs

# Stop container
make docker-stop

# Clean up everything
make docker-clean
```

### Docker Image Details

- **Base Image**: Alpine Linux (latest)
- **Go Version**: 1.25
- **User**: Non-root (`appuser`)
- **Working Directory**: `/app`
- **Exposed Port**: 8080 (for future web interface)
- **Health Check**: Built-in application health monitoring

## Development

### Prerequisites

- Go 1.25 or later
- Docker (optional, for containerized development)
- Make (for using Make targets)

### Running Tests

```bash
make test
# or manually: go test ./...

# With coverage
make test-coverage
# or manually: go test -cover ./...
```

### Code Quality

```bash
# Format code
make fmt
# or manually: go fmt ./...

# Lint code
make lint
# or manually: golangci-lint run
```

### Building for Different Platforms

```bash
# Using Make targets
make build-linux    # Build for Linux
make build-windows  # Build for Windows
make build-macos    # Build for macOS

# Manual cross-compilation
GOOS=linux GOARCH=amd64 go build -o exchange-relayer-linux
GOOS=windows GOARCH=amd64 go build -o exchange-relayer.exe
GOOS=darwin GOARCH=amd64 go build -o exchange-relayer-macos
```

### Development Workflow

1. **Make changes** to the code
2. **Format code**: `make fmt`
3. **Run tests**: `make test`
4. **Lint code**: `make lint`
5. **Build locally**: `make build`
6. **Test locally**: `make run`
7. **Test with Docker**: `make docker-run`
