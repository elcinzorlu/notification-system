-- Enable UUID extension
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- Notifications table
CREATE TABLE IF NOT EXISTS notifications (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    batch_id UUID,
    recipient VARCHAR(255) NOT NULL,
    channel VARCHAR(10) NOT NULL CHECK (channel IN ('sms', 'email', 'push')),
    content TEXT NOT NULL,
    subject VARCHAR(255),
    priority VARCHAR(10) NOT NULL DEFAULT 'normal' CHECK (priority IN ('high', 'normal', 'low')),
    status VARCHAR(20) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'queued', 'processing', 'sent', 'failed', 'cancelled')),
    scheduled_at TIMESTAMPTZ,
    template_id UUID,
    variables JSONB,
    idempotency_key VARCHAR(255),
    provider_message_id VARCHAR(255),
    attempts INT NOT NULL DEFAULT 0,
    max_retries INT NOT NULL DEFAULT 5,
    last_attempt_at TIMESTAMPTZ,
    error_message TEXT,
    correlation_id VARCHAR(255),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Indexes for query performance
CREATE INDEX idx_notifications_status ON notifications(status);
CREATE INDEX idx_notifications_channel ON notifications(channel);
CREATE INDEX idx_notifications_priority ON notifications(priority);
CREATE INDEX idx_notifications_batch_id ON notifications(batch_id);
CREATE INDEX idx_notifications_scheduled_at ON notifications(scheduled_at);
CREATE INDEX idx_notifications_created_at ON notifications(created_at);
CREATE INDEX idx_notifications_correlation_id ON notifications(correlation_id);

-- Partial unique index for idempotency
CREATE UNIQUE INDEX idx_notifications_idempotency_key
    ON notifications(idempotency_key)
    WHERE idempotency_key IS NOT NULL;

-- Composite index for list queries with filtering
CREATE INDEX idx_notifications_status_channel_created
    ON notifications(status, channel, created_at DESC);

-- Templates table
CREATE TABLE IF NOT EXISTS templates (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL,
    channel VARCHAR(10) NOT NULL CHECK (channel IN ('sms', 'email', 'push')),
    subject VARCHAR(255),
    body TEXT NOT NULL,
    variables JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_templates_name ON templates(name);
