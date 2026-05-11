package billing

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	"go.lumeweb.com/portal-plugin-billing/internal/db/models"
	"go.lumeweb.com/portal-plugin-billing/internal/gateway"
	"go.lumeweb.com/portal/core"
	coreTesting "go.lumeweb.com/portal/core/testing"
)

// ============================================================
// Helper Functions
// ============================================================

// createTestPricingPlanPeriodWithDB creates a pricing plan period directly in the database
func createTestPricingPlanPeriodWithDB(t *testing.T, ctx core.Context) *models.PricingPlanPeriod {
	t.Helper()

	db := ctx.DB()
	rollingDays := 30

	// Create a pricing plan first
	plan := &models.PricingPlan{
		Name:         "Test Plan",
		Description:  "Test description",
		FeaturesJSON: new(`["test"]`),
		IsActive:     true,
		IsPublic:     true,
	}
	require.NoError(t, db.Create(plan).Error)

	// Create a pricing plan period
	period := &models.PricingPlanPeriod{
		PricingPlanID: plan.ID,
		Cadence:       "monthly",
		PriceUSD:      9.99,
		QuotaPlanID:   123,
		RollingDays:   &rollingDays,
	}
	require.NoError(t, db.Create(period).Error)

	return period
}

// createTestSubscriberWithDB creates a subscriber directly in the database
func createTestSubscriberWithDB(t *testing.T, ctx core.Context, userID uint, gatewayType, externalID, subscriptionID string, isActive bool, periodID *uint, willCancelAt *time.Time) *models.Subscriber {
	t.Helper()

	db := ctx.DB()

	now := time.Now().UTC()
	billingStart := now.Add(-30 * 24 * time.Hour)
	billingEnd := billingStart.Add(30 * 24 * time.Hour)

	subscriber := &models.Subscriber{
		UserID:              userID,
		GatewayType:         gatewayType,
		ExternalID:          externalID,
		SubscriptionID:      subscriptionID,
		IsActive:            isActive,
		PricingPlanPeriodID: periodID,
		BillingPeriodStart:  &billingStart,
		BillingPeriodEnd:    &billingEnd,
		PaymentStatus:       "succeeded",
		WillCancelAt:        willCancelAt,
		CancelledAt:         nil,
	}

	require.NoError(t, db.Create(subscriber).Error)
	return subscriber
}

// ============================================================
// Unit Tests
// ============================================================

func TestNewCancellationReconciliationJob(t *testing.T) {
	// Act
	job := NewCancellationReconciliationJob()

	// Assert
	assert.NotNil(t, job)
	assert.NotEmpty(t, job.ID())
	assert.Equal(t, core.JobOriginPlugin, job.Origin())
	assert.Equal(t, CancellationReconciliationJobSourceID, job.SourceID())
	assert.Equal(t, "Billing Cancellation Reconciliation", job.DisplayName())
	assert.Equal(t, CancellationReconciliationJobType, job.Type())
}

// Test constants and identifiers
func TestCancellationReconciliationJobConstants(t *testing.T) {
	// Verify constants are defined correctly
	assert.Equal(t, "billing", CancellationReconciliationJobSourceID)
	assert.Equal(t, "plugin.billing.cancellation_reconciliation", CancellationReconciliationJobType)
}

// ============================================================
// GetPendingCancellations DB-Enabled Tests
// ============================================================

func TestGetPendingCancellations_NoSubscribers(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE)
		require.NotNil(tb, service)

		// Act
		subscribers, err := service.GetPendingCancellations(ctx, "stripe", time.Now().UTC())

		// Assert
		assert.NoError(t, err)
		assert.NotNil(t, subscribers)
		assert.Empty(t, subscribers)
	}, getBillingTestOptions())
}

func TestGetPendingCancellations_PastCancellationDue(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE)
		require.NotNil(tb, service)

		// Arrange - Create a subscriber with WillCancelAt in the past
		pastTime := time.Now().UTC().Add(-1 * time.Hour)
		_ = createTestSubscriberWithDB(t, ctx, 123, "stripe", "cus_123", "sub_123", true, nil, &pastTime)

		// Act
		now := time.Now().UTC()
		subscribers, err := service.GetPendingCancellations(ctx, "stripe", now)

		// Assert
		assert.NoError(t, err)
		assert.Len(t, subscribers, 1)
		assert.Equal(t, uint(123), subscribers[0].UserID)
		assert.Equal(t, "stripe", subscribers[0].GatewayType)
		assert.Equal(t, "sub_123", subscribers[0].SubscriptionID)
		assert.True(t, subscribers[0].IsActive)
		assert.NotNil(t, subscribers[0].WillCancelAt)
		assert.True(t, subscribers[0].WillCancelAt.Before(now) || subscribers[0].WillCancelAt.Equal(now))
	}, getBillingTestOptions())
}

func TestGetPendingCancellations_CurrentCancellationDue(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE)
		require.NotNil(tb, service)

		// Arrange - Create a subscriber with WillCancelAt equal to now
		now := time.Now().UTC().Truncate(time.Second) // Truncate for precision
		_ = createTestSubscriberWithDB(t, ctx, 456, "stripe", "cus_456", "sub_456", true, nil, &now)

		// Act
		subscribers, err := service.GetPendingCancellations(ctx, "stripe", now)

		// Assert
		assert.NoError(t, err)
		assert.Len(t, subscribers, 1)
		assert.Equal(t, uint(456), subscribers[0].UserID)
		assert.Equal(t, "sub_456", subscribers[0].SubscriptionID)
	}, getBillingTestOptions())
}

func TestGetPendingCancellations_FutureCancellationDue(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE)
		require.NotNil(tb, service)

		// Arrange - Create a subscriber with WillCancelAt in the future
		futureTime := time.Now().UTC().Add(1 * time.Hour)
		_ = createTestSubscriberWithDB(t, ctx, 789, "stripe", "cus_789", "sub_789", true, nil, &futureTime)

		// Act
		now := time.Now().UTC()
		subscribers, err := service.GetPendingCancellations(ctx, "stripe", now)

		// Assert
		assert.NoError(t, err)
		assert.Empty(t, subscribers)
	}, getBillingTestOptions())
}

func TestGetPendingCancellations_InactiveSubscriber(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE)
		require.NotNil(tb, service)

		// Arrange - Create an inactive subscriber with past WillCancelAt
		pastTime := time.Now().UTC().Add(-1 * time.Hour)
		_ = createTestSubscriberWithDB(t, ctx, 999, "stripe", "cus_999", "sub_999", false, nil, &pastTime)

		// Act
		now := time.Now().UTC()
		subscribers, err := service.GetPendingCancellations(ctx, "stripe", now)

		// Assert
		assert.NoError(t, err)
		assert.Empty(t, subscribers, "Inactive subscribers should not be returned")
	}, getBillingTestOptions())
}

func TestGetPendingCancellations_MultiplePendingCancellations(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE)
		require.NotNil(tb, service)

		// Arrange - Create multiple subscribers with past WillCancelAt
		pastTime1 := time.Now().UTC().Add(-2 * time.Hour)
		pastTime2 := time.Now().UTC().Add(-1 * time.Hour)
		futureTime := time.Now().UTC().Add(1 * time.Hour)

		_ = createTestSubscriberWithDB(t, ctx, 111, "stripe", "cus_111", "sub_111", true, nil, &pastTime1)
		_ = createTestSubscriberWithDB(t, ctx, 222, "stripe", "cus_222", "sub_222", true, nil, &pastTime2)
		_ = createTestSubscriberWithDB(t, ctx, 333, "stripe", "cus_333", "sub_333", true, nil, &futureTime)

		// Act
		now := time.Now().UTC()
		subscribers, err := service.GetPendingCancellations(ctx, "stripe", now)

		// Assert
		assert.NoError(t, err)
		assert.Len(t, subscribers, 2)
		userIDs := make(map[uint]bool)
		for _, sub := range subscribers {
			userIDs[sub.UserID] = true
		}
		assert.True(t, userIDs[111])
		assert.True(t, userIDs[222])
		assert.False(t, userIDs[333])
	}, getBillingTestOptions())
}

func TestGetPendingCancellations_DifferentGatewayTypes(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE)
		require.NotNil(tb, service)

		// Arrange - Create subscribers for different gateways
		pastTime := time.Now().UTC().Add(-1 * time.Hour)

		_ = createTestSubscriberWithDB(t, ctx, 444, "stripe", "cus_444", "sub_444", true, nil, &pastTime)
		_ = createTestSubscriberWithDB(t, ctx, 555, "paypal", "cus_555", "sub_555", true, nil, &pastTime)
		_ = createTestSubscriberWithDB(t, ctx, 666, "stripe", "cus_666", "sub_666", true, nil, &pastTime)

		// Act - Query for stripe only
		now := time.Now().UTC()
		stripeSubscribers, err := service.GetPendingCancellations(ctx, "stripe", now)

		// Assert
		assert.NoError(t, err)
		assert.Len(t, stripeSubscribers, 2)
		userIDs := make(map[uint]bool)
		for _, sub := range stripeSubscribers {
			assert.Equal(t, "stripe", sub.GatewayType)
			userIDs[sub.UserID] = true
		}
		assert.True(t, userIDs[444])
		assert.True(t, userIDs[666])
		assert.False(t, userIDs[555])

		// Act - Query for paypal only
		paypalSubscribers, err := service.GetPendingCancellations(ctx, "paypal", now)

		// Assert
		assert.NoError(t, err)
		assert.Len(t, paypalSubscribers, 1)
		assert.Equal(t, uint(555), paypalSubscribers[0].UserID)
		assert.Equal(t, "paypal", paypalSubscribers[0].GatewayType)
	}, getBillingTestOptions())
}

func TestGetPendingCancellations_WithWillCancelAtNil(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE)
		require.NotNil(tb, service)

		// Arrange - Create a subscriber without WillCancelAt
		_ = createTestSubscriberWithDB(t, ctx, 777, "stripe", "cus_777", "sub_777", true, nil, nil)

		// Act
		now := time.Now().UTC()
		subscribers, err := service.GetPendingCancellations(ctx, "stripe", now)

		// Assert
		assert.NoError(t, err)
		assert.Empty(t, subscribers, "Subscribers without WillCancelAt should not be returned")
	}, getBillingTestOptions())
}

func TestGetPendingCancellations_WithPricingPlanPeriod(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE)
		require.NotNil(tb, service)

		// Arrange - Create a pricing plan period and subscriber with it
		period := createTestPricingPlanPeriodWithDB(t, ctx)
		pastTime := time.Now().UTC().Add(-1 * time.Hour)
		_ = createTestSubscriberWithDB(t, ctx, 888, "stripe", "cus_888", "sub_888", true, &period.ID, &pastTime)

		// Act
		now := time.Now().UTC()
		subscribers, err := service.GetPendingCancellations(ctx, "stripe", now)

		// Assert
		assert.NoError(t, err)
		assert.Len(t, subscribers, 1)
		assert.Equal(t, uint(888), subscribers[0].UserID)
		assert.NotNil(t, subscribers[0].PricingPlanPeriodID)
		assert.Equal(t, period.ID, *subscribers[0].PricingPlanPeriodID)
		// Verify PricingPlanPeriod is preloaded
		assert.NotNil(t, subscribers[0].PricingPlanPeriod)
		assert.Equal(t, period.ID, subscribers[0].PricingPlanPeriod.ID)
	}, getBillingTestOptions())
}

// ============================================================
// CreateOrUpdateSubscriber with WillCancelAt Tests
// ============================================================

func TestCreateOrUpdateSubscriber_WithWillCancelAt(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE)
		require.NotNil(tb, service)

		// Arrange
		futureTime := time.Now().UTC().Add(1 * time.Hour)

		// Act
		err := service.CreateOrUpdateSubscriber(
			context.Background(),
			999,
			"stripe",
			"cus_999",
			"sub_999",
			true,
			nil,
			pluginCore.WithWillCancelAt(&futureTime),
		)

		// Assert
		assert.NoError(t, err)

		// Verify the subscriber was created with WillCancelAt
		subscriber, err := service.GetActiveSubscriber(context.Background(), 999, "stripe")
		assert.NoError(t, err)
		assert.NotNil(t, subscriber)
		assert.NotNil(t, subscriber.WillCancelAt)
		assert.Equal(t, futureTime.Truncate(time.Second), subscriber.WillCancelAt.Truncate(time.Second))
	}, getBillingTestOptions())
}

func TestCreateOrUpdateSubscriber_UpdateWillCancelAt(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE)
		require.NotNil(tb, service)

		// Arrange - Create subscriber with initial WillCancelAt
		initialTime := time.Now().UTC().Add(1 * time.Hour)
		err := service.CreateOrUpdateSubscriber(
			context.Background(),
			1000,
			"stripe",
			"cus_1000",
			"sub_1000",
			true,
			nil,
			pluginCore.WithWillCancelAt(&initialTime),
		)
		assert.NoError(t, err)

		// Act - Update WillCancelAt
		updatedTime := time.Now().UTC().Add(2 * time.Hour)
		err = service.CreateOrUpdateSubscriber(
			context.Background(),
			1000,
			"stripe",
			"cus_1000",
			"sub_1000",
			true,
			nil,
			pluginCore.WithWillCancelAt(&updatedTime),
		)

		// Assert
		assert.NoError(t, err)

		// Verify the subscriber was updated
		subscriber, err := service.GetActiveSubscriber(context.Background(), 1000, "stripe")
		assert.NoError(t, err)
		assert.NotNil(t, subscriber.WillCancelAt)
		assert.Equal(t, updatedTime.Truncate(time.Second), subscriber.WillCancelAt.Truncate(time.Second))
	}, getBillingTestOptions())
}

// ============================================================
// CancellationReconciliationJob Integration Tests
// ============================================================

func TestCancellationReconciliationJob_NoPendingCancellations(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		// Arrange
		job := NewCancellationReconciliationJob()

		// Get pricing service mock to set up expectations
		mockPricingService := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)

		// Mock the GetPricingPlanPeriod calls (called during gateway registry iteration)
		mockPricingService.EXPECT().GetPricingPlanPeriod(mock.Anything, mock.AnythingOfType("uint")).
			Return(&models.PricingPlanPeriod{}, nil).Maybe()

		// Act
		err := job.Run(ctx, context.Background())

		// Assert
		assert.NoError(t, err)
	}, getBillingTestOptions())
}

func TestCancellationReconciliationJob_WithPendingCancellations_Success(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		// Arrange
		billingSvc := core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE)
		require.NotNil(tb, billingSvc)

		// Reset gateway registry
		gateway.GetRegistry().Reset()

		// Create mock gateway - MockPaymentGateway embeds SubscriptionExecutor so it has ReconcileCancellation
		mockGateway := pluginCore.NewMockPaymentGateway(t)
		mockGateway.EXPECT().ID(mock.Anything).Return("test_gateway")

		// Mock ReconcileCancellation call
		mockGateway.EXPECT().ReconcileCancellation(mock.Anything, uint(12345)).Return(nil)

		// Register the mock gateway
		err := billingSvc.RegisterGateway(context.Background(), mockGateway)
		assert.NoError(tb, err)

		// Create subscriber with past cancellation date
		pastTime := time.Now().UTC().Add(-1 * time.Hour)
		_ = createTestSubscriberWithDB(t, ctx, 12345, "test_gateway", "cus_12345", "sub_12345", true, nil, &pastTime)

		// Get pricing service mock
		mockPricingService := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)

		// Mock GetPricingPlanPeriod calls
		mockPricingService.EXPECT().GetPricingPlanPeriod(mock.Anything, mock.AnythingOfType("uint")).
			Return(&models.PricingPlanPeriod{}, nil).Maybe()

		// Act
		job := NewCancellationReconciliationJob()
		err = job.Run(ctx, context.Background())

		// Assert
		assert.NoError(t, err)
	}, getBillingTestOptions())
}

func TestCancellationReconciliationJob_MultipleGateways(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		// Arrange
		billingSvc := core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE)
		require.NotNil(tb, billingSvc)

		// Reset gateway registry
		gateway.GetRegistry().Reset()

		// Create mock gateways
		mockGateway1 := pluginCore.NewMockPaymentGateway(t)
		mockGateway1.EXPECT().ID(mock.Anything).Return("gateway1")
		mockGateway1.EXPECT().ReconcileCancellation(mock.Anything, uint(11111)).Return(nil)

		mockGateway2 := pluginCore.NewMockPaymentGateway(t)
		mockGateway2.EXPECT().ID(mock.Anything).Return("gateway2")
		mockGateway2.EXPECT().ReconcileCancellation(mock.Anything, uint(22222)).Return(nil)

		// Register gateways
		err := billingSvc.RegisterGateway(context.Background(), mockGateway1)
		assert.NoError(tb, err)
		err = billingSvc.RegisterGateway(context.Background(), mockGateway2)
		assert.NoError(tb, err)

		// Create subscribers for both gateways with past cancellations
		pastTime := time.Now().UTC().Add(-1 * time.Hour)
		_ = createTestSubscriberWithDB(t, ctx, 11111, "gateway1", "cus_11111", "sub_11111", true, nil, &pastTime)
		_ = createTestSubscriberWithDB(t, ctx, 22222, "gateway2", "cus_22222", "sub_22222", true, nil, &pastTime)

		// Get pricing service mock
		mockPricingService := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)

		// Mock GetPricingPlanPeriod calls
		mockPricingService.EXPECT().GetPricingPlanPeriod(mock.Anything, mock.AnythingOfType("uint")).
			Return(&models.PricingPlanPeriod{}, nil).Maybe()

		// Act
		job := NewCancellationReconciliationJob()
		err = job.Run(ctx, context.Background())

		// Assert
		assert.NoError(t, err)
	}, getBillingTestOptions())
}

func TestCancellationReconciliationJob_ReconcileCancellation(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		// Arrange
		billingSvc := core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE)
		require.NotNil(tb, billingSvc)

		// Reset gateway registry
		gateway.GetRegistry().Reset()

		// Create mock gateway
		mockGateway := pluginCore.NewMockPaymentGateway(t)
		mockGateway.EXPECT().ID(mock.Anything).Return("test_gateway")

		// Mock ReconcileCancellation call
		mockGateway.EXPECT().ReconcileCancellation(mock.Anything, uint(33333)).Return(nil)

		// Register the mock gateway
		err := billingSvc.RegisterGateway(context.Background(), mockGateway)
		assert.NoError(tb, err)

		// Create subscriber with past cancellation date
		pastTime := time.Now().UTC().Add(-1 * time.Hour)
		_ = createTestSubscriberWithDB(t, ctx, 33333, "test_gateway", "cus_33333", "sub_33333", true, nil, &pastTime)

		// Get pricing service mock
		mockPricingService := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)

		// Mock GetPricingPlanPeriod calls
		mockPricingService.EXPECT().GetPricingPlanPeriod(mock.Anything, mock.AnythingOfType("uint")).
			Return(&models.PricingPlanPeriod{}, nil).Maybe()

		// Act
		job := NewCancellationReconciliationJob()
		err = job.Run(ctx, context.Background())

		// Assert
		assert.NoError(t, err)
	}, getBillingTestOptions())
}

func TestCancellationReconciliationJob_ReconcileError(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		// Arrange
		billingSvc := core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE)
		require.NotNil(tb, billingSvc)

		// Reset gateway registry
		gateway.GetRegistry().Reset()

		// Create mock gateway
		mockGateway := pluginCore.NewMockPaymentGateway(t)
		mockGateway.EXPECT().ID(mock.Anything).Return("test_error_gateway")

		// Mock ReconcileCancellation to return an error
		mockGateway.EXPECT().ReconcileCancellation(mock.Anything, uint(44444)).
			Return(assert.AnError)

		// Register the mock gateway
		err := billingSvc.RegisterGateway(context.Background(), mockGateway)
		assert.NoError(tb, err)

		// Create subscriber with past cancellation date
		pastTime := time.Now().UTC().Add(-1 * time.Hour)
		_ = createTestSubscriberWithDB(t, ctx, 44444, "test_error_gateway", "cus_44444", "sub_44444", true, nil, &pastTime)

		// Get pricing service mock
		mockPricingService := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)

		// Mock GetPricingPlanPeriod calls
		mockPricingService.EXPECT().GetPricingPlanPeriod(mock.Anything, mock.AnythingOfType("uint")).
			Return(&models.PricingPlanPeriod{}, nil).Maybe()

		// Act
		job := NewCancellationReconciliationJob()
		err = job.Run(ctx, context.Background())

		// Assert
		assert.NoError(t, err, "Job should not fail even if individual reconciliation fails")

		// Subscriber should still be pending when reconciliation fails
		pending, err := billingSvc.GetPendingCancellations(ctx, "test_error_gateway", time.Now().UTC())
		assert.NoError(t, err)
		assert.Len(t, pending, 1, "Subscriber should still be pending when reconciliation fails")
	}, getBillingTestOptions())
}

// ============================================================
// Edge Case Tests
// ============================================================

func TestCancellationReconciliationJob_MixedActiveInactive(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		// Arrange
		billingSvc := core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE)
		require.NotNil(tb, billingSvc)

		// Reset gateway registry
		gateway.GetRegistry().Reset()

		// Register stripe gateway mock
		mockGateway := pluginCore.NewMockPaymentGateway(t)
		mockGateway.EXPECT().ID(mock.Anything).Return("stripe")
		mockGateway.EXPECT().ReconcileCancellation(mock.Anything, uint(55555)).Return(nil)
		mockGateway.EXPECT().ReconcileCancellation(mock.Anything, uint(77777)).Return(nil)
		err := billingSvc.RegisterGateway(context.Background(), mockGateway)
		assert.NoError(tb, err)

		// Create mixed active/inactive subscribers
		pastTime := time.Now().UTC().Add(-1 * time.Hour)

		_ = createTestSubscriberWithDB(t, ctx, 55555, "stripe", "cus_55555", "sub_55555", true, nil, &pastTime)
		_ = createTestSubscriberWithDB(t, ctx, 66666, "stripe", "cus_66666", "sub_66666", false, nil, &pastTime)
		_ = createTestSubscriberWithDB(t, ctx, 77777, "stripe", "cus_77777", "sub_77777", true, nil, &pastTime)

		// Get pricing service mock
		mockPricingService := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)

		// Mock GetPricingPlanPeriod calls
		mockPricingService.EXPECT().GetPricingPlanPeriod(mock.Anything, mock.AnythingOfType("uint")).
			Return(&models.PricingPlanPeriod{}, nil).Maybe()

		// Act
		job := NewCancellationReconciliationJob()
		err = job.Run(ctx, context.Background())

		// Assert
		assert.NoError(t, err)
	}, getBillingTestOptions())
}

func TestCancellationReconciliationJob_CancelledAtIsSet(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		// Arrange
		billingSvc := core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE)
		require.NotNil(tb, billingSvc)

		// Reset gateway registry
		gateway.GetRegistry().Reset()

		// Register stripe gateway mock
		mockGateway := pluginCore.NewMockPaymentGateway(t)
		mockGateway.EXPECT().ID(mock.Anything).Return("stripe")
		mockGateway.EXPECT().ReconcileCancellation(mock.Anything, uint(88888)).Return(nil)
		err := billingSvc.RegisterGateway(context.Background(), mockGateway)
		assert.NoError(tb, err)

		// Get pricing service mock
		mockPricingService := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)

		// Mock GetPricingPlanPeriod calls
		mockPricingService.EXPECT().GetPricingPlanPeriod(mock.Anything, mock.AnythingOfType("uint")).
			Return(&models.PricingPlanPeriod{}, nil).Maybe()

		// Create subscriber with past cancellation and already set CancelledAt
		pastTime := time.Now().UTC().Add(-1 * time.Hour)
		cancelledAt := time.Now().UTC().Add(-2 * time.Hour)

		db := ctx.DB()
		billingStart := time.Now().UTC().Add(-30 * 24 * time.Hour)
		billingEnd := billingStart.Add(30 * 24 * time.Hour)

		subscriber := &models.Subscriber{
			UserID:              88888,
			GatewayType:         "stripe",
			ExternalID:          "cus_88888",
			SubscriptionID:      "sub_88888",
			IsActive:            true,
			BillingPeriodStart:  &billingStart,
			BillingPeriodEnd:    &billingEnd,
			PaymentStatus:       "succeeded",
			WillCancelAt:        &pastTime,
			CancelledAt:         &cancelledAt,
		}
		require.NoError(t, db.Create(subscriber).Error)

		// Act
		job := NewCancellationReconciliationJob()
		err = job.Run(ctx, context.Background())

		// Assert - Job should still process subscribers even if CancelledAt is set
		assert.NoError(t, err)
	}, getBillingTestOptions())
}
