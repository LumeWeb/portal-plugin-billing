-- +goose Up
ALTER TABLE billing_x402_nonces ADD COLUMN gateway_payment_id VARCHAR(64) DEFAULT NULL;
CREATE INDEX idx_x402_gateway_payment ON billing_x402_nonces(gateway_payment_id, status, expires_at);

-- +goose Down
DROP INDEX IF EXISTS idx_x402_gateway_payment;
ALTER TABLE billing_x402_nonces DROP COLUMN gateway_payment_id;
