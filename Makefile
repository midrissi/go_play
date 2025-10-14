.PHONY: build run clean test help docker-build docker-run docker-stop docker-clean docker-logs

# Build the application
build:
	go build -o exchange-relayer

# Run with default settings (Binance BTCUSDT 1m)
run: build
	./exchange-relayer

# Run with Binance example
run-binance: build
	./exchange-relayer --exchange binance --symbols BTCUSDT,ETHUSDT --interval 5m

# Run with Hyperliquid example
run-hyperliquid: build
	./exchange-relayer --exchange hyperliquid --symbols BTC,ETH --interval 1m

# Clean build artifacts
clean:
	rm -f exchange-relayer

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
	@echo "  run            					- Run with default settings"
	@echo "  run-binance    					- Run with Binance example"
	@echo "  run-hyperliquid 					- Run with Hyperliquid example"
	@echo "  clean          					- Clean build artifacts"
	@echo "  test           					- Run tests"
	@echo "  test-coverage  					- Run tests with coverage"
	@echo "  fmt            					- Format code"
	@echo "  lint           					- Lint code"
	@echo "  deps           					- Install dependencies"
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
