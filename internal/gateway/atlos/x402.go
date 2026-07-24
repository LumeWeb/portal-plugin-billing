package atlos

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/shopspring/decimal"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
)

// Compile-time check
var _ pluginCore.PaymentProcessor = (*AtlosGateway)(nil)

// WebhookNonceCache stores nonce → payment mapping from ATLOS postbacks
type WebhookNonceCache struct {
	mu      sync.RWMutex
	entries map[string]*cachedPayment
}

type cachedPayment struct {
	TransactionId string
	PaidAmount    decimal.Decimal
	PaidAt        time.Time
}

// NewWebhookNonceCache creates a new in-memory webhook nonce cache
func NewWebhookNonceCache() *WebhookNonceCache {
	return &WebhookNonceCache{entries: make(map[string]*cachedPayment)}
}

// Set stores a payment record for a nonce
func (c *WebhookNonceCache) Set(nonce string, payment *cachedPayment) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[nonce] = payment
}

// Get retrieves a payment record by nonce
func (c *WebhookNonceCache) Get(nonce string) (*cachedPayment, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	p, ok := c.entries[nonce]
	return p, ok
}

// VerifyPaymentSignature verifies EIP-712 signature locally via ecrecover.
func (g *AtlosGateway) VerifyPaymentSignature(ctx context.Context, nonce string, payer string, signature string, amount decimal.Decimal) error {
	// TODO: implement ecrecover using go-ethereum/crypto
	// Reconstruct EIP-712 digest
	// Verify recovered address matches payer
	// Return error if mismatch
	return nil
}

// ConfirmPayment checks if ATLOS has received payment for this nonce.
func (g *AtlosGateway) ConfirmPayment(ctx context.Context, nonce string, expectedAmount decimal.Decimal) (*pluginCore.PaymentConfirmation, error) {
	payment, ok := g.webhookCache.Get(nonce)
	if !ok {
		return nil, pluginCore.ErrPaymentPending
	}

	if !payment.PaidAmount.Equal(expectedAmount) {
		return nil, fmt.Errorf("amount mismatch: expected %s, got %s", expectedAmount, payment.PaidAmount)
	}

	return &pluginCore.PaymentConfirmation{
		Amount:    payment.PaidAmount,
		Currency:  "USD",
		Reference: payment.TransactionId,
	}, nil
}
