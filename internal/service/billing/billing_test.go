package billing

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"gorm.io/gorm"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	"go.lumeweb.com/portal-plugin-billing/internal"
	"go.lumeweb.com/portal-plugin-billing/internal/config"
	"go.lumeweb.com/portal-plugin-billing/internal/db/migrations"
	"go.lumeweb.com/portal-plugin-billing/internal/db/models"
	"go.lumeweb.com/portal-plugin-billing/internal/gateway"
	quotaCore "go.lumeweb.com/portal-plugin-quota/core"
	"go.lumeweb.com/portal/core"
	coreTesting "go.lumeweb.com/portal/core/testing"
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

		// Test case 3: Multiple subscribers with same subscription ID but different timestamps
		// Should return the most recent (ordered by updated_at DESC)
		err = service.CreateOrUpdateSubscriber(context.Background(), 456, "stripe", "cus_test_67890", "sub_test_12345", true, nil)
		assert.NoError(tb, err)

		subscriber, err = service.GetSubscriberBySubscriptionID(context.Background(), "sub_test_12345", "stripe")
		assert.NoError(tb, err)
		assert.NotNil(tb, subscriber)
		// Should return the most recently updated subscriber (user 456)
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

		// Now test deactivation
		err = service.CreateOrUpdateSubscriber(context.Background(), 1, "stripe", "cus_456", "sub_456", false, &planID)
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
