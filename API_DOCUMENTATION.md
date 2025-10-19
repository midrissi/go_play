# Exchange APIs Documentation

This document describes all the APIs used by the exchange-relayer application for fetching data from various cryptocurrency exchanges.

## Table of Contents

1. [Fetch Symbols API](#fetch-symbols-api)
   - [Binance](#binance)
   - [Hyperliquid](#hyperliquid)
2. [WebSocket Streams API](#websocket-streams-api)
   - [Binance](#binance-1)
   - [Hyperliquid](#hyperliquid-1)

---

## Fetch Symbols API

This API is used to retrieve all active trading symbols/coins from each exchange.

### Binance

**Endpoint:** `GET https://api.binance.com/api/v3/exchangeInfo`

**Official Documentation:** [Binance Exchange Info API](https://developers.binance.com/docs/binance-spot-api-docs/rest-api/general-endpoints#exchange-information)

**Description:** Retrieves all active trading symbols and their metadata from Binance.

**Parameters:** None

**Limitations:**
- No authentication required
- Rate limit: Based on IP address, not API keys ([Rate Limits](https://developers.binance.com/docs/binance-spot-api-docs/rest-api/limits))
- Each route has a `weight` which determines how many requests each endpoint counts for
- `/api/v3/exchangeInfo` has weight 10 (heavier endpoint)
- Response headers include `X-MBX-USED-WEIGHT-(intervalNum)(intervalLetter)` showing current used weight
- HTTP 429 error when rate limit exceeded (with `Retry-After` header)
- Repeated violations result in IP ban (HTTP 418) with scaling duration (2 minutes to 3 days)
- Response size can be large (contains all trading pairs)
- Only returns symbols with status "TRADING" and spot trading enabled

**HTTP Example:**
```bash
curl -X GET "https://api.binance.com/api/v3/exchangeInfo" \
  -H "Content-Type: application/json"
```

**Response Format:**
```json
{
  "timezone": "UTC",
  "serverTime": 1234567890000,
  "rateLimits": [...],
  "exchangeFilters": [...],
  "symbols": [
    {
      "symbol": "BTCUSDT",
      "status": "TRADING",
      "baseAsset": "BTC",
      "baseAssetPrecision": 8,
      "quoteAsset": "USDT",
      "quotePrecision": 8,
      "quoteAssetPrecision": 8,
      "orderTypes": ["LIMIT", "LIMIT_MAKER", "MARKET", "STOP_LOSS_LIMIT", "TAKE_PROFIT_LIMIT"],
      "icebergAllowed": true,
      "ocoAllowed": true,
      "isSpotTradingAllowed": true,
      "isMarginTradingAllowed": true,
      "filters": [...],
      "permissions": ["SPOT"]
    }
  ]
}
```

**Implementation Notes:**
- Filters symbols to only include those with `status: "TRADING"` and `isSpotTradingAllowed: true` ([Symbol Status](https://www.binance.com/en/academy/articles/how-to-retrieve-binance-spot-symbols-via-api))
- Caches results for 1 hour to reduce API calls
- Returns array of symbol strings (e.g., `["BTCUSDT", "ETHUSDT", ...]`)

### Hyperliquid

**Endpoint:** `POST https://api.hyperliquid.xyz/info`

**Official Documentation:** [Hyperliquid Info API](https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/api/info-endpoint)

**Description:** Retrieves all active trading assets from Hyperliquid.

**Parameters:**
- `type`: Request type (must be "meta")
- `dex`: Perp dex name (optional, defaults to empty string which represents the first perp dex)

**Limitations:**
- No authentication required
- Rate limit: 1200 requests per minute (REST requests share aggregated weight limit) ([Rate Limits](https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/api/rate-limits-and-user-limits))
- Weight: 20 for `meta` requests
- Returns all assets regardless of trading status
- POST request required (not GET)

**HTTP Example:**
```bash
curl -X POST "https://api.hyperliquid.xyz/info" \
  -H "Content-Type: application/json" \
  -d '{"type": "meta", "dex": ""}'
```

**Request Body:**
```json
{
  "type": "meta",
  "dex": ""
}
```

**Response Format:**
```json
{
  "universe": [
    {
      "name": "BTC",
      "onlyIsolated": false
    },
    {
      "name": "ETH",
      "onlyIsolated": false
    },
    {
      "name": "SOL",
      "onlyIsolated": true
    }
  ]
}
```

**Implementation Notes:**
- Returns asset names without quote currency (e.g., "BTC", "ETH") ([Asset Format](https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/api/info-endpoint))
- `dex` parameter specifies which perpetual DEX to query (empty string = first perp dex)
- Spot mids are only included with the first perp dex (when `dex` is empty string)
- Caches results for 1 hour to reduce API calls
- Includes both isolated and cross-margin assets
- Symbol conversion: removes "USDT", "USD", "BUSD" suffixes

---

## WebSocket Streams API

This API is used for real-time candlestick data streaming via WebSocket connections.

### Binance

**Endpoint:** `wss://stream.binance.com:9443` or `wss://stream.binance.com:443`

**Base Endpoints:**
- `wss://stream.binance.com:9443` - Main WebSocket endpoint
- `wss://stream.binance.com:443` - Alternative WebSocket endpoint  
- `wss://data-stream.binance.vision` - Market data only (no user data streams)

**Official Documentation:** [Binance WebSocket Streams](https://developers.binance.com/docs/binance-spot-api-docs/web-socket-streams)

**Description:** Real-time candlestick data streaming via WebSocket.

**Subscription Message Format:**
```json
{
  "method": "SUBSCRIBE",
  "params": [
    "btcusdt@kline_1m",
    "ethusdt@kline_5m"
  ],
  "id": 1
}
```

**Stream Access Methods:**
- Raw streams: `/ws/<streamName>` (e.g., `/ws/btcusdt@kline_1m`)
- Combined streams: `/stream?streams=<streamName1>/<streamName2>` (e.g., `/stream?streams=btcusdt@kline_1m/ethusdt@kline_5m`)
- Combined stream events are wrapped: `{"stream":"<streamName>","data":<rawPayload>}`

**Parameters:**
- `method`: Always "SUBSCRIBE"
- `params`: Array of stream names in format `{symbol}@kline_{interval}`
- `id`: Unique identifier for the request

**Stream Name Format:**
- Symbol: lowercase (e.g., `btcusdt`, `ethusdt`)
- Interval: `1m`, `3m`, `5m`, `15m`, `30m`, `1h`, `2h`, `4h`, `6h`, `8h`, `12h`, `1d`, `3d`, `1w`, `1M` ([Kline Intervals](https://binance-docs.github.io/apidocs/spot/en/#kline-candlestick-data))

**Limitations:**
- Maximum 1024 streams per connection ([WebSocket Limits](https://developers.binance.com/docs/binance-spot-api-docs/web-socket-streams))
- Rate limit: 5 incoming messages per second (PING, PONG, JSON controlled messages) ([WebSocket Limits](https://developers.binance.com/docs/binance-spot-api-docs/web-socket-streams))
- Maximum 300 connections per attempt every 5 minutes per IP
- Connection timeout: 24 hours (single connection valid for 24 hours)
- Ping/pong: Server sends ping every 20 seconds, client must respond with pong within 1 minute
- Automatic reconnection with exponential backoff
- HTTP 429 error when rate limit exceeded (with `Retry-After` header)
- Repeated violations result in IP ban (HTTP 418) with scaling duration (2 minutes to 3 days)

**Message Format:**
```json
{
  "e": "kline",
  "E": 123456789,
  "s": "BTCUSDT",
  "k": {
    "t": 123400000,
    "T": 123460000,
    "s": "BTCUSDT",
    "i": "1m",
    "f": 100,
    "L": 200,
    "o": "0.0010",
    "c": "0.0020",
    "h": "0.0025",
    "l": "0.0015",
    "v": "1000",
    "n": 100,
    "x": false,
    "q": "0.0015",
    "V": "500",
    "Q": "0.00075",
    "B": "0"
  }
}
```

**Implementation Notes:**
- Can subscribe to up to 1024 streams per connection ([WebSocket Limits](https://developers.binance.com/docs/binance-spot-api-docs/web-socket-streams))
- Uses 200ms delay between batches to avoid rate limiting
- Handles reconnection automatically (24-hour connection limit)
- Parses candlestick data from the `k` object ([Kline Data](https://binance-docs.github.io/apidocs/spot/en/#kline-candlestick-data))
- Ping/pong handling: Responds to server pings within 1 minute to maintain connection
- Supports both raw streams and combined streams for efficient data handling

### Hyperliquid

**Endpoint:** `wss://api.hyperliquid.xyz/ws`

**Network Endpoints:**
- Mainnet: `wss://api.hyperliquid.xyz/ws`
- Testnet: `wss://api.hyperliquid-testnet.xyz/ws`

**Official Documentation:** [Hyperliquid WebSocket API](https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/api/websocket)

**Description:** Real-time candlestick data streaming via WebSocket.

**Subscription Message Format:**
```json
{
  "method": "subscribe",
  "subscription": {
    "type": "candle",
    "coin": "BTC",
    "interval": "1m"
  }
}
```

**Unsubscribe Message Format:**
```json
{
  "method": "unsubscribe",
  "subscription": {
    "type": "candle",
    "coin": "BTC",
    "interval": "1m"
  }
}
```

**Parameters:**
- `method`: Always "subscribe"
- `subscription.type`: Always "candle"
- `subscription.coin`: Asset name (e.g., "BTC", "ETH")
- `subscription.interval`: Time interval

**Supported Subscription Types:**
- `allMids`: All mid prices (optional `dex` parameter)
- `candle`: Candlestick data for specific coin and interval
- `l2Book`: Level 2 order book data (optional `nSigFigs`, `mantissa` parameters)
- `trades`: Trade data for specific coin
- `notification`: User notifications (requires `user` address)
- `webData2`: Web data for specific user (requires `user` address)
- `orderUpdates`: Order updates for specific user (requires `user` address)
- `userEvents`: User events (requires `user` address)
- `userFills`: User fills (requires `user` address, optional `aggregateByTime`)
- `userFundings`: User funding data (requires `user` address)
- `userNonFundingLedgerUpdates`: Non-funding ledger updates (requires `user` address)
- `activeAssetCtx`: Active asset context for specific coin
- `activeSpotAssetCtx`: Active spot asset context for specific coin
- `activeAssetData`: Active asset data for specific user and coin
- `userTwapSliceFills`: TWAP slice fills for specific user
- `userTwapHistory`: TWAP history for specific user

**Subscription Examples:**
```json
// Subscribe to all mid prices
{ "method": "subscribe", "subscription": { "type": "allMids" } }

// Subscribe to candle data
{ "method": "subscribe", "subscription": { "type": "candle", "coin": "BTC", "interval": "1m" } }

// Subscribe to order book
{ "method": "subscribe", "subscription": { "type": "l2Book", "coin": "BTC" } }

// Subscribe to user notifications
{ "method": "subscribe", "subscription": { "type": "notification", "user": "0x..." } }
```

**Limitations:**
- Maximum 100 websocket connections per IP ([Rate Limits](https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/api/rate-limits-and-user-limits))
- Maximum 1000 websocket subscriptions per IP
- Maximum 10 unique users across user-specific websocket subscriptions
- Maximum 2000 messages sent to Hyperliquid per minute across all websocket connections
- Maximum 100 simultaneous inflight post messages across all websocket connections
- Individual subscription messages required (no batching)
- Connection timeout: 30 seconds
- Automatic reconnection with exponential backoff

**Message Format:**
```json
{
  "channel": "candle",
  "data": {
    "s": "BTC",
    "t": 123400000,
    "T": 123460000,
    "o": "0.0010",
    "h": "0.0025",
    "l": "0.0015",
    "c": "0.0020",
    "v": "1000"
  }
}
```

**Implementation Notes:**
- Supports 12+ different subscription types for comprehensive data streaming ([Subscriptions](https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/api/websocket/subscriptions))
- Each symbol-interval combination requires a separate subscription message
- Symbol format: asset name only (e.g., "BTC", not "BTCUSDT")
- Handles reconnection automatically
- Parses candlestick data from the `data` object ([Candle Data](https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/api/websocket/subscriptions))
- Subscription acknowledgments provide snapshots of previous data (tagged with `isSnapshot: true`)
- User-specific subscriptions require valid user addresses
- Supports both perpetual and spot asset subscriptions

---

## Common Implementation Details

### Error Handling
- Both exchanges implement automatic reconnection with exponential backoff
- Connection errors are logged but don't stop the application
- Invalid messages are silently ignored
- API failures fall back to cached data when available
- Rate limit monitoring: Response headers include weight usage information
- HTTP 429 errors include `Retry-After` header indicating wait time
- IP bans (HTTP 418) scale in duration from 2 minutes to 3 days for repeat offenders

### Caching Strategy
- Symbol lists are cached for 1 hour
- Reduces API calls and improves performance
- Cache is invalidated after 1 hour or on API failure

### Connection Management
- Multiple WebSocket connections used when subscription limits are exceeded
- Binance: Up to 1024 streams per connection, 300 connections per 5 minutes per IP
- Hyperliquid: Up to 1000 subscriptions per IP (100 connections max)
- Connections are distributed evenly across available connections
- 24-hour connection lifetime for Binance (automatic reconnection required)

### Data Processing
- All timestamps are converted to Go `time.Time` objects
- Prices and volumes are parsed as `float64`
- Symbol names are normalized to exchange-specific formats
- Invalid or missing data fields are handled gracefully
