package billing

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	"go.lumeweb.com/portal/core"
	coreTesting "go.lumeweb.com/portal/core/testing"
)

// ============================================================
// Pause/Resume Tests
// ============================================================

func TestBillingService_PauseSubscriber(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE)
		require.NotNil(tb, service)

		// Arrange - Create active subscriber
		err := service.CreateOrUpdateSubscriber(context.Background(), 100, "stripe", "cus_100", "sub_100", true, nil)
		require.NoError(tb, err)

		// Verify initial state
		sub, err := service.GetActiveSubscriber(context.Background(), 100, "stripe")
		require.NoError(tb, err)
		require.NotNil(tb, sub)
		assert.True(tb, sub.IsActive)
		assert.Nil(tb, sub.PausedAt)

		// Act - Pause subscriber
		err = service.PauseSubscriber(context.Background(), 100, "stripe")
		require.NoError(tb, err)

		// Assert - Verify paused state
		sub, err = service.GetSubscriberByExternalID(context.Background(), "cus_100", "stripe")
		require.NoError(tb, err)
		require.NotNil(tb, sub)
		assert.False(tb, sub.IsActive)
		assert.NotNil(tb, sub.PausedAt)
	}, getBillingTestOptions())
}

func TestBillingService_ResumeSubscriber(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE)
		require.NotNil(tb, service)

		// Arrange - Create and pause subscriber
		err := service.CreateOrUpdateSubscriber(context.Background(), 101, "stripe", "cus_101", "sub_101", true, nil)
		require.NoError(tb, err)

		err = service.PauseSubscriber(context.Background(), 101, "stripe")
		require.NoError(tb, err)

		// Verify paused state
		sub, err := service.GetActiveSubscriber(context.Background(), 101, "stripe")
		require.NoError(tb, err)
		assert.Nil(tb, sub) // Not found as active

		pausedSub, err := service.GetPausedSubscription(context.Background(), 101)
		require.NoError(tb, err)
		require.NotNil(tb, pausedSub)
		assert.False(tb, pausedSub.IsActive)
		assert.NotNil(tb, pausedSub.PausedAt)

		// Act - Resume subscriber
		err = service.ResumeSubscriber(context.Background(), 101, "stripe")
		require.NoError(tb, err)

		// Assert - Verify resumed state
		sub, err = service.GetActiveSubscriber(context.Background(), 101, "stripe")
		require.NoError(tb, err)
		require.NotNil(tb, sub)
		assert.True(tb, sub.IsActive)
		assert.Nil(tb, sub.PausedAt)

		// Verify no longer in paused state
		pausedSub, err = service.GetPausedSubscription(context.Background(), 101)
		require.NoError(tb, err)
		assert.Nil(tb, pausedSub)
	}, getBillingTestOptions())
}

func TestBillingService_GetPausedSubscription_NoPausedSubscription(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE)
		require.NotNil(tb, service)

		// Act - Get paused subscription for non-existent user
		sub, err := service.GetPausedSubscription(context.Background(), 999)
		require.NoError(tb, err)

		// Assert
		assert.Nil(tb, sub)
	}, getBillingTestOptions())
}

func TestBillingService_GetPausedSubscription_ActiveSubscription(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE)
		require.NotNil(tb, service)

		// Arrange - Create active subscriber
		err := service.CreateOrUpdateSubscriber(context.Background(), 102, "stripe", "cus_102", "sub_102", true, nil)
		require.NoError(tb, err)

		// Act - Active subscription should not appear as paused
		sub, err := service.GetPausedSubscription(context.Background(), 102)
		require.NoError(tb, err)

		// Assert
		assert.Nil(tb, sub)
	}, getBillingTestOptions())
}

func TestBillingService_GetPausedSubscription_CancelledSubscription(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE)
		require.NotNil(tb, service)

		// Arrange - Create, pause, then cancel subscriber
		err := service.CreateOrUpdateSubscriber(context.Background(), 103, "stripe", "cus_103", "sub_103", true, nil)
		require.NoError(tb, err)

		err = service.PauseSubscriber(context.Background(), 103, "stripe")
		require.NoError(tb, err)

		err = service.DeactivateSubscriber(context.Background(), 103, "stripe")
		require.NoError(tb, err)

		// Act - Cancelled subscription should not appear as paused
		sub, err := service.GetPausedSubscription(context.Background(), 103)
		require.NoError(tb, err)

		// Assert
		assert.Nil(tb, sub)
	}, getBillingTestOptions())
}
