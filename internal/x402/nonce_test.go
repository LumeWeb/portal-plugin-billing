package x402

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/portal-plugin-billing/internal"
	"go.lumeweb.com/portal-plugin-billing/internal/db/migrations"
	core "go.lumeweb.com/portal/core"
	coreTesting "go.lumeweb.com/portal/core/testing"
)

// dummyX402Service is a minimal service for the mock plugin builder
type dummyX402Service struct {
	core.BaseComponent
}

func (s *dummyX402Service) ID() string { return "x402.test" }

func newDummyX402Service() (core.Service, []core.ContextBuilderOption, error) {
	return &dummyX402Service{}, nil, nil
}

func getX402TestOptions() coreTesting.TestContextBuilderOption {
	return coreTesting.CombineOptions(
		coreTesting.NewMockPluginBuilder(internal.PLUGIN_NAME).
			WithMigrations(core.DBMigration{core.DB_TYPE_SQLITE: migrations.GetSQLite()}).
			WithService("x402.test", newDummyX402Service).
			WithModels(X402Nonce{}).
			BuilderOption(),
	)
}

func TestDBNonceStore_SetAndGet(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		store := NewDBNonceStore(ctx.DB())
		c := context.Background()

		err := store.Set(c, "nonce-123", 42, "0x1234567890123456789012345678901234567890", decimal.NewFromFloat(5.00), DefaultGatewayType, "", 5*time.Minute)
		require.NoError(t, err)

		userID, amount, gwType, ok, err := store.Get(c, "nonce-123")
		require.NoError(t, err)
		assert.True(t, ok)
		assert.Equal(t, uint(42), userID)
		assert.True(t, amount.Equal(decimal.NewFromFloat(5.00)))
		assert.Equal(t, DefaultGatewayType, gwType)
	}, getX402TestOptions())
}

func TestDBNonceStore_GetExpired_ReturnsNotFound(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		store := NewDBNonceStore(ctx.DB())
		c := context.Background()

		// Insert expired record directly
		ctx.DB().WithContext(c).Create(&X402Nonce{
			Nonce:       "old-nonce",
			UserID:      1,
			Amount:      decimal.NewFromFloat(1.00),
			GatewayType: DefaultGatewayType,
			ExpiresAt:   time.Now().Add(-time.Hour),
		})

		_, _, _, ok, err := store.Get(c, "old-nonce")
		require.NoError(t, err)
		assert.False(t, ok)
	}, getX402TestOptions())
}

func TestDBNonceStore_Delete(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		store := NewDBNonceStore(ctx.DB())
		c := context.Background()

		store.Set(c, "del-nonce", 1, "0x1234567890123456789012345678901234567890", decimal.NewFromFloat(1.00), DefaultGatewayType, "", 5*time.Minute)
		store.Delete(c, "del-nonce")

		_, _, _, ok, _ := store.Get(c, "del-nonce")
		assert.False(t, ok)
	}, getX402TestOptions())
}

func TestDBNonceStore_AutoMigrate(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		// Verify the table exists by running a raw query
		var count int64
		err := ctx.DB().WithContext(context.Background()).Model(&X402Nonce{}).Count(&count).Error
		require.NoError(t, err)
		assert.Equal(t, int64(0), count)
	}, getX402TestOptions())
}

func TestDBNonceStore_Consume_AtomicFirstCallWins(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		store := NewDBNonceStore(ctx.DB())
		c := context.Background()

		err := store.Set(c, "consume-nonce", 1, "0x1234567890123456789012345678901234567890", decimal.NewFromFloat(1.00), DefaultGatewayType, "", 5*time.Minute)
		require.NoError(t, err)

		// First consume succeeds
		consumed, err := store.Consume(c, "consume-nonce")
		require.NoError(t, err)
		assert.True(t, consumed)

		// Second consume fails (already consumed)
		consumed2, err := store.Consume(c, "consume-nonce")
		require.NoError(t, err)
		assert.False(t, consumed2)

		// Get should also fail (row deleted)
		_, _, _, ok, _ := store.Get(c, "consume-nonce")
		assert.False(t, ok)
	}, getX402TestOptions())
}

func TestDBNonceStore_Consume_NonExistentReturnsFalse(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		store := NewDBNonceStore(ctx.DB())
		c := context.Background()

		consumed, err := store.Consume(c, "does-not-exist")
		require.NoError(t, err)
		assert.False(t, consumed)
	}, getX402TestOptions())
}

func TestDBNonceStore_Consume_SettledNonceReturnsFalse(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		store := NewDBNonceStore(ctx.DB())
		c := context.Background()

		err := store.Set(c, "settled-consume-nonce", 1, "0x1234567890123456789012345678901234567890", decimal.NewFromFloat(1.00), DefaultGatewayType, "", 5*time.Minute)
		require.NoError(t, err)

		// Simulate ATLOS webhook settling the nonce before client callback
		err = store.Settle(c, "settled-consume-nonce", "tx-ref-456")
		require.NoError(t, err)

		// Consume must return false — the nonce is already settled.
		// Only pending nonces can be consumed to prevent double-credit races.
		consumed, err := store.Consume(c, "settled-consume-nonce")
		require.NoError(t, err)
		assert.False(t, consumed)
	}, getX402TestOptions())
}

func TestDBNonceStore_Settle_UpdatesStatusToSettled(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		store := NewDBNonceStore(ctx.DB())
		c := context.Background()

		err := store.Set(c, "settle-nonce", 1, "0x1234567890123456789012345678901234567890", decimal.NewFromFloat(1.00), DefaultGatewayType, "", 5*time.Minute)
		require.NoError(t, err)

		err = store.Settle(c, "settle-nonce", "tx-ref-123")
		require.NoError(t, err)

		// Get should fail (only pending nonces are returned)
		_, _, _, ok, _ := store.Get(c, "settle-nonce")
		assert.False(t, ok)

		// Direct query should show settled status
		var record X402Nonce
		ctx.DB().WithContext(c).Where("nonce = ?", "settle-nonce").First(&record)
		assert.Equal(t, NonceStatusSettled, record.Status)
		assert.Equal(t, "tx-ref-123", record.Reference)
		assert.NotNil(t, record.SettledAt)
	}, getX402TestOptions())
}

func TestIsValidEVMAddress(t *testing.T) {
	tests := []struct {
		addr string
		want bool
	}{
		{"0x1234567890123456789012345678901234567890", true},
		{"0xAbCdEf0123456789ABcdEF0123456789AbCdEf01", true},
		{"0X1234567890123456789012345678901234567890", false}, // uppercase 0X rejected (canonical is lowercase)
		{"0x12345678901234567890123456789012345678901", false}, // 43 chars
		{"0x123456789012345678901234567890123456789", false},   // 41 chars
		{"0xG234567890123456789012345678901234567890", false},  // non-hex
		{"", false},
		{"0x", false},
		{"1234567890123456789012345678901234567890", false},   // no 0x prefix
		{"0x1234567890123456789012345678901234567890extra", false},
	}
	for _, tt := range tests {
		t.Run(tt.addr, func(t *testing.T) {
			assert.Equal(t, tt.want, isValidEVMAddressFormat(tt.addr))
		})
	}
}
