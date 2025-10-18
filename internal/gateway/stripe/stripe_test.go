package stripe

import (
	"context"
	"encoding/json"
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
	gw := New(ctx.Logger(), "test_secret", nil, nil, nil)
	assert.Equal(t, "stripe", gw.ID())
}

func TestStripeGateway_SignatureHeader(t *testing.T) {
	ctx, _ := coreTesting.NewTestContext(t)
	gw := New(ctx.Logger(), "test_secret", nil, nil, nil)
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
			gw := New(ctx.Logger(), tt.secret, nil, nil, nil)

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

func TestStripeGateway_HandleWebhook_SubscriptionCreated(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockQuota := core.GetService[*quotaCore.MockQuotaService](ctx, quotaCore.QUOTA_SERVICE)
		mockUsers := core.GetService[*coreMocks.MockUserService](ctx, core.USER_SERVICE)
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)

		// Setup test data
		subscription := createTestSubscription("123", "1")
		rawData, _ := json.Marshal(subscription)
		event := createTestEvent(EventTypeSubscriptionCreated, rawData)
		payload, _ := json.Marshal(event)

		// Setup mock expectations
		mockUsers.On("AccountExists", uint(123)).Return(true, createTestUser(123), nil)
		mockQuota.On("GetQuotaPlan", uint(1)).Return(&quotaCore.QuotaPlan{}, nil)
		mockQuota.On("AssignUserToPlan", uint(123), uint(1)).Return(nil)
		mockBilling.On("CreateOrUpdateSubscriber", uint(123), "stripe", "sub_123", true, mock.AnythingOfType("*uint")).Return(nil)

		gw := New(ctx.Logger(), "test_secret", mockQuota, mockUsers, mockBilling)
		err := gw.HandleWebhook(context.Background(), payload)

		assert.NoError(t, err)
	})
}

func TestStripeGateway_HandleWebhook_SubscriptionCreated_PriceNil(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockQuota := core.GetService[*quotaCore.MockQuotaService](ctx, quotaCore.QUOTA_SERVICE)
		mockUsers := core.GetService[*coreMocks.MockUserService](ctx, core.USER_SERVICE)

		// subscription with item but nil Price
		subscription := stripe.Subscription{
			ID: "sub_123",
			Metadata: map[string]string{
				UserIDMetadataKey: "123",
			},
			Items: &stripe.SubscriptionItemList{
				Data: []*stripe.SubscriptionItem{{Price: nil}},
			},
		}
		rawData, _ := json.Marshal(subscription)
		event := createTestEvent(EventTypeSubscriptionCreated, rawData)
		payload, _ := json.Marshal(event)

		// Setup mock to return false for AccountExists since we expect this check to happen
		// before the price validation
		mockUsers.On("AccountExists", uint(123)).Return(false, nil, nil)

		gw := New(ctx.Logger(), "test_secret", mockQuota, mockUsers, nil)
		err := gw.HandleWebhook(context.Background(), payload)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "user with ID 123 not found")
		// Ensure no quota plan assignment was attempted
		mockQuota.AssertNotCalled(t, "AssignUserToPlan", mock.Anything, mock.Anything)
	})
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

		gw := New(ctx.Logger(), "test_secret", mockQuota, mockUsers, mockBilling)
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
		mockBilling.On("CreateOrUpdateSubscriber", uint(123), "stripe", "sub_123", true, mock.AnythingOfType("*uint")).Return(nil)

		gw := New(ctx.Logger(), "test_secret", mockQuota, mockUsers, mockBilling)
		err := gw.HandleWebhook(context.Background(), payload)

		assert.NoError(t, err)
	})
}

func TestStripeGateway_HandleWebhook_UnknownEvent(t *testing.T) {
	event := createTestEvent("unknown.event.type", nil)
	payload, _ := json.Marshal(event)

	ctx, _ := coreTesting.NewTestContext(t)

	gw := New(ctx.Logger(), "test_secret", nil, nil, nil)
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
			gw := New(ctx.Logger(), "test_secret", nil, nil, nil)

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
			gw := New(ctx.Logger(), "test_secret", nil, nil, nil)

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
	gw := New(ctx.Logger(), secret, nil, nil, nil)
	err := gw.HandleWebhook(context.Background(), signedPayload.Payload)
	assert.Error(t, err)
}

func TestStripeGateway_HandleWebhook_UserNotFound(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockUsers := core.GetService[*coreMocks.MockUserService](ctx, core.USER_SERVICE)

		subscription := createTestSubscription("123", "1")
		rawData, _ := json.Marshal(subscription)
		event := createTestEvent(EventTypeSubscriptionCreated, rawData)
		payload, _ := json.Marshal(event)

		mockUsers.On("AccountExists", uint(123)).Return(false, nil, nil)

		gw := New(ctx.Logger(), "test_secret", nil, mockUsers, nil)
		err := gw.HandleWebhook(context.Background(), payload)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "user with ID 123 not found")
	})
}

func TestStripeGateway_HandleWebhook_MissingPlanID(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockQuota := core.GetService[*quotaCore.MockQuotaService](ctx, quotaCore.QUOTA_SERVICE)
		mockUsers := core.GetService[*coreMocks.MockUserService](ctx, core.USER_SERVICE)

		subscription := createTestSubscription("123", "")
		rawData, _ := json.Marshal(subscription)
		event := createTestEvent(EventTypeSubscriptionCreated, rawData)
		payload, _ := json.Marshal(event)

		gw := New(ctx.Logger(), "test_secret", mockQuota, mockUsers, nil)
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

		// Create a subscription with nil Items
		subscription := stripe.Subscription{
			ID: "sub_123",
			Metadata: map[string]string{
				UserIDMetadataKey: "123",
			},
			Items: nil, // Explicitly set to nil
		}

		rawData, _ := json.Marshal(subscription)
		event := createTestEvent(EventTypeSubscriptionCreated, rawData)
		payload, _ := json.Marshal(event)

		gw := New(ctx.Logger(), "test_secret", nil, mockUsers, nil)
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

		gw := New(ctx.Logger(), "test_secret", mockQuota, mockUsers, mockBilling)
		err := gw.HandleWebhook(context.Background(), payload)

		// Should not return error but log warning about missing plan_id
		assert.NoError(t, err)
		
		// Verify no external calls were made
		mockUsers.AssertNotCalled(t, "AccountExists", mock.Anything)
		mockQuota.AssertNotCalled(t, "AssignUserToPlan", mock.Anything, mock.Anything)
		mockQuota.AssertNotCalled(t, "RemoveUserFromPlan", mock.Anything)
		mockBilling.AssertNotCalled(t, "CreateOrUpdateSubscriber", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
		mockBilling.AssertNotCalled(t, "DeactivateSubscriber", mock.Anything, mock.Anything)
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

		gw := New(ctx.Logger(), "test_secret", mockQuota, mockUsers, mockBilling)
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
		mockBilling.On("CreateOrUpdateSubscriber", uint(123), "stripe", "sub_123", true, mock.AnythingOfType("*uint")).Return(nil)

		gw := New(ctx.Logger(), "test_secret", mockQuota, mockUsers, mockBilling)
		err := gw.HandleWebhook(context.Background(), payload)

		assert.NoError(t, err)
	})
}
