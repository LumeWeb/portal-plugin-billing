package billing

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	"go.lumeweb.com/portal-plugin-billing/internal"
	"go.lumeweb.com/portal-plugin-billing/internal/config"
	"go.lumeweb.com/portal-plugin-billing/internal/db/migrations"
	"go.lumeweb.com/portal-plugin-billing/internal/db/models"
	"go.lumeweb.com/portal-plugin-billing/internal/gateway"
	quotaCore "go.lumeweb.com/portal-plugin-quota/core"
	"go.lumeweb.com/portal/core"
	coreTesting "go.lumeweb.com/portal/core/testing"
	queryutil "go.lumeweb.com/queryutil"
	"gorm.io/gorm"
)

// Helper function to reduce duplication in test setup
func getBillingTestOptions() coreTesting.TestContextBuilderOption {
	return coreTesting.CombineOptions(
		coreTesting.NewMockPluginBuilder("quota").
			WithMockServiceFactory(quotaCore.QUOTA_SERVICE, quotaCore.NewMockQuotaService).
			WithServiceConfig(quotaCore.QUOTA_SERVICE, coreTesting.NewConfigBuilder().Build()).
			BuilderOption(),
		coreTesting.NewMockPluginBuilder(internal.PLUGIN_NAME).
			WithMigrations(core.DBMigration{core.DB_TYPE_SQLITE: migrations.GetSQLite()}).
			WithService(pluginCore.BILLING_SERVICE, NewBillingService).
			WithServiceConfig(pluginCore.BILLING_SERVICE, &config.ServiceConfig{}).
			WithMockServiceFactory(pluginCore.PRICING_SERVICE, pluginCore.NewMockPricingService).
			WithMockServiceFactory(pluginCore.CREDIT_SERVICE, pluginCore.NewMockCreditService).
			WithServiceConfig(pluginCore.PRICING_SERVICE, coreTesting.NewConfigBuilder().Build()).
			BuilderOption(),
	)
}

func TestBillingService_GetSignatureHeader(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		// Setup
		registry := gateway.NewRegistry()
		mockGateway := pluginCore.NewMockPaymentGateway(tb)
		mockGateway.EXPECT().ID(mock.Anything).Return("stripe")
		mockGateway.EXPECT().SignatureHeader(mock.Anything).Return("Stripe-Signature")

		svc, _, err := NewBillingServiceWithRegistry(registry)
		assert.NoError(tb, err)
		service := svc.(pluginCore.BillingService)

		// Register gateway using the service's RegisterGateway method
		err = service.RegisterGateway(context.Background(), mockGateway)
		assert.NoError(tb, err)

		// Test cases
		tests := []struct {
			name          string
			gatewayType   string
			expected      string
			expectedError error
		}{
			{
				name:        "valid gateway",
				gatewayType: "stripe",
				expected:    "Stripe-Signature",
			},
			{
				name:          "invalid gateway",
				gatewayType:   "invalid",
				expectedError: pluginCore.ErrGatewayNotFound,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(innerT *testing.T) {
				header, err := service.GetSignatureHeader(context.Background(), tt.gatewayType)
				if tt.expectedError != nil {
					assert.ErrorIs(innerT, err, tt.expectedError)
					return
				}
				assert.NoError(innerT, err)
				assert.Equal(innerT, tt.expected, header)
			})
		}

		mockGateway.AssertExpectations(tb)
	})
}

func TestBillingService_ProcessWebhook(t *testing.T) {
	// Test cases
	tests := []struct {
		name          string
		gatewayType   string
		signature     string
		payload       []byte
		expectedError error
		setup         func(mockGateway *pluginCore.MockPaymentGateway)
	}{
		{
			name:        "valid webhook",
			gatewayType: "test",
			signature:   "test_sig",
			payload:     []byte("test_payload"),
			setup: func(mockGateway *pluginCore.MockPaymentGateway) {
				mockGateway.EXPECT().ValidateWebhook(mock.Anything, "test_sig", []byte("test_payload")).
					Return(nil)
				mockGateway.EXPECT().ExtractEventID(mock.Anything, []byte("test_payload")).
					Return("test_event_id", nil)
				mockGateway.EXPECT().ExtractEventType(mock.Anything, []byte("test_payload")).
					Return("test_event_type", nil)
				mockGateway.EXPECT().HandleWebhook(mock.Anything, []byte("test_payload")).
					Return(nil)
			},
		},
		{
			name:          "invalid gateway",
			gatewayType:   "invalid",
			signature:     "test_sig",
			payload:       []byte("test_payload"),
			expectedError: pluginCore.ErrGatewayNotFound,
		},
		{
			name:          "validation failed",
			gatewayType:   "test",
			signature:     "invalid_sig",
			payload:       []byte("test_payload"),
			expectedError: errors.New("webhook validation failed"),
			setup: func(mockGateway *pluginCore.MockPaymentGateway) {
				mockGateway.EXPECT().ExtractEventID(mock.Anything, []byte("test_payload")).
					Return("test_event_id", nil)
				mockGateway.EXPECT().ExtractEventType(mock.Anything, []byte("test_payload")).
					Return("test_event_type", nil)
				mockGateway.EXPECT().ValidateWebhook(mock.Anything, "invalid_sig", []byte("test_payload")).
					Return(errors.New("invalid signature"))
			},
		},
		{
			name:          "handle failed",
			gatewayType:   "test",
			signature:     "test_sig",
			payload:       []byte("test_payload"),
			expectedError: errors.New("failed to handle webhook"),
			setup: func(mockGateway *pluginCore.MockPaymentGateway) {
				mockGateway.EXPECT().ValidateWebhook(mock.Anything, "test_sig", []byte("test_payload")).
					Return(nil)
				mockGateway.EXPECT().ExtractEventID(mock.Anything, []byte("test_payload")).
					Return("test_event_id", nil)
				mockGateway.EXPECT().ExtractEventType(mock.Anything, []byte("test_payload")).
					Return("test_event_type", nil)
				mockGateway.EXPECT().HandleWebhook(mock.Anything, []byte("test_payload")).
					Return(errors.New("processing error"))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
				// Setup registry and service
				var mockGateway *pluginCore.MockPaymentGateway

				service := core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE)
				gateway.GetRegistry().Reset()

				if tt.gatewayType != "invalid" {
					// Setup mock gateway for valid gateway tests
					mockGateway = pluginCore.NewMockPaymentGateway(t)
					mockGateway.EXPECT().ID(mock.Anything).Return("test").Maybe()
					err := service.RegisterGateway(context.Background(), mockGateway)
					assert.NoError(tb, err)
				}

				if tt.setup != nil && mockGateway != nil {
					tt.setup(mockGateway)
				}

				err := service.ProcessWebhook(context.Background(), tt.gatewayType, tt.signature, tt.payload)
				if tt.expectedError != nil {
					if tt.gatewayType == "invalid" {
						assert.ErrorIs(t, err, tt.expectedError)
					} else {
						assert.ErrorContains(t, err, tt.expectedError.Error())
					}
					return
				}
				assert.NoError(t, err)
			},
				getBillingTestOptions())
		})
	}
}

func TestBillingService_ID(t *testing.T) {
	svc, _, err := NewBillingServiceWithRegistry(gateway.NewRegistry())
	assert.NoError(t, err)
	service := svc.(pluginCore.BillingService)
	assert.Equal(t, pluginCore.BILLING_SERVICE, service.ID())
}

func TestBillingService_GetSignatureHeader_UninitializedRegistry(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		// Create service with nil registry - this should return an error
		svc, _, err := NewBillingServiceWithRegistry(nil)
		assert.Error(t, err)
		assert.Nil(t, svc)
		assert.Contains(t, err.Error(), "gateway registry is nil")
	})
}

func TestBillingService_GetGateway(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		// Setup
		registry := gateway.NewRegistry()
		mockGateway := pluginCore.NewMockPaymentGateway(tb)
		mockGateway.EXPECT().ID(mock.Anything).Return("stripe")

		svc, _, err := NewBillingServiceWithRegistry(registry)
		assert.NoError(tb, err)
		service := svc.(pluginCore.BillingService)

		// Register gateway using the service's RegisterGateway method
		err = service.RegisterGateway(context.Background(), mockGateway)
		assert.NoError(tb, err)

		// Test cases
		tests := []struct {
			name          string
			gatewayType   string
			expectedError error
		}{
			{
				name:        "valid gateway",
				gatewayType: "stripe",
			},
			{
				name:          "invalid gateway",
				gatewayType:   "invalid",
				expectedError: pluginCore.ErrGatewayNotFound,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(innerT *testing.T) {
				gateway, err := service.GetGateway(context.Background(), tt.gatewayType)
				if tt.expectedError != nil {
					assert.ErrorIs(innerT, err, tt.expectedError)
					assert.Nil(innerT, gateway)
					return
				}
				assert.NoError(innerT, err)
				assert.Equal(innerT, mockGateway, gateway)
			})
		}

		mockGateway.AssertExpectations(tb)
	})
}

func TestBillingService_GetGateway_UninitializedRegistry(t *testing.T) {
	// Create service with nil registry
	svc, _, err := NewBillingServiceWithRegistry(nil)
	assert.Error(t, err)
	assert.Nil(t, svc)
	assert.Contains(t, err.Error(), "gateway registry is nil")
}

// Tests for new subscriber management methods

func TestBillingService_CreateOrUpdateSubscriber(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE)

		// Test creating a new subscriber
		err := service.CreateOrUpdateSubscriber(context.Background(), 1, "stripe", "cus_123", "sub_123", true, nil)
		assert.NoError(t, err)

		// Verify the subscriber was created
		subscriber, err := service.GetActiveSubscriber(context.Background(), 1, "stripe")
		assert.NoError(t, err)
		assert.NotNil(t, subscriber)
		assert.Equal(t, uint(1), subscriber.UserID)
		assert.Equal(t, "stripe", subscriber.GatewayType)
		assert.Equal(t, "cus_123", subscriber.ExternalID)
		assert.Equal(t, "sub_123", subscriber.SubscriptionID)
		assert.True(t, subscriber.IsActive)
		assert.Nil(t, subscriber.PricingPlanPeriodID)
	},
		getBillingTestOptions())
}

func TestBillingService_GetSubscriberBySubscriptionID(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE)

		// Test case 1: Subscriber not found
		subscriber, err := service.GetSubscriberBySubscriptionID(context.Background(), "nonexistent_sub_id", "stripe")
		assert.NoError(tb, err)
		assert.Nil(tb, subscriber)

		// Test case 2: Create subscriber and find by subscription ID and gateway type
		err = service.CreateOrUpdateSubscriber(context.Background(), 123, "stripe", "cus_test_12345", "sub_test_12345", true, nil)
		assert.NoError(tb, err)

		subscriber, err = service.GetSubscriberBySubscriptionID(context.Background(), "sub_test_12345", "stripe")
		assert.NoError(tb, err)
		assert.NotNil(tb, subscriber)
		assert.Equal(tb, uint(123), subscriber.UserID)
		assert.Equal(tb, "stripe", subscriber.GatewayType)
		assert.Equal(tb, "sub_test_12345", subscriber.SubscriptionID)
		assert.Equal(tb, "cus_test_12345", subscriber.ExternalID)
		assert.True(tb, subscriber.IsActive)

		// Test case 3: Writing the same subscription ID for a (different) user now
		// upserts into the single canonical row for that (gateway, subscription),
		// because a subscription must map to exactly one local row (unique index on
		// gateway_type + sub_key). The row reflects the latest writer (user 456).
		err = service.CreateOrUpdateSubscriber(context.Background(), 456, "stripe", "cus_test_67890", "sub_test_12345", true, nil)
		assert.NoError(tb, err)

		subscriber, err = service.GetSubscriberBySubscriptionID(context.Background(), "sub_test_12345", "stripe")
		assert.NoError(tb, err)
		assert.NotNil(tb, subscriber)
		assert.Equal(tb, uint(456), subscriber.UserID)

		// Test case 4: Different gateway types with same subscription ID (should work independently)
		err = service.CreateOrUpdateSubscriber(context.Background(), 789, "paypal", "cus_test_paypal", "sub_test_12345", true, nil)
		assert.NoError(tb, err)

		// Should find the stripe subscriber (not paypal) when querying for stripe
		subscriber, err = service.GetSubscriberBySubscriptionID(context.Background(), "sub_test_12345", "stripe")
		assert.NoError(tb, err)
		assert.NotNil(tb, subscriber)
		assert.Equal(tb, uint(456), subscriber.UserID)
		assert.Equal(tb, "stripe", subscriber.GatewayType)

		// Should find the paypal subscriber when querying for paypal
		paypalSubscriber, err := service.GetSubscriberBySubscriptionID(context.Background(), "sub_test_12345", "paypal")
		assert.NoError(tb, err)
		assert.NotNil(tb, paypalSubscriber)
		assert.Equal(tb, uint(789), paypalSubscriber.UserID)
		assert.Equal(tb, "paypal", paypalSubscriber.GatewayType)

		// Test case 5: Deactivated subscriber should still be found
		// (method doesn't filter by active status, only by subscription_id and gateway_type)
		err = service.DeactivateSubscriber(context.Background(), 456, "stripe")
		assert.NoError(tb, err)

		subscriber, err = service.GetSubscriberBySubscriptionID(context.Background(), "sub_test_12345", "stripe")
		assert.NoError(tb, err)
		assert.NotNil(tb, subscriber)
		assert.Equal(tb, uint(456), subscriber.UserID)
		assert.False(tb, subscriber.IsActive)

		// Test case 6: Verify all subscriber fields are correctly populated
		planID := uint(99)

		// Mock the GetPricingPlanPeriod call to validate the planID
		mockPricingService := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)
		mockPricingService.EXPECT().GetPricingPlanPeriod(mock.Anything, planID).
			Return(&models.PricingPlanPeriod{
				Model: gorm.Model{ID: planID},
			}, nil)

		err = service.CreateOrUpdateSubscriber(context.Background(), 101, "stripe", "cus_field_test", "sub_field_test", true, &planID)
		assert.NoError(tb, err)

		subscriber, err = service.GetSubscriberBySubscriptionID(context.Background(), "sub_field_test", "stripe")
		assert.NoError(tb, err)
		assert.NotNil(tb, subscriber)
		assert.Equal(tb, uint(101), subscriber.UserID)
		assert.Equal(tb, "stripe", subscriber.GatewayType)
		assert.Equal(tb, "sub_field_test", subscriber.SubscriptionID)
		assert.Equal(tb, "cus_field_test", subscriber.ExternalID)
		assert.True(tb, subscriber.IsActive)
		assert.Equal(tb, planID, *subscriber.PricingPlanPeriodID)

		// Test case 7: Subscribers with different subscription IDs don't interfere
		err = service.CreateOrUpdateSubscriber(context.Background(), 202, "stripe", "cus_202", "sub_202", true, nil)
		assert.NoError(tb, err)

		sub1, err := service.GetSubscriberBySubscriptionID(context.Background(), "sub_field_test", "stripe")
		assert.NoError(tb, err)
		assert.Equal(tb, uint(101), sub1.UserID)

		sub2, err := service.GetSubscriberBySubscriptionID(context.Background(), "sub_202", "stripe")
		assert.NoError(tb, err)
		assert.Equal(tb, uint(202), sub2.UserID)
	},
		getBillingTestOptions())
}

func TestBillingService_GetSubscriberByExternalID(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		// Setup
		service := core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE)

		// Test case 1: Subscriber not found
		subscriber, err := service.GetSubscriberByExternalID(context.Background(), "nonexistent_customer_id", "stripe")
		assert.NoError(tb, err)
		assert.Nil(tb, subscriber)

		// Test case 2: Create subscriber and find by external ID
		err = service.CreateOrUpdateSubscriber(context.Background(), 123, "stripe", "cus_test_12345", "sub_test_12345", true, nil)
		assert.NoError(tb, err)

		subscriber, err = service.GetSubscriberByExternalID(context.Background(), "cus_test_12345", "stripe")
		assert.NoError(tb, err)
		assert.NotNil(tb, subscriber)
		assert.Equal(tb, uint(123), subscriber.UserID)
		assert.Equal(tb, "stripe", subscriber.GatewayType)
		assert.Equal(tb, "cus_test_12345", subscriber.ExternalID)
		assert.Equal(tb, "sub_test_12345", subscriber.SubscriptionID)
		assert.True(tb, subscriber.IsActive)

		// Test case 3: Multiple subscribers with different external IDs
		err = service.CreateOrUpdateSubscriber(context.Background(), 456, "stripe", "cus_test_67890", "sub_test_67890", true, nil)
		assert.NoError(tb, err)

		subscriber1, err := service.GetSubscriberByExternalID(context.Background(), "cus_test_12345", "stripe")
		assert.NoError(tb, err)
		assert.Equal(tb, uint(123), subscriber1.UserID)

		subscriber2, err := service.GetSubscriberByExternalID(context.Background(), "cus_test_67890", "stripe")
		assert.NoError(tb, err)
		assert.Equal(tb, uint(456), subscriber2.UserID)

		// Test case 4: Deactivated subscriber should still be found (method doesn't filter by active status)
		err = service.DeactivateSubscriber(context.Background(), 123, "stripe")
		assert.NoError(tb, err)

		subscriber, err = service.GetSubscriberByExternalID(context.Background(), "cus_test_12345", "stripe")
		assert.NoError(tb, err)
		assert.NotNil(tb, subscriber)
		assert.Equal(tb, uint(123), subscriber.UserID)
		assert.False(tb, subscriber.IsActive)

		// Test case 5: Different gateway types with same external ID (should work independently)
		err = service.CreateOrUpdateSubscriber(context.Background(), 789, "paypal", "cus_test_12345", "sub_paypal_12345", true, nil)
		assert.NoError(tb, err)

		// Should find the stripe subscriber (not paypal) when querying for stripe
		subscriber, err = service.GetSubscriberByExternalID(context.Background(), "cus_test_12345", "stripe")
		assert.NoError(tb, err)
		assert.NotNil(tb, subscriber)
		// Should find the stripe subscriber since we're filtering by gateway type
		assert.Equal(tb, uint(123), subscriber.UserID)
		assert.Equal(tb, "stripe", subscriber.GatewayType)

		// Should find the paypal subscriber when querying for paypal
		paypalSubscriber, err := service.GetSubscriberByExternalID(context.Background(), "cus_test_12345", "paypal")
		assert.NoError(tb, err)
		assert.NotNil(tb, paypalSubscriber)
		assert.Equal(tb, uint(789), paypalSubscriber.UserID)
		assert.Equal(tb, "paypal", paypalSubscriber.GatewayType)
	},
		getBillingTestOptions())
}

func TestBillingService_GetSubscriberByUserAndPeriod(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		// Setup
		service := core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE)
		db := ctx.DB()

		planID := uint(42)
		periodID1 := uint(100)
		periodID2 := uint(200)

		// Create pricing plan
		plan := models.PricingPlan{
			Name:         "Test Plan",
			FeaturesJSON: new(""),
			IsActive:     true,
			IsPublic:     true,
		}
		err := db.Create(&plan).Error
		assert.NoError(tb, err)
		// Get the auto-generated ID
		planID = plan.ID

		// Create pricing plan periods
		pp1 := models.PricingPlanPeriod{
			PricingPlanID: planID,
			Cadence:       "monthly",
			PriceUSD:      10.0,
		}
		err = db.Create(&pp1).Error
		assert.NoError(tb, err)
		periodID1 = pp1.ID

		pp2 := models.PricingPlanPeriod{
			PricingPlanID: planID,
			Cadence:       "yearly",
			PriceUSD:      100.0,
		}
		err = db.Create(&pp2).Error
		assert.NoError(tb, err)
		periodID2 = pp2.ID

		// Test case 1: No subscriber found
		subscriber, err := service.GetSubscriberByUserAndPeriod(context.Background(), 123, periodID1)
		assert.NoError(tb, err)
		assert.Nil(tb, subscriber)

		// Test case 2: Insert subscriber directly with specific period ID
		sub1 := models.Subscriber{
			UserID:              123,
			GatewayType:         "stripe",
			ExternalID:          "cus_test_12345",
			SubscriptionID:      "sub_test_12345",
			IsActive:            true,
			PricingPlanPeriodID: &periodID1,
		}
		err = db.Create(&sub1).Error
		assert.NoError(tb, err)

		// Query by user + period
		subscriber, err = service.GetSubscriberByUserAndPeriod(context.Background(), 123, periodID1)
		assert.NoError(tb, err)
		assert.NotNil(tb, subscriber)
		assert.Equal(tb, uint(123), subscriber.UserID)
		assert.Equal(tb, periodID1, *subscriber.PricingPlanPeriodID)
		assert.True(tb, subscriber.IsActive)

		// Test case 3: Different user with same period should find different subscriber
		sub2 := models.Subscriber{
			UserID:              456,
			GatewayType:         "stripe",
			ExternalID:          "cus_test_45678",
			SubscriptionID:      "sub_test_45678",
			IsActive:            true,
			PricingPlanPeriodID: &periodID1,
		}
		err = db.Create(&sub2).Error
		assert.NoError(tb, err)

		// Should find the subscriber for user 123 using its period ID
		subscriber, err = service.GetSubscriberByUserAndPeriod(context.Background(), 123, periodID1)
		assert.NoError(tb, err)
		assert.NotNil(tb, subscriber)
		assert.Equal(tb, uint(123), subscriber.UserID)

		// Should find the subscriber for user 456 using same period ID
		subscriber, err = service.GetSubscriberByUserAndPeriod(context.Background(), 456, periodID1)
		assert.NoError(tb, err)
		assert.NotNil(tb, subscriber)
		assert.Equal(tb, uint(456), subscriber.UserID)

		// Test case 4: Create another user with a different period, verify GetSubscriberByUserAndPeriod
		// correctly filters by period ID
		sub3 := models.Subscriber{
			UserID:              789,
			GatewayType:         "stripe",
			ExternalID:          "cus_test_789",
			SubscriptionID:      "sub_test_789",
			IsActive:            true,
			PricingPlanPeriodID: &periodID2,
		}
		err = db.Create(&sub3).Error
		assert.NoError(tb, err)

		// Query for periodID2 should find user 789, not user 123
		subscriber, err = service.GetSubscriberByUserAndPeriod(context.Background(), 789, periodID2)
		assert.NoError(tb, err)
		assert.NotNil(tb, subscriber)
		assert.Equal(tb, uint(789), subscriber.UserID)
		assert.Equal(tb, periodID2, *subscriber.PricingPlanPeriodID)

		// Query for periodID1 with user 123 should still find user 123
		subscriber, err = service.GetSubscriberByUserAndPeriod(context.Background(), 123, periodID1)
		assert.NoError(tb, err)
		assert.NotNil(tb, subscriber)
		assert.Equal(tb, uint(123), subscriber.UserID)
		assert.Equal(tb, periodID1, *subscriber.PricingPlanPeriodID)

		// Test case 5: Deactivated subscriber should create history and set nil period
		err = service.DeactivateSubscriber(context.Background(), 123, "stripe")
		assert.NoError(tb, err)

		// Deactivated subscriber has nil period ID (cleared during deactivation)
		subscriber, err = service.GetSubscriberByUserAndPeriod(context.Background(), 123, periodID1)
		assert.NoError(tb, err)
		assert.Nil(tb, subscriber)

		// History should now contain the ended subscription for proration lookups
		history, err := service.GetSubscriptionHistoryByUserAndPeriod(context.Background(), 123, periodID1)
		assert.NoError(tb, err)
		assert.NotNil(tb, history)
		assert.Equal(tb, uint(123), history.UserID)
		assert.Equal(tb, periodID1, history.PricingPlanPeriodID)
		assert.Equal(tb, "stripe", history.PaymentGatewayType)
		assert.NotZero(tb, history.EndedAt)
	},
		getBillingTestOptions())
}

func TestBillingService_CreateOrUpdateSubscriber_WithPlanID(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE)
		mockPricingService := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)

		planID := uint(42)

		// Mock the GetPricingPlanPeriod call to validate the planID
		mockPricingService.EXPECT().GetPricingPlanPeriod(mock.Anything, planID).
			Return(&models.PricingPlanPeriod{
				Model: gorm.Model{ID: planID},
			}, nil)

		err := service.CreateOrUpdateSubscriber(context.Background(), 1, "stripe", "cus_123", "sub_123", true, &planID)
		assert.NoError(t, err)

		// Verify the subscriber was created with plan ID
		subscriber, err := service.GetActiveSubscriber(context.Background(), 1, "stripe")
		assert.NoError(t, err)
		assert.NotNil(t, subscriber)
		assert.NotNil(t, subscriber.PricingPlanPeriodID)
		assert.Equal(t, planID, *subscriber.PricingPlanPeriodID)
		assert.Equal(t, "sub_123", subscriber.SubscriptionID)
	},
		getBillingTestOptions())
}

func TestBillingService_CreateOrUpdateSubscriber_UpdateExisting(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE)
		mockPricingService := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)

		// Create initial subscriber
		err := service.CreateOrUpdateSubscriber(context.Background(), 1, "stripe", "cus_123", "sub_123", true, nil)
		assert.NoError(t, err)

		// Verify initial state
		subscriber, err := service.GetActiveSubscriber(context.Background(), 1, "stripe")
		assert.NoError(t, err)
		assert.NotNil(t, subscriber)
		assert.True(t, subscriber.IsActive)

		// Update the subscriber (change external ID and plan, keep active)
		planID := uint(99)

		// Mock the GetPricingPlanPeriod call to validate the planID
		mockPricingService.EXPECT().GetPricingPlanPeriod(mock.Anything, planID).
			Return(&models.PricingPlanPeriod{
				Model: gorm.Model{ID: planID},
			}, nil)

		err = service.CreateOrUpdateSubscriber(context.Background(), 1, "stripe", "cus_456", "sub_456", true, &planID)
		assert.NoError(t, err)

		// Verify the subscriber was updated and is still active
		subscriber, err = service.GetActiveSubscriber(context.Background(), 1, "stripe")
		assert.NoError(t, err)
		assert.NotNil(t, subscriber)
		assert.Equal(t, "cus_456", subscriber.ExternalID)
		assert.Equal(t, "sub_456", subscriber.SubscriptionID)
		assert.True(t, subscriber.IsActive)
		assert.Equal(t, planID, *subscriber.PricingPlanPeriodID)

		// Now test deactivation. Production deactivation goes through
		// DeactivateSubscriber (cancel/pause events). CreateOrUpdateSubscriber(false)
		// is a monotonic pending-write and intentionally does NOT regress an active
		// subscription (out-of-order webhook safety).
		err = service.DeactivateSubscriber(context.Background(), 1, "stripe")
		assert.NoError(t, err)

		// Verify the subscriber is no longer active
		subscriber, err = service.GetActiveSubscriber(context.Background(), 1, "stripe")
		assert.NoError(t, err) // Should not find active subscriber, return nil without error
		assert.Nil(t, subscriber)

		// Verify user is not active subscriber
		isActive, err := service.IsUserActiveSubscriber(context.Background(), 1)
		assert.NoError(t, err)
		assert.False(t, isActive)
	},
		getBillingTestOptions())
}

func TestBillingService_CreateOrUpdateSubscriber_InvalidPricingPlanPeriod(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE)
		mockPricingService := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)

		// Test with a non-existent pricing plan period ID
		invalidPeriodID := uint(99999)
		mockPricingService.EXPECT().GetPricingPlanPeriod(mock.Anything, invalidPeriodID).
			Return(nil, errors.New("pricing plan period not found"))

		err := service.CreateOrUpdateSubscriber(context.Background(), 1, "stripe", "cus_123", "sub_123", true, &invalidPeriodID)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "pricing plan period")
	},
		getBillingTestOptions())
}

func TestBillingService_CreateOrUpdateSubscriber_ValidPricingPlanPeriod(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		billingService := core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE)
		mockPricingService := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)

		// Mock a valid pricing plan period ID
		validPeriodID := uint(123)
		mockPricingService.EXPECT().GetPricingPlanPeriod(mock.Anything, validPeriodID).
			Return(&models.PricingPlanPeriod{
				Model:         gorm.Model{ID: validPeriodID},
				PricingPlanID: 1,
				Cadence:       "monthly",
				PriceUSD:      19.99,
				QuotaPlanID:   1,
			}, nil)

		// Create a subscriber with the valid period ID
		err := billingService.CreateOrUpdateSubscriber(context.Background(), 1, "stripe", "cus_123", "sub_123", true, &validPeriodID)
		assert.NoError(t, err)

		// Verify the subscriber was created with the period ID
		subscriber, err := billingService.GetActiveSubscriber(context.Background(), 1, "stripe")
		assert.NoError(t, err)
		assert.NotNil(t, subscriber)
		assert.Equal(t, validPeriodID, *subscriber.PricingPlanPeriodID)
	},
		getBillingTestOptions())
}

func TestBillingService_DeactivateSubscriber(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE)

		// Create active subscriber
		err := service.CreateOrUpdateSubscriber(context.Background(), 1, "stripe", "cus_123", "sub_123", true, nil)
		assert.NoError(t, err)

		// Verify it's active
		subscriber, err := service.GetActiveSubscriber(context.Background(), 1, "stripe")
		assert.NoError(t, err)
		assert.True(t, subscriber.IsActive)

		// Deactivate the subscriber
		err = service.DeactivateSubscriber(context.Background(), 1, "stripe")
		assert.NoError(t, err)

		// Verify it's no longer active
		subscriber, err = service.GetActiveSubscriber(context.Background(), 1, "stripe")
		assert.NoError(t, err) // Should not find active subscriber, return nil without error
		assert.Nil(t, subscriber)
	},
		getBillingTestOptions())
}

func TestBillingService_GetActiveSubscriber(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE)

		// Test with no subscribers
		subscriber, err := service.GetActiveSubscriber(context.Background(), 1, "stripe")
		assert.NoError(t, err)
		assert.Nil(t, subscriber)

		// Create an active subscriber
		err = service.CreateOrUpdateSubscriber(context.Background(), 1, "stripe", "cus_123", "sub_123", true, nil)
		assert.NoError(t, err)

		// Should find the active subscriber
		subscriber, err = service.GetActiveSubscriber(context.Background(), 1, "stripe")
		assert.NoError(t, err)
		assert.NotNil(t, subscriber)
		assert.Equal(t, uint(1), subscriber.UserID)
		assert.Equal(t, "stripe", subscriber.GatewayType)
		assert.True(t, subscriber.IsActive)

		// Test with inactive subscriber
		err = service.DeactivateSubscriber(context.Background(), 1, "stripe")
		assert.NoError(t, err)

		// Should not find inactive subscriber
		subscriber, err = service.GetActiveSubscriber(context.Background(), 1, "stripe")
		assert.NoError(t, err)
		assert.Nil(t, subscriber)
	},
		getBillingTestOptions())
}

func TestBillingService_IsUserActiveSubscriber(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE)

		// Test with no subscribers
		isActive, err := service.IsUserActiveSubscriber(context.Background(), 1)
		assert.NoError(t, err)
		assert.False(t, isActive)

		// Create an active subscriber
		err = service.CreateOrUpdateSubscriber(context.Background(), 1, "stripe", "cus_123", "sub_123", true, nil)
		assert.NoError(t, err)

		// Should be active
		isActive, err = service.IsUserActiveSubscriber(context.Background(), 1)
		assert.NoError(t, err)
		assert.True(t, isActive)

		// Test with different user
		isActive, err = service.IsUserActiveSubscriber(context.Background(), 2)
		assert.NoError(t, err)
		assert.False(t, isActive)

		// Deactivate subscriber
		err = service.DeactivateSubscriber(context.Background(), 1, "stripe")
		assert.NoError(t, err)

		// Should no longer be active
		isActive, err = service.IsUserActiveSubscriber(context.Background(), 1)
		assert.NoError(t, err)
		assert.False(t, isActive)
	},
		getBillingTestOptions())
}

func TestBillingService_GetActiveSubscribersByGateway(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE)

		// Test with no subscribers
		subscribers, err := service.GetActiveSubscribersByGateway(context.Background(), "stripe")
		assert.NoError(t, err)
		assert.Empty(t, subscribers)

		// Create multiple active subscribers for stripe
		err = service.CreateOrUpdateSubscriber(context.Background(), 1, "stripe", "sub_123", "", true, nil)
		assert.NoError(t, err)
		err = service.CreateOrUpdateSubscriber(context.Background(), 2, "stripe", "sub_456", "", true, nil)
		assert.NoError(t, err)

		// Create subscriber for different gateway
		err = service.CreateOrUpdateSubscriber(context.Background(), 3, "paypal", "sub_789", "", true, nil)
		assert.NoError(t, err)

		// Should find only stripe subscribers
		subscribers, err = service.GetActiveSubscribersByGateway(context.Background(), "stripe")
		assert.NoError(t, err)
		assert.Len(t, subscribers, 2)

		// Create inactive subscriber
		err = service.CreateOrUpdateSubscriber(context.Background(), 4, "stripe", "sub_999", "", false, nil)
		assert.NoError(t, err)

		// Should still only find active ones
		subscribers, err = service.GetActiveSubscribersByGateway(context.Background(), "stripe")
		assert.NoError(t, err)
		assert.Len(t, subscribers, 2)
	},
		getBillingTestOptions())
}

func TestBillingService_GetActiveSubscription(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE)

		// Test with no subscriptions
		subscription, err := service.GetActiveSubscription(context.Background(), 1)
		assert.NoError(t, err)
		assert.Nil(t, subscription)

		// Create active subscription for stripe
		err = service.CreateOrUpdateSubscriber(context.Background(), 1, "stripe", "sub_123", "", true, nil)
		assert.NoError(t, err)

		// Should find the subscription
		subscription, err = service.GetActiveSubscription(context.Background(), 1)
		assert.NoError(t, err)
		assert.NotNil(t, subscription)
		assert.Equal(t, "stripe", subscription.GatewayType)

		// Create active subscription for paypal (different user)
		err = service.CreateOrUpdateSubscriber(context.Background(), 2, "paypal", "sub_456", "", true, nil)
		assert.NoError(t, err)

		// Should still find stripe subscription for user 1
		subscription, err = service.GetActiveSubscription(context.Background(), 1)
		assert.NoError(t, err)
		assert.Equal(t, "stripe", subscription.GatewayType)

		// Create multiple active subscriptions for same user (different gateways)
		err = service.CreateOrUpdateSubscriber(context.Background(), 1, "paypal", "sub_789", "", true, nil)
		assert.NoError(t, err)

		// Should find one of them (order not guaranteed)
		subscription, err = service.GetActiveSubscription(context.Background(), 1)
		assert.NoError(t, err)
		assert.NotNil(t, subscription)
		assert.Equal(t, uint(1), subscription.UserID)
		assert.True(t, subscription.IsActive)

		// Deactivate stripe subscription
		err = service.DeactivateSubscriber(context.Background(), 1, "stripe")
		assert.NoError(t, err)

		// Should find paypal subscription
		subscription, err = service.GetActiveSubscription(context.Background(), 1)
		assert.NoError(t, err)
		assert.Equal(t, "paypal", subscription.GatewayType)

		// Deactivate paypal subscription
		err = service.DeactivateSubscriber(context.Background(), 1, "paypal")
		assert.NoError(t, err)

		// Should find no subscription
		subscription, err = service.GetActiveSubscription(context.Background(), 1)
		assert.NoError(t, err)
		assert.Nil(t, subscription)
	},
		getBillingTestOptions())
}

func TestBillingService_GetSubscriberByID(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE)

		subscriber, err := service.GetSubscriberByID(context.Background(), 999)
		assert.NoError(t, err)
		assert.Nil(t, subscriber)

		err = service.CreateOrUpdateSubscriber(context.Background(), 1, "stripe", "sub_123", "", true, nil)
		assert.NoError(t, err)

		allSubs, err := service.GetActiveSubscribersByGateway(context.Background(), "stripe")
		assert.NoError(t, err)
		assert.Len(tb, allSubs, 1)
		subscriberID := allSubs[0].ID

		subscriber, err = service.GetSubscriberByID(context.Background(), subscriberID)
		assert.NoError(t, err)
		assert.NotNil(t, subscriber)
		assert.Equal(t, uint(1), subscriber.UserID)
		assert.Equal(t, "stripe", subscriber.GatewayType)
		assert.True(t, subscriber.IsActive)
	},
		getBillingTestOptions())
}

func TestBillingService_GetSubscribersByUserID(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE)

		subscribers, err := service.GetSubscribersByUserID(context.Background(), 999)
		assert.NoError(t, err)
		assert.Empty(t, subscribers)

		err = service.CreateOrUpdateSubscriber(context.Background(), 1, "stripe", "ext_123", "", true, nil)
		assert.NoError(t, err)
		err = service.CreateOrUpdateSubscriber(context.Background(), 1, "paypal", "ext_456", "", true, nil)
		assert.NoError(t, err)

		err = service.CreateOrUpdateSubscriber(context.Background(), 2, "stripe", "ext_999", "", true, nil)
		assert.NoError(t, err)

		subscribers, err = service.GetSubscribersByUserID(context.Background(), 1)
		assert.NoError(t, err)
		assert.Len(tb, subscribers, 2)

		for _, sub := range subscribers {
			assert.Equal(t, uint(1), sub.UserID)
		}
	},
		getBillingTestOptions())
}

func TestBillingService_ListSubscribers(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE)

		pagination, _ := queryutil.NewPagination(0, 10)

		subscribers, total, err := service.ListSubscribers(context.Background(), nil, nil, pagination)
		assert.NoError(t, err)
		assert.Empty(t, subscribers)
		assert.Equal(t, int64(0), total)

		err = service.CreateOrUpdateSubscriber(context.Background(), 1, "stripe", "sub_123", "", true, nil)
		assert.NoError(t, err)
		err = service.CreateOrUpdateSubscriber(context.Background(), 2, "stripe", "sub_456", "", true, nil)
		assert.NoError(t, err)
		err = service.CreateOrUpdateSubscriber(context.Background(), 3, "paypal", "sub_789", "", true, nil)
		assert.NoError(t, err)

		subscribers, total, err = service.ListSubscribers(context.Background(), nil, nil, pagination)
		assert.NoError(t, err)
		assert.Len(tb, subscribers, 3)
		assert.Equal(t, int64(3), total)

		filters := []queryutil.CrudFilter{
			queryutil.Equal("gateway_type", "stripe"),
		}
		subscribers, total, err = service.ListSubscribers(context.Background(), filters, nil, pagination)
		assert.NoError(t, err)
		assert.Len(tb, subscribers, 2)
		assert.Equal(t, int64(2), total)

		limitedPagination, _ := queryutil.NewPagination(0, 1)
		subscribers, total, err = service.ListSubscribers(context.Background(), nil, nil, limitedPagination)
		assert.NoError(t, err)
		assert.Len(tb, subscribers, 1)
		assert.Equal(t, int64(3), total)
	},
		getBillingTestOptions())
}

// --- Subscriber duplicate/ordering hardening regressions ---------------------
//
// A single signup produced TWO billing_subscribers rows for the same user, both
// is_active=false, and activation then permanently failed with "UNIQUE constraint
// failed ... gateway_type, sub_key".
//
// The fix has three layers:
//   1. A unique index (gateway_type, sub_key) so one subscription maps to at most one
//      local row - concurrent pending creates (checkout + invoice) can no longer both
//      insert.
//   2. CreateOrUpdateSubscriber is now subscription-scoped + atomic (ON CONFLICT), so
//      the check-then-act TOCTOU that created duplicates is gone.
//   3. Activation is monotonic (never regresses active->inactive) and retires peer
//      active rows, preserving the "one active subscription per user per gateway"
//      invariant under out-of-order webhook delivery.

// Two concurrent pending creates for the SAME subscription must yield exactly one row.
func TestCreateOrUpdateSubscriber_RaceCreatesSingleRow(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE)
		db := ctx.DB()

		const userID = uint(14)
		const gateway = "stripe"
		const subscriptionID = "sub_1U3eGcAB5Hx8WYZCOAt2X3ML"

		var wg sync.WaitGroup
		errs := make([]error, 4)
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				errs[idx] = service.CreateOrUpdateSubscriber(
					context.Background(), userID, gateway, "cus_V3lmEbst79DDE5", subscriptionID, false, nil,
				)
			}(i)
		}
		wg.Wait()

		for _, e := range errs {
			assert.NoError(t, e)
		}

		var count int64
		err := db.Model(&models.Subscriber{}).
			Where("gateway_type = ? AND subscription_id = ?", gateway, subscriptionID).
			Count(&count).Error
		assert.NoError(t, err)
		assert.Equalf(t, int64(1), count,
			"one subscription must map to exactly one row; got %d (duplicate pending rows)", count)
	},
		getBillingTestOptions())
}

// The unique index (gateway_type, sub_key) rejects a second row for an existing
// subscription_id at the database level, even bypassing the service layer.
func TestSubscriberUniqueIndexRejectsDuplicateSubscription(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		db := ctx.DB()

		// The hardening migration must have applied: the generated sub_key column and
		// the composite unique index must exist in the schema.
		assert.True(t, db.Migrator().HasColumn(&models.Subscriber{}, "sub_key"),
			"sub_key generated column missing - hardening migration did not apply")

		// Two rows for the SAME user with the same subscription must be rejected.
		first := models.Subscriber{
			UserID: 1, GatewayType: "stripe", ExternalID: "cus_1",
			SubscriptionID: "sub_dup", IsActive: false,
		}
		assert.NoError(t, db.Create(&first).Error)

		dupSameUser := models.Subscriber{
			UserID: 1, GatewayType: "stripe", ExternalID: "cus_1b",
			SubscriptionID: "sub_dup", IsActive: false,
		}
		err := db.Create(&dupSameUser).Error
		assert.Error(t, err, "second row for same user+gateway+subscription must be rejected by unique index")

		// A DIFFERENT user may hold the same subscription id (per-user scoping).
		other := models.Subscriber{
			UserID: 2, GatewayType: "stripe", ExternalID: "cus_2",
			SubscriptionID: "sub_dup", IsActive: false,
		}
		assert.NoError(t, db.Create(&other).Error, "different user with same subscription id must be allowed")
	},
		getBillingTestOptions())
}

// Out-of-order delivery: checkout.session.completed (is_active=false) processed AFTER
// invoice.paid (is_active=true) must NOT deactivate the paid subscription. Activation
// is monotonic.
func TestPendingWriteDoesNotDeactivateActiveSubscription(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE)

		const userID = uint(14)
		const gateway = "stripe"
		const externalID = "cus_V3lmEbst79DDE5"
		const subscriptionID = "sub_1U3eGcAB5Hx8WYZCOAt2X3ML"

		// invoice.paid activates first.
		assert.NoError(t, service.CreateOrUpdateSubscriber(
			context.Background(), userID, gateway, externalID, subscriptionID, true, nil))

		// checkout.session.completed arrives late with a pending (false) write.
		assert.NoError(t, service.CreateOrUpdateSubscriber(
			context.Background(), userID, gateway, externalID, subscriptionID, false, nil))

		active, err := service.GetActiveSubscription(context.Background(), userID)
		assert.NoError(t, err)
		assert.NotNil(t, active, "late pending write must not deactivate a paid subscription")
		assert.True(t, active.IsActive)
	},
		getBillingTestOptions())
}

// Plan change: activating a NEW subscription for a user must retire the old active
// row (one active subscription per user per gateway) while keeping the rows distinct.
func TestNewSubscriptionReplacesActiveSubscription(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE)
		db := ctx.DB()

		const userID = uint(4)
		const gateway = "stripe"

		assert.NoError(t, service.CreateOrUpdateSubscriber(
			context.Background(), userID, gateway, "cus_old", "sub_old", true, nil))
		assert.NoError(t, service.CreateOrUpdateSubscriber(
			context.Background(), userID, gateway, "cus_new", "sub_new", true, nil))

		active, err := service.GetActiveSubscription(context.Background(), userID)
		assert.NoError(t, err)
		assert.NotNil(t, active)
		assert.Equal(t, "sub_new", active.SubscriptionID)

		// Old row should be deactivated.
		var count int64
		assert.NoError(t, db.Model(&models.Subscriber{}).
			Where("subscription_id = ? AND is_active = ?", "sub_old", true).
			Count(&count).Error)
		assert.Zero(t, count)
	},
		getBillingTestOptions())
}

// A cross-user write must NOT hijack or deactivate another user's subscription row.
// The unique index is scoped (user_id, gateway_type, sub_key), so a different user's
// pending create for the same subscription id lands in its own separate row instead of
// colliding with, reassigning, or deactivating the owner's active row.
func TestCrossUserWriteDoesNotHijackActiveSubscription(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE)
		db := ctx.DB()

		const gateway = "stripe"
		const subscriptionID = "sub_shared"

		// Pre-existing active row owned by user 999.
		assert.NoError(t, db.Create(&models.Subscriber{
			UserID: 999, GatewayType: gateway, ExternalID: "cus_owner",
			SubscriptionID: subscriptionID, IsActive: true,
		}).Error)

		// user 14 writes a pending (inactive) create for the same subscription id.
		// With per-user scoping this must NOT reassign user 999's row to user 14.
		assert.NoError(t, service.CreateOrUpdateSubscriber(
			context.Background(), 14, gateway, "cus_14", subscriptionID, false, nil))

		// Owner's row must remain active and owned by user 999.
		var owner models.Subscriber
		assert.NoError(t, db.Unscoped().Where("user_id = ? AND subscription_id = ?", 999, subscriptionID).
			First(&owner).Error)
		assert.True(t, owner.IsActive, "owner's active subscription must not be deactivated or hijacked")
		assert.Equal(t, "cus_owner", owner.ExternalID)

		// User 14 must have its own separate row.
		var mine models.Subscriber
		assert.NoError(t, db.Unscoped().Where("user_id = ? AND subscription_id = ?", 14, subscriptionID).
			First(&mine).Error)
		assert.False(t, mine.IsActive, "user 14's pending row must remain their own (inactive) row")
		assert.NotEqual(t, 999, mine.UserID)
	},
		getBillingTestOptions())
}

// A late pending write carrying a nil pricing plan period must NOT wipe the plan
// already set on an active subscription (out-of-order webhook safety for plan/quota
// mapping). pricing_plan_period_id is only written when this call carries a non-nil
// value.
func TestLatePendingWriteDoesNotClearPlanPeriod(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE)
		mockPricingService := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)

		const userID = uint(14)
		const gateway = "stripe"
		const externalID = "cus_V3lmEbst79DDE5"
		const subscriptionID = "sub_1U3eGcAB5Hx8WYZCOAt2X3ML"
		planID := uint(1)

		// Plan validation is invoked only when a non-nil plan id is passed.
		mockPricingService.EXPECT().GetPricingPlanPeriod(mock.Anything, planID).
			Return(&models.PricingPlanPeriod{Model: gorm.Model{ID: planID}}, nil).Maybe()

		// invoice.paid activates the subscription with a plan period set.
		assert.NoError(t, service.CreateOrUpdateSubscriber(
			context.Background(), userID, gateway, externalID, subscriptionID, true, &planID))

		// checkout.session.completed arrives late with a pending (false) write that
		// carries no plan period - it must not clear the plan already set.
		assert.NoError(t, service.CreateOrUpdateSubscriber(
			context.Background(), userID, gateway, externalID, subscriptionID, false, nil))

		db := ctx.DB()
		var sub models.Subscriber
		assert.NoError(t, db.Unscoped().
			Where("user_id = ? AND subscription_id = ?", userID, subscriptionID).
			First(&sub).Error)
		assert.True(t, sub.IsActive)
		assert.NotNil(t, sub.PricingPlanPeriodID, "late nil-plan write must not clear the active plan period")
		assert.Equal(t, planID, *sub.PricingPlanPeriodID)
	},
		getBillingTestOptions())
}

// End-to-end recovery: a pending (inactive) subscriber is activated by a later
// webhook (e.g. invoice.paid) and becomes the recognized active subscription.
// This is the recovery path for a subscription left in a permanently-inactive state.
func TestRecovery_ActivationAfterPendingCreate(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE)

		const userID = uint(14)
		const gateway = "stripe"
		const externalID = "cus_V3lmEbst79DDE5"
		const subscriptionID = "sub_1U3eGcAB5Hx8WYZCOAt2X3ML"

		// checkout.session.completed creates a pending (inactive) subscription first.
		assert.NoError(t, service.CreateOrUpdateSubscriber(
			context.Background(), userID, gateway, externalID, subscriptionID, false, nil))

		// Webhook replay (invoice.paid) activates the canonical row.
		assert.NoError(t, service.CreateOrUpdateSubscriber(
			context.Background(), userID, gateway, externalID, subscriptionID, true, nil))

		active, err := service.GetActiveSubscription(context.Background(), userID)
		assert.NoError(t, err)
		assert.NotNil(t, active, "activation must make the subscription recognized")
		assert.True(t, active.IsActive)
		assert.Equal(t, subscriptionID, active.SubscriptionID)
	},
		getBillingTestOptions())
}
