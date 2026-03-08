# Notification System

An event-driven notification processing system built with Go. It processes and delivers messages through SMS, Email, and Push channels with high throughput, reliable delivery via RabbitMQ, and real-time status tracking.

Built to send millions of notifications daily with burst traffic handling, intelligent retry, and full delivery visibility.


## Getting Started

The fastest way to run everything:

```bash
docker compose up -d
```

This starts PostgreSQL, Redis, RabbitMQ, the API server, and the Worker.

For local development without Docker, see the [Development guide](docs/development.md).
For production deployment details, see the [Deployment guide](docs/deployment.md).


## Usage

```bash
# Send a notification
curl -X POST http://localhost:8081/api/v1/notifications \
  -H "Content-Type: application/json" \
  -d '{"recipient": "+905551234567", "channel": "sms", "content": "Your order shipped!", "priority": "high"}'

# Check health
curl http://localhost:8081/api/v1/health
```

Full endpoint documentation is available interactively via **Swagger UI** at `http://localhost:8081/swagger/index.html` (Docker).

For a detailed static reference, see the [API Reference](docs/api-reference.md).


## Architecture

```
API (Fiber) → RabbitMQ (priority queue) → Worker Pool → Rate Limiter (Redis) → Provider
```

Priority queues, exponential backoff retry with error classification, per-channel rate limiting, idempotency, scheduled delivery, real-time WebSocket updates, and structured logging with correlation IDs.

See [Architecture](docs/architecture.md) for the complete system design.


## Testing

```bash
make ci          # Full pipeline: vet + staticcheck + test + build
make test        # Tests with race detector
make lint        # go vet + staticcheck
```


## Documentation

| Document | Description |
|----------|-------------|
| [Architecture](docs/architecture.md) | System design, layers, queue topology, retry logic |
| [API Reference](docs/api-reference.md) | Endpoints, request/response formats, WebSocket |
| [Configuration](docs/configuration.md) | Environment variables and defaults |
| [Deployment](docs/deployment.md) | Docker Compose, image builds, production tips |
| [Development](docs/development.md) | Local setup, testing, building |


## Tech Stack

| Component | Technology |
|-----------|-----------|
| Language | Go 1.23 |
| HTTP | Fiber v2 |
| Database | PostgreSQL 16 |
| Queue | RabbitMQ 3.13 |
| Cache / Rate Limiting | Redis 7 |
| ORM | GORM |
| Logging | Uber Zap |


## License

MIT
