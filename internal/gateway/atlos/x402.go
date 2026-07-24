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

// x402NonceDB mirrors the billing_x402_nonces table for direct DB queries.
type x402NonceDB struct {
	ID               uint
	Nonce            string
	GatewayPaymentID *string
	UserID           uint
	Amount           decimal.Decimal `gorm:"type:decimal(20,10)"`
	GatewayType      string
	Status           string
	Reference        string
	ExpiresAt        time.Time
	CreatedAt        time.Time
	SettledAt        *time.Time
}

func (x402NonceDB) TableName() string { return "billing_x402_nonces" }

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
// Queries the database first (source of truth), falls back to in-memory webhook cache.
func (g *AtlosGateway) ConfirmPayment(ctx context.Context, nonce string, expectedAmount decimal.Decimal) (*pluginCore.PaymentConfirmation, error) {
	// Primary: check database for settled status
	var record x402NonceDB
	err := g.coreCtx.DB().WithContext(ctx).
		Where("nonce = ? AND status = ?", nonce, "settled").
		First(&record).Error
	if err == nil {
		return &pluginCore.PaymentConfirmation{
			Amount:    record.Amount,
			Currency:  "USD",
			Reference: record.Reference,
		}, nil
	}

	// Fallback: check in-memory webhook cache (for settled-before-DB scenarios during migration)
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
// Marks the nonce as settled in the database and caches for in-memory lookup.
func (g *AtlosGateway) handleX402Webhook(ctx context.Context, notification atlos.PostbackNotification) error {
	paidAmount := decimal.NewFromFloat(notification.PaidAmount)

	nonce := notification.OrderId
	if !g.isX402Nonce(nonce) {
		g.coreCtx.Logger().Debug("webhook OrderId is not an x402 nonce, skipping",
			zap.String("order_id", nonce),
		)
		return nil
	}

	// Mark nonce as settled in database (source of truth)
	result := g.coreCtx.DB().WithContext(ctx).
		Model(&x402NonceDB{}).
		Where("nonce = ? AND status = ?", nonce, "pending").
		Updates(map[string]interface{}{
			"status":     "settled",
			"settled_at": time.Now(),
			"reference":  notification.TransactionId,
		})
	if result.Error != nil {
		g.coreCtx.Logger().Error("failed to settle x402 nonce in DB",
			zap.String("nonce", nonce),
			zap.Error(result.Error),
		)
		return fmt.Errorf("failed to settle nonce: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		g.coreCtx.Logger().Warn("x402 webhook received for unknown or already-settled nonce",
			zap.String("nonce", nonce),
		)
	}

	// Also cache in-memory for fast lookup (optional, cache is secondary)
	g.webhookCache.Set(nonce, &cachedPayment{
		TransactionId: notification.TransactionId,
		PaidAmount:    paidAmount,
		PaidAt:        time.Now(),
	})

	g.coreCtx.Logger().Info("x402 payment settled",
		zap.String("nonce", nonce),
		zap.String("transaction_id", notification.TransactionId),
		zap.String("paid_amount", paidAmount.String()),
		zap.Int64("rows_affected", result.RowsAffected),
	)

	return nil
}

func strPtr(s string) *string {
	return &s
}
