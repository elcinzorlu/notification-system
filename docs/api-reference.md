# API Reference

Base URL: `http://localhost:8081/api/v1` (Docker) or `http://localhost:8080/api/v1` (local)

## 📚 Interactive API Docs (Swagger UI)
Swagger UI is available out of the box. You can use it to interactively explore and test all API endpoints:
- **URL**: `http://localhost:8081/swagger/index.html` (Docker) or `http://localhost:8080/swagger/index.html` (local)

## Notifications

### Create Notification

```
POST /api/v1/notifications
```

**Headers:**
| Header | Required | Description |
|--------|----------|-------------|
| `Content-Type` | Yes | `application/json` |
| `X-Correlation-ID` | No | Tracing ID (auto-generated if omitted) |

**Request Body:**
```json
{
  "recipient": "+905551234567",
  "channel": "sms",
  "content": "Your order has been shipped!",
  "priority": "high",
  "subject": "Order Update",
  "scheduled_at": "2026-03-08T10:00:00Z",
  "idempotency_key": "order-12345-confirmation",
  "template_id": "uuid",
  "variables": {"name": "Elçin", "order_id": "12345"}
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `recipient` | string | Yes | Phone number, email, or device token |
| `channel` | string | Yes | `sms`, `email`, or `push` |
| `content` | string | Yes* | Message body (*or `template_id`) |
| `priority` | string | No | `high`, `normal` (default), `low` |
| `subject` | string | Email only | Required for email channel |
| `scheduled_at` | datetime | No | ISO 8601 future delivery time |
| `idempotency_key` | string | No | Prevent duplicate sends |
| `template_id` | uuid | No | Use template instead of inline content |
| `variables` | object | No | Template variable substitution |

**Channel content limits:**
- SMS: max 160 characters
- Push: max 256 characters
- Email: subject required

**Response:** `200 OK`
```json
{
  "id": "0cf41438-54d4-445e-9ec6-2aac4021b542",
  "recipient": "+905551234567",
  "channel": "sms",
  "content": "Your order has been shipped!",
  "priority": "high",
  "status": "queued",
  "attempts": 0,
  "max_retries": 5,
  "correlation_id": "test-trace-001",
  "created_at": "2026-03-07T17:34:28.810Z",
  "updated_at": "2026-03-07T17:34:28.810Z"
}
```

---

### Create Batch

```
POST /api/v1/notifications/batch
```

**Request Body:**
```json
{
  "notifications": [
    {"recipient": "+905551234567", "channel": "sms", "content": "Flash sale!", "priority": "high"},
    {"recipient": "user@example.com", "channel": "email", "content": "New arrivals", "subject": "Hello", "priority": "normal"},
    {"recipient": "device-token-123", "channel": "push", "content": "You have a message", "priority": "low"}
  ]
}
```

- Maximum **1000** notifications per batch
- All notifications share a `batch_id` and `correlation_id`

**Response:** `200 OK`
```json
{
  "batch_id": "f71b225d-3ae1-4f62-a3a7-5efb4087afc7",
  "total": 3,
  "notifications": [...]
}
```

---

### Get Notification

```
GET /api/v1/notifications/:id
```

**Response:** `200 OK` — Single notification object.

---

### Get Batch

```
GET /api/v1/notifications/batch/:batchId
```

**Response:** `200 OK` — Array of notifications in the batch.

---

### Cancel Notification

```
PATCH /api/v1/notifications/:id/cancel
```

Only cancels notifications in `pending` or `queued` status.

**Response:** `200 OK`

---

### List Notifications

```
GET /api/v1/notifications
```

**Query Parameters:**
| Param | Type | Default | Description |
|-------|------|---------|-------------|
| `page` | int | 1 | Page number |
| `page_size` | int | 20 | Items per page (max 100) |
| `status` | string | — | Filter: `pending`, `queued`, `processing`, `sent`, `failed`, `cancelled` |
| `channel` | string | — | Filter: `sms`, `email`, `push` |
| `date_from` | datetime | — | Filter: created after (ISO 8601) |
| `date_to` | datetime | — | Filter: created before (ISO 8601) |

**Response:** `200 OK`
```json
{
  "data": [...],
  "page": 1,
  "page_size": 20,
  "total": 125
}
```

---

## System

### Health Check

```
GET /api/v1/health
```

**Response:** `200 OK` (all healthy) or `503 Service Unavailable`
```json
{
  "status": "ok",
  "db": "ok",
  "redis": "ok",
  "rabbitmq": "ok"
}
```

---

### Metrics

```
GET /api/v1/metrics
```

**Response:** `200 OK`
```json
{
  "queue_depth": {
    "notifications.main": 42,
    "notifications.retry": 3,
    "notifications.dlq": 0
  },
  "notifications_sent_total": {"sms": 1523, "email": 892, "push": 2341},
  "notifications_failed_total": {"sms": 12, "email": 5, "push": 8},
  "processing_latency_ms": {"p50": 45.2, "p95": 120.8, "p99": 250.3},
  "success_rate": 99.5,
  "failure_rate": 0.5
}
```

---

## WebSocket

### Real-Time Status Updates

```
GET /ws/notifications
```

Upgrade to WebSocket. Receives JSON messages on every status transition:

```json
{
  "notification_id": "0cf41438-...",
  "status": "sent",
  "channel": "sms",
  "timestamp": "2026-03-07T17:34:29Z",
  "attempt": 1,
  "error": ""
}
```

**JavaScript example:**
```javascript
const ws = new WebSocket('ws://localhost:8081/ws/notifications');
ws.onmessage = (event) => {
  const update = JSON.parse(event.data);
  console.log(`${update.notification_id}: ${update.status}`);
};
```

---

## Error Responses

All errors follow a consistent format:

```json
{
  "error": "error_code",
  "message": "Human-readable description",
  "code": 400
}
```
