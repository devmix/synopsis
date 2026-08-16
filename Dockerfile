# Build stage
FROM golang:1.25.5-alpine AS builder

RUN apk add --no-cache git gcc musl-dev

WORKDIR /build

# Copy go module files first for better caching.
COPY go.mod go.sum ./
RUN go mod download

# Copy source code.
COPY . .

# Build the binary with CGO enabled (required for sqlite3).
ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN CGO_ENABLED=1 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -o /build/synopsis ./cmd/app/

# Runtime stage
FROM alpine:3.21

RUN apk add --no-cache ca-certificates sqlite-libs

WORKDIR /app

# Copy binary from builder.
COPY --from=builder /build/synopsis /app/synopsis

# Create directories for data and workspace.
RUN mkdir -p /app/data /app/workspace /app/models

# Default configuration volume mount points:
#   /app/configs    — configuration files
#   /app/data       — SQLite database
#   /app/workspace  — source documents
#   /app/models     — ONNX model files

EXPOSE 8080  # MCP HTTP (SSE) endpoint

ENTRYPOINT ["/app/synopsis"]
CMD ["--config", "/app/configs/config.default.yaml", "serve"]
