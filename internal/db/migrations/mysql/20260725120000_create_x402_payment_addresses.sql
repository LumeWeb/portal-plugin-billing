-- +goose Up
CREATE TABLE IF NOT EXISTS billing_x402_payment_addresses (
    id INTEGER PRIMARY KEY AUTO_INCREMENT,
    nonce VARCHAR(64) NOT NULL,
    payment_id VARCHAR(64) NOT NULL UNIQUE,
    wallet_address VARCHAR(128) NOT NULL,
    asset_code VARCHAR(16) NOT NULL,
    blockchain_code FLOAT NOT NULL,
    amount VARCHAR(64) NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_billing_x402_payment_addresses_nonce ON billing_x402_payment_addresses(nonce);

-- +goose Down
DROP TABLE IF EXISTS billing_x402_payment_addresses;
