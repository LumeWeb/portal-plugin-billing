-- +goose Up
-- +goose StatementBegin
-- Webhook Events Table
CREATE TABLE IF NOT EXISTS billing_webhook_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    gateway_type VARCHAR(255) NOT NULL,
    event_id VARCHAR(255) NOT NULL,
    event_type VARCHAR(255) NOT NULL,
    processed_at DATETIME NULL DEFAULT NULL,
    payload BLOB,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    deleted_at DATETIME NULL
);

CREATE INDEX IF NOT EXISTS idx_billing_webhook_events_gateway_type ON billing_webhook_events(gateway_type);
CREATE INDEX IF NOT EXISTS idx_billing_webhook_events_event_id ON billing_webhook_events(event_id);
CREATE INDEX IF NOT EXISTS idx_billing_webhook_events_processed_at ON billing_webhook_events(processed_at);
CREATE INDEX IF NOT EXISTS idx_billing_webhook_events_created_at ON billing_webhook_events(created_at);

-- Subscribers Table
CREATE TABLE IF NOT EXISTS billing_subscribers (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL,
    gateway_type VARCHAR(255) NOT NULL,
    gateway_id VARCHAR(255) NOT NULL,
    is_active BOOLEAN DEFAULT FALSE,
    plan_id INTEGER NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    deleted_at DATETIME NULL
);

CREATE INDEX IF NOT EXISTS idx_billing_subscribers_user_id ON billing_subscribers(user_id);
CREATE INDEX IF NOT EXISTS idx_billing_subscribers_gateway_type ON billing_subscribers(gateway_type);
CREATE INDEX IF NOT EXISTS idx_billing_subscribers_is_active ON billing_subscribers(is_active);
CREATE INDEX IF NOT EXISTS idx_billing_subscribers_gateway_id ON billing_subscribers(gateway_id);
CREATE UNIQUE INDEX IF NOT EXISTS uniq_billing_subscribers_user_gateway
  ON billing_subscribers(user_id, gateway_type) WHERE deleted_at IS NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS uniq_billing_subscribers_user_gateway;
DROP INDEX IF EXISTS idx_billing_subscribers_gateway_id;
DROP INDEX IF EXISTS idx_billing_subscribers_is_active;
DROP INDEX IF EXISTS idx_billing_subscribers_gateway_type;
DROP INDEX IF EXISTS idx_billing_subscribers_user_id;
DROP TABLE billing_subscribers;

DROP INDEX IF EXISTS idx_billing_webhook_events_created_at;
DROP INDEX IF EXISTS idx_billing_webhook_events_processed_at;
DROP INDEX IF EXISTS idx_billing_webhook_events_event_id;
DROP INDEX IF EXISTS idx_billing_webhook_events_gateway_type;
DROP TABLE billing_webhook_events;
-- +goose StatementEnd
