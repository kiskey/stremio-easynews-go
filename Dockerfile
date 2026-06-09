# ===========================================================================
# Stage 1: Dynamic Compilation Builder
# ===========================================================================
FROM golang:1.23-alpine AS builder

# Install certs for secure connections to metadata APIs
RUN apk --no-cache add ca-certificates

WORKDIR /app

# Copy all source code
COPY . .

# Resolve all dependencies dynamically and generate go.sum checksum entries
RUN go mod tidy

# Build static binary optimized for size and security
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w -X github.com/kiskey/stremio-easynews-go/internal/shared.Version=prod" \
    -o stremio-easynews cmd/addon/main.go

# ===========================================================================
# Stage 2: Clean, Secure Runtime Environment
# ===========================================================================
FROM alpine:latest

# Install runtime dependencies (timezone databases and root CA certs)
RUN apk --no-cache add ca-certificates tzdata

# Create secure unprivileged system account
RUN addgroup -S appgroup && adduser -S appuser -G appgroup

WORKDIR /app

# Import the statically compiled binary from Stage 1
COPY --from=builder /app/stremio-easynews .

# Enforce secure user execution
USER appuser

# Expose default Stremio gateway port
EXPOSE 1337

# Define execution command
CMD ["./stremio-easynews"]
