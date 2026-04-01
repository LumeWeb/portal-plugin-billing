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
CREATE UNIQUE INDEX IF NOT EXISTS uniq_billing_webhook_events_gateway_event
  ON billing_webhook_events(gateway_type, event_id) WHERE deleted_at IS NULL;

-- Pricing Plans Table
CREATE TABLE IF NOT EXISTS billing_pricing_plans (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    deleted_at DATETIME NULL,
    name VARCHAR(255) NOT NULL,
    description TEXT NOT NULL,
    features_json TEXT NOT NULL,
    currency VARCHAR(3) DEFAULT 'USD',
    is_active BOOLEAN DEFAULT TRUE,
    is_public BOOLEAN DEFAULT FALSE
);

CREATE INDEX IF NOT EXISTS idx_billing_pricing_plans_name ON billing_pricing_plans(name);
CREATE INDEX IF NOT EXISTS idx_billing_pricing_plans_is_active ON billing_pricing_plans(is_active);
CREATE INDEX IF NOT EXISTS idx_billing_pricing_plans_is_public ON billing_pricing_plans(is_public);

-- Pricing Plan Periods Table
-- Stores pricing plan variations for different billing cadences (monthly, yearly, quarterly, weekly)
-- quota_plan_id references external portal-plugin-quota service and is validated by that service
CREATE TABLE IF NOT EXISTS billing_pricing_plan_periods (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    pricing_plan_id INTEGER NOT NULL,
    cadence VARCHAR(50) NOT NULL,
    price_usd DECIMAL NOT NULL,
    quota_plan_id INTEGER NULL,
    rolling_days INTEGER NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    deleted_at DATETIME NULL,
    UNIQUE (pricing_plan_id, cadence),
    FOREIGN KEY (pricing_plan_id) REFERENCES billing_pricing_plans(id) ON DELETE CASCADE
);

-- Subscribers Table
CREATE TABLE IF NOT EXISTS billing_subscribers (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL,
    gateway_type VARCHAR(255) NOT NULL,
    external_id VARCHAR(255) NOT NULL,
    subscription_id VARCHAR(255) NULL,
    is_active BOOLEAN DEFAULT FALSE,
    pricing_plan_period_id INTEGER NULL,
    billing_period_start DATETIME NULL DEFAULT NULL,
    billing_period_end DATETIME NULL DEFAULT NULL,
    will_cancel_at DATETIME NULL DEFAULT NULL,
    cancelled_at DATETIME NULL DEFAULT NULL,
    payment_status TEXT DEFAULT 'pending',
    previous_plan_id INTEGER NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    deleted_at DATETIME NULL,
    FOREIGN KEY (pricing_plan_period_id) REFERENCES billing_pricing_plan_periods(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_billing_subscribers_user_id ON billing_subscribers(user_id);
CREATE INDEX IF NOT EXISTS idx_billing_subscribers_gateway_type ON billing_subscribers(gateway_type);
CREATE INDEX IF NOT EXISTS idx_billing_subscribers_is_active ON billing_subscribers(is_active);
CREATE INDEX IF NOT EXISTS idx_billing_subscribers_external_id ON billing_subscribers(external_id);
CREATE INDEX IF NOT EXISTS idx_billing_subscribers_pricing_plan_period_id ON billing_subscribers(pricing_plan_period_id);
CREATE INDEX IF NOT EXISTS idx_billing_subscribers_billing_period_start ON billing_subscribers(billing_period_start);
CREATE INDEX IF NOT EXISTS idx_billing_subscribers_billing_period_end ON billing_subscribers(billing_period_end);
CREATE INDEX IF NOT EXISTS idx_billing_subscribers_will_cancel_at ON billing_subscribers(will_cancel_at);
CREATE INDEX IF NOT EXISTS idx_billing_subscribers_cancelled_at ON billing_subscribers(cancelled_at);
CREATE INDEX IF NOT EXISTS idx_billing_subscribers_payment_status ON billing_subscribers(payment_status);
-- Partial unique index: only one active subscription per user per gateway
CREATE UNIQUE INDEX IF NOT EXISTS uniq_billing_subscribers_user_gateway_active
  ON billing_subscribers(user_id, gateway_type) WHERE is_active = TRUE AND deleted_at IS NULL;

-- Price Lines Table
CREATE TABLE IF NOT EXISTS billing_pricelines (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    deleted_at DATETIME NULL,
    name VARCHAR(255) NOT NULL,
    description TEXT NOT NULL,
    is_active BOOLEAN DEFAULT TRUE,
    is_default BOOLEAN DEFAULT FALSE
);

CREATE INDEX IF NOT EXISTS idx_billing_pricelines_name ON billing_pricelines(name);
CREATE INDEX IF NOT EXISTS idx_billing_pricelines_is_default ON billing_pricelines(is_default);
CREATE INDEX IF NOT EXISTS idx_billing_pricelines_is_active ON billing_pricelines(is_active);

-- Price Line Plans Junction Table
CREATE TABLE IF NOT EXISTS billing_priceline_plans (
    price_line_id INTEGER NOT NULL,
    plan_id INTEGER NOT NULL,
    position INTEGER NOT NULL,
    PRIMARY KEY (price_line_id, plan_id),
    FOREIGN KEY (price_line_id) REFERENCES billing_pricelines(id) ON DELETE CASCADE,
    FOREIGN KEY (plan_id) REFERENCES billing_pricing_plans(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_billing_priceline_plans_price_line_id ON billing_priceline_plans(price_line_id);
CREATE INDEX IF NOT EXISTS idx_billing_priceline_plans_plan_id ON billing_priceline_plans(plan_id);

-- Price Line Assignments Table
CREATE TABLE IF NOT EXISTS billing_priceline_assignments (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    deleted_at DATETIME NULL,
    price_line_id INTEGER NOT NULL,
    user_id INTEGER NOT NULL,
    UNIQUE (user_id),
    FOREIGN KEY (price_line_id) REFERENCES billing_pricelines(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_billing_priceline_assignments_price_line_id ON billing_priceline_assignments(price_line_id);
CREATE INDEX IF NOT EXISTS idx_billing_priceline_assignments_user_id ON billing_priceline_assignments(user_id);

-- Gateway Product Mappings Table
CREATE TABLE IF NOT EXISTS billing_gateway_product_mappings (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    deleted_at DATETIME NULL,
    pricing_plan_period_id INTEGER NULL,
    gateway_type VARCHAR(255) NOT NULL,
    remote_product_id VARCHAR(255) NOT NULL,
    remote_price_id VARCHAR(255) NULL,
    sync_status VARCHAR(50) DEFAULT 'pending',
    last_synced_at DATETIME NULL,
    error_message TEXT NULL,
    retries INTEGER DEFAULT 0,
    portal_configuration_id TEXT,
    FOREIGN KEY (pricing_plan_period_id) REFERENCES billing_pricing_plan_periods(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_billing_gateway_product_mappings_pricing_plan_period_id ON billing_gateway_product_mappings(pricing_plan_period_id);
CREATE INDEX IF NOT EXISTS idx_billing_gateway_product_mappings_gateway_type ON billing_gateway_product_mappings(gateway_type);
CREATE UNIQUE INDEX IF NOT EXISTS uniq_billing_gateway_product_mappings_period_gateway
  ON billing_gateway_product_mappings(pricing_plan_period_id, gateway_type) WHERE deleted_at IS NULL;

-- Credits Table
CREATE TABLE IF NOT EXISTS billing_credits (
    id TEXT PRIMARY KEY,
    user_id INTEGER NOT NULL,
    amount DECIMAL NOT NULL,
    type TEXT NOT NULL,
    direction TEXT NOT NULL,
    reference_id TEXT,
    reference_type TEXT,
    description TEXT,
    metadata TEXT,
    created_by INTEGER NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    deleted_at DATETIME
);

CREATE INDEX IF NOT EXISTS idx_credits_user_id ON billing_credits(user_id);
CREATE INDEX IF NOT EXISTS idx_credits_reference_id ON billing_credits(reference_id);
CREATE INDEX IF NOT EXISTS idx_credits_deleted_at ON billing_credits(deleted_at);

-- Credits Views

-- Active credits (non-deleted) view
CREATE VIEW IF NOT EXISTS billing_credits_active AS
SELECT
    id,
    user_id,
    amount,
    type,
    direction,
    reference_id,
    reference_type,
    description,
    metadata,
    created_by,
    created_at,
    updated_at
FROM billing_credits
WHERE deleted_at IS NULL;

-- User balance view (pre-computed balances)
CREATE VIEW IF NOT EXISTS billing_credits_balance AS
SELECT
    user_id,
    SUM(CASE WHEN direction = 'credit' THEN amount ELSE -amount END) as balance
FROM billing_credits
WHERE deleted_at IS NULL
GROUP BY user_id;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
PRAGMA foreign_keys=OFF;

DROP VIEW IF EXISTS billing_credits_balance;
DROP VIEW IF EXISTS billing_credits_active;

DROP INDEX IF EXISTS uniq_billing_subscribers_user_gateway_active;
DROP INDEX IF EXISTS idx_billing_subscribers_payment_status;
DROP INDEX IF EXISTS idx_billing_subscribers_cancelled_at;
DROP INDEX IF EXISTS idx_billing_subscribers_will_cancel_at;
DROP INDEX IF EXISTS idx_billing_subscribers_billing_period_end;
DROP INDEX IF EXISTS idx_billing_subscribers_billing_period_start;
DROP INDEX IF EXISTS idx_billing_subscribers_pricing_plan_period_id;
DROP INDEX IF EXISTS idx_billing_subscribers_external_id;
DROP INDEX IF EXISTS idx_billing_subscribers_is_active;
DROP INDEX IF EXISTS idx_billing_subscribers_gateway_type;
DROP INDEX IF EXISTS idx_billing_subscribers_user_id;
DROP TABLE billing_subscribers;

DROP INDEX IF EXISTS idx_billing_gateway_product_mappings_gateway_type;
DROP INDEX IF EXISTS idx_billing_gateway_product_mappings_pricing_plan_period_id;
DROP TABLE IF EXISTS billing_gateway_product_mappings;

DROP TABLE IF EXISTS billing_pricing_plan_periods;

DROP INDEX IF EXISTS idx_credits_deleted_at;
DROP INDEX IF EXISTS idx_credits_reference_id;
DROP INDEX IF EXISTS idx_credits_user_id;
DROP TABLE IF EXISTS billing_credits;

DROP INDEX IF EXISTS idx_billing_priceline_assignments_price_line_id;
DROP TABLE IF EXISTS billing_priceline_assignments;

DROP INDEX IF EXISTS idx_billing_priceline_plans_plan_id;
DROP INDEX IF EXISTS idx_billing_priceline_plans_price_line_id;
DROP TABLE IF EXISTS billing_priceline_plans;

DROP INDEX IF EXISTS idx_billing_pricelines_is_active;
DROP INDEX IF EXISTS idx_billing_pricelines_is_default;
DROP INDEX IF EXISTS idx_billing_pricelines_name;
DROP TABLE IF EXISTS billing_pricelines;

DROP INDEX IF EXISTS idx_billing_pricing_plans_is_public;
DROP INDEX IF EXISTS idx_billing_pricing_plans_is_active;
DROP INDEX IF EXISTS idx_billing_pricing_plans_name;
DROP TABLE billing_pricing_plans;

DROP INDEX IF EXISTS uniq_billing_webhook_events_gateway_event;
DROP INDEX IF EXISTS idx_billing_webhook_events_created_at;
DROP INDEX IF EXISTS idx_billing_webhook_events_processed_at;
DROP INDEX IF EXISTS idx_billing_webhook_events_event_id;
DROP INDEX IF EXISTS idx_billing_webhook_events_gateway_type;
DROP TABLE billing_webhook_events;

PRAGMA foreign_keys=ON;
-- +goose StatementEnd
