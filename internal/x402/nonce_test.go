package x402

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	core "go.lumeweb.com/portal/core"
	coreTesting "go.lumeweb.com/portal/core/testing"
	"go.lumeweb.com/portal-plugin-billing/internal"
	"go.lumeweb.com/portal-plugin-billing/internal/db/migrations"
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

		err := store.Set(c, "nonce-123", 42, decimal.NewFromFloat(5.00), DefaultGatewayType, 5*time.Minute)
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
		ctx.DB().Create(&X402Nonce{
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

		store.Set(c, "del-nonce", 1, decimal.NewFromFloat(1.00), DefaultGatewayType, 5*time.Minute)
		store.Delete(c, "del-nonce")

		_, _, _, ok, _ := store.Get(c, "del-nonce")
		assert.False(t, ok)
	}, getX402TestOptions())
}

func TestDBNonceStore_AutoMigrate(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		// Verify the table exists by running a raw query
		var count int64
		err := ctx.DB().Model(&X402Nonce{}).Count(&count).Error
		require.NoError(t, err)
		assert.Equal(t, int64(0), count)
	}, getX402TestOptions())
}
