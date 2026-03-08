# Architecture

## System Flow

```
┌──────────────┐     ┌──────────────┐     ┌──────────────────┐
│   API (Fiber)│────▶│  RabbitMQ    │────▶│  Worker Pool     │
│              │     │              │     │  (N goroutines)  │
│ POST /notify │     │ Priority Q   │     │                  │
│ GET  /status │     │ Retry Q      │     │ Rate Limiter ──▶ │──▶ webhook.site
│ WS  /updates│     │ Dead Letter Q│     │ Retry Engine     │
└──────┬───────┘     └──────────────┘     └────────┬─────────┘
       │                                           │
       ▼                                           ▼
┌──────────────┐                          ┌──────────────┐
│ PostgreSQL   │                          │    Redis     │
│ (persistence)│                          │ (rate limit, │
│              │                          │  scheduling) │
└──────────────┘                          └──────────────┘
```

## Clean Architecture Layers

Dependencies flow inward:

- **Handler** → **Service** → **Repository** (database)
- **Handler** → **Service** → **Queue** (RabbitMQ)
- **Handler** → **Service** → **Provider** (external API)

Service orchestrates all business logic; handlers only parse HTTP requests and return responses.

```
cmd/api/main.go              # API server entrypoint
cmd/worker/main.go           # Worker entrypoint
internal/config/             # Env-based configuration (godotenv)
internal/notification/
  ├── model/                 # Domain models, DTOs, enums
  ├── handler/               # Fiber HTTP handlers (Swagger annotated)
  ├── service/               # Business logic orchestration
  ├── repository/            # GORM-backed PostgreSQL persistence
  └── queue/                 # RabbitMQ queue with DLQ topology
internal/provider/           # Provider interface + SMS/Email/Push
internal/rate_limiter/       # Redis sliding window rate limiter
internal/retry/              # Exponential backoff + error classification
internal/metrics/            # In-memory atomic counters + latency percentiles
internal/websocket/          # WebSocket hub for real-time status updates
pkg/database/                # Database connection helper
migrations/                  # SQL migration files
```

## Core Domain Models

### Notification

The main work item:

| Field | Description |
|-------|-------------|
| `id` | UUID primary key |
| `batch_id` | Optional UUID for grouping batch notifications |
| `recipient` | Target address (phone, email, device token) |
| `channel` | `sms`, `email`, or `push` |
| `priority` | `high` (10), `normal` (5), `low` (1) — maps to RabbitMQ priority |
| `status` | `pending` → `queued` → `processing` → `sent` / `failed` / `cancelled` |
| `idempotency_key` | Partial unique index (`WHERE idempotency_key IS NOT NULL`) |
| `correlation_id` | Distributed tracing identifier |
| `scheduled_at` | Optional future delivery time |
| `attempts` | Current retry count |
| `max_retries` | Maximum retry attempts (default 5) |

### Template

Message templates with `{{.Variable}}` Go template substitution.

## RabbitMQ Queue Topology

```
                         ┌──────────────────┐
    notifications.main   │  x-max-priority  │
    (priority: 0-10)     │  DLX → dlq       │
                         └────────┬─────────┘
                                  │
                     ┌────────────┼────────────┐
                     │ success    │ retry      │ max retries
                     ▼            ▼            ▼
                  ✅ sent   notifications.retry  notifications.dlq
                            (TTL → main)         (terminal)
```

- **notifications.main** — Priority queue (`x-max-priority: 10`). Messages are consumed by the worker pool.
- **notifications.retry** — Messages sit here with per-message TTL for exponential backoff. On TTL expiry, dead-letter routing sends them back to `notifications.main`.
- **notifications.dlq** — Terminal storage for permanently failed messages after max retries.

## Rate Limiting

Redis sliding window counter per channel:

- Key format: `rate:{channel}:{unix_timestamp}`
- TTL: 2 seconds, limit: 100 msg/sec per channel
- `Wait()` blocks until rate allows (with context cancellation)

## Retry & Error Classification

Exponential backoff: `1s → 2s → 4s → 8s → 16s` (5 max attempts, with jitter).

| Error Type | Action |
|-----------|--------|
| Network error / timeout | Retry |
| HTTP 5xx | Retry |
| HTTP 4xx | Fail immediately |
| Validation error | Fail immediately |

## Correlation ID Propagation

Full request tracing:

```
HTTP Header (X-Correlation-ID) → Fiber middleware → Service
  → RabbitMQ message header → Worker log context → Provider HTTP header
```

Auto-generated UUID if not provided by the caller.

## Idempotency

- `idempotency_key` field with partial unique DB index
- Service checks existing key before creating: returns existing notification if found
- Prevents duplicate sends on retried API calls

## WebSocket Real-Time Updates

Hub pattern at `/ws/notifications`:

- Worker broadcasts on every status transition
- Clients receive: `{notification_id, status, channel, timestamp, attempt, error}`
