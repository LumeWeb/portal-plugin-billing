package stripe

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stripe/stripe-go/v83"
	"github.com/stripe/stripe-go/v83/webhook"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	quotaCore "go.lumeweb.com/portal-plugin-quota/core"
	"go.lumeweb.com/portal/core"
	coreTesting "go.lumeweb.com/portal/core/testing"
	coreMocks "go.lumeweb.com/portal/core/testing/mocks"
	"go.lumeweb.com/portal/db/models"
	"gorm.io/gorm"
)

const (
	// StripeAPIVersion is the API version that matches the stripe-go library version
	StripeAPIVersion = "2025-09-30.clover"
)

func TestMain(m *testing.M) {
	coreTesting.WithOptions(m,
		coreTesting.WithMockServiceFactory(quotaCore.QUOTA_SERVICE, quotaCore.NewMockQuotaService),
		coreTesting.WithMockServiceFactory(pluginCore.BILLING_SERVICE, pluginCore.NewMockBillingService),
	)
}

func TestStripeGateway_ID(t *testing.T) {
	ctx, _ := coreTesting.NewTestContext(t)
	gw := New(ctx.Logger(), "test_secret", "", nil, nil, nil)
	assert.Equal(t, "stripe", gw.ID())
}

func TestStripeGateway_SignatureHeader(t *testing.T) {
	ctx, _ := coreTesting.NewTestContext(t)
	gw := New(ctx.Logger(), "test_secret", "", nil, nil, nil)
	assert.Equal(t, "Stripe-Signature", gw.SignatureHeader())
}

func TestStripeGateway_ValidateWebhook(t *testing.T) {
	secret := "whsec_test_secret"

	// Create a valid JSON payload that mimics a Stripe event
	event := stripe.Event{
		ID:         "evt_test123",
		Object:     "event",
		Type:       "test.event",
		APIVersion: StripeAPIVersion,
	}

	payload, _ := json.Marshal(event)

	// Generate a valid signature for the JSON payload
	unsignedPayload := &webhook.UnsignedPayload{
		Payload:   payload,
		Secret:    secret,
		Timestamp: time.Now(),
	}
	signedPayload := webhook.GenerateTestSignedPayload(unsignedPayload)

	// Signed payload with a stale timestamp (beyond default tolerance)
	oldUnsigned := &webhook.UnsignedPayload{
		Payload:   payload,
		Secret:    secret,
		Timestamp: time.Now().Add(-10 * time.Minute),
	}
	oldSigned := webhook.GenerateTestSignedPayload(oldUnsigned)

	// Signed payload for invalid JSON: signature valid, JSON should fail to unmarshal
	invalidJSON := []byte("invalid json")
	invalidUnsigned := &webhook.UnsignedPayload{
		Payload:   invalidJSON,
		Secret:    secret,
		Timestamp: time.Now(),
	}
	invalidSigned := webhook.GenerateTestSignedPayload(invalidUnsigned)

	tests := []struct {
		name        string
		signature   string
		payload     []byte
		secret      string
		expectError bool
	}{
		{
			name:      "valid signature",
			signature: signedPayload.Header,
			payload:   signedPayload.Payload,
			secret:    secret,
		},
		{
			name:        "missing secret",
			signature:   signedPayload.Header,
			payload:     signedPayload.Payload,
			secret:      "",
			expectError: true,
		},
		{
			name:        "stale timestamp",
			signature:   oldSigned.Header,
			payload:     oldSigned.Payload,
			secret:      secret,
			expectError: true,
		},
		{
			name:        "invalid signature",
			signature:   "t=123,v1=invalidsignature",
			payload:     payload,
			secret:      secret,
			expectError: true,
		},
		{
			name:        "malformed signature header",
			signature:   "invalid_sig",
			payload:     payload,
			secret:      secret,
			expectError: true,
		},
		{
			name:        "invalid JSON payload",
			signature:   invalidSigned.Header,
			payload:     invalidSigned.Payload,
			secret:      secret,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, _ := coreTesting.NewTestContext(t)
			gw := New(ctx.Logger(), tt.secret, "", nil, nil, nil)

			err := gw.ValidateWebhook(context.Background(), tt.signature, tt.payload)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// Helper function to create a test subscription
func createTestSubscription(userID string, planID string) stripe.Subscription {
	subscription := stripe.Subscription{
		ID: "sub_123",
		Customer: &stripe.Customer{
			ID: "cus_123",
			Metadata: map[string]string{
				UserIDMetadataKey: userID,
			},
		},
		Metadata: map[string]string{
			UserIDMetadataKey: userID,
		},
	}

	if planID != "" {
		subscription.Items = &stripe.SubscriptionItemList{
			Data: []*stripe.SubscriptionItem{
				{
					Price: &stripe.Price{
						ID: "price_123",
						Metadata: map[string]string{
							PlanIDMetadataKey: planID,
						},
					},
				},
			},
		}
	}

	return subscription
}

// Helper function to create a test event
func createTestEvent(eventType string, data []byte) stripe.Event {
	return stripe.Event{
		Type:       stripe.EventType(eventType),
		APIVersion: StripeAPIVersion,
		Data: &stripe.EventData{
			Raw: data,
		},
	}
}

// Helper function to create a test user
func createTestUser(id uint) *models.User {
	return &models.User{Model: gorm.Model{ID: id}}
}



func TestStripeGateway_HandleWebhook_SubscriptionDeleted(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockQuota := core.GetService[*quotaCore.MockQuotaService](ctx, quotaCore.QUOTA_SERVICE)
		mockUsers := core.GetService[*coreMocks.MockUserService](ctx, core.USER_SERVICE)
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)

		subscription := createTestSubscription("123", "")
		rawData, _ := json.Marshal(subscription)
		event := createTestEvent(EventTypeSubscriptionDeleted, rawData)
		payload, _ := json.Marshal(event)

		mockUsers.On("AccountExists", uint(123)).Return(true, createTestUser(123), nil)
		mockQuota.On("RemoveUserFromPlan", uint(123)).Return(nil)
		mockBilling.On("DeactivateSubscriber", uint(123), "stripe").Return(nil)

		gw := New(ctx.Logger(), "test_secret", "", mockQuota, mockUsers, mockBilling)
		err := gw.HandleWebhook(context.Background(), payload)

		assert.NoError(t, err)
	})
}

func TestStripeGateway_HandleWebhook_SubscriptionUpdated(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockQuota := core.GetService[*quotaCore.MockQuotaService](ctx, quotaCore.QUOTA_SERVICE)
		mockUsers := core.GetService[*coreMocks.MockUserService](ctx, core.USER_SERVICE)
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)

		subscription := createTestSubscription("123", "2")
		rawData, _ := json.Marshal(subscription)
		event := createTestEvent(EventTypeSubscriptionUpdated, rawData)
		payload, _ := json.Marshal(event)

		mockUsers.On("AccountExists", uint(123)).Return(true, createTestUser(123), nil)
		mockQuota.On("GetQuotaPlan", uint(2)).Return(&quotaCore.QuotaPlan{}, nil)
		mockQuota.On("AssignUserToPlan", uint(123), uint(2)).Return(nil)
		mockBilling.On("CreateOrUpdateSubscriber", uint(123), "stripe", "cus_123", true, mock.AnythingOfType("*uint")).Return(nil)

		gw := New(ctx.Logger(), "test_secret", "", mockQuota, mockUsers, mockBilling)
		err := gw.HandleWebhook(context.Background(), payload)

		assert.NoError(t, err)
	})
}

func TestStripeGateway_HandleWebhook_UnknownEvent(t *testing.T) {
	event := createTestEvent("unknown.event.type", nil)
	payload, _ := json.Marshal(event)

	ctx, _ := coreTesting.NewTestContext(t)

	gw := New(ctx.Logger(), "test_secret", "", nil, nil, nil)
	err := gw.HandleWebhook(context.Background(), payload)
	assert.NoError(t, err)
}

func TestStripeGateway_ExtractEventID(t *testing.T) {
	tests := []struct {
		name        string
		payload     []byte
		expectedID  string
		expectError bool
	}{
		{
			name: "valid event payload",
			payload: func() []byte {
				event := stripe.Event{
					ID: "evt_test123",
				}
				payload, _ := json.Marshal(event)
				return payload
			}(),
			expectedID: "evt_test123",
		},
		{
			name:        "invalid json payload",
			payload:     []byte("invalid json"),
			expectError: true,
		},
		{
			name: "event without ID",
			payload: func() []byte {
				event := stripe.Event{
					Type: "test.event",
				}
				payload, _ := json.Marshal(event)
				return payload
			}(),
			expectedID: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, _ := coreTesting.NewTestContext(t)
			gw := New(ctx.Logger(), "test_secret", "", nil, nil, nil)

			eventID, err := gw.ExtractEventID(tt.payload)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedID, eventID)
			}
		})
	}
}

func TestStripeGateway_ExtractEventType(t *testing.T) {
	tests := []struct {
		name         string
		payload      []byte
		expectedType string
		expectError  bool
	}{
		{
			name: "valid event payload",
			payload: func() []byte {
				event := stripe.Event{
					Type: "customer.subscription.created",
				}
				payload, _ := json.Marshal(event)
				return payload
			}(),
			expectedType: "customer.subscription.created",
		},
		{
			name:        "invalid json payload",
			payload:     []byte("invalid json"),
			expectError: true,
		},
		{
			name: "event without type",
			payload: func() []byte {
				event := stripe.Event{
					ID: "evt_test123",
				}
				payload, _ := json.Marshal(event)
				return payload
			}(),
			expectedType: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, _ := coreTesting.NewTestContext(t)
			gw := New(ctx.Logger(), "test_secret", "", nil, nil, nil)

			eventType, err := gw.ExtractEventType(tt.payload)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedType, eventType)
			}
		})
	}
}

func TestStripeGateway_HandleWebhook_InvalidPayload(t *testing.T) {
	secret := "whsec_test_secret"
	payload := []byte("invalid json")

	// Generate a valid signature for the invalid payload
	unsignedPayload := &webhook.UnsignedPayload{
		Payload:   payload,
		Secret:    secret,
		Timestamp: time.Now(),
	}
	signedPayload := webhook.GenerateTestSignedPayload(unsignedPayload)

	ctx, _ := coreTesting.NewTestContext(t)
	gw := New(ctx.Logger(), secret, "", nil, nil, nil)
	err := gw.HandleWebhook(context.Background(), signedPayload.Payload)
	assert.Error(t, err)
}

func TestStripeGateway_HandleWebhook_UserNotFound(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockUsers := core.GetService[*coreMocks.MockUserService](ctx, core.USER_SERVICE)

		subscription := createTestSubscription("123", "1")
		rawData, _ := json.Marshal(subscription)
		event := createTestEvent(EventTypeSubscriptionUpdated, rawData)
		payload, _ := json.Marshal(event)

		mockUsers.On("AccountExists", uint(123)).Return(false, nil, nil)
		mockQuota := core.GetService[*quotaCore.MockQuotaService](ctx, quotaCore.QUOTA_SERVICE)

		gw := New(ctx.Logger(), "test_secret", "", mockQuota, mockUsers, nil)
		err := gw.HandleWebhook(context.Background(), payload)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "user with ID 123 not found")
	})
}

func TestStripeGateway_HandleWebhook_MissingPlanID(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockQuota := core.GetService[*quotaCore.MockQuotaService](ctx, quotaCore.QUOTA_SERVICE)
		mockUsers := core.GetService[*coreMocks.MockUserService](ctx, core.USER_SERVICE)

		mockQuota = core.GetService[*quotaCore.MockQuotaService](ctx, quotaCore.QUOTA_SERVICE)
		mockUsers = core.GetService[*coreMocks.MockUserService](ctx, core.USER_SERVICE)

		subscription := createTestSubscription("123", "")
		rawData, _ := json.Marshal(subscription)
		event := createTestEvent(EventTypeSubscriptionUpdated, rawData)
		payload, _ := json.Marshal(event)

		gw := New(ctx.Logger(), "test_secret", "", mockQuota, mockUsers, nil)
		err := gw.HandleWebhook(context.Background(), payload)

		// Should return error when subscription has missing items
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "subscription missing items")
		// Ensure no external side effects
		mockUsers.AssertNotCalled(t, "AccountExists", mock.Anything)
		mockQuota.AssertNotCalled(t, "AssignUserToPlan", mock.Anything, mock.Anything)
	})
}

func TestStripeGateway_HandleWebhook_NilSubscriptionItems(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockUsers := core.GetService[*coreMocks.MockUserService](ctx, core.USER_SERVICE)
		mockQuota := core.GetService[*quotaCore.MockQuotaService](ctx, quotaCore.QUOTA_SERVICE)

		// Create a subscription with nil Items
		subscription := stripe.Subscription{
			ID: "sub_123",
			Metadata: map[string]string{
				UserIDMetadataKey: "123",
			},
			Items: nil, // Explicitly set to nil
		}

		rawData, _ := json.Marshal(subscription)
		event := createTestEvent(EventTypeSubscriptionUpdated, rawData)
		payload, _ := json.Marshal(event)

		gw := New(ctx.Logger(), "test_secret", "", mockQuota, mockUsers, nil)
		err := gw.HandleWebhook(context.Background(), payload)

		// Should return error when subscription has missing items
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "subscription missing items")
		mockUsers.AssertNotCalled(t, "AccountExists", mock.Anything)
	})
}

func TestStripeGateway_HandleWebhook_SubscriptionUpdated_AllPricesNil(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockUsers := core.GetService[*coreMocks.MockUserService](ctx, core.USER_SERVICE)
		mockQuota := core.GetService[*quotaCore.MockQuotaService](ctx, quotaCore.QUOTA_SERVICE)
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)

		// Create subscription with items but all Price fields nil
		subscription := stripe.Subscription{
			ID: "sub_123",
			Customer: &stripe.Customer{
				ID: "cus_123",
				Metadata: map[string]string{
					UserIDMetadataKey: "123",
				},
			},
			Metadata: map[string]string{
				UserIDMetadataKey: "123",
			},
			Items: &stripe.SubscriptionItemList{
				Data: []*stripe.SubscriptionItem{
					{Price: nil},
					{Price: nil},
				},
			},
		}

		rawData, _ := json.Marshal(subscription)
		event := createTestEvent(EventTypeSubscriptionUpdated, rawData)
		payload, _ := json.Marshal(event)

		// Set up mock expectations for the deactivation path
		mockUsers.On("AccountExists", uint(123)).Return(true, createTestUser(123), nil)
		mockQuota.On("RemoveUserFromPlan", uint(123)).Return(nil)
		mockBilling.On("DeactivateSubscriber", uint(123), "stripe").Return(nil)

		gw := New(ctx.Logger(), "test_secret", "", mockQuota, mockUsers, mockBilling)
		err := gw.HandleWebhook(context.Background(), payload)

		// Should not return error but should proceed with deactivation
		assert.NoError(t, err)

		// Verify the expected calls were made
		mockUsers.AssertExpectations(t)
		mockQuota.AssertExpectations(t)
		mockBilling.AssertExpectations(t)
	})
}

func TestStripeGateway_HandleWebhook_SubscriptionPaused(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockQuota := core.GetService[*quotaCore.MockQuotaService](ctx, quotaCore.QUOTA_SERVICE)
		mockUsers := core.GetService[*coreMocks.MockUserService](ctx, core.USER_SERVICE)
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)

		subscription := createTestSubscription("123", "")
		rawData, _ := json.Marshal(subscription)
		event := createTestEvent(EventTypeSubscriptionPaused, rawData)
		payload, _ := json.Marshal(event)

		mockUsers.On("AccountExists", uint(123)).Return(true, createTestUser(123), nil)
		mockQuota.On("RemoveUserFromPlan", uint(123)).Return(nil)
		mockBilling.On("DeactivateSubscriber", uint(123), "stripe").Return(nil)

		gw := New(ctx.Logger(), "test_secret", "", mockQuota, mockUsers, mockBilling)
		err := gw.HandleWebhook(context.Background(), payload)

		assert.NoError(t, err)
	})
}

func TestStripeGateway_HandleWebhook_SubscriptionResumed(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockQuota := core.GetService[*quotaCore.MockQuotaService](ctx, quotaCore.QUOTA_SERVICE)
		mockUsers := core.GetService[*coreMocks.MockUserService](ctx, core.USER_SERVICE)
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)

		subscription := createTestSubscription("123", "1")
		rawData, _ := json.Marshal(subscription)
		event := createTestEvent(EventTypeSubscriptionResumed, rawData)
		payload, _ := json.Marshal(event)

		mockUsers.On("AccountExists", uint(123)).Return(true, createTestUser(123), nil)
		mockQuota.On("GetQuotaPlan", uint(1)).Return(&quotaCore.QuotaPlan{}, nil)
		mockQuota.On("AssignUserToPlan", uint(123), uint(1)).Return(nil)
		mockBilling.On("CreateOrUpdateSubscriber", uint(123), "stripe", "cus_123", true, mock.AnythingOfType("*uint")).Return(nil)

		gw := New(ctx.Logger(), "test_secret", "", mockQuota, mockUsers, mockBilling)
		err := gw.HandleWebhook(context.Background(), payload)

		assert.NoError(t, err)
	})
}

func TestStripeGateway_GetCustomerPortalURL_Success(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)

		// Mock active subscription
		planID := uint(42)
		mockSubscriber := &pluginCore.Subscriber{
			UserID:      123,
			GatewayType: "stripe",
			GatewayID:   "cus_123",
			IsActive:    true,
			PlanID:      &planID,
		}
		mockBilling.On("GetActiveSubscription", uint(123)).Return(mockSubscriber, nil)

		gw := New(ctx.Logger(), "test_secret", "", nil, nil, mockBilling)

		// Note: This test will fail in real execution because we can't mock the Stripe API
		// but it verifies the logic flow and error handling
		url, err := gw.GetCustomerPortalURL(context.Background(), 123, "https://example.com/return")

		// We expect this to fail because we can't mock the Stripe billing portal session creation
		// but we can verify the error is about session creation, not about subscription lookup
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create billing portal session")
		assert.Empty(t, url)

		// Verify the billing service was called correctly
		mockBilling.AssertExpectations(t)
	})
}

func TestStripeGateway_HandleWebhook_CheckoutSessionCompleted(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockQuota := core.GetService[*quotaCore.MockQuotaService](ctx, quotaCore.QUOTA_SERVICE)
		mockUsers := core.GetService[*coreMocks.MockUserService](ctx, core.USER_SERVICE)
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)

		// Create a mock subscription retriever
		mockSubService := &MockSubscriptionRetriever{}

		// Create a checkout session with client_reference_id
		clientRefID := "456"
		subscription := &stripe.Subscription{
			ID: "sub_456",
			Customer: &stripe.Customer{
				ID: "cus_456",
			},
			Items: &stripe.SubscriptionItemList{
				Data: []*stripe.SubscriptionItem{
					{
						Price: &stripe.Price{
							ID: "price_456",
							Metadata: map[string]string{
								PlanIDMetadataKey: "3",
							},
						},
					},
				},
			},
		}
		
		checkoutSession := stripe.CheckoutSession{
			ID:                 "cs_test_123",
			ClientReferenceID:  clientRefID,
			Subscription:       subscription,
			Mode:               "subscription",
		}
		rawData, _ := json.Marshal(checkoutSession)
		event := createTestEvent(EventTypeCheckoutSessionCompleted, rawData)
		payload, _ := json.Marshal(event)

		// Setup mock expectations using helper methods
		mockSubService.SetupGetSuccess(subscription)
		mockUsers.On("AccountExists", uint(456)).Return(true, createTestUser(456), nil)
		mockQuota.On("GetQuotaPlan", uint(3)).Return(&quotaCore.QuotaPlan{}, nil)
		mockQuota.On("AssignUserToPlan", uint(456), uint(3)).Return(nil)
		mockBilling.On("CreateOrUpdateSubscriber", uint(456), "stripe", "cus_456", true, mock.AnythingOfType("*uint")).Return(nil)

		gw := New(ctx.Logger(), "test_secret", "test_api_key", mockQuota, mockUsers, mockBilling)
		gw.subService = mockSubService
		err := gw.HandleWebhook(context.Background(), payload)

		assert.NoError(t, err)
		
		// Verify mock expectations were met
		mockSubService.AssertExpectations(t)
		mockUsers.AssertExpectations(t)
		mockQuota.AssertExpectations(t)
		mockBilling.AssertExpectations(t)
	})
}

// MockSubscriptionRetriever is a mock implementation of the SubscriptionRetriever interface
// for testing purposes. It allows tests to control the subscription data returned without
// making actual API calls to Stripe.
//
// This mock uses testify/mock to provide flexible stubbing capabilities. Tests can configure
// it to return specific subscription objects or errors for different input parameters.
type MockSubscriptionRetriever struct {
	mock.Mock
}

// Get is the mock implementation of the SubscriptionRetriever.Get method.
// It records the call and returns predefined values set up by the test.
//
// Parameters:
// - ctx: The context for the request (not validated in mock)
// - id: The subscription ID being requested
// - params: The parameters for the request (not validated in mock)
//
// Returns:
// - *stripe.Subscription: The subscription object configured in the mock setup
// - error: Any error configured in the mock setup, or nil
func (m *MockSubscriptionRetriever) Get(ctx context.Context, id string, params *stripe.SubscriptionRetrieveParams) (*stripe.Subscription, error) {
	args := m.Called(ctx, id, params)
	sub, ok := args.Get(0).(*stripe.Subscription)
	if !ok && args.Get(0) != nil {
		return nil, fmt.Errorf("mock setup error: expected *stripe.Subscription, got %T", args.Get(0))
	}
	return sub, args.Error(1)
}

// SetupGetSuccess configures the mock to return a successful subscription retrieval.
// This helper method simplifies test setup by handling the mock configuration.
//
// Parameters:
// - subscription: The subscription object to return
func (m *MockSubscriptionRetriever) SetupGetSuccess(subscription *stripe.Subscription) {
	m.On("Get", mock.Anything, subscription.ID, mock.Anything).Return(subscription, nil)
}

// SetupGetError configures the mock to return an error for subscription retrieval.
// This helper method simplifies test setup for error handling scenarios.
//
// Parameters:
// - subscriptionID: The ID of the subscription that should fail
// - err: The error to return
func (m *MockSubscriptionRetriever) SetupGetError(subscriptionID string, err error) {
	m.On("Get", mock.Anything, subscriptionID, mock.Anything).Return((*stripe.Subscription)(nil), err)
}

func TestStripeGateway_GetCustomerPortalURL_NoActiveSubscription(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)

		// Mock no active subscription
		mockBilling.On("GetActiveSubscription", uint(123)).Return((*pluginCore.Subscriber)(nil), nil)

		gw := New(ctx.Logger(), "test_secret", "", nil, nil, mockBilling)

		url, err := gw.GetCustomerPortalURL(context.Background(), 123, "https://example.com/return")

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no active stripe subscription found")
		assert.Empty(t, url)

		mockBilling.AssertExpectations(t)
	})
}

func TestStripeGateway_GetCustomerPortalURL_NonStripeSubscription(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)

		// Mock active subscription with different gateway
		planID := uint(42)
		mockSubscriber := &pluginCore.Subscriber{
			UserID:      123,
			GatewayType: "paypal", // Different gateway
			GatewayID:   "cus_123",
			IsActive:    true,
			PlanID:      &planID,
		}
		mockBilling.On("GetActiveSubscription", uint(123)).Return(mockSubscriber, nil)

		gw := New(ctx.Logger(), "test_secret", "", nil, nil, mockBilling)

		url, err := gw.GetCustomerPortalURL(context.Background(), 123, "https://example.com/return")

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no active stripe subscription found")
		assert.Empty(t, url)

		mockBilling.AssertExpectations(t)
	})
}

func TestStripeGateway_GetCustomerPortalURL_BillingServiceError(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)

		// Mock billing service error
		mockBilling.On("GetActiveSubscription", uint(123)).Return((*pluginCore.Subscriber)(nil), assert.AnError)

		gw := New(ctx.Logger(), "test_secret", "", nil, nil, mockBilling)

		url, err := gw.GetCustomerPortalURL(context.Background(), 123, "https://example.com/return")

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get active subscription")
		assert.Empty(t, url)

		mockBilling.AssertExpectations(t)
	})
}

func TestStripeGateway_GetCustomerPortalURL_InvalidCustomerID(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)

		// Mock active subscription with invalid GatewayID (not starting with cus_)
		planID := uint(42)
		mockSubscriber := &pluginCore.Subscriber{
			UserID:      123,
			GatewayType: "stripe",
			GatewayID:   "sub_123", // This is a subscription ID, not a customer ID
			IsActive:    true,
			PlanID:      &planID,
		}
		mockBilling.On("GetActiveSubscription", uint(123)).Return(mockSubscriber, nil)

		gw := New(ctx.Logger(), "test_secret", "", nil, nil, mockBilling)

		url, err := gw.GetCustomerPortalURL(context.Background(), 123, "https://example.com/return")

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid GatewayID: must be a Stripe customer ID starting with 'cus_'")
		assert.Empty(t, url)

		mockBilling.AssertExpectations(t)
	})
}

func TestStripeGateway_GetCustomerPortalURL_EmptyCustomerID(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)

		// Mock active subscription with empty GatewayID
		planID := uint(42)
		mockSubscriber := &pluginCore.Subscriber{
			UserID:      123,
			GatewayType: "stripe",
			GatewayID:   "", // Empty GatewayID
			IsActive:    true,
			PlanID:      &planID,
		}
		mockBilling.On("GetActiveSubscription", uint(123)).Return(mockSubscriber, nil)

		gw := New(ctx.Logger(), "test_secret", "", nil, nil, mockBilling)

		url, err := gw.GetCustomerPortalURL(context.Background(), 123, "https://example.com/return")

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "subscriber GatewayID is empty")
		assert.Empty(t, url)

		mockBilling.AssertExpectations(t)
	})
}
