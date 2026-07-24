-- +goose Up
ALTER TABLE billing_x402_nonces ADD COLUMN reference VARCHAR(128) DEFAULT NULL;

-- +goose Down
ALTER TABLE billing_x402_nonces DROP COLUMN reference;
