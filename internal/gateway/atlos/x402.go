package atlos

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/shopspring/decimal"
	atlos "go.lumeweb.com/atlos-sdk"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	"go.lumeweb.com/portal-plugin-billing/internal/x402"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// x402Nonce is a minimal GORM model for the billing_x402_nonces table.
type x402Nonce struct {
	ID               uint
	Nonce            string
	GatewayPaymentID *string
	UserID           uint
	Amount           decimal.Decimal `gorm:"type:decimal(20,10)"`
	Wallet           string          `gorm:"size:64;not null"`
	GatewayType      string          `gorm:"size:32;not null"`
	Status           string
	Reference        string
	ChallengeAccepts string
	ExpiresAt        time.Time
	CreatedAt        time.Time
	SettledAt       *time.Time
}

func (x402Nonce) TableName() string { return "billing_x402_nonces" }

// Compile-time checks
var (
	_ pluginCore.PaymentProcessor            = (*AtlosGateway)(nil)
	_ pluginCore.PaymentAddressProvider      = (*AtlosGateway)(nil)
	_ pluginCore.BatchPaymentAddressProvider = (*AtlosGateway)(nil)
)

// WebhookNonceCache is a simple in-memory TTL cache for x402 webhook payment
// lookups. It bridges the gap between the ATLOS webhook (async) and the
// client's ConfirmPayment call (sync, seconds later).
//
// A plain map+mutex is intentional — not an LRU. Nonces are UUIDs with a 5-min
// DB expiry. The map self-bounds: entries are deleted on Get if expired, and
// the nonce is consumed (deleted from DB) on successful checkout. Even under
// a flood of unconsumed nonces, the 10-min TTL caps growth at
// (request_rate × 10min) entries, and the rate limiter (10 req/min/IP) gates
// that further. An LRU bound of 10000 would never be reached in practice.
type WebhookNonceCache struct {
	mu      sync.Mutex
	entries map[string]*cachedPayment
}

type cachedPayment struct {
	TransactionId string
	PaidAmount    decimal.Decimal
	PaidAt        time.Time
	expiresAt     time.Time
}

func NewWebhookNonceCache() *WebhookNonceCache {
	return &WebhookNonceCache{
		entries: make(map[string]*cachedPayment),
	}
}

func (c *WebhookNonceCache) Set(nonce string, payment *cachedPayment) {
	c.mu.Lock()
	defer c.mu.Unlock()
	payment.expiresAt = time.Now().Add(10 * time.Minute)
	c.entries[nonce] = payment
}

func (c *WebhookNonceCache) Get(nonce string) (*cachedPayment, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	p, ok := c.entries[nonce]
	if !ok {
		return nil, false
	}
	if time.Now().After(p.expiresAt) {
		delete(c.entries, nonce)
		return nil, false
	}
	return p, true
}

type AtlosPaymentInfo struct {
	PaymentID      string
	WalletAddress  string
	AssetCode      string
	BlockchainCode int64
	Amount         string // smallest unit, no decimal point
}

// SupportedAssets returns the cached list of stablecoin assets supported by
// ATLOS for EVM chains. The cache has a 5-minute TTL.
//
// A fetch mutex (supportedAssetsFetchMux) prevents a cache stampede: when the
// TTL expires, the first caller acquires the fetch lock and refreshes. Other
// callers wait, then read the freshly-cached result. This is simpler than
// singleflight and sufficient for a rate-limited 5-min TTL cache.
func (g *AtlosGateway) SupportedAssets(ctx context.Context) ([]pluginCore.SupportedAsset, error) {
	// Fast path: read from cache under RLock.
	g.supportedAssetsMux.RLock()
	if g.supportedAssets != nil && time.Since(g.supportedAssetsAt) < supportedAssetsTTL {
		assets := g.supportedAssets
		g.supportedAssetsMux.RUnlock()
		return assets, nil
	}
	g.supportedAssetsMux.RUnlock()

	// Slow path: acquire fetch lock so only one goroutine calls ATLOS.
	g.supportedAssetsFetchMux.Lock()
	defer g.supportedAssetsFetchMux.Unlock()

	// Double-check after acquiring the fetch lock — another goroutine may
	// have already refreshed while we were waiting.
	g.supportedAssetsMux.RLock()
	if g.supportedAssets != nil && time.Since(g.supportedAssetsAt) < supportedAssetsTTL {
		assets := g.supportedAssets
		g.supportedAssetsMux.RUnlock()
		return assets, nil
	}
	g.supportedAssetsMux.RUnlock()

	// Cache miss or expired — fetch from ATLOS.
	client, err := g.newAtlosClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create ATLOS client: %w", err)
	}

	assets, err := client.AssetList(ctx, atlos.AssetListPostRequest{
		MerchantId:    g.config.MerchantID,
		OrderAmount:   1.00,
		OrderCurrency: strPtr("USD"),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to fetch asset list from ATLOS: %w", err)
	}

	var result []pluginCore.SupportedAsset
	for _, asset := range assets {
		if asset.Code == nil || asset.IsStable == nil || !*asset.IsStable {
			continue
		}
		if asset.IsToken == nil || !*asset.IsToken {
			continue
		}
		if asset.Blockchains == nil {
			continue
		}
		for _, chain := range *asset.Blockchains {
			if chain.Code == nil || chain.ChainId == nil || chain.IsEvm == nil || !*chain.IsEvm {
				continue
			}
			tokenAddr := ""
			if chain.TokenAddress != nil {
				tokenAddr = *chain.TokenAddress
			}
			var decimals int32
			if chain.Decimals != nil {
				decimals = int32(*chain.Decimals)
			}
			chainName := ""
			if chain.Name != nil {
				chainName = *chain.Name
			}
			result = append(result, pluginCore.SupportedAsset{
				AssetCode:      strings.ToLower(*asset.Code),
				AssetName:      ptrStr(asset.Name),
				BlockchainCode: int64(*chain.ChainId),
				BlockchainName: chainName,
				TokenAddress:   tokenAddr,
				TokenVersion:   tokenEIP712Version(*asset.Code),
				Decimals:       decimals,
				IsStable:       true,
			})
		}
	}

	// Cache the result.
	g.supportedAssetsMux.Lock()
	g.supportedAssets = result
	g.supportedAssetsAt = time.Now()
	g.supportedAssetsMux.Unlock()

	return result, nil
}

func (g *AtlosGateway) CreatePaymentAddress(ctx context.Context, assetCode string, blockchainCode int64, amount decimal.Decimal, nonce string) (*pluginCore.PaymentAddress, error) {
	client, err := g.newAtlosClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create ATLOS client: %w", err)
	}

	decimals, err := g.findDecimals(ctx, assetCode, blockchainCode)
	if err != nil {
		return nil, fmt.Errorf("failed to determine decimals: %w", err)
	}
	amountStr := amount.Mul(decimal.NewFromInt(10).Pow(decimal.NewFromInt(int64(decimals)))).Truncate(0).String()

	// Prefix with "x402-" so isX402OrderID can reliably distinguish x402
	// orders from other ATLOS subscription order IDs.
	orderID := fmt.Sprintf("x402-%s-%s-%d", nonce, strings.ToLower(assetCode), blockchainCode)

	// Parse the USD amount as float32 for ATLOS. Truncate to 2 decimals
	// first to avoid floating-point noise from sub-cent digits.
	f, _ := amount.Truncate(2).Float64()
	orderAmount := float32(f)

	invoiceResp, err := client.InvoiceCreate(ctx, atlos.InvoiceCreatePostRequest{
		MerchantId:    g.config.MerchantID,
		OrderAmount:   orderAmount,
		OrderCurrency: strPtr("USD"),
		OrderId:       &orderID,
		Memo:          strPtr(fmt.Sprintf("x402 payment for nonce %s", nonce)),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create ATLOS invoice: %w", err)
	}
	if invoiceResp.Id == nil {
		return nil, fmt.Errorf("ATLOS invoice response missing id")
	}

	invoiceId := *invoiceResp.Id

	payment, err := client.CreatePayment(ctx, atlos.CreatePaymentPostRequest{
		AssetCode:      assetCode,
		BlockchainCode: float32(blockchainCode),
		InvoiceId:      invoiceId,
		IsEvm:          "1",
	})
	if err != nil {
		// Cancel the orphaned invoice before returning.
		if cancelErr := client.InvoiceCancel(ctx, atlos.InvoiceCancelPostRequest{InvoiceId: invoiceResp.Id}); cancelErr != nil {
			g.coreCtx.Logger().Error("failed to cancel orphaned ATLOS invoice after payment creation error",
				zap.String("invoice_id", invoiceId),
				zap.Error(cancelErr),
			)
		}
		return nil, fmt.Errorf("failed to create ATLOS payment: %w", err)
	}
	if payment.Id == nil || payment.RecipientAddress == nil {
		if cancelErr := client.InvoiceCancel(ctx, atlos.InvoiceCancelPostRequest{InvoiceId: invoiceResp.Id}); cancelErr != nil {
			g.coreCtx.Logger().Error("failed to cancel ATLOS invoice after missing payment fields",
				zap.String("invoice_id", invoiceId),
				zap.Error(cancelErr),
			)
		}
		return nil, fmt.Errorf("ATLOS payment response missing required fields")
	}

	g.coreCtx.Logger().Info("ATLOS payment created for x402",
		zap.String("nonce", nonce),
		zap.String("payment_id", *payment.Id),
		zap.String("wallet_address", *payment.RecipientAddress),
		zap.String("asset", assetCode),
		zap.Int64("blockchain", blockchainCode),
	)

	return &pluginCore.PaymentAddress{
		PaymentID:      *payment.Id,
		InvoiceID:      invoiceId,
		WalletAddress:  *payment.RecipientAddress,
		AssetCode:      assetCode,
		BlockchainCode: blockchainCode,
		Amount:         amountStr,
	}, nil
}

// CancelPaymentAddress cancels a previously-created payment session by
// cancelling the associated ATLOS invoice.
func (g *AtlosGateway) CancelPaymentAddress(ctx context.Context, invoiceID string) error {
	if invoiceID == "" {
		return nil
	}
	client, err := g.newAtlosClient()
	if err != nil {
		return fmt.Errorf("failed to create ATLOS client: %w", err)
	}
	return client.InvoiceCancel(ctx, atlos.InvoiceCancelPostRequest{
		InvoiceId: strPtr(invoiceID),
	})
}

// CreatePaymentAddresses creates a single ATLOS invoice for the nonce, then
// creates per-asset payment sessions against that one invoice. This avoids
// creating N orphaned invoices when only one asset is actually paid.
func (g *AtlosGateway) CreatePaymentAddresses(ctx context.Context, assets []pluginCore.SupportedAsset, amount decimal.Decimal, nonce string) ([]*pluginCore.PaymentAddress, error) {
	client, err := g.newAtlosClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create ATLOS client: %w", err)
	}

	// Create a single invoice for this nonce.
	f, _ := amount.Truncate(2).Float64()
	orderAmount := float32(f)
	invoiceResp, err := client.InvoiceCreate(ctx, atlos.InvoiceCreatePostRequest{
		MerchantId:    g.config.MerchantID,
		OrderAmount:   orderAmount,
		OrderCurrency: strPtr("USD"),
		OrderId:       strPtr(fmt.Sprintf("x402-%s", nonce)),
		Memo:          strPtr(fmt.Sprintf("x402 payment for nonce %s", nonce)),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create ATLOS invoice: %w", err)
	}
	if invoiceResp.Id == nil {
		return nil, fmt.Errorf("ATLOS invoice response missing id")
	}
	invoiceId := *invoiceResp.Id

	// Create per-asset payment sessions against the single invoice.
	addresses := make([]*pluginCore.PaymentAddress, len(assets))
	for i, asset := range assets {
		decimals, err := g.findDecimals(ctx, asset.AssetCode, asset.BlockchainCode)
		if err != nil {
			// Cancel the shared invoice before returning.
			if cancelErr := client.InvoiceCancel(ctx, atlos.InvoiceCancelPostRequest{InvoiceId: &invoiceId}); cancelErr != nil {
				g.coreCtx.Logger().Error("failed to cancel ATLOS invoice after findDecimals error",
					zap.String("invoice_id", invoiceId),
					zap.Error(cancelErr),
				)
			}
			return nil, fmt.Errorf("failed to determine decimals for %s on chain %v: %w", asset.AssetCode, asset.BlockchainCode, err)
		}
		amountStr := amount.Mul(decimal.NewFromInt(10).Pow(decimal.NewFromInt(int64(decimals)))).Truncate(0).String()

		payment, err := client.CreatePayment(ctx, atlos.CreatePaymentPostRequest{
			AssetCode:      asset.AssetCode,
			BlockchainCode: float32(asset.BlockchainCode),
			InvoiceId:      invoiceId,
			IsEvm:          "1",
		})
		if err != nil {
			// Rollback: cancel the invoice so ATLOS doesn't leave orphan
			// payment sessions from previously-created entries.
			if cancelErr := client.InvoiceCancel(ctx, atlos.InvoiceCancelPostRequest{
				InvoiceId: &invoiceId,
			}); cancelErr != nil {
				g.coreCtx.Logger().Error("failed to cancel ATLOS invoice after payment creation error",
					zap.String("invoice_id", invoiceId),
					zap.Error(cancelErr),
				)
			}
			return nil, fmt.Errorf("failed to create ATLOS payment for %s on chain %v: %w", asset.AssetCode, asset.BlockchainCode, err)
		}
		if payment.Id == nil || payment.RecipientAddress == nil {
			if cancelErr := client.InvoiceCancel(ctx, atlos.InvoiceCancelPostRequest{InvoiceId: &invoiceId}); cancelErr != nil {
				g.coreCtx.Logger().Error("failed to cancel ATLOS invoice after missing payment fields",
					zap.String("invoice_id", invoiceId),
					zap.Error(cancelErr),
				)
			}
			return nil, fmt.Errorf("ATLOS payment response missing required fields for %s on chain %v", asset.AssetCode, asset.BlockchainCode)
		}

		g.coreCtx.Logger().Info("ATLOS payment created for x402",
			zap.String("nonce", nonce),
			zap.String("invoice_id", invoiceId),
			zap.String("payment_id", *payment.Id),
			zap.String("wallet_address", *payment.RecipientAddress),
			zap.String("asset", asset.AssetCode),
			zap.Int64("blockchain", asset.BlockchainCode),
		)

		addresses[i] = &pluginCore.PaymentAddress{
			PaymentID:      *payment.Id,
			InvoiceID:      invoiceId,
			WalletAddress:  *payment.RecipientAddress,
			AssetCode:      asset.AssetCode,
			BlockchainCode: asset.BlockchainCode,
			Amount:         amountStr,
		}
	}

	return addresses, nil
}

func (g *AtlosGateway) findDecimals(ctx context.Context, assetCode string, blockchainCode int64) (int32, error) {
	assets, err := g.SupportedAssets(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to fetch supported assets: %w", err)
	}
	for _, asset := range assets {
		if strings.EqualFold(asset.AssetCode, assetCode) && asset.BlockchainCode == blockchainCode {
			return asset.Decimals, nil
		}
	}
	return 0, fmt.Errorf("asset %s on chain %d not supported", assetCode, blockchainCode)
}

func ptrStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// tokenEIP712Version returns the EIP-712 domain separator version for a
// token contract. This is read from the token's EIP712Domain() on-chain.
// ATLOS SDK does not expose this field, so we maintain a lookup for known
// stablecoin contracts. Unknown tokens default to "1".
var knownTokenVersions = map[string]string{
	"usdc": "2",
	"usdt": "1",
}

func tokenEIP712Version(assetCode string) string {
	if v, ok := knownTokenVersions[strings.ToLower(assetCode)]; ok {
		return v
	}
	return "1"
}

func (g *AtlosGateway) ConfirmPayment(ctx context.Context, nonce string, expectedAmount decimal.Decimal) (*pluginCore.PaymentConfirmation, error) {
	// Check for mismatch status first — if ATLOS paid a different amount than
	// challenged, the webhook sets status to "mismatch". Return a permanent
	// error so the client stops retrying (funds are captured by ATLOS).
	var mismatchRecord x402Nonce
	err := g.coreCtx.DB().WithContext(ctx).
		Where("nonce = ? AND status = ?", nonce, x402.NonceStatusMismatch).
		First(&mismatchRecord).Error
	if err == nil {
		return nil, fmt.Errorf("payment amount mismatch: expected %s, ATLOS paid a different amount (ref %s)",
			expectedAmount.String(), mismatchRecord.Reference)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("failed to query mismatch status: %w", err)
	}

	var record x402Nonce
	err = g.coreCtx.DB().WithContext(ctx).
		Where("nonce = ? AND status = ?", nonce, x402.NonceStatusSettled).
		First(&record).Error
	if err == nil {
		tolerance := decimal.NewFromFloat(0.01)
		if record.Amount.Sub(expectedAmount).Abs().GreaterThan(tolerance) {
			return nil, fmt.Errorf("amount mismatch: expected %s, got %s", expectedAmount, record.Amount.String())
		}
		return &pluginCore.PaymentConfirmation{
			Amount:    record.Amount,
			Currency:  "USD",
			Reference: record.Reference,
		}, nil
	}
	// Only fall through to cache on "not found" — other DB errors are transient.
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("failed to query settled nonce: %w", err)
	}

	payment, ok := g.webhookCache.Get(nonce)
	if !ok {
		return nil, pluginCore.ErrPaymentPending
	}

	tolerance := decimal.NewFromFloat(0.01)
	if payment.PaidAmount.Sub(expectedAmount).Abs().GreaterThan(tolerance) {
		return nil, fmt.Errorf("amount mismatch: expected %s, got %s", expectedAmount, payment.PaidAmount)
	}

	return &pluginCore.PaymentConfirmation{
		Amount:    payment.PaidAmount,
		Currency:  "USD",
		Reference: payment.TransactionId,
	}, nil
}

// isX402OrderID checks if an ATLOS OrderId originated from x402.
// All x402 order IDs are prefixed with "x402-". Two formats:
//   - Batch: "x402-0x<64hex>" (nonce only)
//   - Per-asset: "x402-0x<64hex>-<assetCode>-<blockchainCode>"
//
// The nonce is a 0x-prefixed 32-byte hex value (no dashes), so splitting
// by "-" gives 1 part (batch) or 3 parts (per-asset).
func (g *AtlosGateway) isX402OrderID(orderID string) bool {
	const x402Prefix = "x402-"
	if !strings.HasPrefix(orderID, x402Prefix) {
		return false
	}
	rest := orderID[len(x402Prefix):]
	parts := strings.Split(rest, "-")
	if len(parts) != 1 && len(parts) != 3 {
		return false
	}
	// Part 0: nonce — must be 0x-prefixed hex (66 chars total)
	nonce := parts[0]
	if !strings.HasPrefix(nonce, "0x") || len(nonce) != 66 {
		return false
	}
	for _, c := range nonce[2:] {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	// If 3 parts, validate the trailing asset/blockchain tokens.
	if len(parts) == 3 {
		// Part 1: assetCode — alphanumeric lowercase (e.g. "usdc")
		assetCode := parts[1]
		if len(assetCode) == 0 {
			return false
		}
		for _, c := range assetCode {
			if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')) {
				return false
			}
		}
		// Part 2: blockchainCode — numeric chain ID (e.g. "1", "137")
		blockchainCode := parts[2]
		if len(blockchainCode) == 0 {
			return false
		}
		for _, c := range blockchainCode {
			if c < '0' || c > '9' {
				return false
			}
		}
	}
	return true
}

// nonceFromOrderID extracts the nonce from an x402 OrderId.
// Strips the "x402-" prefix, then handles both batch and per-asset formats.
func (g *AtlosGateway) nonceFromOrderID(orderID string) string {
	const x402Prefix = "x402-"
	rest := strings.TrimPrefix(orderID, x402Prefix)
	// The nonce is the first "-"-delimited segment (0x-prefixed hex, no dashes).
	idx := strings.Index(rest, "-")
	if idx == -1 {
		return rest
	}
	return rest[:idx]
}

func (g *AtlosGateway) handleX402Webhook(ctx context.Context, notification atlos.PostbackNotification) error {
	// Validate PaidAmount: reject NaN, Inf, or non-positive values that would
	// corrupt decimal comparisons and ledger entries.
	if math.IsNaN(notification.PaidAmount) || math.IsInf(notification.PaidAmount, 0) {
		g.coreCtx.Logger().Error("x402 webhook received invalid PaidAmount (NaN/Inf)",
			zap.String("order_id", notification.OrderId),
			zap.Float64("paid_amount", notification.PaidAmount),
		)
		return fmt.Errorf("invalid paid amount: NaN or Inf")
	}
	if notification.PaidAmount <= 0 {
		g.coreCtx.Logger().Error("x402 webhook received non-positive PaidAmount",
			zap.String("order_id", notification.OrderId),
			zap.Float64("paid_amount", notification.PaidAmount),
		)
		return fmt.Errorf("invalid paid amount: must be positive")
	}

	paidAmount := decimal.NewFromFloat(notification.PaidAmount).Truncate(2)

	nonce := g.nonceFromOrderID(notification.OrderId)
	if !g.isX402OrderID(notification.OrderId) {
		g.coreCtx.Logger().Debug("webhook OrderId is not an x402 order, skipping",
			zap.String("order_id", notification.OrderId),
		)
		return nil
	}

	// Load the nonce to verify the paid amount matches the expected amount.
	var record x402Nonce
	err := g.coreCtx.DB().WithContext(ctx).
		Where("nonce = ? AND status = ?", nonce, x402.NonceStatusPending).
		First(&record).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			g.coreCtx.Logger().Warn("x402 webhook received for unknown or already-settled nonce",
				zap.String("nonce", nonce),
			)
		} else {
			// Transient DB error — return error so ATLOS retries the webhook.
			g.coreCtx.Logger().Error("x402 webhook DB query failed",
				zap.String("nonce", nonce),
				zap.Error(err),
			)
			return fmt.Errorf("failed to query nonce: %w", err)
		}
		return nil
	}

	// Verify paid amount matches the expected amount (within 0.01 tolerance).
	tolerance := decimal.NewFromFloat(0.01)
	if record.Amount.Sub(paidAmount).Abs().GreaterThan(tolerance) {
		g.coreCtx.Logger().Error("x402 webhook paid amount mismatch — recording mismatch",
			zap.String("nonce", nonce),
			zap.String("expected", record.Amount.String()),
			zap.String("paid", paidAmount.String()),
		)
		// Mark the nonce as mismatched so ConfirmPayment returns a permanent
		// error instead of ErrPaymentPending forever. Returning nil acks the
		// webhook so ATLOS doesn't retry indefinitely.
		if err := g.coreCtx.DB().WithContext(ctx).
			Model(&x402Nonce{}).
			Where("nonce = ? AND status = ?", nonce, x402.NonceStatusPending).
			Updates(map[string]interface{}{
				"status":    x402.NonceStatusMismatch,
				"reference": notification.TransactionId,
			}).Error; err != nil {
			g.coreCtx.Logger().Error("failed to record amount mismatch in DB",
				zap.String("nonce", nonce),
				zap.Error(err),
			)
			// Return error so ATLOS retries — the record needs to be updated.
			return fmt.Errorf("failed to record amount mismatch: %w", err)
		}
		return nil
	}

	// Use a transaction to atomically settle the nonce. Credit issuance uses
	// the original ctx (not tx) because IssueCreditWithIdempotency is a
	// service-layer method that manages its own DB access and cannot accept
	// a *gorm.DB. This is safe because:
	//   1. The idempotency key (x402-{nonce}) prevents double-credit on retry.
	//   2. If tx.Commit fails after credit, the nonce stays pending. ATLOS
	//      retries the webhook, the idempotency key blocks the second credit,
	//      and the WHERE status='pending' guard blocks the second settle.
	//   3. If IssueCreditWithIdempotency fails, tx.Rollback undoes the settle.
	tx := g.coreCtx.DB().WithContext(ctx).Begin()
	result := tx.Model(&x402Nonce{}).
		Where("nonce = ? AND status = ?", nonce, x402.NonceStatusPending).
		Updates(map[string]interface{}{
			"status":     x402.NonceStatusSettled,
			"settled_at": time.Now(),
			"reference":  notification.TransactionId,
		})
	if result.Error != nil {
		tx.Rollback()
		g.coreCtx.Logger().Error("failed to settle x402 nonce in DB",
			zap.String("nonce", nonce),
			zap.Error(result.Error),
		)
		return fmt.Errorf("failed to settle nonce: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		tx.Rollback()
		g.coreCtx.Logger().Warn("x402 webhook received for unknown or already-settled nonce",
			zap.String("nonce", nonce),
		)
		return nil
	}

	// Issue credit before committing the settlement. If the credit service
	// is nil, return an error so ATLOS retries — never commit a settlement
	// without issuing credit.
	if g.credit == nil {
		tx.Rollback()
		g.coreCtx.Logger().Error("x402 credit service unavailable, cannot settle webhook",
			zap.String("nonce", nonce),
		)
		return fmt.Errorf("credit service unavailable")
	}
	if err := g.credit.IssueCreditWithIdempotency(
		ctx,
		uint64(record.UserID),
		pluginCore.TransactionTypeCharge,
		paidAmount,
		pluginCore.ReferenceTypeX402Payment,
		fmt.Sprintf("x402-%s", nonce),
		fmt.Sprintf("x402 payment for nonce %s", nonce),
		0,
	); err != nil {
		tx.Rollback()
		g.coreCtx.Logger().Error("failed to issue x402 credit from webhook",
			zap.String("nonce", nonce),
			zap.Uint("user_id", record.UserID),
			zap.Error(err),
		)
		return fmt.Errorf("failed to issue credit: %w", err)
	}

	if err := tx.Commit().Error; err != nil {
		g.coreCtx.Logger().Error("failed to commit x402 webhook transaction",
			zap.String("nonce", nonce),
			zap.Error(err),
		)
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Cache after commit (not before) so a failed transaction doesn't leave
	// a stale cache entry that would make ConfirmPayment return a phantom
	// payment. The tiny window between commit and cache set is harmless —
	// ConfirmPayment falls back to a DB query on cache miss.
	g.webhookCache.Set(nonce, &cachedPayment{
		TransactionId: notification.TransactionId,
		PaidAmount:    paidAmount,
		PaidAt:        time.Now(),
	})

	g.coreCtx.Logger().Info("x402 payment settled",
		zap.String("nonce", nonce),
		zap.String("transaction_id", notification.TransactionId),
		zap.String("paid_amount", paidAmount.String()),
	)

	return nil
}

func strPtr(s string) *string {
	return &s
}
