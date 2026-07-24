CREATE TABLE billing_x402_nonces (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    nonce VARCHAR(64) UNIQUE NOT NULL,
    user_id INTEGER NOT NULL,
    amount DECIMAL(20,10) NOT NULL,
    gateway_type VARCHAR(32) NOT NULL,
    -- gateway_type stored but not defaulted; x402 handler sets via const DefaultGatewayType
    status VARCHAR(16) NOT NULL DEFAULT 'pending',
    expires_at DATETIME NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    settled_at DATETIME
);

CREATE INDEX idx_nonce_status_expires ON billing_x402_nonces(nonce, status, expires_at);
