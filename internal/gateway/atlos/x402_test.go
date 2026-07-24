package atlos

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func TestIsX402Nonce(t *testing.T) {
	g := &AtlosGateway{}

	tests := []struct {
		name     string
		orderID  string
		expected bool
	}{
		{
			name:     "valid UUID lowercase",
			orderID:  "550e8400-e29b-41d4-a716-446655440000",
			expected: true,
		},
		{
			name:     "valid UUID uppercase",
			orderID:  "550E8400-E29B-41D4-A716-446655440000",
			expected: true,
		},
		{
			name:     "valid UUID mixed case",
			orderID:  "550e8400-E29B-41d4-a716-446655440000",
			expected: true,
		},
		{
			name:     "ATLOS HMAC order ID",
			orderID:  "userID:123:periodID:456:hmac:abc123",
			expected: false,
		},
		{
			name:     "too short",
			orderID:  "550e8400",
			expected: false,
		},
		{
			name:     "invalid characters (g)",
			orderID:  "550e8400-e29b-41d4-a716-44665544000g",
			expected: false,
		},
		{
			name:     "missing dashes",
			orderID:  "550e8400e29b41d4a716446655440000",
			expected: false,
		},
		{
			name:     "wrong segment lengths",
			orderID:  "550e8400-e29b-41d4-a716-44665544000",
			expected: false,
		},
		{
			name:     "empty string",
			orderID:  "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := g.isX402Nonce(tt.orderID)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestWebhookNonceCache(t *testing.T) {
	cache := NewWebhookNonceCache()

	cache.Set("nonce-123", &cachedPayment{
		TransactionId: "tx-456",
		PaidAmount:    decimal.NewFromInt(100),
	})

	payment, ok := cache.Get("nonce-123")
	assert.True(t, ok)
	assert.Equal(t, "tx-456", payment.TransactionId)
	assert.Equal(t, decimal.NewFromInt(100), payment.PaidAmount)

	_, ok = cache.Get("nonce-missing")
	assert.False(t, ok)
}
