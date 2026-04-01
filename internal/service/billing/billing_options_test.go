package billing

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	coreTesting "go.lumeweb.com/portal/core/testing"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	"go.lumeweb.com/portal/core"
)

// TestBillingService_CreateOrUpdateSubscriber_WithBillingPeriodOption demonstrates
// the use of the functional options pattern to set BillingPeriodStart and BillingPeriodEnd
func TestBillingService_CreateOrUpdateSubscriber_WithBillingPeriodOption(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE)
		require.NotNil(t, service)

		// Create time pointers for billing period
		now := time.Now().UTC()
		startTime := now
		endTime := now.AddDate(0, 1, 0) // 1 month from now

		// Create a subscriber with billing period options
		err := service.CreateOrUpdateSubscriber(
			context.Background(),
			1,
			"stripe",
			"cus_billing_test",
			"sub_billing_test",
			true,
			nil,
			pluginCore.WithBillingPeriodStart(&startTime),
			pluginCore.WithBillingPeriodEnd(&endTime),
		)
		assert.NoError(t, err)

		// Verify the subscriber was created with billing period
		subscriber, err := service.GetActiveSubscriber(context.Background(), 1, "stripe")
		assert.NoError(t, err)
		assert.NotNil(t, subscriber)
		assert.Equal(t, uint(1), subscriber.UserID)
		assert.Equal(t, "stripe", subscriber.GatewayType)
		assert.True(t, subscriber.IsActive)

		// Verify billing period fields are set
		assert.NotNil(t, subscriber.BillingPeriodStart)
		assert.NotNil(t, subscriber.BillingPeriodEnd)
		assert.WithinDuration(t, startTime, *subscriber.BillingPeriodStart, time.Second)
		assert.WithinDuration(t, endTime, *subscriber.BillingPeriodEnd, time.Second)
	},
		getBillingTestOptions())
}

// TestBillingService_CreateOrUpdateSubscriber_WithOnlyBillingPeriodStart demonstrates
// setting only BillingPeriodStart
func TestBillingService_CreateOrUpdateSubscriber_WithOnlyBillingPeriodStart(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE)
		require.NotNil(t, service)

		now := time.Now().UTC()

		// Create a subscriber with only billing period start
		err := service.CreateOrUpdateSubscriber(
			context.Background(),
			2,
			"stripe",
			"cus_billing_test2",
			"sub_billing_test2",
			true,
			nil,
			pluginCore.WithBillingPeriodStart(&now),
		)
		assert.NoError(t, err)

		// Verify the subscriber was created
		subscriber, err := service.GetActiveSubscriber(context.Background(), 2, "stripe")
		assert.NoError(t, err)
		assert.NotNil(t, subscriber)

		// Verify only BillingPeriodStart is set
		assert.NotNil(t, subscriber.BillingPeriodStart)
		assert.WithinDuration(t, now, *subscriber.BillingPeriodStart, time.Second)
		assert.Nil(t, subscriber.BillingPeriodEnd)
	},
		getBillingTestOptions())
}

// TestBillingService_CreateOrUpdateSubscriber_WithOnlyBillingPeriodEnd demonstrates
// setting only BillingPeriodEnd
func TestBillingService_CreateOrUpdateSubscriber_WithOnlyBillingPeriodEnd(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE)
		require.NotNil(t, service)

		endTime := time.Now().UTC().AddDate(0, 1, 0)

		// Create a subscriber with only billing period end
		err := service.CreateOrUpdateSubscriber(
			context.Background(),
			3,
			"stripe",
			"cus_billing_test3",
			"sub_billing_test3",
			true,
			nil,
			pluginCore.WithBillingPeriodEnd(&endTime),
		)
		assert.NoError(t, err)

		// Verify the subscriber was created
		subscriber, err := service.GetActiveSubscriber(context.Background(), 3, "stripe")
		assert.NoError(t, err)
		assert.NotNil(t, subscriber)

		// Verify only BillingPeriodEnd is set
		assert.Nil(t, subscriber.BillingPeriodStart)
		assert.NotNil(t, subscriber.BillingPeriodEnd)
		assert.WithinDuration(t, endTime, *subscriber.BillingPeriodEnd, time.Second)
	},
		getBillingTestOptions())
}

// TestBillingService_CreateOrUpdateSubscriber_UpdateWithBillingPeriod demonstrates
// updating an existing subscriber to add billing period
func TestBillingService_CreateOrUpdateSubscriber_UpdateWithBillingPeriod(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE)
		require.NotNil(t, service)

		// Create a subscriber without billing period
		err := service.CreateOrUpdateSubscriber(
			context.Background(),
			4,
			"stripe",
			"cus_billing_test4",
			"sub_billing_test4",
			true,
			nil,
		)
		assert.NoError(t, err)

		// Verify subscriber was created without billing period
		subscriber, err := service.GetActiveSubscriber(context.Background(), 4, "stripe")
		assert.NoError(t, err)
		assert.NotNil(t, subscriber)
		assert.Nil(t, subscriber.BillingPeriodStart)
		assert.Nil(t, subscriber.BillingPeriodEnd)

		// Now update the subscriber with billing period
		now := time.Now().UTC()
		endTime := now.AddDate(0, 1, 0)
		err = service.CreateOrUpdateSubscriber(
			context.Background(),
			4,
			"stripe",
			"cus_billing_test4_updated",
			"sub_billing_test4_updated",
			true,
			nil,
			pluginCore.WithBillingPeriodStart(&now),
			pluginCore.WithBillingPeriodEnd(&endTime),
		)
		assert.NoError(t, err)

		// Verify billing period was added
		subscriber, err = service.GetActiveSubscriber(context.Background(), 4, "stripe")
		assert.NoError(t, err)
		assert.NotNil(t, subscriber)
		assert.NotNil(t, subscriber.BillingPeriodStart)
		assert.NotNil(t, subscriber.BillingPeriodEnd)
		assert.WithinDuration(t, now, *subscriber.BillingPeriodStart, time.Second)
		assert.WithinDuration(t, endTime, *subscriber.BillingPeriodEnd, time.Second)
	},
		getBillingTestOptions())
}
