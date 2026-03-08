# Configuration

All configuration is managed through environment variables. Copy `.env.example` to `.env` and adjust values as needed.

```bash
cp .env.example .env
```

## Variables

### Application

| Variable | Default | Description |
|----------|---------|-------------|
| `APP_PORT` | `8080` | API server port |
| `APP_ENV` | `development` | Environment (`development`, `production`) |

### PostgreSQL

| Variable | Default | Description |
|----------|---------|-------------|
| `DB_HOST` | `localhost` | Database host |
| `DB_PORT` | `5432` | Database port |
| `DB_USER` | `notification` | Database user |
| `DB_PASSWORD` | `notification_secret` | Database password |
| `DB_NAME` | `notification_system` | Database name |
| `DB_SSLMODE` | `disable` | SSL mode (`disable`, `require`, `verify-full`) |

### Redis

| Variable | Default | Description |
|----------|---------|-------------|
| `REDIS_HOST` | `localhost` | Redis host |
| `REDIS_PORT` | `6379` | Redis port |

### RabbitMQ

| Variable | Default | Description |
|----------|---------|-------------|
| `RABBITMQ_HOST` | `localhost` | RabbitMQ host |
| `RABBITMQ_PORT` | `5672` | RabbitMQ AMQP port |
| `RABBITMQ_USER` | `guest` | RabbitMQ user |
| `RABBITMQ_PASSWORD` | `guest` | RabbitMQ password |

### Worker

| Variable | Default | Description |
|----------|---------|-------------|
| `WORKER_CONCURRENCY` | `10` | Number of worker goroutines consuming from queue |
| `WORKER_MAX_RETRIES` | `5` | Max retry attempts before sending to DLQ |
| `RATE_LIMIT_PER_SECOND` | `100` | Max messages per second per channel |

### External Provider

| Variable | Default | Description |
|----------|---------|-------------|
| `WEBHOOK_URL` | `https://webhook.site/...` | URL where SMS/Email/Push providers POST |

## Docker Compose Ports

When running via `docker compose`, host ports are mapped as follows to avoid conflicts:

| Service | Host Port | Container Port |
|---------|-----------|---------------|
| PostgreSQL | 5433 | 5432 |
| Redis | 6379 | 6379 |
| RabbitMQ (AMQP) | 5672 | 5672 |
| RabbitMQ (Management) | 15672 | 15672 |
| API | 8081 | 8080 |

RabbitMQ management UI: `http://localhost:15672` (guest/guest)
