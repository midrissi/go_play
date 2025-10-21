.PHONY: build run clean test help docker-build docker-run docker-stop docker-clean docker-logs proto build-server run-server

# Generate protobuf files
proto:
	export PATH=$$PATH:$$(go env GOPATH)/bin && \
	protoc --go_out=. --go_opt=paths=source_relative \
		--go-grpc_out=. --go-grpc_opt=paths=source_relative \
		proto/service.proto

# Build the application
build:
	go build -o exchange-relayer

# Build the gRPC server
build-server: proto
	go build -o grpc-server ./cmd/grpc-server

# Run with default settings (Binance BTCUSDT 1m)
run: build
	./exchange-relayer

# Run with Binance example
run-binance: build
	./exchange-relayer --exchange binance --symbols BTCUSDT,ETHUSDT --interval 5m

# Run with Hyperliquid example
run-hyperliquid: build
	./exchange-relayer --exchange hyperliquid --symbols BTC,ETH --interval 1m

# Run gRPC server with specific symbols
run-server-symbols: build-server
	./grpc-server --exchange binance --symbols BTCUSDT,ETHUSDT,ADAUSDT

# Run gRPC server with all symbols
run-server-all: build-server
	./grpc-server --exchange binance --all

# Clean build artifacts
clean:
	rm -f exchange-relayer grpc-server
	rm -f proto/*.pb.go

# Run tests
test:
	go test ./...

# Run tests with coverage
test-coverage:
	go test -cover ./...

# Format code
fmt:
	go fmt ./...

# Lint code
lint:
	golangci-lint run

# Install dependencies
deps:
	go mod tidy
	go mod download

# Install protobuf dependencies
deps-proto:
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

# Cross-compile for different platforms
build-linux:
	GOOS=linux GOARCH=amd64 go build -o exchange-relayer-linux

build-windows:
	GOOS=windows GOARCH=amd64 go build -o exchange-relayer.exe

build-macos:
	GOOS=darwin GOARCH=amd64 go build -o exchange-relayer-macos

# Docker targets
docker-build:
	docker build -t exchange-relayer .

docker-run: docker-build
	docker run --rm -it exchange-relayer

docker-run-binance: docker-build
	docker run --name exchange-relayer --rm -it exchange-relayer --exchange binance --symbols BTCUSDT,ETHUSDT --interval 5m

docker-run-hyperliquid: docker-build
	docker run --name exchange-relayer --rm -it exchange-relayer --exchange hyperliquid --symbols BTC,ETH --interval 1m

docker-run-detached: docker-build
	docker run -d --name exchange-relayer exchange-relayer

docker-stop:
	docker stop exchange-relayer || true

docker-logs:
	docker logs -f exchange-relayer

docker-clean:
	docker stop exchange-relayer || true
	docker rm exchange-relayer || true
	docker rmi exchange-relayer || true

# Show help
help:
	@echo "Available targets:"
	@echo "  build          					- Build the application"
	@echo "  build-server   					- Build the gRPC server"
	@echo "  proto          					- Generate protobuf files"
	@echo "  run            					- Run with default settings"
	@echo "  run-binance    					- Run with Binance example"
	@echo "  run-hyperliquid 					- Run with Hyperliquid example"
	@echo "  run-server-symbols 				- Run gRPC server with specific symbols"
	@echo "  run-server-all 					- Run gRPC server with all symbols"
	@echo "  clean          					- Clean build artifacts"
	@echo "  test           					- Run tests"
	@echo "  test-coverage  					- Run tests with coverage"
	@echo "  fmt            					- Format code"
	@echo "  lint           					- Lint code"
	@echo "  deps           					- Install dependencies"
	@echo "  deps-proto     					- Install protobuf dependencies"
	@echo "  build-linux    					- Cross-compile for Linux"
	@echo "  build-windows  					- Cross-compile for Windows"
	@echo "  build-macos    					- Cross-compile for macOS"
	@echo "  docker-build   					- Build Docker image"
	@echo "  docker-run     					- Run Docker container interactively"
	@echo "  docker-run-binance 			- Run Docker container with Binance example"
	@echo "  docker-run-hyperliquid 	- Run Docker container with Hyperliquid example"
	@echo "  docker-run-detached 			- Run Docker container in background"
	@echo "  docker-stop    					- Stop running Docker container"
	@echo "  docker-logs    					- Show Docker container logs"
	@echo "  docker-clean   					- Clean up Docker containers and images"
	@echo "  help           					- Show this help"
