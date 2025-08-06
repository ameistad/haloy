FROM golang:1.24-alpine AS builder

# Install build dependencies
RUN apk add --no-cache \
    git \
    gcc \
    musl-dev \
    ca-certificates

WORKDIR /app

# Copy go.mod and go.sum first for better caching
COPY go.mod go.sum ./
RUN go mod download && go mod verify

# Copy source code
COPY . .

# Build with optimizations and version info
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s -X 'github.com/ameistad/haloy/internal/version.Version=${VERSION}'" \
    -a \
    -o /haloy-manager ./cmd/manager

# Docker CLI stage
FROM alpine:latest AS docker-cli
RUN apk add --no-cache docker-cli && \
    cp /usr/bin/docker /tmp/docker

FROM alpine:latest

# Install runtime dependencies
RUN apk add --no-cache ca-certificates

# Create non-root user
RUN adduser -D -s /bin/sh appuser

# Copy Docker CLI from intermediate stage
COPY --from=docker-cli /tmp/docker /usr/local/bin/docker

# Copy the binary
COPY --from=builder /haloy-manager /usr/local/bin/haloy-manager

# Change to non-root user
USER appuser

ENTRYPOINT ["/usr/local/bin/haloy-manager"]
