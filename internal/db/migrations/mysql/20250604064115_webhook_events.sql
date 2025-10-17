-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS billing_webhook_events (
    id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    gateway_type VARCHAR(255) NOT NULL,
    event_id VARCHAR(255) NOT NULL,
    event_type VARCHAR(255),
    processed_at TIMESTAMP NOT NULL,
    payload LONGBLOB,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP NULL DEFAULT NULL,
    INDEX idx_gateway_type (gateway_type),
    INDEX idx_event_type (event_type),
    INDEX idx_processed_at (processed_at),
    UNIQUE KEY uniq_gateway_event (gateway_type, event_id)
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE billing_webhook_events;
-- +goose StatementEnd
