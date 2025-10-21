# Exchange Relayer gRPC Server

A Go gRPC server that streams real-time quotes from cryptocurrency exchanges to clients.

## Features

- **Real-time streaming**: Streams live quotes from Binance and Hyperliquid exchanges
- **Symbol filtering**: Track specific symbols or all supported symbols
- **Concurrent clients**: Handle multiple clients safely with proper synchronization
- **gRPC streaming**: Server-streaming RPC for efficient real-time data delivery
- **Graceful shutdown**: Proper cleanup on server termination

## Prerequisites

- Go 1.25.2 or later
- Protocol Buffers compiler (`protoc`)
- Internet connection for exchange data

### Installing Protocol Buffers Compiler (protoc)

#### macOS (using Homebrew)
```bash
brew install protobuf
```

#### Ubuntu/Debian
```bash
sudo apt-get update
sudo apt-get install protobuf-compiler
```

#### CentOS/RHEL/Fedora
```bash
# For CentOS/RHEL
sudo yum install protobuf-compiler

# For Fedora
sudo dnf install protobuf-compiler
```

#### Windows
1. Download the latest release from [GitHub releases](https://github.com/protocolbuffers/protobuf/releases)
2. Extract the zip file
3. Add the `bin` directory to your PATH environment variable

#### Manual Installation (Linux/macOS)
```bash
# Download and install protoc
curl -LO https://github.com/protocolbuffers/protobuf/releases/download/v25.1/protoc-25.1-linux-x86_64.zip
unzip protoc-25.1-linux-x86_64.zip -d protoc
sudo mv protoc/bin/* /usr/local/bin/
sudo mv protoc/include/* /usr/local/include/
```

#### Verify Installation
```bash
protoc --version
# Should output: libprotoc 25.1 (or similar version)
```

## Installation

1. **Install protobuf dependencies**:
   ```bash
   make deps-proto
   ```

2. **Install Go dependencies**:
   ```bash
   make deps
   ```

3. **Generate protobuf files**:
   ```bash
   make proto
   ```

4. **Build the gRPC server**:
   ```bash
   make build-server
   ```

## Usage

### Command Line Options

- `--port`: gRPC server port (default: 50051)
- `--exchange`: Exchange to use (`binance` or `hyperliquid`, default: `binance`)
- `--symbols`: Comma-separated list of symbols to track (e.g., `BTCUSDT,ETHUSDT`)
- `--all`: Track all supported symbols from the exchange

### Examples

**Run server with specific symbols**:
```bash
make run-server-symbols
# or manually:
./grpc-server --exchange binance --symbols BTCUSDT,ETHUSDT,ADAUSDT
```

**Run server with all symbols**:
```bash
make run-server-all
# or manually:
./grpc-server --exchange binance --all
```

**Run server on custom port**:
```bash
./grpc-server --port 8080 --exchange binance --symbols BTCUSDT,ETHUSDT
```

## gRPC API

### Service: MarketData

#### StreamQuotes

**Request**:
```protobuf
message SubscribeRequest {
  repeated string symbols = 1; // List of symbols to subscribe to
  string interval = 2;         // Candle interval (e.g., "1m", "5m", "1h", "1d")
}
```

**Response** (streaming):
```protobuf
message Quote {
  string symbol = 1;           // Trading symbol (e.g., "BTCUSDT")
  double price = 2;            // Current price
  double volume = 3;           // Volume
  int64 timestamp = 4;         // Unix timestamp in milliseconds
  string exchange = 5;         // Exchange name (e.g., "binance")
}
```

### Error Handling

- `INVALID_ARGUMENT`: No symbols specified, no interval specified, invalid interval, or none of the requested symbols are tracked
- `UNAVAILABLE`: Server is shutting down or exchange connection lost
- `INTERNAL`: Internal server error

## Testing with Postman

1. **Import the gRPC service**:
   - Open Postman
   - Create a new gRPC request
   - Set server URL: `localhost:50051`
   - Import the proto file: `proto/service.proto`

2. **Test StreamQuotes**:
   - Method: `MarketData/StreamQuotes`
   - Request body:
     ```json
     {
       "symbols": ["BTCUSDT", "ETHUSDT"],
       "interval": "1m"
     }
     ```
   - Click "Send" to start streaming

3. **Expected response**:
   ```json
   {
     "symbol": "BTCUSDT",
     "price": 43250.50,
     "volume": 1234.56,
     "timestamp": 1703123456789,
     "exchange": "binance"
   }
   ```

### Supported Intervals

The server supports the following intervals (depending on the exchange):

**Binance**:
- `1m`, `3m`, `5m`, `15m`, `30m`
- `1h`, `2h`, `4h`, `6h`, `8h`, `12h`
- `1d`, `3d`, `1w`, `1M`

**Hyperliquid**:
- Check exchange documentation for supported intervals

## Architecture

### Components

1. **MarketDataServer**: Main gRPC server implementation
2. **QuoteStream**: Represents a client's subscription
3. **Exchange Integration**: Uses existing exchange interfaces
4. **Concurrent Handling**: Safe multi-client streaming with mutexes

### Data Flow

1. Server starts and tracks configured symbols
2. Client connects and specifies symbols + interval
3. Server starts exchange WebSocket connection for the interval (if not already active)
4. Exchange streams candle data to server for the interval
5. Server converts candles to quotes
6. Server broadcasts quotes to clients subscribed to the same symbols and interval
7. Clients receive real-time quote updates

### Thread Safety

- Client map protected by `sync.RWMutex`
- Individual client streams protected by `sync.RWMutex`
- Quote channels per interval protected by `sync.RWMutex`
- Quote channels buffered to prevent blocking
- Graceful client cleanup on disconnect
- Multiple exchange connections managed safely

## Development

### Project Structure

```
├── proto/
│   └── service.proto          # gRPC service definition
├── exchanges/
│   ├── exchange.go            # Exchange interface
│   ├── binance/
│   │   └── binance.go         # Binance implementation
│   └── hyperliquid/
│       └── hyperliquid.go     # Hyperliquid implementation
├── server.go                  # gRPC server implementation
├── main.go                    # Original CLI application
├── Makefile                   # Build automation
└── go.mod                     # Go dependencies
```

### Adding New Exchanges

1. Implement the `exchanges.Exchange` interface
2. Add exchange case to server initialization
3. Update CLI flags if needed

### Building from Source

```bash
# Clean previous builds
make clean

# Install dependencies
make deps-proto
make deps

# Generate protobuf files
make proto

# Build server
make build-server

# Run server
make run-server-symbols
```

## Troubleshooting

### Common Issues

1. **"protoc not found"**: 
   - Install Protocol Buffers compiler using the instructions above
   - Ensure `protoc` is in your PATH: `which protoc`
   - Verify installation: `protoc --version`

2. **"protoc-gen-go: program not found"**:
   - Install Go protobuf plugins: `make deps-proto`
   - Ensure Go bin directory is in PATH: `export PATH=$PATH:$(go env GOPATH)/bin`

3. **"symbol not found"**: Check if symbol is supported by the exchange

4. **"connection refused"**: Ensure server is running and port is correct

5. **"no symbols tracked"**: Use `--all` flag or specify valid symbols

### Logs

The server provides detailed logging:
- Connection status
- Symbol subscriptions
- Client connections/disconnections
- Error messages

### Performance

- Server handles up to 1000 concurrent quotes per interval in buffer
- Multiple clients can subscribe to same symbols and intervals efficiently
- Graceful degradation when quote channels are full
- Dynamic exchange connection management per interval
- Efficient broadcasting to clients with matching symbol+interval subscriptions
