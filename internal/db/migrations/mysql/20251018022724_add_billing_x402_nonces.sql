CREATE TABLE billing_x402_nonces (
    id INT PRIMARY KEY AUTO_INCREMENT,
    nonce VARCHAR(64) UNIQUE NOT NULL,
    user_id INT NOT NULL,
    amount DECIMAL(20,10) NOT NULL,
    gateway_type VARCHAR(32) NOT NULL,
    -- gateway_type stored but not defaulted; x402 handler sets via const DefaultGatewayType
    status VARCHAR(16) NOT NULL DEFAULT 'pending',
    expires_at DATETIME NOT NULL,
    created_at DATETIME DEFAULT NOW(),
    settled_at DATETIME,
    INDEX idx_nonce_status_expires (nonce, status, expires_at)
);
