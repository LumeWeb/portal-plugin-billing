-- +goose Up
-- +goose StatementBegin
-- Webhook Events Table
CREATE TABLE IF NOT EXISTS billing_webhook_events (
    id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    gateway_type VARCHAR(255) NOT NULL,
    event_id VARCHAR(255) NOT NULL,
    event_type VARCHAR(255) NOT NULL,
    processed_at TIMESTAMP NULL DEFAULT NULL,
    payload LONGBLOB,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP NULL DEFAULT NULL,
    UNIQUE INDEX uniq_gateway_event (gateway_type, event_id),
    INDEX idx_gateway_type (gateway_type),
    INDEX idx_event_id (event_id),
    INDEX idx_processed_at (processed_at),
    INDEX idx_created_at (created_at)
);

-- Subscribers Table
CREATE TABLE IF NOT EXISTS billing_subscribers (
    id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    user_id BIGINT UNSIGNED NOT NULL,
    gateway_type VARCHAR(255) NOT NULL,
    gateway_id VARCHAR(255) NOT NULL,
    is_active BOOLEAN DEFAULT FALSE,
    plan_id BIGINT UNSIGNED NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP NULL DEFAULT NULL,
    UNIQUE KEY uniq_user_gateway (user_id, gateway_type),
    INDEX idx_user_id (user_id),
    INDEX idx_gateway_type (gateway_type),
    INDEX idx_is_active (is_active),
    INDEX idx_gateway_id (gateway_id)
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE billing_subscribers;
DROP TABLE billing_webhook_events;
-- +goose StatementEnd
