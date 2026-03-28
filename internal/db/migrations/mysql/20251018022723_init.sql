-- +goose Up
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
    external_id VARCHAR(255) NOT NULL,
    subscription_id VARCHAR(255) NULL,
    is_active BOOLEAN DEFAULT FALSE,
    plan_id BIGINT UNSIGNED NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP NULL DEFAULT NULL,
    -- Generated column for partial unique constraint: only active subscriptions
    active_gateway_key VARCHAR(255) GENERATED ALWAYS AS (
        CASE 
            WHEN is_active = TRUE AND deleted_at IS NULL THEN CONCAT(user_id, ':', gateway_type)
            ELSE NULL
        END
    ) STORED,
    UNIQUE KEY uniq_user_gateway_active (active_gateway_key),
    INDEX idx_user_id (user_id),
    INDEX idx_gateway_type (gateway_type),
    INDEX idx_is_active (is_active),
    INDEX idx_external_id (external_id)
);

-- Pricing Plans Table
CREATE TABLE IF NOT EXISTS billing_pricing_plans (
    id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP NULL DEFAULT NULL,
    name VARCHAR(255) NOT NULL,
    description TEXT NOT NULL,
    features_json TEXT NOT NULL,
    monthly_price_usd DECIMAL(10, 2) NULL,
    yearly_price_usd DECIMAL(10, 2) NULL,
    quota_plan_id BIGINT UNSIGNED NULL,
    currency VARCHAR(3) DEFAULT 'USD',
    is_active BOOLEAN DEFAULT TRUE,
    is_public BOOLEAN DEFAULT FALSE,
    INDEX idx_name (name),
    INDEX idx_is_active (is_active),
    INDEX idx_is_public (is_public)
);

-- Price Lines Table
CREATE TABLE IF NOT EXISTS billing_pricelines (
    id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP NULL DEFAULT NULL,
    name VARCHAR(255) NOT NULL,
    description TEXT NOT NULL,
    is_active BOOLEAN DEFAULT TRUE,
    is_default BOOLEAN DEFAULT FALSE,
    INDEX idx_name (name),
    INDEX idx_is_default (is_default),
    INDEX idx_is_active (is_active)
);

-- Price Line Plans Junction Table
CREATE TABLE IF NOT EXISTS billing_priceline_plans (
    price_line_id BIGINT UNSIGNED NOT NULL,
    plan_id BIGINT UNSIGNED NOT NULL,
    position INT NOT NULL,
    PRIMARY KEY (price_line_id, plan_id),
    INDEX idx_price_line_id (price_line_id),
    INDEX idx_plan_id (plan_id),
    KEY fk_billing_priceline_plans_price_lines (price_line_id),
    KEY fk_billing_priceline_plans_pricing_plans (plan_id),
    CONSTRAINT fk_billing_priceline_plans_price_lines FOREIGN KEY (price_line_id) REFERENCES billing_pricelines(id) ON DELETE CASCADE,
    CONSTRAINT fk_billing_priceline_plans_pricing_plans FOREIGN KEY (plan_id) REFERENCES billing_pricing_plans(id) ON DELETE CASCADE
);

-- Price Line Assignments Table
CREATE TABLE IF NOT EXISTS billing_priceline_assignments (
    id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP NULL DEFAULT NULL,
    price_line_id BIGINT UNSIGNED NOT NULL,
    user_id BIGINT UNSIGNED NOT NULL,
    INDEX idx_price_line_id (price_line_id),
    INDEX idx_user_id (user_id),
    KEY fk_billing_priceline_assignments_price_lines (price_line_id),
    CONSTRAINT fk_billing_priceline_assignments_price_lines FOREIGN KEY (price_line_id) REFERENCES billing_pricelines(id) ON DELETE CASCADE,
    UNIQUE KEY uniq_user_assignment (user_id)
);

-- Gateway Product Mappings Table
CREATE TABLE IF NOT EXISTS billing_gateway_product_mappings (
    id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP NULL DEFAULT NULL,
    plan_id BIGINT UNSIGNED NOT NULL,
    gateway_type VARCHAR(255) NOT NULL,
    remote_product_id VARCHAR(255) NOT NULL,
    remote_monthly_price_id VARCHAR(255) NULL,
    remote_yearly_price_id VARCHAR(255) NULL,
    sync_status VARCHAR(50) DEFAULT 'pending',
    last_synced_at TIMESTAMP NULL DEFAULT NULL,
    error_message TEXT NULL,
    retries INT DEFAULT 0,
    portal_configuration_id VARCHAR(255) DEFAULT NULL,
    INDEX idx_plan_id (plan_id),
    INDEX idx_gateway_type (gateway_type),
    KEY fk_billing_gateway_product_mappings_pricing_plans (plan_id),
    CONSTRAINT fk_billing_gateway_product_mappings_pricing_plans FOREIGN KEY (plan_id) REFERENCES billing_pricing_plans(id) ON DELETE CASCADE,
    UNIQUE KEY uniq_plan_gateway (plan_id, gateway_type)
);

-- +goose Down
DROP TABLE IF EXISTS billing_gateway_product_mappings;
DROP TABLE IF EXISTS billing_priceline_assignments;
DROP TABLE IF EXISTS billing_priceline_plans;
DROP TABLE IF EXISTS billing_pricelines;
DROP TABLE IF EXISTS billing_pricing_plans;
DROP TABLE IF EXISTS billing_subscribers;
DROP TABLE IF EXISTS billing_webhook_events;