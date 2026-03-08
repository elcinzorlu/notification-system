# Build stage
FROM golang:1.23-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build API server
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /bin/api ./cmd/api

# Build Worker
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /bin/worker ./cmd/worker

# --- API runtime ---
FROM alpine:3.19 AS api

RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app

COPY --from=builder /bin/api /app/api
COPY --from=builder /app/.env.example /app/.env.example
COPY --from=builder /app/migrations /app/migrations

EXPOSE 8080

CMD ["/app/api"]

# --- Worker runtime ---
FROM alpine:3.19 AS worker

RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app

COPY --from=builder /bin/worker /app/worker
COPY --from=builder /app/.env.example /app/.env.example

CMD ["/app/worker"]
