# Notification System

This file provides guidance to AI coding agents working with this repository.

## What is Notification System?

An event-driven notification processing system built with Go 1.23. It processes and delivers messages through SMS, Email, and Push channels with high throughput, reliable delivery via RabbitMQ, and real-time status tracking. Designed for Insider One to send millions of notifications daily with burst traffic handling, intelligent retry, and full delivery visibility.

## Development Commands

### Setup
```bash
cp .env.example .env           # Create local environment config
docker compose up -d postgres redis rabbitmq  # Start infrastructure
go mod download                # Install Go dependencies
```

### Running Locally
```bash
go run cmd/api/main.go         # Start API server (port 8080)
go run cmd/worker/main.go      # Start worker (separate terminal)
```

API URL: `http://localhost:8081` (Docker) or `http://localhost:8080` (local)

### Testing & CI
```bash
make ci                        # Full CI pipeline: vet + staticcheck + test + build
make test                      # Run tests with race detector
make lint                      # Run go vet + staticcheck
make build                     # Build API and Worker binaries to bin/
make test-cover                # Tests with coverage report (coverage.html)
```

CI pipeline (`make ci`) runs:
1. `go vet ./...` (static analysis)
2. `staticcheck ./...` (advanced linting)
3. `go test ./... -v -race` (unit tests with race detector)
4. `go build` both `cmd/api` and `cmd/worker`

### Docker
```bash
docker compose up -d           # Start all services (one-command setup)
docker compose down            # Stop everything
make docker-rebuild            # Rebuild and restart API + Worker
```

Docker Compose starts: PostgreSQL (5433), Redis (6379), RabbitMQ (5672/15672), API (8081), Worker.

### Database
Migrations are in `migrations/` and auto-applied via GORM AutoMigrate on startup. SQL files in `migrations/` are also loaded by PostgreSQL's `docker-entrypoint-initdb.d`.

### Swagger
```bash
make swagger                   # Generate Swagger docs (requires swag)
```

## Architecture Overview

### System Flow

```
API Request → Fiber Handler → Service (validate + idempotency) → PostgreSQL
                                    ↓
                              RabbitMQ (priority queue)
                                    ↓
                           Worker Pool (N goroutines)
                                    ↓
                         Rate Limiter (Redis, 100/s/channel)
                                    ↓
                          Provider → webhook.site POST
                                    ↓
                     Status Update → DB + WebSocket broadcast
```

### Clean Architecture Layers

The codebase follows **layered/clean architecture**. Dependencies flow inward:

- **Handler** → **Service** → **Repository** (database)
- **Handler** → **Service** → **Queue** (RabbitMQ)
- **Handler** → **Service** → **Provider** (external API)
- **Service** orchestrates all business logic; handlers only parse HTTP

### Project Structure

```
cmd/api/main.go              # API server entrypoint (bootstrap all deps)
cmd/worker/main.go           # Worker entrypoint (consumer pool + scheduler)
internal/config/             # Env-based configuration via godotenv
internal/notification/
  ├── model/                 # Domain models, DTOs, enums (no business logic)
  ├── handler/               # Fiber HTTP handlers (Swagger annotated)
  ├── service/               # Business logic orchestration
  ├── repository/            # GORM-backed PostgreSQL persistence
  └── queue/                 # RabbitMQ queue with DLQ topology
internal/provider/           # Provider interface + SMS/Email/Push implementations
internal/rate_limiter/       # Redis sliding window rate limiter
internal/retry/              # Exponential backoff + error classification
internal/metrics/            # In-memory atomic counters + latency percentiles
internal/websocket/          # WebSocket hub for real-time status updates
pkg/database/                # Database connection helper
migrations/                  # SQL migration files
docker/Dockerfile            # Multi-stage build (api + worker targets)
```

### Core Domain Models

**Notification** — Main work item
- UUID primary key, optional `batch_id` for grouping
- Channels: `sms`, `email`, `push`
- Priorities: `high` (10), `normal` (5), `low` (1) — maps to RabbitMQ priority
- Status lifecycle: `pending` → `queued` → `processing` → `sent` / `failed` / `cancelled`
- `idempotency_key` with partial unique index (`WHERE idempotency_key IS NOT NULL`)
- `correlation_id` for distributed tracing

**Template** — Message templates with `{{.Variable}}` Go template substitution

### RabbitMQ Queue Topology

```
notifications.main       (priority queue, x-max-priority: 10)
  ├── on success → status=sent
  ├── on retryable error → notifications.retry (per-message TTL for backoff)
  └── on max retries → notifications.dlq (dead-letter queue, terminal)

notifications.retry      (TTL expires → re-routes to notifications.main)
notifications.dlq        (terminal storage for permanently failed messages)
```

Dead-letter routing: `x-dead-letter-exchange` + `x-dead-letter-routing-key` on queue declarations.

### Rate Limiting

Redis sliding window counter per channel:
- Key format: `rate:{channel}:{unix_timestamp}`
- TTL: 2 seconds, limit: 100 msg/sec per channel
- `Wait()` blocks until rate allows (with context cancellation)

### Retry & Error Classification

Exponential backoff: `1s, 2s, 4s, 8s, 16s` (5 max attempts, with jitter).

| Error Type | Action |
|-----------|--------|
| Network error / timeout | Retry |
| HTTP 5xx | Retry |
| HTTP 4xx | Fail immediately |
| Validation error | Fail immediately |

### Correlation ID Propagation

Full request tracing flow:
```
HTTP Header (X-Correlation-ID) → Fiber middleware → Service
  → RabbitMQ message header → Worker log context → Provider HTTP header
```

Auto-generated UUID if not provided by caller.

### Idempotency

- `idempotency_key` field with partial unique DB index
- Service checks existing key before creating: returns existing notification if found
- Prevents duplicate sends on retried API calls

### WebSocket Real-Time Updates

Hub pattern at `/ws/notifications`:
- Worker broadcasts on every status transition (`queued` → `processing` → `sent`/`failed`)
- Clients receive JSON: `{notification_id, status, channel, timestamp, attempt, error}`

### External Provider Integration

All three channels (SMS, Email, Push) POST to webhook.site:
- URL configured via `WEBHOOK_URL` env var
- HTTP client timeout: **5 seconds**
- Timeout errors are classified as retryable

## Configuration

All config via environment variables (`.env` file loaded by godotenv):

| Variable | Default | Description |
|----------|---------|-------------|
| `APP_PORT` | 8080 | API server port |
| `DB_HOST/PORT/USER/PASSWORD/NAME` | localhost:5432 | PostgreSQL connection |
| `REDIS_HOST/PORT` | localhost:6379 | Redis connection |
| `RABBITMQ_HOST/PORT/USER/PASSWORD` | localhost:5672 | RabbitMQ connection |
| `WEBHOOK_URL` | webhook.site/... | External provider endpoint |
| `RATE_LIMIT_PER_SECOND` | 100 | Max messages per second per channel |
| `WORKER_CONCURRENCY` | 10 | Number of worker goroutines |
| `WORKER_MAX_RETRIES` | 5 | Max retry attempts before DLQ |

## API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/v1/notifications` | Create single notification |
| `POST` | `/api/v1/notifications/batch` | Create batch (≤1000) |
| `GET` | `/api/v1/notifications/:id` | Get by ID |
| `GET` | `/api/v1/notifications/batch/:batchId` | Get batch |
| `PATCH` | `/api/v1/notifications/:id/cancel` | Cancel pending/queued |
| `GET` | `/api/v1/notifications` | List with filters + pagination |
| `GET` | `/api/v1/metrics` | System metrics |
| `GET` | `/api/v1/health` | Health check (DB + Redis + RabbitMQ) |
| `GET` | `/ws/notifications` | WebSocket status updates |

### Pagination Response Contract
```json
{"data": [], "page": 1, "page_size": 20, "total": 125}
```
Query params: `page`, `page_size`, `status`, `channel`, `date_from`, `date_to`

### Health Check Response
```json
{"status": "ok", "db": "ok", "redis": "ok", "rabbitmq": "ok"}
```
Returns HTTP 503 if any service is unhealthy.

## Testing Conventions

- Unit tests live alongside source files: `foo.go` → `foo_test.go`
- Use `testify/assert` and `testify/require` for assertions
- Table-driven tests for enum validation and error classification
- No external dependencies required for unit tests (repository/queue tests would need mocks)
- Run with `-race` flag to detect data races
