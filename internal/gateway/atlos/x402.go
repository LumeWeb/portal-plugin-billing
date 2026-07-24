package atlos

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/shopspring/decimal"
	atlos "go.lumeweb.com/atlos-sdk"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	"go.uber.org/zap"
)

// Compile-time checks
var (
	_ pluginCore.PaymentProcessor       = (*AtlosGateway)(nil)
	_ pluginCore.PaymentAddressProvider = (*AtlosGateway)(nil)
)

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

// NewWebhookNonceCache creates a new in-memory webhook nonce cache.
func NewWebhookNonceCache() *WebhookNonceCache {
	return &WebhookNonceCache{entries: make(map[string]*cachedPayment)}
}

// Set stores a payment record for a nonce.
func (c *WebhookNonceCache) Set(nonce string, payment *cachedPayment) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[nonce] = payment
}

// Get retrieves a payment record by nonce.
func (c *WebhookNonceCache) Get(nonce string) (*cachedPayment, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	p, ok := c.entries[nonce]
	return p, ok
}

// AtlosPaymentInfo stores the ATLOS payment details returned from CreatePayment API.
type AtlosPaymentInfo struct {
	PaymentID       string
	WalletAddress   string
	AssetCode       string
	BlockchainCode  float32
	Amount          string // smallest unit, no decimal point
}

// CreatePaymentAddress creates an ATLOS payment and returns the receiving wallet address.
func (g *AtlosGateway) CreatePaymentAddress(ctx context.Context, assetCode string, blockchainCode float32, amount decimal.Decimal, nonce string) (*pluginCore.PaymentAddress, error) {
	client, err := g.newAtlosClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create ATLOS client: %w", err)
	}

	// Convert amount to smallest unit string (no decimal point)
	// For USDC with 6 decimals, $10.50 becomes "10500000"
	amountStr := amount.Mul(decimal.NewFromInt(1e6)).Truncate(0).String()

	// Create the payment with an invoice that contains the nonce as OrderId
	// This allows ATLOS webhooks to correlate by nonce directly
	invoiceResp, err := client.InvoiceCreate(ctx, atlos.InvoiceCreatePostRequest{
		MerchantId:    g.config.MerchantID,
		OrderAmount:   float32(amount.InexactFloat64()),
		OrderCurrency: strPtr("USD"),
		OrderId:       &nonce,
		Memo:          strPtr(fmt.Sprintf("x402 payment for nonce %s", nonce)),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create ATLOS invoice: %w", err)
	}

	invoiceId := *invoiceResp.Id

	// Now create the payment against this invoice
	payment, err := client.CreatePayment(ctx, atlos.CreatePaymentPostRequest{
		AssetCode:      assetCode,
		BlockchainCode: blockchainCode,
		InvoiceId:      invoiceId,
		IsEvm:          "1",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create ATLOS payment: %w", err)
	}

	g.coreCtx.Logger().Info("ATLOS payment created for x402",
		zap.String("nonce", nonce),
		zap.String("payment_id", *payment.Id),
		zap.String("wallet_address", *payment.RecipientAddress),
		zap.String("asset", assetCode),
		zap.Float32("blockchain", blockchainCode),
	)

	return &pluginCore.PaymentAddress{
		PaymentID:      *payment.Id,
		WalletAddress:  *payment.RecipientAddress,
		AssetCode:      assetCode,
		BlockchainCode: blockchainCode,
		Amount:         amountStr,
	}, nil
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

// isX402Nonce checks if an order ID is an x402 nonce (UUID format).
func (g *AtlosGateway) isX402Nonce(orderID string) bool {
	if len(orderID) != 36 {
		return false
	}
	parts := strings.Split(orderID, "-")
	if len(parts) != 5 {
		return false
	}
	expectedLens := []int{8, 4, 4, 4, 12}
	for i, part := range parts {
		if len(part) != expectedLens[i] {
			return false
		}
		for _, c := range part {
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
				return false
			}
		}
	}
	return true
}

// handleX402Webhook handles ATLOS webhooks for x402 payments.
// The OrderId in the postback is our invoice ID. We correlate via the nonce
// stored in the invoice memo or via the ATLOS payment ID in our nonce store.
func (g *AtlosGateway) handleX402Webhook(ctx context.Context, notification atlos.PostbackNotification) error {
	paidAmount := decimal.NewFromFloat(notification.PaidAmount)

	// Try to use OrderId as nonce (for direct correlation)
	// or look up by OrderId if it's an invoice ID
	nonce := notification.OrderId
	if !g.isX402Nonce(nonce) {
		// OrderId is not a nonce (e.g., it's an invoice ID)
		// The x402 flow should set OrderId to our nonce via the invoice
		// For now, skip non-x402 orders
		g.coreCtx.Logger().Debug("webhook OrderId is not an x402 nonce, skipping cache",
			zap.String("order_id", nonce),
		)
		return nil
	}

	g.webhookCache.Set(nonce, &cachedPayment{
		TransactionId: notification.TransactionId,
		PaidAmount:    paidAmount,
		PaidAt:        time.Now(),
	})

	g.coreCtx.Logger().Info("x402 payment cached from webhook",
		zap.String("nonce", nonce),
		zap.String("transaction_id", notification.TransactionId),
		zap.String("paid_amount", paidAmount.String()),
	)

	return nil
}

func strPtr(s string) *string {
	return &s
}
