# Build stage
FROM golang:1.24-alpine AS builder
WORKDIR /

# Cache deps
COPY go.mod go.sum ./
RUN go mod download

# Copy sources
COPY . .

# Build (без создания лишних директорий/файлов)
# По умолчанию собираем из cmd/server/main.
ARG BUILD_TARGET=./cmd/server
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o app "$BUILD_TARGET"

# Runtime stage (root по умолчанию)
FROM alpine:3.20
COPY --from=builder / /usr/local/bin/app

EXPOSE 8080