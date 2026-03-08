# Deployment

## Docker Compose (Recommended)

The simplest way to run the full stack:

```bash
docker compose up -d
```

This starts five services:
- **PostgreSQL** — persistence
- **Redis** — rate limiting & scheduled notification storage
- **RabbitMQ** — message queue with management UI
- **API** — Fiber HTTP server
- **Worker** — queue consumer pool

All services have health checks configured. The API and Worker wait for PostgreSQL, Redis, and RabbitMQ to be healthy before starting.

### Rebuilding After Code Changes

```bash
docker compose up -d --build api worker
# or
make docker-rebuild
```

### Stopping

```bash
docker compose down            # Stop and remove containers
docker compose down -v         # Also remove volumes (database data)
```

## Docker Image

The project uses a multi-stage Dockerfile (`docker/Dockerfile`) with two targets:

```bash
# Build API image
docker build --target api -t notification-api -f docker/Dockerfile .

# Build Worker image
docker build --target worker -t notification-worker -f docker/Dockerfile .
```

Both images are based on `alpine:3.19` and include only the compiled binary (~15MB each).

## Production Considerations

### Environment Variables

Set all variables from [Configuration](configuration.md) via your deployment platform's secret management. Key items:

- Change `DB_PASSWORD` and `RABBITMQ_PASSWORD` from defaults
- Set `DB_SSLMODE=require` for remote databases
- Set `APP_ENV=production` for production logging

### Scaling

- **API**: Stateless — scale horizontally behind a load balancer
- **Worker**: Scale by increasing `WORKER_CONCURRENCY` or running multiple worker instances. RabbitMQ handles competing consumers automatically.
- **Rate Limiting**: Redis-based, shared across all worker instances

### Database Migrations

Migrations are applied automatically on API startup via GORM AutoMigrate. SQL files in `migrations/` are also loaded by PostgreSQL's `docker-entrypoint-initdb.d` on first database initialization.
