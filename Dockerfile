# Multi-stage build for multiarch combobreak
FROM --platform=$BUILDPLATFORM golang:alpine AS builder

WORKDIR /app

# Download dependencies (if any in future)
COPY go.mod ./
RUN go mod download

# Copy application source
COPY . .

# Build statically linked binary using Go cross-compilation
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} go build -ldflags="-s -w" -o /combobreak .

# Final minimal runtime image
FROM alpine:latest

# Install CA certificates for secure HTTPS calls to OmniRoute and tzdata for timezones
RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app

# Copy binary from builder
COPY --from=builder /combobreak /usr/local/bin/combobreak

# Run as non-root user for security
RUN addgroup -S combobreak && adduser -S combobreak -G combobreak
USER combobreak

ENTRYPOINT ["combobreak"]
