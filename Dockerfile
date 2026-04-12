# Multi-stage Dockerfile for KubeOrch Core

# Stage 1: Builder
FROM golang:1.25-alpine AS builder

# Install git and ca-certificates for fetching dependencies
RUN apk add --no-cache git ca-certificates

# Set working directory
WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s" \
    -o kubeorch-core \
    .

# Stage 2: Final image
FROM alpine:3.19

# Install ca-certificates for HTTPS and wget for healthcheck
RUN apk --no-cache add ca-certificates wget

# Create non-root user
RUN adduser -D -g '' appuser

# Set working directory
WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/kubeorch-core .

# Copy config file if it exists (optional, can be mounted or use env vars)
COPY --from=builder /app/config.yaml* ./

# Change ownership to non-root user
RUN chown -R appuser:appuser /app

# Switch to non-root user
USER appuser

# Expose port
EXPOSE 3000

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:3000/v1/ || exit 1

# Run the application
ENTRYPOINT ["./kubeorch-core"]