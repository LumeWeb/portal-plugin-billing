-- +goose Up
CREATE TABLE billing_x402_nonces (
    id INT PRIMARY KEY AUTO_INCREMENT,
    nonce VARCHAR(66) UNIQUE NOT NULL,
    user_id INT NOT NULL,
    amount DECIMAL(20,10) NOT NULL,
    wallet VARCHAR(64) NOT NULL,
    gateway_type VARCHAR(32) NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'pending',
    expires_at DATETIME NOT NULL,
    created_at DATETIME DEFAULT NOW(),
    settled_at DATETIME,
    gateway_payment_id VARCHAR(64) DEFAULT NULL,
    reference VARCHAR(128) DEFAULT NULL,
    challenge_accepts TEXT DEFAULT NULL
);

CREATE INDEX idx_nonce_status_expires ON billing_x402_nonces(nonce, status, expires_at);
CREATE INDEX idx_x402_gateway_payment ON billing_x402_nonces(gateway_payment_id, status, expires_at);

CREATE TABLE billing_x402_payment_addresses (
    id INT PRIMARY KEY AUTO_INCREMENT,
    nonce VARCHAR(66) NOT NULL,
    payment_id VARCHAR(64) NOT NULL UNIQUE,
    wallet_address VARCHAR(128) NOT NULL,
    asset_code VARCHAR(32) NOT NULL,
    blockchain_code BIGINT NOT NULL,
    amount VARCHAR(64) NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_billing_x402_payment_addresses_nonce ON billing_x402_payment_addresses(nonce);

-- +goose Down
DROP TABLE billing_x402_payment_addresses;
DROP TABLE billing_x402_nonces;
