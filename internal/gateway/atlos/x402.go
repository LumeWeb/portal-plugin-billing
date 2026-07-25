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

// x402Nonce is a minimal GORM model for the billing_x402_nonces table.
type x402Nonce struct {
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

func (x402Nonce) TableName() string { return "billing_x402_nonces" }

// Compile-time checks
var (
	_ pluginCore.PaymentProcessor       = (*AtlosGateway)(nil)
	_ pluginCore.PaymentAddressProvider = (*AtlosGateway)(nil)
)

type WebhookNonceCache struct {
	mu      sync.RWMutex
	entries map[string]*cachedPayment
}

type cachedPayment struct {
	TransactionId string
	PaidAmount    decimal.Decimal
	PaidAt        time.Time
}

func NewWebhookNonceCache() *WebhookNonceCache {
	return &WebhookNonceCache{entries: make(map[string]*cachedPayment)}
}

func (c *WebhookNonceCache) Set(nonce string, payment *cachedPayment) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[nonce] = payment
}

func (c *WebhookNonceCache) Get(nonce string) (*cachedPayment, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	p, ok := c.entries[nonce]
	return p, ok
}

type AtlosPaymentInfo struct {
	PaymentID      string
	WalletAddress  string
	AssetCode      string
	BlockchainCode float32
	Amount         string // smallest unit, no decimal point
}

func (g *AtlosGateway) SupportedAssets(ctx context.Context) ([]pluginCore.SupportedAsset, error) {
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
				AssetCode:      *asset.Code,
				AssetName:      ptrStr(asset.Name),
				BlockchainCode: *chain.ChainId,
				BlockchainName: chainName,
				TokenAddress:   tokenAddr,
				Decimals:       decimals,
				IsStable:       true,
			})
		}
	}
	return result, nil
}

func (g *AtlosGateway) CreatePaymentAddress(ctx context.Context, assetCode string, blockchainCode float32, amount decimal.Decimal, nonce string) (*pluginCore.PaymentAddress, error) {
	client, err := g.newAtlosClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create ATLOS client: %w", err)
	}

	decimals := g.findDecimals(assetCode, blockchainCode)
	amountStr := amount.Mul(decimal.NewFromInt(10).Pow(decimal.NewFromInt(int64(decimals)))).Truncate(0).String()

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

type assetLookup struct {
	cache   []atlos.Asset
	expires time.Time
	ttl     time.Duration
}

func newAssetLookup() *assetLookup {
	return &assetLookup{ttl: 5 * time.Minute}
}

func (a *assetLookup) get(ctx context.Context, g *AtlosGateway) ([]atlos.Asset, error) {
	if a.cache != nil && time.Now().Before(a.expires) {
		return a.cache, nil
	}
	client, err := g.newAtlosClient()
	if err != nil {
		return nil, err
	}
	assets, err := client.AssetList(ctx, atlos.AssetListPostRequest{
		MerchantId:    g.config.MerchantID,
		OrderAmount:   1.00,
		OrderCurrency: strPtr("USD"),
	})
	if err != nil {
		return nil, err
	}
	a.cache = assets
	a.expires = time.Now().Add(a.ttl)
	return assets, nil
}

func (g *AtlosGateway) findDecimals(assetCode string, blockchainCode float32) int32 {
	assets, err := g.assetCache.get(context.Background(), g)
	if err != nil {
		return 6
	}
	for _, asset := range assets {
		if asset.Code == nil || !strings.EqualFold(*asset.Code, assetCode) {
			continue
		}
		if asset.Blockchains == nil {
			continue
		}
		for _, chain := range *asset.Blockchains {
			if chain.ChainId != nil && *chain.ChainId == blockchainCode {
				if chain.Decimals != nil {
					return int32(*chain.Decimals)
				}
			}
		}
	}
	return 6
}

func ptrStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func (g *AtlosGateway) ConfirmPayment(ctx context.Context, nonce string, expectedAmount decimal.Decimal) (*pluginCore.PaymentConfirmation, error) {
	var record x402Nonce
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

func (g *AtlosGateway) handleX402Webhook(ctx context.Context, notification atlos.PostbackNotification) error {
	paidAmount := decimal.NewFromFloat(notification.PaidAmount)

	nonce := notification.OrderId
	if !g.isX402Nonce(nonce) {
		g.coreCtx.Logger().Debug("webhook OrderId is not an x402 nonce, skipping",
			zap.String("order_id", nonce),
		)
		return nil
	}

	result := g.coreCtx.DB().WithContext(ctx).
		Model(&x402Nonce{}).
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
