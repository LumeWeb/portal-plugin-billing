-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS billing_webhook_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    gateway_type VARCHAR(255) NOT NULL,
    event_id VARCHAR(255) NOT NULL,
    event_type VARCHAR(255),
    processed_at DATETIME NOT NULL,
    payload BLOB,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    deleted_at DATETIME DEFAULT NULL
);

CREATE INDEX IF NOT EXISTS idx_billing_webhook_events_gateway_type ON billing_webhook_events(gateway_type);
CREATE INDEX IF NOT EXISTS idx_billing_webhook_events_event_type ON billing_webhook_events(event_type);
CREATE INDEX IF NOT EXISTS idx_billing_webhook_events_processed_at ON billing_webhook_events(processed_at);
CREATE UNIQUE INDEX IF NOT EXISTS uniq_billing_webhook_events_gateway_event
  ON billing_webhook_events(gateway_type, event_id) WHERE deleted_at IS NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS uniq_billing_webhook_events_gateway_event;
DROP INDEX IF EXISTS idx_billing_webhook_events_processed_at;
DROP INDEX IF EXISTS idx_billing_webhook_events_event_type;
DROP INDEX IF EXISTS idx_billing_webhook_events_gateway_type;
DROP TABLE billing_webhook_events;
-- +goose StatementEnd
