package billing

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	"go.lumeweb.com/portal-plugin-billing/internal"
	"go.lumeweb.com/portal-plugin-billing/internal/config"
	"go.lumeweb.com/portal-plugin-billing/internal/db/migrations"
	"go.lumeweb.com/portal-plugin-billing/internal/gateway"
	"go.lumeweb.com/portal/core"
	coreTesting "go.lumeweb.com/portal/core/testing"
)

// Helper function to reduce duplication in test setup
func getBillingTestOptions() coreTesting.TestContextBuilderOption {
	return coreTesting.CombineOptions(
		coreTesting.NewMockPluginBuilder(internal.PLUGIN_NAME).
			WithMigrations(core.DBMigration{core.DB_TYPE_SQLITE: migrations.GetSQLite()}).
			WithService(pluginCore.BILLING_SERVICE, NewBillingService).
			BuilderOption(),
		coreTesting.WithServiceConfig(internal.PLUGIN_NAME, pluginCore.BILLING_SERVICE, &config.ServiceConfig{}),
	)
}

func TestBillingService_GetSignatureHeader(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		// Setup
		registry := gateway.NewRegistry()
		mockGateway := new(pluginCore.MockPaymentGateway)
		mockGateway.On("ID").Return("stripe")
		mockGateway.On("SignatureHeader").Return("Stripe-Signature")

		svc, _, err := NewBillingServiceWithRegistry(registry)
		assert.NoError(tb, err)
		service := svc.(pluginCore.BillingService)

		// Register gateway using the service's RegisterGateway method
		err = service.RegisterGateway(mockGateway)
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

		t := tb.(*testing.T)

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				header, err := service.GetSignatureHeader(tt.gatewayType)
				if tt.expectedError != nil {
					assert.ErrorIs(t, err, tt.expectedError)
					return
				}
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, header)
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
				mockGateway.On("ValidateWebhook", mock.Anything, "test_sig", []byte("test_payload")).
					Return(nil)
				mockGateway.On("ExtractEventID", []byte("test_payload")).
					Return("test_event_id", nil)
				mockGateway.On("ExtractEventType", []byte("test_payload")).
					Return("test_event_type", nil)
				mockGateway.On("HandleWebhook", mock.Anything, []byte("test_payload")).
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
				mockGateway.On("ValidateWebhook", mock.Anything, "invalid_sig", []byte("test_payload")).
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
				mockGateway.On("ValidateWebhook", mock.Anything, "test_sig", []byte("test_payload")).
					Return(nil)
				mockGateway.On("ExtractEventID", []byte("test_payload")).
					Return("test_event_id", nil)
				mockGateway.On("ExtractEventType", []byte("test_payload")).
					Return("test_event_type", nil)
				mockGateway.On("HandleWebhook", mock.Anything, []byte("test_payload")).
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
					mockGateway.On("ID").Return("test")
					err := service.RegisterGateway(mockGateway)
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
				coreTesting.CombineOptions(
					coreTesting.NewMockPluginBuilder(internal.PLUGIN_NAME).
						WithMigrations(core.DBMigration{core.DB_TYPE_SQLITE: migrations.GetSQLite()}).
						WithService(pluginCore.BILLING_SERVICE, NewBillingService).
						BuilderOption(),
					coreTesting.WithServiceConfig(internal.PLUGIN_NAME, pluginCore.BILLING_SERVICE, &config.ServiceConfig{}),
				))
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
		mockGateway := new(pluginCore.MockPaymentGateway)
		mockGateway.On("ID").Return("stripe")

		svc, _, err := NewBillingServiceWithRegistry(registry)
		assert.NoError(tb, err)
		service := svc.(pluginCore.BillingService)

		// Register gateway using the service's RegisterGateway method
		err = service.RegisterGateway(mockGateway)
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

		t := tb.(*testing.T)

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				gateway, err := service.GetGateway(tt.gatewayType)
				if tt.expectedError != nil {
					assert.ErrorIs(t, err, tt.expectedError)
					assert.Nil(t, gateway)
					return
				}
				assert.NoError(t, err)
				assert.Equal(t, mockGateway, gateway)
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
		err := service.CreateOrUpdateSubscriber(1, "stripe", "sub_123", true, nil)
		assert.NoError(t, err)

		// Verify the subscriber was created
		subscriber, err := service.GetActiveSubscriber(1, "stripe")
		assert.NoError(t, err)
		assert.NotNil(t, subscriber)
		assert.Equal(t, uint(1), subscriber.UserID)
		assert.Equal(t, "stripe", subscriber.GatewayType)
		assert.Equal(t, "sub_123", subscriber.GatewayID)
		assert.True(t, subscriber.IsActive)
		assert.Nil(t, subscriber.PlanID)
	},
		getBillingTestOptions())
}

func TestBillingService_CreateOrUpdateSubscriber_WithPlanID(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE)

		planID := uint(42)
		err := service.CreateOrUpdateSubscriber(1, "stripe", "sub_123", true, &planID)
		assert.NoError(t, err)

		// Verify the subscriber was created with plan ID
		subscriber, err := service.GetActiveSubscriber(1, "stripe")
		assert.NoError(t, err)
		assert.NotNil(t, subscriber)
		assert.NotNil(t, subscriber.PlanID)
		assert.Equal(t, planID, *subscriber.PlanID)
	},
		getBillingTestOptions())
}

func TestBillingService_CreateOrUpdateSubscriber_UpdateExisting(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE)

		// Create initial subscriber
		err := service.CreateOrUpdateSubscriber(1, "stripe", "sub_123", true, nil)
		assert.NoError(t, err)

		// Verify initial state
		subscriber, err := service.GetActiveSubscriber(1, "stripe")
		assert.NoError(t, err)
		assert.NotNil(t, subscriber)
		assert.True(t, subscriber.IsActive)

		// Update the subscriber (change gateway ID and plan, keep active)
		planID := uint(99)
		err = service.CreateOrUpdateSubscriber(1, "stripe", "sub_456", true, &planID)
		assert.NoError(t, err)

		// Verify the subscriber was updated and is still active
		subscriber, err = service.GetActiveSubscriber(1, "stripe")
		assert.NoError(t, err)
		assert.NotNil(t, subscriber)
		assert.Equal(t, "sub_456", subscriber.GatewayID)
		assert.True(t, subscriber.IsActive)
		assert.Equal(t, planID, *subscriber.PlanID)

		// Now test deactivation
		err = service.CreateOrUpdateSubscriber(1, "stripe", "sub_456", false, &planID)
		assert.NoError(t, err)

		// Verify the subscriber is no longer active
		subscriber, err = service.GetActiveSubscriber(1, "stripe")
		assert.NoError(t, err) // Should not find active subscriber, return nil without error
		assert.Nil(t, subscriber)

		// Verify user is not active subscriber
		isActive, err := service.IsUserActiveSubscriber(1)
		assert.NoError(t, err)
		assert.False(t, isActive)
	},
		getBillingTestOptions())
}

func TestBillingService_DeactivateSubscriber(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE)

		// Create active subscriber
		err := service.CreateOrUpdateSubscriber(1, "stripe", "sub_123", true, nil)
		assert.NoError(t, err)

		// Verify it's active
		subscriber, err := service.GetActiveSubscriber(1, "stripe")
		assert.NoError(t, err)
		assert.True(t, subscriber.IsActive)

		// Deactivate the subscriber
		err = service.DeactivateSubscriber(1, "stripe")
		assert.NoError(t, err)

		// Verify it's no longer active
		subscriber, err = service.GetActiveSubscriber(1, "stripe")
		assert.NoError(t, err) // Should not find active subscriber, return nil without error
		assert.Nil(t, subscriber)
	},
		getBillingTestOptions())
}

func TestBillingService_GetActiveSubscriber(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE)

		// Test with no subscribers
		subscriber, err := service.GetActiveSubscriber(1, "stripe")
		assert.NoError(t, err)
		assert.Nil(t, subscriber)

		// Create an active subscriber
		err = service.CreateOrUpdateSubscriber(1, "stripe", "sub_123", true, nil)
		assert.NoError(t, err)

		// Should find the active subscriber
		subscriber, err = service.GetActiveSubscriber(1, "stripe")
		assert.NoError(t, err)
		assert.NotNil(t, subscriber)
		assert.Equal(t, uint(1), subscriber.UserID)
		assert.Equal(t, "stripe", subscriber.GatewayType)
		assert.True(t, subscriber.IsActive)

		// Test with inactive subscriber
		err = service.DeactivateSubscriber(1, "stripe")
		assert.NoError(t, err)

		// Should not find inactive subscriber
		subscriber, err = service.GetActiveSubscriber(1, "stripe")
		assert.NoError(t, err)
		assert.Nil(t, subscriber)
	},
		getBillingTestOptions())
}

func TestBillingService_IsUserActiveSubscriber(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE)

		// Test with no subscribers
		isActive, err := service.IsUserActiveSubscriber(1)
		assert.NoError(t, err)
		assert.False(t, isActive)

		// Create an active subscriber
		err = service.CreateOrUpdateSubscriber(1, "stripe", "sub_123", true, nil)
		assert.NoError(t, err)

		// Should be active
		isActive, err = service.IsUserActiveSubscriber(1)
		assert.NoError(t, err)
		assert.True(t, isActive)

		// Test with different user
		isActive, err = service.IsUserActiveSubscriber(2)
		assert.NoError(t, err)
		assert.False(t, isActive)

		// Deactivate subscriber
		err = service.DeactivateSubscriber(1, "stripe")
		assert.NoError(t, err)

		// Should no longer be active
		isActive, err = service.IsUserActiveSubscriber(1)
		assert.NoError(t, err)
		assert.False(t, isActive)
	},
		getBillingTestOptions())
}

func TestBillingService_GetActiveSubscribersByGateway(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE)

		// Test with no subscribers
		subscribers, err := service.GetActiveSubscribersByGateway("stripe")
		assert.NoError(t, err)
		assert.Empty(t, subscribers)

		// Create multiple active subscribers for stripe
		err = service.CreateOrUpdateSubscriber(1, "stripe", "sub_123", true, nil)
		assert.NoError(t, err)
		err = service.CreateOrUpdateSubscriber(2, "stripe", "sub_456", true, nil)
		assert.NoError(t, err)

		// Create subscriber for different gateway
		err = service.CreateOrUpdateSubscriber(3, "paypal", "sub_789", true, nil)
		assert.NoError(t, err)

		// Should find only stripe subscribers
		subscribers, err = service.GetActiveSubscribersByGateway("stripe")
		assert.NoError(t, err)
		assert.Len(t, subscribers, 2)

		// Create inactive subscriber
		err = service.CreateOrUpdateSubscriber(4, "stripe", "sub_999", false, nil)
		assert.NoError(t, err)

		// Should still only find active ones
		subscribers, err = service.GetActiveSubscribersByGateway("stripe")
		assert.NoError(t, err)
		assert.Len(t, subscribers, 2)
	},
		getBillingTestOptions())
}

func TestBillingService_GetActiveSubscription(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE)

		// Test with no subscriptions
		subscription, err := service.GetActiveSubscription(1)
		assert.NoError(t, err)
		assert.Nil(t, subscription)

		// Create active subscription for stripe
		err = service.CreateOrUpdateSubscriber(1, "stripe", "sub_123", true, nil)
		assert.NoError(t, err)

		// Should find the subscription
		subscription, err = service.GetActiveSubscription(1)
		assert.NoError(t, err)
		assert.NotNil(t, subscription)
		assert.Equal(t, "stripe", subscription.GatewayType)

		// Create active subscription for paypal (different user)
		err = service.CreateOrUpdateSubscriber(2, "paypal", "sub_456", true, nil)
		assert.NoError(t, err)

		// Should still find stripe subscription for user 1
		subscription, err = service.GetActiveSubscription(1)
		assert.NoError(t, err)
		assert.Equal(t, "stripe", subscription.GatewayType)

		// Create multiple active subscriptions for same user (different gateways)
		err = service.CreateOrUpdateSubscriber(1, "paypal", "sub_789", true, nil)
		assert.NoError(t, err)

		// Should find one of them (order not guaranteed)
		subscription, err = service.GetActiveSubscription(1)
		assert.NoError(t, err)
		assert.NotNil(t, subscription)
		assert.Equal(t, uint(1), subscription.UserID)
		assert.True(t, subscription.IsActive)

		// Deactivate stripe subscription
		err = service.DeactivateSubscriber(1, "stripe")
		assert.NoError(t, err)

		// Should find paypal subscription
		subscription, err = service.GetActiveSubscription(1)
		assert.NoError(t, err)
		assert.Equal(t, "paypal", subscription.GatewayType)

		// Deactivate paypal subscription
		err = service.DeactivateSubscriber(1, "paypal")
		assert.NoError(t, err)

		// Should find no subscription
		subscription, err = service.GetActiveSubscription(1)
		assert.NoError(t, err)
		assert.Nil(t, subscription)
	},
		getBillingTestOptions())
}
