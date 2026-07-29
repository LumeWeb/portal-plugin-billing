package atlos

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/atlos-sdk"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	"go.lumeweb.com/portal-plugin-billing/internal"
	"go.lumeweb.com/portal-plugin-billing/internal/db/migrations"
	"go.lumeweb.com/portal-plugin-billing/internal/x402"
	core "go.lumeweb.com/portal/core"
	coreTesting "go.lumeweb.com/portal/core/testing"
)

// dummyAtlosService is a minimal service for the mock plugin builder.
type dummyAtlosService struct {
	core.BaseComponent
}

func (s *dummyAtlosService) ID() string { return "atlos.x402.test" }

func newDummyAtlosService() (core.Service, []core.ContextBuilderOption, error) {
	return &dummyAtlosService{}, nil, nil
}

// getAtlosX402TestOptions registers the billing plugin's models and migrations
// so that RunTestCaseWithDB creates the billing_x402_nonces table.
func getAtlosX402TestOptions() coreTesting.TestContextBuilderOption {
	return coreTesting.CombineOptions(
		coreTesting.NewMockPluginBuilder(internal.PLUGIN_NAME).
			WithMigrations(core.DBMigration{core.DB_TYPE_SQLITE: migrations.GetSQLite()}).
			WithModels(x402Nonce{}).
			WithService("atlos.x402.test", newDummyAtlosService).
			BuilderOption(),
	)
}

func TestIsX402OrderID(t *testing.T) {
	g := &AtlosGateway{}

	tests := []struct {
		name     string
		orderID  string
		expected bool
	}{
		{
			name:     "valid composite order ID",
			orderID:  "x402-0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-usdc-8453",
			expected: true,
		},
		{
			name:     "valid uppercase hex nonce",
			orderID:  "x402-0xAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA-usdc-8453",
			expected: true,
		},
		{
			name:     "bare hex nonce without asset/chain (batch format)",
			orderID:  "x402-0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			expected: true,
		},
		{
			name:     "ATLOS HMAC order ID",
			orderID:  fmt.Sprintf("userID:123:periodID:456:hmac:%d", time.Now().UnixNano()),
			expected: false,
		},
		{
			name:     "no x402 prefix",
			orderID:  "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-usdc-8453",
			expected: false,
		},
		{
			name:     "too short (not 66 chars)",
			orderID:  "x402-0x1234",
			expected: false,
		},
		{
			name:     "invalid hex characters",
			orderID:  "x402-0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaag-usdc-8453",
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
			result := g.isX402OrderID(tt.orderID)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNonceFromOrderID(t *testing.T) {
	g := &AtlosGateway{}

	tests := []struct {
		name    string
		orderID string
		want    string
	}{
		{
			name:    "composite order ID",
			orderID: "x402-0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-usdc-8453",
			want:    "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		},
		{
			name:    "bare UUID passthrough",
			orderID: "x402-0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			want:    "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, g.nonceFromOrderID(tt.orderID))
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

func TestTokenEIP712Version(t *testing.T) {
	assert.Equal(t, "2", tokenEIP712Version("usdc"))
	assert.Equal(t, "2", tokenEIP712Version("USDC"))
	assert.Equal(t, "1", tokenEIP712Version("usdt"))
	assert.Equal(t, "1", tokenEIP712Version("unknown"))
}

// --- Regression: Webhook amount mismatch sets nonce to "mismatch" status (Kody) ---

func TestRegression_WebhookAmountMismatch_SetsMismatchStatus(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		db := ctx.DB()

		gw := &AtlosGateway{
			coreCtx:      ctx,
			webhookCache: NewWebhookNonceCache(),
		}

		// Insert a pending nonce with $5.00 expected amount.
		nonce := "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		require.NoError(t, db.Create(&x402Nonce{
			Nonce:       nonce,
			UserID:      42,
			Amount:      decimal.NewFromFloat(5.00),
			Wallet:      "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			GatewayType: "atlos",
			Status:      "pending",
			ExpiresAt:   time.Now().Add(5 * time.Minute),
		}).Error)

		// Webhook reports ATLOS paid $3.00 — mismatch.
		notification := atlos.PostbackNotification{
			OrderId:       fmt.Sprintf("x402-%s-usdc-8453", nonce),
			PaidAmount:    3.00,
			TransactionId: "tx-mismatch-001",
		}

		err := gw.handleX402Webhook(context.Background(), notification)
		require.NoError(t, err)

		// Verify nonce is now "mismatch" status with the transaction reference.
		var record x402Nonce
		require.NoError(t, db.Where("nonce = ?", nonce).First(&record).Error)
		assert.Equal(t, x402.NonceStatusMismatch, x402.NonceStatus(record.Status))
		assert.Equal(t, "tx-mismatch-001", record.Reference)

		// ConfirmPayment should now return a permanent error, not ErrPaymentPending.
		_, err = gw.ConfirmPayment(context.Background(), nonce, decimal.NewFromFloat(5.00))
		require.Error(t, err)
		assert.NotContains(t, err.Error(), "pending")
		assert.Contains(t, err.Error(), "mismatch")
	}, getAtlosX402TestOptions())
}

// --- Regression: Webhook amount match settles normally ---

func TestRegression_WebhookAmountMatch_SettlesNormally(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		db := ctx.DB()

		mockCredit := pluginCore.NewMockCreditService(t)
		mockCredit.On("IssueCreditWithIdempotency", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

		gw := &AtlosGateway{
			coreCtx:      ctx,
			webhookCache: NewWebhookNonceCache(),
			credit:       mockCredit,
		}

		// Insert a pending nonce with $5.00 expected amount.
		nonce := "0xcccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
		require.NoError(t, db.Create(&x402Nonce{
			Nonce:       nonce,
			UserID:      42,
			Amount:      decimal.NewFromFloat(5.00),
			Wallet:      "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			GatewayType: "atlos",
			Status:      "pending",
			ExpiresAt:   time.Now().Add(5 * time.Minute),
		}).Error)

		// Webhook reports ATLOS paid $5.00 — match. Use $5.001 to test tolerance.
		notification := atlos.PostbackNotification{
			OrderId:       fmt.Sprintf("x402-%s-usdc-8453", nonce),
			PaidAmount:    5.001,
			TransactionId: "tx-match-001",
		}

		err := gw.handleX402Webhook(context.Background(), notification)
		require.NoError(t, err)

		// Verify nonce is settled.
		var record x402Nonce
		require.NoError(t, db.Where("nonce = ?", nonce).First(&record).Error)
		assert.Equal(t, x402.NonceStatusSettled, x402.NonceStatus(record.Status))
	}, getAtlosX402TestOptions())
}

// --- Regression: findDecimals returns error instead of silently falling back (Kody 3675329298) ---

func TestRegression_FindDecimals_ReturnsErrorOnUnsupportedAsset(t *testing.T) {
	ctx, err := coreTesting.NewTestContext(t)
	require.NoError(t, err)

	gw := &AtlosGateway{
		coreCtx: ctx,
	}

	// SupportedAssets will fail because no ATLOS client is configured.
	// findDecimals should return an error, not silently return 6.
	_, err = gw.findDecimals(context.Background(), "USDC", 8453)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to fetch supported assets")
}

// --- Regression: order ID lowercases asset code (Kody) ---

func TestRegression_OrderID_LowercasesAssetCode(t *testing.T) {
	ctx, err := coreTesting.NewTestContext(t)
	require.NoError(t, err)
	gw := &AtlosGateway{coreCtx: ctx}

	// isX402OrderID only accepts lowercase alphanumeric asset codes.
	// Verify that a dashed-UUID nonce + uppercase asset code produces
	// an order ID that passes isX402OrderID.
	nonce := "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	orderID := fmt.Sprintf("x402-%s-%s-%d", nonce, strings.ToLower("USDC"), 8453)
	assert.True(t, gw.isX402OrderID(orderID), "lowercased order ID should pass validation: %s", orderID)

	// With uppercase asset code (the old bug), it should fail.
	orderIDUpper := fmt.Sprintf("x402-%s-%s-%d", nonce, "USDC", 8453)
	assert.False(t, gw.isX402OrderID(orderIDUpper), "uppercase order ID should fail validation: %s", orderIDUpper)
}

// --- Regression: hex nonce round-trip through isX402OrderID and nonceFromOrderID ---

func TestRegression_HexNonce_RoundTrip(t *testing.T) {
	gw := &AtlosGateway{}

	// Simulate what generateNonce + CreatePaymentAddress produces
	nonce := "0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	assert.Equal(t, 66, len(nonce), "nonce must be 66 chars (0x + 64 hex)")

	// Per-asset order ID
	orderID := fmt.Sprintf("x402-%s-usdc-8453", nonce)
	assert.True(t, gw.isX402OrderID(orderID), "per-asset order ID should pass: %s", orderID)
	extracted := gw.nonceFromOrderID(orderID)
	assert.Equal(t, nonce, extracted, "extracted nonce should match: got %s, want %s", extracted, nonce)

	// Batch order ID (nonce only, no asset/chain)
	batchOrderID := fmt.Sprintf("x402-%s", nonce)
	assert.True(t, gw.isX402OrderID(batchOrderID), "batch order ID should pass: %s", batchOrderID)
	extracted = gw.nonceFromOrderID(batchOrderID)
	assert.Equal(t, nonce, extracted, "extracted nonce should match: got %s, want %s", extracted, nonce)
}

// --- Regression: nonce value fits in DB column (VARCHAR(66)) ---

func TestRegression_NonceFitsInDBColumn(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		db := ctx.DB()

		// Generate a real nonce and try to insert it
		nonce := "0x" + strings.Repeat("a", 64) // 66 chars, same format as generateNonce
		require.Equal(t, 66, len(nonce), "nonce must be 66 chars")

		// Insert should succeed — no truncation, no error
		record := &x402Nonce{
			Nonce:       nonce,
			UserID:      42,
			Amount:      decimal.NewFromFloat(5.00),
			Wallet:      "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			GatewayType: "atlos",
			Status:      "pending",
			ExpiresAt:   time.Now().Add(5 * time.Minute),
		}
		require.NoError(t, db.Create(record).Error)

		// Read back and verify it's not truncated
		var read x402Nonce
		require.NoError(t, db.Where("nonce = ?", nonce).First(&read).Error)
		assert.Equal(t, nonce, read.Nonce, "nonce should not be truncated in DB")
	}, getAtlosX402TestOptions())
}

// --- Regression: ConfirmPayment returns DB errors, not ErrPaymentPending ---

func TestRegression_ConfirmPayment_DBErrorOnMismatchCheck_ReturnsError(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		db := ctx.DB()

		// Drop the nonces table so the mismatch status query fails.
		require.NoError(t, db.Migrator().DropTable("billing_x402_nonces"))

		gw := &AtlosGateway{coreCtx: ctx}

		// ConfirmPayment should return a DB error, not ErrPaymentPending.
		_, err := gw.ConfirmPayment(context.Background(), "0x"+strings.Repeat("a", 64), decimal.NewFromFloat(5.00))
		require.Error(t, err)
		// Must NOT be ErrPaymentPending — it should be a real DB error.
		assert.NotContains(t, err.Error(), "pending")
		assert.Contains(t, err.Error(), "failed to query mismatch status")
	}, getAtlosX402TestOptions())
}
