# Development

## Prerequisites

- Go 1.23+
- Docker & Docker Compose

## Setup

```bash
# Clone and enter the project
cd notification-system

# Create local config
cp .env.example .env

# Start infrastructure (PostgreSQL, Redis, RabbitMQ)
docker compose up -d postgres redis rabbitmq

# Install Go dependencies
go mod download
```

## Running

You need two separate terminal sessions:

```bash
# Terminal 1: API server
go run cmd/api/main.go

# Terminal 2: Worker
go run cmd/worker/main.go
```

The API runs at `http://localhost:8080`. The worker connects to RabbitMQ and starts consuming messages.

## Testing

```bash
make ci            # Full CI pipeline: vet + staticcheck + test + build
make test          # Run tests with race detector
make lint          # Run go vet + staticcheck
make test-cover    # Tests with coverage report (opens coverage.html)
```

### Test Conventions

- Unit tests live alongside source files: `foo.go` → `foo_test.go`
- Use `testify/assert` and `testify/require` for assertions
- Table-driven tests for enum validation and error classification
- No external service dependencies for unit tests
- Always run with `-race` flag

## Building

```bash
make build
# Produces: bin/api, bin/worker
```

## Swagger Docs

```bash
make swagger
# Generates docs from handler annotations using swag
```

## Useful Services

| Service | URL | Credentials |
|---------|-----|-------------|
| API | http://localhost:8080 | — |
| RabbitMQ Management | http://localhost:15672 | guest / guest |
| Webhook.site | https://webhook.site/a2d24e8e-0d36-417d-bc4d-74fe82398181 | — |

## Project Structure

See [Architecture](architecture.md) for detailed layer descriptions.
