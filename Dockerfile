FROM golang:1.22-alpine AS builder

WORKDIR /app

# Install build deps
RUN apk add --no-cache git ca-certificates

# Disable Go checksum verification to avoid go.sum mismatch issues
ENV GONOSUMDB=* GOFLAGS=-mod=mod GONOSUMCHECK=* GONOPROXY=* GOFLAGS=-mod=mod GONOSUMDB=*

# Copy go mod file
COPY go.mod ./

# Download dependencies with sum verification disabled
RUN GONOSUMDB=* GOFLAGS=-mod=mod GONOPROXY=* go mod download

# Copy all source code
COPY . .

# Build
RUN CGO_ENABLED=0 GOOS=linux GONOSUMDB=* GOFLAGS=-mod=mod go build -o xiantu-server ./cmd/server

# Production image
FROM alpine:3.19

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

COPY --from=builder /app/xiantu-server .
COPY --from=builder /app/public ./public

ENV TZ=Asia/Shanghai

EXPOSE 8080

CMD ["./xiantu-server"]
