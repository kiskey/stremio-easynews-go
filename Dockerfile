# ===========================================================================
# Stage 1: Dynamic Compilation Builder
# ===========================================================================
FROM golang:1.23-alpine AS builder

# Install certs for secure connections to metadata APIs
RUN apk --no-cache add ca-certificates

WORKDIR /app

# Cache Go modules first to optimize build layers
COPY go.mod ./
RUN go mod download

# Copy source tree and compile
COPY . .

# Build flags optimized for minimal footprint and maximum security:
# - CGO_ENABLED=0: Statically links libc to ensure native compatibility on Alpine
# - -ldflags="-s -w": Strips debugging schemas to decrease binary size
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w -X github.com/kiskey/stremio-easynews-go/internal/shared.Version=prod" \
    -o stremio-easynews cmd/addon/main.go

# ===========================================================================
# Stage 2: Clean, Secure Runtime Environment
# ===========================================================================
FROM alpine:latest

# Install runtime dependencies (e.g., timezone databases and root CA certs)
RUN apk --no-cache add ca-certificates tzdata

# Create a secure, unprivileged service account
RUN addgroup -S appgroup && adduser -S appuser -G appgroup

WORKDIR /app

# Import the statically compiled binary from Stage 1
COPY --from=builder /app/stremio-easynews .

# Enforce secure user execution
USER appuser

# Expose standard gateway port
EXPOSE 1337

# Define execution command
CMD ["./stremio-easynews"]
