# Multi-stage build for Go application
FROM golang:1.25-alpine AS builder

# Set working directory
WORKDIR /app

# Install git and ca-certificates (needed for go mod download)
RUN apk add --no-cache git ca-certificates

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o exchange-relayer .

# Final stage - minimal image
FROM alpine:latest

# Install ca-certificates for HTTPS requests
RUN apk --no-cache add ca-certificates

# Create non-root user for security
RUN addgroup -g 1001 -S appgroup && \
    adduser -u 1001 -S appuser -G appgroup

# Set working directory
WORKDIR /app

# Copy binary from builder stage
COPY --from=builder /app/exchange-relayer .

# Change ownership to non-root user and make executable
RUN chown appuser:appgroup exchange-relayer && chmod +x exchange-relayer

# Switch to non-root user
USER appuser

# Expose port (if needed for future web interface)
EXPOSE 8080

# Set default command
ENTRYPOINT ["./exchange-relayer"]
CMD ["--help"]

# Health check (optional - can be customized)
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD ./exchange-relayer --help > /dev/null || exit 1
