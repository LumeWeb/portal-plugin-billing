package stripe

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stripe/stripe-go/v83"
	"github.com/stripe/stripe-go/v83/webhook"
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
	)
}

func TestStripeGateway_ID(t *testing.T) {
	ctx, _ := coreTesting.NewTestContext(t)
	gw := New(ctx.Logger(), "test_secret", nil, nil)
	assert.Equal(t, "stripe", gw.ID())
}

func TestStripeGateway_SignatureHeader(t *testing.T) {
	ctx, _ := coreTesting.NewTestContext(t)
	gw := New(ctx.Logger(), "test_secret", nil, nil)
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
			signature:   "",
			payload:     []byte("invalid json"),
			secret:      secret,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, _ := coreTesting.NewTestContext(t)
			gw := New(ctx.Logger(), tt.secret, nil, nil)
			
			// For the invalid JSON test case, we need to generate a valid signature for the invalid payload
			if tt.name == "invalid JSON payload" {
				unsignedPayload := &webhook.UnsignedPayload{
					Payload:   tt.payload,
					Secret:    tt.secret,
					Timestamp: time.Now(),
				}
				signedPayload := webhook.GenerateTestSignedPayload(unsignedPayload)
				tt.signature = signedPayload.Header
				tt.payload = signedPayload.Payload
			}
			
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

		// Setup test data
		subscription := createTestSubscription("123", "1")
		rawData, _ := json.Marshal(subscription)
		event := createTestEvent(EventTypeSubscriptionCreated, rawData)
		payload, _ := json.Marshal(event)

		// Setup mock expectations
		mockUsers.On("AccountExists", uint(123)).Return(true, createTestUser(123), nil)
		mockQuota.On("AssignUserToPlan", uint(123), uint(1)).Return(nil)

		gw := New(ctx.Logger(), "test_secret", mockQuota, mockUsers)
		err := gw.HandleWebhook(context.Background(), payload)

		assert.NoError(t, err)
	})
}

func TestStripeGateway_HandleWebhook_SubscriptionDeleted(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockQuota := core.GetService[*quotaCore.MockQuotaService](ctx, quotaCore.QUOTA_SERVICE)
		mockUsers := core.GetService[*coreMocks.MockUserService](ctx, core.USER_SERVICE)

		subscription := createTestSubscription("123", "")
		rawData, _ := json.Marshal(subscription)
		event := createTestEvent(EventTypeSubscriptionDeleted, rawData)
		payload, _ := json.Marshal(event)

		mockUsers.On("AccountExists", uint(123)).Return(true, createTestUser(123), nil)
		mockQuota.On("RemoveUserFromPlan", uint(123)).Return(nil)

		gw := New(ctx.Logger(), "test_secret", mockQuota, mockUsers)
		err := gw.HandleWebhook(context.Background(), payload)

		assert.NoError(t, err)
	})
}

func TestStripeGateway_HandleWebhook_SubscriptionUpdated(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockQuota := core.GetService[*quotaCore.MockQuotaService](ctx, quotaCore.QUOTA_SERVICE)
		mockUsers := core.GetService[*coreMocks.MockUserService](ctx, core.USER_SERVICE)

		subscription := createTestSubscription("123", "2")
		rawData, _ := json.Marshal(subscription)
		event := createTestEvent(EventTypeSubscriptionUpdated, rawData)
		payload, _ := json.Marshal(event)

		mockUsers.On("AccountExists", uint(123)).Return(true, createTestUser(123), nil)
		mockQuota.On("AssignUserToPlan", uint(123), uint(2)).Return(nil)

		gw := New(ctx.Logger(), "test_secret", mockQuota, mockUsers)
		err := gw.HandleWebhook(context.Background(), payload)

		assert.NoError(t, err)
	})
}

func TestStripeGateway_HandleWebhook_UnknownEvent(t *testing.T) {
	event := createTestEvent("unknown.event.type", nil)
	payload, _ := json.Marshal(event)

	ctx, _ := coreTesting.NewTestContext(t)

	gw := New(ctx.Logger(), "test_secret", nil, nil)
	err := gw.HandleWebhook(context.Background(), payload)
	assert.NoError(t, err)
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
	gw := New(ctx.Logger(), secret, nil, nil)
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

		gw := New(ctx.Logger(), "test_secret", nil, mockUsers)
		err := gw.HandleWebhook(context.Background(), payload)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "user with ID 123 not found")
	})
}

func TestStripeGateway_HandleWebhook_MissingPlanID(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockUsers := core.GetService[*coreMocks.MockUserService](ctx, core.USER_SERVICE)

		subscription := createTestSubscription("123", "")
		rawData, _ := json.Marshal(subscription)
		event := createTestEvent(EventTypeSubscriptionCreated, rawData)
		payload, _ := json.Marshal(event)
		gw := New(ctx.Logger(), "test_secret", nil, mockUsers)
		err := gw.HandleWebhook(context.Background(), payload)

		// Should handle missing plan id gracefully
		assert.NoError(t, err)
	})
}

func TestStripeGateway_HandleWebhook_NilSubscriptionItems(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockUsers := core.GetService[*coreMocks.MockUserService](ctx, core.USER_SERVICE)

		// Create a subscription with nil Items
		subscription := stripe.Subscription{
			ID: "sub_123",
			Metadata: map[string]string{
				"user_id": "123",
			},
			Items: nil, // Explicitly set to nil
		}

		rawData, _ := json.Marshal(subscription)
		event := createTestEvent(EventTypeSubscriptionCreated, rawData)
		payload, _ := json.Marshal(event)

		gw := New(ctx.Logger(), "test_secret", nil, mockUsers)
		err := gw.HandleWebhook(context.Background(), payload)

		// Should handle nil subscription items gracefully
		assert.NoError(t, err)
	})
}
