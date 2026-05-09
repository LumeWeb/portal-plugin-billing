package stripe

import (
	"fmt"
	"context"
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stripe/stripe-go/v85"
	"github.com/stripe/stripe-go/v85/webhook"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	pluginConfig "go.lumeweb.com/portal-plugin-billing/internal/config"
	billingModels "go.lumeweb.com/portal-plugin-billing/internal/db/models"
	quotaCore "go.lumeweb.com/portal-plugin-quota/core"
	"go.lumeweb.com/portal/core"
	coreTesting "go.lumeweb.com/portal/core/testing"
	portalModels "go.lumeweb.com/portal/db/models"
	"gorm.io/gorm"
)

const (
	// StripeAPIVersion is the API version string from stripe-go/v85
	StripeAPIVersion = stripe.APIVersion

	// Test constants for commonly used values
	TestUserID         = uint(123)
	TestCustomerID     = "cus_123"
	TestSubscriptionID = "sub_123"
	TestPlanID         = uint(1)
	TestWebhookSecret  = "wh_test_secret"
)

func testConfigWithSecrets(webhookSecret, secretKey string) *pluginConfig.ServiceConfig {
	return &pluginConfig.ServiceConfig{
		Stripe: pluginConfig.StripeConfig{
			WebhookSecret: webhookSecret,
			SecretKey:     secretKey,
		},
	}
}

func testConfig() *pluginConfig.ServiceConfig {
	return testConfigWithSecrets(TestWebhookSecret, "")
}

func TestMain(m *testing.M) {
	coreTesting.WithOptions(m,
		coreTesting.WithMockServiceFactory(quotaCore.QUOTA_SERVICE, quotaCore.NewMockQuotaService, &quotaCore.QuotaConfig{}),
		coreTesting.WithMockServiceFactory(pluginCore.BILLING_SERVICE, pluginCore.NewMockBillingService, &pluginConfig.ServiceConfig{}),
		coreTesting.WithMockServiceFactory(pluginCore.PRICING_SERVICE, pluginCore.NewMockPricingService, coreTesting.NewConfigBuilder().Build()),
		coreTesting.WithMockServiceFactory(pluginCore.CREDIT_SERVICE, pluginCore.NewMockCreditService, coreTesting.NewConfigBuilder().Build()),
	)
}

func TestStripeGateway_ID(t *testing.T) {
	ctx, _ := coreTesting.NewTestContext(t)
	gw := NewWithConfig(ctx.Logger(), ctx, testConfig(), nil, nil, nil, nil, nil)
	assert.Equal(t, "stripe", gw.ID(context.Background()))
}

func TestStripeGateway_SignatureHeader(t *testing.T) {
	ctx, _ := coreTesting.NewTestContext(t)
	gw := NewWithConfig(ctx.Logger(), ctx, testConfig(), nil, nil, nil, nil, nil)
	assert.Equal(t, "Stripe-Signature", gw.SignatureHeader(context.Background()))
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
			gw := NewWithConfig(ctx.Logger(), ctx, testConfigWithSecrets(tt.secret, ""), nil, nil, nil, nil, nil)

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
	return createTestSubscriptionWithPeriod(userID, planID, "")
}

// Helper function to create a test subscription with period_id in price metadata
func createTestSubscriptionWithPeriod(userID string, planID string, periodID string) stripe.Subscription {
	subscription := stripe.Subscription{
		ID: TestSubscriptionID,
		Customer: &stripe.Customer{
			ID: TestCustomerID,
			Metadata: map[string]string{
				UserIDMetadataKey: userID,
			},
		},
		Metadata: map[string]string{
			UserIDMetadataKey: userID,
		},
	}

	if planID != "" || periodID != "" {
		priceMetadata := map[string]string{}
		if planID != "" {
			priceMetadata[PlanIDMetadataKey] = planID
		}
		if periodID != "" {
			priceMetadata["period_id"] = periodID
		}

		subscription.Items = &stripe.SubscriptionItemList{
			Data: []*stripe.SubscriptionItem{
				{
					Price: &stripe.Price{
						ID: "price_123",
						Product: &stripe.Product{
							ID:       "prod_123",
							Metadata: priceMetadata,
						},
						Metadata: map[string]string{
							"period_id": periodID,
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
func createTestUser(id uint) *portalModels.User {
	return &portalModels.User{Model: gorm.Model{ID: id}}
}

// Helper function to setup common mock services
func setupMockServices(ctx coreTesting.TestContext) (*quotaCore.MockQuotaService, *coreTesting.MockUserService, *pluginCore.MockBillingService, *pluginCore.MockPricingService) {
	mockQuota := core.GetService[*quotaCore.MockQuotaService](ctx, quotaCore.QUOTA_SERVICE)
	mockUsers := core.GetService[*coreTesting.MockUserService](ctx, core.USER_SERVICE)
	mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)
	mockPricing := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)
	return mockQuota, mockUsers, mockBilling, mockPricing
}

// Helper function to setup mocks for subscription activation scenarios
func setupSubscriptionActivationMocks(mockQuota *quotaCore.MockQuotaService, mockUsers *coreTesting.MockUserService, mockBilling *pluginCore.MockBillingService, mockPricing *pluginCore.MockPricingService, userID uint, pricingPlanPeriodID, quotaPlanID uint) {
	mockUsers.EXPECT().AccountExists(mock.Anything, userID).Return(true, createTestUser(userID), nil)

	// Mock GetActiveSubscription for uncancel detection (returns subscriber without WillCancelAt)
	planID := uint(1)
	mockBilling.EXPECT().GetActiveSubscription(mock.Anything, userID).Return(&pluginCore.Subscriber{
		UserID:              userID,
		GatewayType:         "stripe",
		IsActive:            true,
		PricingPlanPeriodID: &planID,
		WillCancelAt:        nil,
	}, nil).Maybe()

	mockBilling.EXPECT().GetPausedSubscription(mock.Anything, userID).Return(nil, nil).Maybe()

	// Mock fetching the period to get the plan ID for the event
	pricingPlanID := uint(1)
	period := &billingModels.PricingPlanPeriod{
		PricingPlanID: pricingPlanID,
	}
	period.ID = pricingPlanPeriodID
	mockPricing.EXPECT().GetPricingPlanPeriod(mock.Anything, pricingPlanPeriodID).Return(period, nil)

	// Billing service tracks with PricingPlanPeriodID
	mockBilling.EXPECT().CreateOrUpdateSubscriber(
		mock.Anything,
		userID,
		"stripe",
		TestCustomerID,
		TestSubscriptionID,
		true,
		mock.MatchedBy(func(p *uint) bool {
			return p != nil && *p == pricingPlanPeriodID
		}),
	).Return(nil)
}

// Helper function to setup mocks for subscription deactivation scenarios
func setupSubscriptionDeactivationMocks(mockQuota *quotaCore.MockQuotaService, mockUsers *coreTesting.MockUserService, mockBilling *pluginCore.MockBillingService, userID uint) {
	mockUsers.EXPECT().AccountExists(mock.Anything, userID).Return(true, createTestUser(userID), nil)

	// Mock GetActiveSubscription for uncancel detection (returns subscriber without WillCancelAt)
	planID := uint(1)
	mockBilling.EXPECT().GetActiveSubscription(mock.Anything, userID).Return(&pluginCore.Subscriber{
		UserID:              userID,
		GatewayType:         "stripe",
		IsActive:            true,
		PricingPlanPeriodID: &planID,
		WillCancelAt:        nil,
	}, nil).Maybe()

	mockBilling.EXPECT().GetPausedSubscription(mock.Anything, userID).Return(nil, nil).Maybe()

	mockQuota.EXPECT().RemoveUserFromPlan(mock.Anything, userID).Return(nil)
	mockBilling.EXPECT().DeactivateSubscriber(mock.Anything, userID, "stripe").Return(nil)
}

// Helper function to run a subscription activation test scenario
func runSubscriptionActivationTest(t *testing.T, eventType string, pricingPlanPeriodID, quotaPlanID uint) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockQuota, mockUsers, mockBilling, mockPricing := setupMockServices(ctx)
		mockSubService := &MockSubscriptionRetriever{}

		subscription := createTestSubscriptionWithPeriod(strconv.FormatUint(uint64(TestUserID), 10), "", strconv.FormatUint(uint64(pricingPlanPeriodID), 10))
		rawData, _ := json.Marshal(subscription)
		event := createTestEvent(eventType, rawData)
		payload, _ := json.Marshal(event)

		mockSubService.SetupGetSuccess(&subscription)
		setupSubscriptionActivationMocks(mockQuota, mockUsers, mockBilling, mockPricing, TestUserID, pricingPlanPeriodID, quotaPlanID)

		gw := NewWithConfig(ctx.Logger(), ctx, testConfig(), mockQuota, mockUsers, mockBilling, mockPricing, nil)
		gw.subService = mockSubService
		err := gw.HandleWebhook(context.Background(), payload)

		assert.NoError(t, err)
	})
}

// Helper function to run a subscription deactivation test scenario
func runSubscriptionDeactivationTest(t *testing.T, eventType string) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockQuota, mockUsers, mockBilling, _ := setupMockServices(ctx)
		mockSubService := &MockSubscriptionRetriever{}

		subscription := createTestSubscription("123", "")
		rawData, _ := json.Marshal(subscription)
		event := createTestEvent(eventType, rawData)
		payload, _ := json.Marshal(event)

		mockSubService.SetupGetSuccess(&subscription)
		setupSubscriptionDeactivationMocks(mockQuota, mockUsers, mockBilling, TestUserID)

		gw := NewWithConfig(ctx.Logger(), ctx, testConfig(), mockQuota, mockUsers, mockBilling, nil, nil)
		gw.subService = mockSubService
		err := gw.HandleWebhook(context.Background(), payload)

		assert.NoError(t, err)
	})
}

func TestStripeGateway_HandleWebhook_SubscriptionDeleted(t *testing.T) {
	runSubscriptionDeactivationTest(t, EventTypeSubscriptionDeleted)
}

func TestStripeGateway_HandleWebhook_SubscriptionUpdated(t *testing.T) {
	pricingPlanID := uint(2)
	quotaPlanID := uint(10) // Different ID to test the mapping
	runSubscriptionActivationTest(t, EventTypeSubscriptionUpdated, pricingPlanID, quotaPlanID)
}

func TestStripeGateway_HandleWebhook_UnknownEvent(t *testing.T) {
	event := createTestEvent("unknown.event.type", nil)
	payload, _ := json.Marshal(event)

	ctx, _ := coreTesting.NewTestContext(t)

	gw := NewWithConfig(ctx.Logger(), ctx, testConfig(), nil, nil, nil, nil, nil)
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
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, _ := coreTesting.NewTestContext(t)
			gw := NewWithConfig(ctx.Logger(), ctx, testConfig(), nil, nil, nil, nil, nil)

			eventID, err := gw.ExtractEventID(context.Background(), tt.payload)
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
			gw := NewWithConfig(ctx.Logger(), ctx, testConfig(), nil, nil, nil, nil, nil)

			eventType, err := gw.ExtractEventType(context.Background(), tt.payload)
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
	gw := NewWithConfig(ctx.Logger(), ctx, testConfigWithSecrets(secret, ""), nil, nil, nil, nil, nil)
	err := gw.HandleWebhook(context.Background(), signedPayload.Payload)
	assert.Error(t, err)
}

func TestStripeGateway_HandleWebhook_UserNotFound(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockQuota, mockUsers, _, _ := setupMockServices(ctx)
		mockSubService := &MockSubscriptionRetriever{}

		subscription := createTestSubscription("123", "1")
		rawData, _ := json.Marshal(subscription)
		event := createTestEvent(EventTypeSubscriptionUpdated, rawData)
		payload, _ := json.Marshal(event)

		mockSubService.SetupGetSuccess(&subscription)
		mockUsers.EXPECT().AccountExists(mock.Anything, TestUserID).Return(false, nil, nil)

		gw := NewWithConfig(ctx.Logger(), ctx, testConfig(), mockQuota, mockUsers, nil, nil, nil)
		gw.subService = mockSubService
		err := gw.HandleWebhook(context.Background(), payload)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "user with ID 123 not found")
	})
}

func TestStripeGateway_HandleWebhook_MissingPlanID(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockQuota, mockUsers, mockBilling, _ := setupMockServices(ctx)
		mockSubService := &MockSubscriptionRetriever{}

		subscription := createTestSubscription("123", "")
		rawData, _ := json.Marshal(subscription)
		event := createTestEvent(EventTypeSubscriptionUpdated, rawData)
		payload, _ := json.Marshal(event)

		mockSubService.SetupGetSuccess(&subscription)
		setupSubscriptionDeactivationMocks(mockQuota, mockUsers, mockBilling, TestUserID)

		gw := NewWithConfig(ctx.Logger(), ctx, testConfig(), mockQuota, mockUsers, mockBilling, nil, nil)
		gw.subService = mockSubService
		err := gw.HandleWebhook(context.Background(), payload)

		// Should proceed with deactivation when no plan ID is found
		assert.NoError(t, err)
	})
}

func TestStripeGateway_HandleWebhook_NilSubscriptionItems(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockQuota, mockUsers, mockBilling, _ := setupMockServices(ctx)
		mockSubService := &MockSubscriptionRetriever{}

		// Create a subscription with nil Items
		subscription := stripe.Subscription{
			ID: TestSubscriptionID,
			Customer: &stripe.Customer{
				ID: TestCustomerID,
				Metadata: map[string]string{
					UserIDMetadataKey: "123",
				},
			},
			Metadata: map[string]string{
				UserIDMetadataKey: "123",
			},
			Items: nil, // Explicitly set to nil
		}

		rawData, _ := json.Marshal(subscription)
		event := createTestEvent(EventTypeSubscriptionUpdated, rawData)
		payload, _ := json.Marshal(event)

		mockSubService.SetupGetSuccess(&subscription)
		setupSubscriptionDeactivationMocks(mockQuota, mockUsers, mockBilling, TestUserID)

		gw := NewWithConfig(ctx.Logger(), ctx, testConfig(), mockQuota, mockUsers, mockBilling, nil, nil)
		gw.subService = mockSubService
		err := gw.HandleWebhook(context.Background(), payload)

		// Should proceed with deactivation when subscription items are nil
		assert.NoError(t, err)
	})
}

func TestStripeGateway_HandleWebhook_SubscriptionUpdated_CancellationRequest(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockQuota, mockUsers, mockBilling, _ := setupMockServices(ctx)
		mockSubService := &MockSubscriptionRetriever{}

		// Create subscription with cancellation request (cancel_at set)
		cancelAt := time.Now().Add(30 * 24 * time.Hour).Unix() // 30 days from now
		planID := uint(2)
		billingStart := time.Now().Add(-30 * 24 * time.Hour)
		billingEnd := time.Now().Add(30 * 24 * time.Hour)

		subscription := stripe.Subscription{
			ID: TestSubscriptionID,
			Customer: &stripe.Customer{
				ID: TestCustomerID,
				Metadata: map[string]string{
					UserIDMetadataKey: "123",
				},
			},
			Metadata: map[string]string{
				UserIDMetadataKey: "123",
			},
			CancelAt: cancelAt,
			CancellationDetails: &stripe.SubscriptionCancellationDetails{
				Reason: "cancellation_requested",
			},
			Items: &stripe.SubscriptionItemList{
				Data: []*stripe.SubscriptionItem{
					{
						Price: &stripe.Price{
							ID: "price_123",
							Product: &stripe.Product{
								ID: "prod_123",
								Metadata: map[string]string{
									PlanIDMetadataKey: "2",
								},
							},
						},
					},
				},
			},
		}

		rawData, _ := json.Marshal(subscription)
		event := createTestEvent(EventTypeSubscriptionUpdated, rawData)
		payload, _ := json.Marshal(event)

		mockSubService.SetupGetSuccess(&subscription)

		// Mock existing subscriber
		existingSubscriber := &pluginCore.Subscriber{
			UserID:              TestUserID,
			GatewayType:         "stripe",
			ExternalID:          TestCustomerID,
			SubscriptionID:      TestSubscriptionID,
			IsActive:            true,
			PricingPlanPeriodID: &planID,
			BillingPeriodStart:  &billingStart,
			BillingPeriodEnd:    &billingEnd,
		}
		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, TestUserID).Return(existingSubscriber, nil)

		// Expect CreateOrUpdateSubscriber to be called with WillCancelAt
		mockBilling.EXPECT().CreateOrUpdateSubscriber(
			mock.Anything,
			TestUserID,
			"stripe",
			TestCustomerID,
			TestSubscriptionID,
			true,
			&planID,
			mock.Anything, mock.Anything, mock.Anything, // variadic SubscriberOption args
		).Return(nil)

		gw := NewWithConfig(ctx.Logger(), ctx, testConfig(), mockQuota, mockUsers, mockBilling, nil, nil)
		gw.subService = mockSubService
		err := gw.HandleWebhook(context.Background(), payload)

		assert.NoError(t, err)

		// Verify that quota operations were not called
		mockQuota.AssertNotCalled(t, "AssignUserToPlan")
		mockQuota.AssertNotCalled(t, "RemoveUserFromPlan")
		// Verify DeactivateSubscriber was not called
		mockBilling.AssertNotCalled(t, "DeactivateSubscriber", mock.Anything, mock.Anything, mock.Anything)
	})
}

func TestStripeGateway_HandleWebhook_SubscriptionUpdated_Uncancel(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockQuota, mockUsers, mockBilling, _ := setupMockServices(ctx)
		mockSubService := &MockSubscriptionRetriever{}

		// Create subscription WITHOUT cancellation (cancel_at=0, no cancellation details)
		planID := uint(2)
		billingStart := time.Now().Add(-30 * 24 * time.Hour)
		billingEnd := time.Now().Add(30 * 24 * time.Hour)
		futureCancelAt := time.Now().Add(7 * 24 * time.Hour)

		subscription := stripe.Subscription{
			ID: TestSubscriptionID,
			Customer: &stripe.Customer{
				ID: TestCustomerID,
				Metadata: map[string]string{
					UserIDMetadataKey: "123",
				},
			},
			Metadata: map[string]string{
				UserIDMetadataKey: "123",
			},
			CancelAt:             0,
			CancellationDetails:  nil,
			CancelAtPeriodEnd:    false,
			Items: &stripe.SubscriptionItemList{
				Data: []*stripe.SubscriptionItem{
					{
						Price: &stripe.Price{
							ID: "price_123",
							Product: &stripe.Product{
								ID: "prod_123",
								Metadata: map[string]string{
									PlanIDMetadataKey: "2",
								},
							},
						},
					},
				},
			},
		}

		rawData, _ := json.Marshal(subscription)
		event := createTestEvent(EventTypeSubscriptionUpdated, rawData)
		payload, _ := json.Marshal(event)

		mockSubService.SetupGetSuccess(&subscription)

		// Mock existing subscriber WITH WillCancelAt set (i.e., previously had cancellation scheduled)
		existingSubscriber := &pluginCore.Subscriber{
			UserID:              TestUserID,
			GatewayType:         "stripe",
			ExternalID:          TestCustomerID,
			SubscriptionID:      TestSubscriptionID,
			IsActive:            true,
			PricingPlanPeriodID: &planID,
			BillingPeriodStart:  &billingStart,
			BillingPeriodEnd:    &billingEnd,
			WillCancelAt:        &futureCancelAt,
		}
		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, TestUserID).Return(existingSubscriber, nil)

		// Expect CreateOrUpdateSubscriber to be called with WithClearWillCancelAt
		mockBilling.EXPECT().CreateOrUpdateSubscriber(
			mock.Anything,
			TestUserID,
			"stripe",
			TestCustomerID,
			TestSubscriptionID,
			true,
			&planID,
			mock.Anything, mock.Anything, mock.Anything, // variadic SubscriberOption args
		).Return(nil)

		gw := NewWithConfig(ctx.Logger(), ctx, testConfig(), mockQuota, mockUsers, mockBilling, nil, nil)
		gw.subService = mockSubService
		err := gw.HandleWebhook(context.Background(), payload)

		assert.NoError(t, err)

		// Verify that quota operations were not called
		mockQuota.AssertNotCalled(t, "AssignUserToPlan")
		mockQuota.AssertNotCalled(t, "RemoveUserFromPlan")
		mockBilling.AssertNotCalled(t, "DeactivateSubscriber", mock.Anything, mock.Anything, mock.Anything)
	})
}



func TestStripeGateway_HandleWebhook_SubscriptionUpdated_CanceledStatus(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockQuota, mockUsers, mockBilling, _ := setupMockServices(ctx)
		mockSubService := &MockSubscriptionRetriever{}

		subscription := stripe.Subscription{
			ID: TestSubscriptionID,
			Status: stripe.SubscriptionStatusCanceled,
			Customer: &stripe.Customer{
				ID: TestCustomerID,
				Metadata: map[string]string{
					UserIDMetadataKey: "123",
				},
			},
			Metadata: map[string]string{
				UserIDMetadataKey: "123",
			},
			Items: &stripe.SubscriptionItemList{
				Data: []*stripe.SubscriptionItem{
					{
						Price: &stripe.Price{
							ID: "price_123",
							Product: &stripe.Product{
								ID: "prod_123",
								Metadata: map[string]string{
									PlanIDMetadataKey: "2",
								},
							},
						},
					},
				},
			},
		}

		rawData, _ := json.Marshal(subscription)
		event := createTestEvent(EventTypeSubscriptionUpdated, rawData)
		payload, _ := json.Marshal(event)

		mockSubService.SetupGetSuccess(&subscription)

		gw := NewWithConfig(ctx.Logger(), ctx, testConfig(), mockQuota, mockUsers, mockBilling, nil, nil)
		gw.subService = mockSubService
		err := gw.HandleWebhook(context.Background(), payload)

		assert.NoError(t, err)

		mockQuota.AssertNotCalled(t, "AssignUserToPlan")
		mockQuota.AssertNotCalled(t, "RemoveUserFromPlan")
		mockBilling.AssertNotCalled(t, "CreateOrUpdateSubscriber")
		mockBilling.AssertNotCalled(t, "DeactivateSubscriber")
	})
}

func TestStripeGateway_HandleWebhook_SubscriptionUpdated_PauseCollectionSet(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockQuota, mockUsers, mockBilling, _ := setupMockServices(ctx)
		mockSubService := &MockSubscriptionRetriever{}

		subscription := stripe.Subscription{
			ID: TestSubscriptionID,
			Customer: &stripe.Customer{
				ID: TestCustomerID,
				Metadata: map[string]string{
					UserIDMetadataKey: "123",
				},
			},
			Metadata: map[string]string{
				UserIDMetadataKey: "123",
			},
			PauseCollection: &stripe.SubscriptionPauseCollection{
				Behavior: stripe.SubscriptionPauseCollectionBehaviorMarkUncollectible,
			},
		}

		rawData, _ := json.Marshal(subscription)
		event := createTestEvent(EventTypeSubscriptionUpdated, rawData)
		payload, _ := json.Marshal(event)

		mockSubService.SetupGetSuccess(&subscription)

		planID := uint(2)
		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, TestUserID).Return(&pluginCore.Subscriber{
			UserID:              TestUserID,
			GatewayType:         "stripe",
			ExternalID:          TestCustomerID,
			SubscriptionID:      TestSubscriptionID,
			IsActive:            true,
			PricingPlanPeriodID: &planID,
		}, nil)

		mockUsers.EXPECT().AccountExists(mock.Anything, TestUserID).Return(true, createTestUser(TestUserID), nil)
		mockQuota.EXPECT().RemoveUserFromPlan(mock.Anything, TestUserID).Return(nil)
		mockBilling.EXPECT().PauseSubscriber(mock.Anything, TestUserID, "stripe").Return(nil)

		gw := NewWithConfig(ctx.Logger(), ctx, testConfig(), mockQuota, mockUsers, mockBilling, nil, nil)
		gw.subService = mockSubService
		err := gw.HandleWebhook(context.Background(), payload)

		assert.NoError(t, err)
	})
}

func TestStripeGateway_HandleWebhook_SubscriptionUpdated_PauseCollectionCleared(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockQuota, mockUsers, mockBilling, mockPricing := setupMockServices(ctx)
		mockSubService := &MockSubscriptionRetriever{}

		pricingPlanPeriodID := uint(2)
		quotaPlanID := uint(10)

		subscription := createTestSubscriptionWithPeriod(
			strconv.FormatUint(uint64(TestUserID), 10),
			"",
			strconv.FormatUint(uint64(pricingPlanPeriodID), 10),
		)

		rawData, _ := json.Marshal(subscription)
		event := createTestEvent(EventTypeSubscriptionUpdated, rawData)
		payload, _ := json.Marshal(event)

		mockSubService.SetupGetSuccess(&subscription)

		// Uncancel detection calls GetActiveSubscription first — returns nil because subscriber is paused
		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, TestUserID).Return(nil, nil)

		mockBilling.EXPECT().GetPausedSubscription(mock.Anything, TestUserID).Return(&pluginCore.Subscriber{
			UserID:              TestUserID,
			GatewayType:         "stripe",
			ExternalID:          TestCustomerID,
			SubscriptionID:      TestSubscriptionID,
			IsActive:            false,
			PricingPlanPeriodID: &pricingPlanPeriodID,
		}, nil)

		setupSubscriptionResumeMocks(mockQuota, mockUsers, mockBilling, mockPricing, TestUserID, pricingPlanPeriodID, quotaPlanID)

		gw := NewWithConfig(ctx.Logger(), ctx, testConfig(), mockQuota, mockUsers, mockBilling, mockPricing, nil)
		gw.subService = mockSubService
		err := gw.HandleWebhook(context.Background(), payload)

		assert.NoError(t, err)
	})
}

func TestStripeGateway_HandleWebhook_SubscriptionUpdated_PauseCollectionSetAlreadyPaused(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockQuota, mockUsers, mockBilling, _ := setupMockServices(ctx)
		mockSubService := &MockSubscriptionRetriever{}

		subscription := stripe.Subscription{
			ID: TestSubscriptionID,
			Customer: &stripe.Customer{
				ID: TestCustomerID,
				Metadata: map[string]string{
					UserIDMetadataKey: "123",
				},
			},
			Metadata: map[string]string{
				UserIDMetadataKey: "123",
			},
			PauseCollection: &stripe.SubscriptionPauseCollection{
				Behavior: stripe.SubscriptionPauseCollectionBehaviorMarkUncollectible,
			},
		}

		rawData, _ := json.Marshal(subscription)
		event := createTestEvent(EventTypeSubscriptionUpdated, rawData)
		payload, _ := json.Marshal(event)

		mockSubService.SetupGetSuccess(&subscription)

		// No active subscriber (they're already paused)
		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, TestUserID).Return(nil, nil)

		// GetPausedSubscription returns the already-paused subscriber
		planID := uint(2)
		mockBilling.EXPECT().GetPausedSubscription(mock.Anything, TestUserID).Return(&pluginCore.Subscriber{
			UserID:              TestUserID,
			GatewayType:         "stripe",
			ExternalID:          TestCustomerID,
			SubscriptionID:      TestSubscriptionID,
			IsActive:            false,
			PricingPlanPeriodID: &planID,
		}, nil)

		gw := NewWithConfig(ctx.Logger(), ctx, testConfig(), mockQuota, mockUsers, mockBilling, nil, nil)
		gw.subService = mockSubService
		err := gw.HandleWebhook(context.Background(), payload)

		assert.NoError(t, err)

		// Verify no quota or billing state changes
		mockQuota.AssertNotCalled(t, "RemoveUserFromPlan")
		mockQuota.AssertNotCalled(t, "AssignUserToPlan")
		mockBilling.AssertNotCalled(t, "PauseSubscriber")
		mockBilling.AssertNotCalled(t, "ResumeSubscriber")
		mockBilling.AssertNotCalled(t, "DeactivateSubscriber")
		mockBilling.AssertNotCalled(t, "CreateOrUpdateSubscriber")
	})
}

func TestStripeGateway_HandleWebhook_SubscriptionUpdated_AllPricesNil(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockQuota, mockUsers, mockBilling, _ := setupMockServices(ctx)
		mockSubService := &MockSubscriptionRetriever{}

		// Create subscription with items but all Price fields nil
		subscription := stripe.Subscription{
			ID: TestSubscriptionID,
			Customer: &stripe.Customer{
				ID: TestCustomerID,
				Metadata: map[string]string{
					UserIDMetadataKey: "123",
				},
			},
			Metadata: map[string]string{
				UserIDMetadataKey: "123",
			},
			Items: &stripe.SubscriptionItemList{
				Data: []*stripe.SubscriptionItem{
					{
						Price: nil,
					},
					{
						Price: nil,
					},
				},
			},
		}

		rawData, _ := json.Marshal(subscription)
		event := createTestEvent(EventTypeSubscriptionUpdated, rawData)
		payload, _ := json.Marshal(event)

		// Set up mock expectations for the deactivation path
		mockSubService.SetupGetSuccess(&subscription)
		setupSubscriptionDeactivationMocks(mockQuota, mockUsers, mockBilling, TestUserID)

		gw := NewWithConfig(ctx.Logger(), ctx, testConfig(), mockQuota, mockUsers, mockBilling, nil, nil)
		gw.subService = mockSubService
		err := gw.HandleWebhook(context.Background(), payload)

		// Should not return error but should proceed with deactivation
		assert.NoError(t, err)
	})
}

func TestStripeGateway_HandleWebhook_SubscriptionPaused(t *testing.T) {
	runSubscriptionPauseTest(t, EventTypeSubscriptionPaused)
}


// Helper function to run a subscription pause test scenario
func runSubscriptionPauseTest(t *testing.T, eventType string) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockQuota, mockUsers, mockBilling, _ := setupMockServices(ctx)
		mockSubService := &MockSubscriptionRetriever{}

		subscription := createTestSubscription("123", "")
		rawData, _ := json.Marshal(subscription)
		event := createTestEvent(eventType, rawData)
		payload, _ := json.Marshal(event)

		mockSubService.SetupGetSuccess(&subscription)
		mockUsers.EXPECT().AccountExists(mock.Anything, TestUserID).Return(true, createTestUser(TestUserID), nil)
		mockQuota.EXPECT().RemoveUserFromPlan(mock.Anything, TestUserID).Return(nil)
		mockBilling.EXPECT().PauseSubscriber(mock.Anything, TestUserID, "stripe").Return(nil)

		gw := NewWithConfig(ctx.Logger(), ctx, testConfig(), mockQuota, mockUsers, mockBilling, nil, nil)
		gw.subService = mockSubService
		err := gw.HandleWebhook(context.Background(), payload)

		assert.NoError(t, err)
	})
}

// Helper function to setup mocks for subscription resume scenarios
func setupSubscriptionResumeMocks(mockQuota *quotaCore.MockQuotaService, mockUsers *coreTesting.MockUserService, mockBilling *pluginCore.MockBillingService, mockPricing *pluginCore.MockPricingService, userID uint, pricingPlanPeriodID, quotaPlanID uint) {
	mockUsers.EXPECT().AccountExists(mock.Anything, userID).Return(true, createTestUser(userID), nil)
	mockBilling.EXPECT().ResumeSubscriber(mock.Anything, userID, "stripe").Return(nil)

	mockPricing.EXPECT().GetPricingPlanPeriod(mock.Anything, pricingPlanPeriodID).Return(&billingModels.PricingPlanPeriod{
		QuotaPlanID: quotaPlanID,
	}, nil)

	mockQuota.EXPECT().AssignUserToPlan(mock.Anything, userID, quotaPlanID).Return(nil)
}

// Helper function to run a subscription resume test scenario
func runSubscriptionResumeTest(t *testing.T, eventType string, pricingPlanPeriodID, quotaPlanID uint) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockQuota, mockUsers, mockBilling, mockPricing := setupMockServices(ctx)
		mockSubService := &MockSubscriptionRetriever{}

		subscription := createTestSubscriptionWithPeriod(strconv.FormatUint(uint64(TestUserID), 10), "", strconv.FormatUint(uint64(pricingPlanPeriodID), 10))
		rawData, _ := json.Marshal(subscription)
		event := createTestEvent(eventType, rawData)
		payload, _ := json.Marshal(event)

		mockSubService.SetupGetSuccess(&subscription)
		setupSubscriptionResumeMocks(mockQuota, mockUsers, mockBilling, mockPricing, TestUserID, pricingPlanPeriodID, quotaPlanID)

		gw := NewWithConfig(ctx.Logger(), ctx, testConfig(), mockQuota, mockUsers, mockBilling, mockPricing, nil)
		gw.subService = mockSubService
		err := gw.HandleWebhook(context.Background(), payload)

		assert.NoError(t, err)
	})
}

func TestStripeGateway_HandleWebhook_SubscriptionResumed(t *testing.T) {
	pricingPlanPeriodID := uint(2)
	quotaPlanID := uint(10)
	runSubscriptionResumeTest(t, EventTypeSubscriptionResumed, pricingPlanPeriodID, quotaPlanID)
}

func TestStripeGateway_GetCustomerPortalURL_Success(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)
		mockStripeClient := &MockStripeClient{}

		// Mock active subscription
		planID := uint(42)
		mockSubscriber := &pluginCore.Subscriber{
			UserID:               123,
			GatewayType:          "stripe",
			ExternalID:           "cus_123",
			SubscriptionID:       "sub_123",
			IsActive:             true,
			PricingPlanPeriodID:  &planID,
		}
		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, uint(123)).Return(mockSubscriber, nil)

		// Mock successful billing portal session creation
		mockSession := &stripe.BillingPortalSession{
			URL: "https://billing.stripe.com/p/session/test_session_id",
		}
		mockStripeClient.BillingPortalSessionsService = &MockBillingPortalSessions{}
		mockStripeClient.BillingPortalSessionsService.
			On("Create", mock.Anything, mock.AnythingOfType("*stripe.BillingPortalSessionCreateParams")).
			Run(func(args mock.Arguments) {
				params := args.Get(1).(*stripe.BillingPortalSessionCreateParams)
				assert.Equal(t, "cus_123", stripe.StringValue(params.Customer))
				assert.Equal(t, "https://example.com/return", stripe.StringValue(params.ReturnURL))
			}).
			Return(mockSession, nil)

		gw := NewWithConfig(ctx.Logger(), ctx, testConfigWithSecrets(TestWebhookSecret, "test_api_key"), nil, nil, mockBilling, nil, nil)
		gw.stripeClient = mockStripeClient

		url, err := gw.GetCustomerPortalURL(context.Background(), 123, "https://example.com/return")

		assert.NoError(t, err)
		assert.Equal(t, "https://billing.stripe.com/p/session/test_session_id", url)
	})
}

func TestStripeGateway_ExecuteCancel_Immediate(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)
		mockStripeClient := &MockStripeClient{}

		userID := uint(123)
		planID := uint(42)
		mockSubscriber := &pluginCore.Subscriber{
			UserID:              userID,
			GatewayType:         "stripe",
			ExternalID:          TestCustomerID,
			SubscriptionID:      TestSubscriptionID,
			IsActive:            true,
			PricingPlanPeriodID: &planID,
		}
		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, userID).Return(mockSubscriber, nil)

		mockStripeClient.SubscriptionsService = &MockSubscriptions{}
		mockStripeClient.SubscriptionsService.
			On("Cancel", mock.Anything, TestSubscriptionID, mock.AnythingOfType("*stripe.SubscriptionCancelParams")).
			Return(&stripe.Subscription{ID: TestSubscriptionID}, nil)

		gw := NewWithConfig(ctx.Logger(), ctx, testConfig(), nil, nil, mockBilling, nil, nil)
		gw.stripeClient = mockStripeClient

		result, err := gw.ExecuteCancel(context.Background(), userID, true)

		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, pluginCore.CancellationStatusImmediate, result.Status)
		assert.NotNil(t, result.EffectiveAt)
		assert.False(t, result.CanAbort)
		mockStripeClient.SubscriptionsService.AssertExpectations(t)
	})
}

func TestStripeGateway_ExecuteCancel_Scheduled(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)
		mockStripeClient := &MockStripeClient{}

		userID := uint(123)
		planID := uint(42)
		mockSubscriber := &pluginCore.Subscriber{
			UserID:              userID,
			GatewayType:         "stripe",
			ExternalID:          TestCustomerID,
			SubscriptionID:      TestSubscriptionID,
			IsActive:            true,
			PricingPlanPeriodID: &planID,
		}
		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, userID).Return(mockSubscriber, nil)

		periodEnd := time.Now().Add(30 * 24 * time.Hour).Unix()
		mockStripeClient.SubscriptionsService = &MockSubscriptions{}
		mockStripeClient.SubscriptionsService.
			On("Update", mock.Anything, TestSubscriptionID, mock.AnythingOfType("*stripe.SubscriptionUpdateParams")).
			Return(&stripe.Subscription{
				ID: TestSubscriptionID,
				Items: &stripe.SubscriptionItemList{
					Data: []*stripe.SubscriptionItem{
						{CurrentPeriodEnd: periodEnd},
					},
				},
			}, nil)

		gw := NewWithConfig(ctx.Logger(), ctx, testConfig(), nil, nil, mockBilling, nil, nil)
		gw.stripeClient = mockStripeClient

		result, err := gw.ExecuteCancel(context.Background(), userID, false)

		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, pluginCore.CancellationStatusScheduled, result.Status)
		assert.NotNil(t, result.EffectiveAt)
		assert.True(t, result.CanAbort)
		mockStripeClient.SubscriptionsService.AssertExpectations(t)
	})
}

func TestStripeGateway_ExecuteCancel_Immediate_ApiError(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)
		mockStripeClient := &MockStripeClient{}

		userID := uint(123)
		planID := uint(42)
		mockSubscriber := &pluginCore.Subscriber{
			UserID:              userID,
			GatewayType:         "stripe",
			ExternalID:          TestCustomerID,
			SubscriptionID:      TestSubscriptionID,
			IsActive:            true,
			PricingPlanPeriodID: &planID,
		}
		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, userID).Return(mockSubscriber, nil)

		mockStripeClient.SubscriptionsService = &MockSubscriptions{}
		mockStripeClient.SubscriptionsService.
			On("Cancel", mock.Anything, TestSubscriptionID, mock.AnythingOfType("*stripe.SubscriptionCancelParams")).
			Return((*stripe.Subscription)(nil), fmt.Errorf("stripe api error"))

		gw := NewWithConfig(ctx.Logger(), ctx, testConfig(), nil, nil, mockBilling, nil, nil)
		gw.stripeClient = mockStripeClient

		result, err := gw.ExecuteCancel(context.Background(), userID, true)

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "failed to cancel subscription")
	})
}

func TestStripeGateway_ExecuteCancel_NoActiveSubscription(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)

		userID := uint(123)
		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, userID).Return(nil, nil)

		gw := NewWithConfig(ctx.Logger(), ctx, testConfig(), nil, nil, mockBilling, nil, nil)

		result, err := gw.ExecuteCancel(context.Background(), userID, true)

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "no active stripe subscription found")
	})
}

func TestStripeGateway_ExecuteCancel_WrongGateway(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)

		userID := uint(123)
		mockSubscriber := &pluginCore.Subscriber{
			UserID:         userID,
			GatewayType:    "atlos",
			SubscriptionID: TestSubscriptionID,
			IsActive:       true,
		}
		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, userID).Return(mockSubscriber, nil)

		gw := NewWithConfig(ctx.Logger(), ctx, testConfig(), nil, nil, mockBilling, nil, nil)

		result, err := gw.ExecuteCancel(context.Background(), userID, true)

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "no active stripe subscription found")
	})
}

func TestStripeGateway_ExecuteCancel_NoBillingService(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		gw := NewWithConfig(ctx.Logger(), ctx, testConfig(), nil, nil, nil, nil, nil)

		result, err := gw.ExecuteCancel(context.Background(), 123, true)

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "billing service not configured")
	})
}

func TestStripeGateway_ExecuteCancel_Immediate_DoesNotDeactivateOrFireEvent(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)
		mockStripeClient := &MockStripeClient{}

		userID := uint(123)
		planID := uint(42)
		mockSubscriber := &pluginCore.Subscriber{
			UserID:              userID,
			GatewayType:         "stripe",
			ExternalID:          TestCustomerID,
			SubscriptionID:      TestSubscriptionID,
			IsActive:            true,
			PricingPlanPeriodID: &planID,
		}
		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, userID).Return(mockSubscriber, nil)

		mockStripeClient.SubscriptionsService = &MockSubscriptions{}
		mockStripeClient.SubscriptionsService.
			On("Cancel", mock.Anything, TestSubscriptionID, mock.AnythingOfType("*stripe.SubscriptionCancelParams")).
			Return(&stripe.Subscription{ID: TestSubscriptionID}, nil)

		gw := NewWithConfig(ctx.Logger(), ctx, testConfig(), nil, nil, mockBilling, nil, nil)
		gw.stripeClient = mockStripeClient

		result, err := gw.ExecuteCancel(context.Background(), userID, true)

		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, pluginCore.CancellationStatusImmediate, result.Status)

		// Verify DeactivateSubscriber was NOT called — webhook handles that
		mockBilling.AssertNotCalled(t, "DeactivateSubscriber", mock.Anything, userID, "stripe")
	})
}

func TestStripeGateway_HandleWebhook_CheckoutSessionCompleted(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockQuota, mockUsers, mockBilling, mockPricing := setupMockServices(ctx)
		mockSubService := &MockSubscriptionRetriever{}

		userID := uint(456)
		customerID := "cus_456"
		periodID := uint(100)
		subscriptionID := "sub_456"

		checkoutSession := stripe.CheckoutSession{
			ID:                "cs_test_123",
			ClientReferenceID: strconv.FormatUint(uint64(userID), 10),
			Customer:          &stripe.Customer{ID: customerID},
			Subscription:      &stripe.Subscription{ID: subscriptionID},
			Mode:              stripe.CheckoutSessionModeSubscription,
		}
		rawData, _ := json.Marshal(checkoutSession)
		event := createTestEvent(EventTypeCheckoutSessionCompleted, rawData)
		payload, _ := json.Marshal(event)

		// Create subscription with period_id in price metadata
		subscription := stripe.Subscription{
			ID: subscriptionID,
			Customer: &stripe.Customer{
				ID: customerID,
				Metadata: map[string]string{
					UserIDMetadataKey: strconv.FormatUint(uint64(userID), 10),
				},
			},
			Items: &stripe.SubscriptionItemList{
				Data: []*stripe.SubscriptionItem{
					{
						Price: &stripe.Price{
							ID: "price_456",
							Product: &stripe.Product{
								ID:       "prod_456",
								Metadata: map[string]string{},
							},
							Metadata: map[string]string{
								"period_id": strconv.FormatUint(uint64(periodID), 10),
							},
						},
					},
				},
			},
		}
		mockSubService.SetupGetSuccess(&subscription)

		// Check for existing subscriber (no race condition - still inactive)
		mockBilling.EXPECT().GetSubscriberBySubscriptionID(mock.Anything, subscriptionID, "stripe").Return(nil, nil)

		// Billing service creates a pending (inactive) subscriber with period ID
		mockBilling.EXPECT().CreateOrUpdateSubscriber(
			mock.Anything,
			userID,
			"stripe",
			customerID,
			subscriptionID,
			false,
			mock.MatchedBy(func(p *uint) bool {
				return p != nil && *p == periodID
			}),
		).Return(nil)

		gw := NewWithConfig(ctx.Logger(), ctx, testConfigWithSecrets(TestWebhookSecret, "test_api_key"), mockQuota, mockUsers, mockBilling, mockPricing, nil)
		gw.subService = mockSubService

		err := gw.HandleWebhook(context.Background(), payload)

		assert.NoError(t, err)
	})
}

func TestStripeGateway_GetCustomerPortalURL_NoActiveSubscription(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)

		// Mock no active subscription and no paused subscription
		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, uint(123)).Return((*pluginCore.Subscriber)(nil), nil)
		mockBilling.EXPECT().GetPausedSubscription(mock.Anything, uint(123)).Return((*pluginCore.Subscriber)(nil), nil)

		gw := NewWithConfig(ctx.Logger(), ctx, testConfig(), nil, nil, mockBilling, nil, nil)

		url, err := gw.GetCustomerPortalURL(context.Background(), 123, "https://example.com/return")

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no active or paused stripe subscription found")
		assert.Empty(t, url)
	})
}

func TestStripeGateway_ExtractUserIDFromSubscription_DatabaseFallback(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		// Setup mock services
		mockUser := core.GetService[*coreTesting.MockUserService](ctx, core.USER_SERVICE)
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)

		// Create a mock customer retriever
		mockCustomerRetriever := &mockCustomerRetriever{}

		// Setup gateway with mock services and inject the mock customer retriever
		gw := NewWithConfig(ctx.Logger(), ctx, testConfig(), nil, mockUser, mockBilling, nil, nil)
		gw.SetCustomerRetrieverForTesting(mockCustomerRetriever)

		// Test case 1: Customer metadata has user_id
		customerWithMetadata := &stripe.Customer{
			ID: "cus_test_123",
			Metadata: map[string]string{
				"user_id": "456",
			},
		}
		subscription := &stripe.Subscription{
			ID:       "sub_test_123",
			Customer: customerWithMetadata,
		}

		userID, err := gw.ExtractUserIDFromSubscriptionForTesting(context.Background(), subscription)
		assert.NoError(t, err)
		assert.Equal(t, uint(456), userID)

		// Test case 2: Customer metadata missing, but database has mapping
		customerWithoutMetadata := &stripe.Customer{
			ID:       "cus_test_456",
			Metadata: map[string]string{},
		}
		subscription2 := &stripe.Subscription{
			ID:       "sub_test_456",
			Customer: customerWithoutMetadata,
		}

		// Setup mock billing service to return subscriber
		planID := uint(100)
		mockSubscriber := &pluginCore.Subscriber{
			UserID:              789,
			GatewayType:         "stripe",
			ExternalID:          "cus_test_456",
			SubscriptionID:      "sub_test_456",
			IsActive:            true,
			PricingPlanPeriodID: &planID,
		}
		mockBilling.EXPECT().GetSubscriberByExternalID(mock.Anything, "cus_test_456", "stripe").Return(mockSubscriber, nil)

		userID, err = gw.ExtractUserIDFromSubscriptionForTesting(context.Background(), subscription2)
		assert.NoError(t, err)
		assert.Equal(t, uint(789), userID)

		// Test case 3: Customer metadata missing, database also missing
		customerWithoutMapping := &stripe.Customer{
			ID:       "cus_test_789",
			Metadata: map[string]string{},
		}
		subscription3 := &stripe.Subscription{
			ID:       "sub_test_789",
			Customer: customerWithoutMapping,
		}

		// Setup mock billing service to return nil (not found)
		mockBilling.EXPECT().GetSubscriberByExternalID(mock.Anything, "cus_test_789", "stripe").Return(nil, nil)

		// Setup mock customer retriever to return customer without metadata
		mockCustomerRetriever.On("Get", mock.Anything, "cus_test_789", (*stripe.CustomerRetrieveParams)(nil)).Return(customerWithoutMapping, nil)

		userID, err = gw.ExtractUserIDFromSubscriptionForTesting(context.Background(), subscription3)
		assert.NoError(t, err)
		assert.Equal(t, uint(0), userID) // Should return 0 when not found
	})
}

func TestStripeGateway_GetCustomerPortalURL_SessionCreateError(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)
		mockStripeClient := &MockStripeClient{}

		planID := uint(42)
		mockSubscriber := &pluginCore.Subscriber{
			UserID: 123, GatewayType: "stripe", ExternalID: "cus_123", IsActive: true, PricingPlanPeriodID: &planID,
		}
		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, uint(123)).Return(mockSubscriber, nil)

		mockStripeClient.BillingPortalSessionsService = &MockBillingPortalSessions{}
		mockStripeClient.BillingPortalSessionsService.
			On("Create", mock.Anything, mock.AnythingOfType("*stripe.BillingPortalSessionCreateParams")).
			Return((*stripe.BillingPortalSession)(nil), assert.AnError)

		gw := NewWithConfig(ctx.Logger(), ctx, testConfigWithSecrets(TestWebhookSecret, "test_api_key"), nil, nil, mockBilling, nil, nil)
		gw.stripeClient = mockStripeClient

		url, err := gw.GetCustomerPortalURL(context.Background(), 123, "https://example.com/return")
		assert.Error(t, err)
		assert.Empty(t, url)
	})
}

func TestStripeGateway_GetCustomerPortalURL_NonStripeSubscription(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)

		// Mock active subscription with different gateway
		planID := uint(42)
		mockSubscriber := &pluginCore.Subscriber{
			UserID:              123,
			GatewayType:         "paypal", // Different gateway
			IsActive:            true,
			PricingPlanPeriodID: &planID,
		}
		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, uint(123)).Return(mockSubscriber, nil)
		mockBilling.EXPECT().GetPausedSubscription(mock.Anything, uint(123)).Return((*pluginCore.Subscriber)(nil), nil)

		gw := NewWithConfig(ctx.Logger(), ctx, testConfig(), nil, nil, mockBilling, nil, nil)

		url, err := gw.GetCustomerPortalURL(context.Background(), 123, "https://example.com/return")

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no active or paused stripe subscription found")
		assert.Empty(t, url)
	})
}

// mockCustomerRetriever is a mock implementation of CustomerRetriever for testing
type mockCustomerRetriever struct {
	mock.Mock
}

func (m *mockCustomerRetriever) Get(ctx context.Context, id string, params *stripe.CustomerRetrieveParams) (*stripe.Customer, error) {
	args := m.Called(ctx, id, params)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*stripe.Customer), args.Error(1)
}

func TestStripeGateway_GetCustomerPortalURL_BillingServiceError(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)

		// Mock billing service error
		mockBilling.On("GetActiveSubscription", mock.Anything, uint(123)).Return((*pluginCore.Subscriber)(nil), assert.AnError)

		gw := NewWithConfig(ctx.Logger(), ctx, testConfig(), nil, nil, mockBilling, nil, nil)

		url, err := gw.GetCustomerPortalURL(context.Background(), 123, "https://example.com/return")

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get active subscription")
		assert.Empty(t, url)
	})
}

func TestStripeGateway_GetCustomerPortalURL_InvalidCustomerID(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)

		// Mock active subscription with invalid ExternalID (not starting with cus_)
		planID := uint(42)
		mockSubscriber := &pluginCore.Subscriber{
			UserID:               123,
			GatewayType:          "stripe",
			ExternalID:           "sub_123", // This is a subscription ID, not a customer ID
			SubscriptionID:       "sub_123",
			IsActive:             true,
			PricingPlanPeriodID:  &planID,
		}
		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, uint(123)).Return(mockSubscriber, nil)

		gw := NewWithConfig(ctx.Logger(), ctx, testConfig(), nil, nil, mockBilling, nil, nil)

		url, err := gw.GetCustomerPortalURL(context.Background(), 123, "https://example.com/return")

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid ExternalID: must be a Stripe customer ID starting with 'cus_'")
		assert.Empty(t, url)
	})
}

func TestStripeGateway_GetCustomerPortalURL_EmptyCustomerID(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)

		// Mock active subscription with empty ExternalID
		planID := uint(42)
		mockSubscriber := &pluginCore.Subscriber{
			UserID:               123,
			GatewayType:          "stripe",
			ExternalID:           "", // Empty ExternalID
			SubscriptionID:       "sub_123",
			IsActive:             true,
			PricingPlanPeriodID:  &planID,
		}
		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, uint(123)).Return(mockSubscriber, nil)

		gw := NewWithConfig(ctx.Logger(), ctx, testConfig(), nil, nil, mockBilling, nil, nil)

		url, err := gw.GetCustomerPortalURL(context.Background(), 123, "https://example.com/return")

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "subscriber ExternalID is empty")
		assert.Empty(t, url)
	})
}

// ============== SyncPlan Tests for Flexible Pricing ==============
// Note: Most SyncPlan tests are in stripe_syncplan_test.go to avoid conflicts
// This file contains additional edge case tests not covered elsewhere

// TestStripeGateway_SyncPlan_MultiplePeriodsDifferentCadences tests plan with multiple periods of different cadences (monthly, quarterly, yearly, weekly)
func TestStripeGateway_SyncPlan_MultiplePeriodsDifferentCadences(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)
		mockPricing := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)
		mockStripeClient := &MockStripeClient{
			V1ProductsService:                   &MockProducts{},
			V1PricesService:                     &MockPrices{},
			V1BillingPortalConfigurationsService: &MockBillingPortalConfigurations{},
			BillingPortalSessionsService:        &MockBillingPortalSessions{},
			CustomersService:                    &MockCustomers{},
			SubscriptionsService:                &MockSubscriptions{},
			V1CheckoutSessionsService:           &MockCheckoutSessions{},
		}

		planID := uint(100)
		monthlyID := uint(200)
		quarterlyID := uint(201)
		yearlyID := uint(202)
		weeklyID := uint(203)
		productID := "prod_test"

		planInfo := &pluginCore.PricingPlanInfo{
			ID:          planID,
			Name:        "Multi-Cadence Plan",
			Description: "All billing cadences",
			Currency:    "USD",
			PricingVariants: []pluginCore.PricingVariant{
				{BillingPeriodID: monthlyID, PriceUSD: 9.99, QuotaPlanID: 300, Cadence: "monthly"},
				{BillingPeriodID: quarterlyID, PriceUSD: 24.99, QuotaPlanID: 300, Cadence: "quarterly"},
				{BillingPeriodID: yearlyID, PriceUSD: 99.99, QuotaPlanID: 300, Cadence: "yearly"},
				{BillingPeriodID: weeklyID, PriceUSD: 2.49, QuotaPlanID: 300, Cadence: "weekly"},
			},
			IsActive: true,
			IsPublic: true,
		}

		mockPricing.EXPECT().GetPriceLinesForPlan(mock.Anything, planID).Return([]*billingModels.PriceLinePlan{}, nil)
		mockPricing.EXPECT().GetPricingPlanPeriods(mock.Anything, planID).Return([]*billingModels.PricingPlanPeriod{
			{Model: gorm.Model{ID: monthlyID}, PricingPlanID: planID, Cadence: "monthly", PriceUSD: 9.99, QuotaPlanID: 300},
			{Model: gorm.Model{ID: quarterlyID}, PricingPlanID: planID, Cadence: "quarterly", PriceUSD: 24.99, QuotaPlanID: 300},
			{Model: gorm.Model{ID: yearlyID}, PricingPlanID: planID, Cadence: "yearly", PriceUSD: 99.99, QuotaPlanID: 300},
			{Model: gorm.Model{ID: weeklyID}, PricingPlanID: planID, Cadence: "weekly", PriceUSD: 2.49, QuotaPlanID: 300},
		}, nil)
		mockPricing.EXPECT().GetGatewayProductMappingsByPlan(mock.Anything, planID).Return([]*billingModels.GatewayProductMapping{}, nil)
		mockPricing.EXPECT().CreateGatewayProductMapping(mock.Anything, mock.Anything).Return(nil).Times(4)

		mockStripeClient.V1ProductsService.On("Create", mock.Anything, mock.Anything).Return(&stripe.Product{ID: productID}, nil)
		mockStripeClient.V1ProductsService.On("Update", mock.Anything, productID, mock.AnythingOfType("*stripe.ProductUpdateParams")).Return(&stripe.Product{ID: productID}, nil)

		priceCreatedCount := 0
		mockStripeClient.V1PricesService.On("Create", mock.Anything, mock.MatchedBy(func(params *stripe.PriceCreateParams) bool {
			priceCreatedCount++
			interval := stripe.StringValue(params.Recurring.Interval)

			// Validate each cadence mapping
			switch priceCreatedCount {
			case 1:
				return interval == "month" && params.Recurring.IntervalCount == nil
			case 2:
				return interval == "month" && params.Recurring.IntervalCount != nil &&
					stripe.Int64Value(params.Recurring.IntervalCount) == 3
			case 3:
				return interval == "year" && params.Recurring.IntervalCount == nil
			case 4:
				return interval == "week" && params.Recurring.IntervalCount == nil
			default:
				return false
			}
		})).Return(&stripe.Price{ID: "price_test"}, nil)

		cfg := &pluginConfig.ServiceConfig{
			Stripe: pluginConfig.StripeConfig{
				WebhookSecret: TestWebhookSecret,
				SecretKey:     "test_key",
			},
		}
		gw := NewWithConfig(ctx.Logger(), ctx, cfg, nil, nil, mockBilling, mockPricing, nil)
		gw.stripeClient = mockStripeClient

		result, err := gw.SyncPlan(context.Background(), planInfo)

		assert.NoError(t, err)
		assert.True(t, result.Success)
		assert.Len(t, result.RemotePriceIDs, 4)
		assert.Equal(t, priceCreatedCount, 4)
	})
}

// TestStripeGateway_SyncPlan_UnsupportedCadence tests that unsupported cadences (like "biennially") return error
func TestStripeGateway_SyncPlan_UnsupportedCadence(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)
		mockPricing := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)
		mockStripeClient := &MockStripeClient{
			V1ProductsService:                   &MockProducts{},
			V1PricesService:                     &MockPrices{},
			V1BillingPortalConfigurationsService: &MockBillingPortalConfigurations{},
			BillingPortalSessionsService:        &MockBillingPortalSessions{},
			CustomersService:                    &MockCustomers{},
			SubscriptionsService:                &MockSubscriptions{},
			V1CheckoutSessionsService:           &MockCheckoutSessions{},
		}

		planID := uint(100)
		periodID := uint(200)
		productID := "prod_test"

		planInfo := &pluginCore.PricingPlanInfo{
			ID:          planID,
			Name:        "Unsupported Cadence Plan",
			Description: "Tests unsupported cadence rejection",
			Currency:    "USD",
			PricingVariants: []pluginCore.PricingVariant{
				{
					BillingPeriodID: periodID,
					PriceUSD:        29.99,
					QuotaPlanID:     uint(300),
					Cadence:         "biennially",
				},
			},
			IsActive: true,
			IsPublic: true,
		}

		mockPricing.EXPECT().GetPriceLinesForPlan(mock.Anything, planID).Return([]*billingModels.PriceLinePlan{}, nil)
		mockPricing.EXPECT().GetPricingPlanPeriods(mock.Anything, planID).Return([]*billingModels.PricingPlanPeriod{
			{
				Model:         gorm.Model{ID: periodID},
				PricingPlanID: planID,
				Cadence:       "biennially",
				PriceUSD:      29.99,
				QuotaPlanID:   300,
			},
		}, nil)

		mockStripeClient.V1ProductsService.On("Create", mock.Anything, mock.Anything).Return(&stripe.Product{ID: productID}, nil)

		gw := NewWithConfig(ctx.Logger(), ctx, testConfigWithSecrets(TestWebhookSecret, "test_key"), nil, nil, mockBilling, mockPricing, nil)
		gw.stripeClient = mockStripeClient

		result, err := gw.SyncPlan(context.Background(), planInfo)

		assert.Error(t, err)
		assert.False(t, result.Success)
		assert.Contains(t, err.Error(), "unsupported cadence 'biennially'")
	})
}


func TestStripeGateway_HandleWebhook_InvoicePaid_Success(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockQuota, mockUsers, mockBilling, mockPricing := setupMockServices(ctx)
		mockCredit := core.GetService[*pluginCore.MockCreditService](ctx, pluginCore.CREDIT_SERVICE)

		subscriptionID := TestSubscriptionID
		customerID := TestCustomerID
		invoiceID := "in_test_123"
		pricingPlanPeriodID := uint(200)
		userID := TestUserID

		planID := uint(pricingPlanPeriodID)
		mockSubscriber := &pluginCore.Subscriber{
			UserID:              userID,
			GatewayType:         "stripe",
			ExternalID:          customerID,
			SubscriptionID:      subscriptionID,
			IsActive:            false,
			PricingPlanPeriodID: &planID,
		}

		mockBilling.EXPECT().GetSubscriberBySubscriptionID(mock.Anything, subscriptionID, "stripe").Return(mockSubscriber, nil)
		mockUsers.EXPECT().AccountExists(mock.Anything, userID).Return(true, createTestUser(userID), nil).Maybe()

		mockPricing.EXPECT().GetPricingPlanPeriod(mock.Anything, pricingPlanPeriodID).Return(&billingModels.PricingPlanPeriod{
			Model:         gorm.Model{ID: pricingPlanPeriodID},
			PricingPlanID: 1,
			Cadence:       "monthly",
			PriceUSD:      19.99,
			QuotaPlanID:   100,
		}, nil)

		subscription := createTestSubscriptionWithPeriod("123", "", fmt.Sprintf("%d", pricingPlanPeriodID))
		mockSubService := &MockSubscriptionRetriever{}
		mockSubService.SetupGetSuccess(&subscription)

		mockCredit.EXPECT().ValidateSubscriptionChange(mock.Anything, uint64(userID), pluginCore.ChangeTypeRenewal, mock.Anything).Return(nil)
		mockCredit.EXPECT().IssueUsageCredit(
			mock.Anything,
			uint64(userID),
			pluginCore.TransactionTypeTime,
			mock.Anything,
			invoiceID,
			mock.AnythingOfType("string"),
			uint64(0),
		).Return(nil).Times(1)

		mockCredit.EXPECT().IssueCreditWithIdempotency(
			mock.Anything,
			uint64(userID),
			pluginCore.TransactionTypeCharge,
			mock.Anything,
			pluginCore.ReferenceTypeStripeInvoice,
			invoiceID,
			mock.AnythingOfType("string"),
			uint64(0),
		).Return(nil).Times(1)

		mockCredit.EXPECT().GetUserBalance(mock.Anything, uint64(userID)).Return(decimal.NewFromFloat(19.99), nil).Times(1)

		mockQuota.EXPECT().RemoveUserFromPlan(mock.Anything, mock.Anything).Return(nil).Maybe()
		mockQuota.EXPECT().AssignUserToPlan(mock.Anything, userID, uint(100)).Return(nil).Maybe()
		mockBilling.On("CreateOrUpdateSubscriber", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()

		gw := NewWithConfig(ctx.Logger(), ctx, testConfig(), mockQuota, mockUsers, mockBilling, mockPricing, mockCredit)
		gw.subService = mockSubService

		event := createTestInvoiceEvent(invoiceID, customerID, subscriptionID, 19.99)

		err := gw.handleInvoicePaid(ctx, event)
		assert.NoError(t, err)
	})
}

func TestStripeGateway_HandleWebhook_InvoicePaid_InsufficientBalance(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockQuota, mockUsers, mockBilling, mockPricing := setupMockServices(ctx)
		mockCredit := core.GetService[*pluginCore.MockCreditService](ctx, pluginCore.CREDIT_SERVICE)

		subscriptionID := TestSubscriptionID
		customerID := TestCustomerID
		invoiceID := "in_test_insufficient"
		userID := TestUserID

		planID := uint(200)
		mockSubscriber := &pluginCore.Subscriber{
			UserID:              userID,
			GatewayType:         "stripe",
			ExternalID:          customerID,
			SubscriptionID:      subscriptionID,
			IsActive:            false,
			PricingPlanPeriodID: &planID,
		}

		mockBilling.EXPECT().GetSubscriberBySubscriptionID(mock.Anything, subscriptionID, "stripe").Return(mockSubscriber, nil)
		mockUsers.EXPECT().AccountExists(mock.Anything, userID).Return(true, createTestUser(userID), nil).Maybe()

		subscription := createTestSubscription("123", "200")
		mockSubService := &MockSubscriptionRetriever{}
		mockSubService.SetupGetSuccess(&subscription)

		mockCredit.EXPECT().ValidateSubscriptionChange(mock.Anything, uint64(userID), pluginCore.ChangeTypeRenewal, mock.Anything).Return(nil)

		mockCredit.EXPECT().IssueCreditWithIdempotency(
			mock.Anything,
			uint64(userID),
			pluginCore.TransactionTypeCharge,
			mock.Anything,
			pluginCore.ReferenceTypeStripeInvoice,
			invoiceID,
			mock.AnythingOfType("string"),
			uint64(0),
		).Return(nil).Times(1)

		mockCredit.EXPECT().GetUserBalance(mock.Anything, uint64(userID)).Return(decimal.NewFromFloat(-5.00), nil).Times(1)

		gw := NewWithConfig(ctx.Logger(), ctx, testConfig(), mockQuota, mockUsers, mockBilling, mockPricing, mockCredit)
		gw.subService = mockSubService

		event := createTestInvoiceEvent(invoiceID, customerID, subscriptionID, 19.99)

		err := gw.handleInvoicePaid(ctx, event)
		assert.NoError(t, err)
		mockBilling.AssertNotCalled(t, "CreateOrUpdateSubscriber", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, true, mock.Anything, mock.Anything)
	})
}

func TestStripeGateway_HandleWebhook_InvoicePaid_RaceWithCheckout(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockQuota, mockUsers, mockBilling, mockPricing := setupMockServices(ctx)
		mockCredit := core.GetService[*pluginCore.MockCreditService](ctx, pluginCore.CREDIT_SERVICE)

		subscriptionID := TestSubscriptionID
		customerID := TestCustomerID
		invoiceID := "in_test_race"
		pricingPlanPeriodID := uint(200)
		userID := TestUserID

		planID := uint(pricingPlanPeriodID)
		mockSubscriber := &pluginCore.Subscriber{
			UserID:              userID,
			GatewayType:         "stripe",
			ExternalID:          customerID,
			SubscriptionID:      subscriptionID,
			IsActive:            false,
			PricingPlanPeriodID: &planID,
		}

		// First call returns nil (race condition), second call returns subscriber (after creation)
		mockBilling.On("GetSubscriberBySubscriptionID", mock.Anything, subscriptionID, "stripe").
			Return(nil, nil).Once()
		mockBilling.On("GetSubscriberBySubscriptionID", mock.Anything, subscriptionID, "stripe").
			Return(mockSubscriber, nil).Once()

		// Fallback 1: GetSubscriberByExternalID also returns nil (no subscriber yet)
		mockBilling.On("GetSubscriberByExternalID", mock.Anything, customerID, "stripe").Return(nil, nil).Maybe()

		// Fallback 2: GetActiveSubscription returns nil (no active subscription)
		mockBilling.On("GetActiveSubscription", mock.Anything, userID).Return(nil, nil).Maybe()

		// Fallback 3: CreateOrUpdateSubscriber is called to create the pending subscriber
		mockBilling.On("CreateOrUpdateSubscriber", mock.Anything, userID, "stripe", customerID, subscriptionID, false, mock.Anything).Return(nil).Maybe()

		mockUsers.EXPECT().AccountExists(mock.Anything, userID).Return(true, createTestUser(userID), nil).Maybe()

		mockPricing.EXPECT().GetPricingPlanPeriod(mock.Anything, pricingPlanPeriodID).Return(&billingModels.PricingPlanPeriod{
			Model:         gorm.Model{ID: pricingPlanPeriodID},
			PricingPlanID: 1,
			Cadence:       "monthly",
			PriceUSD:      19.99,
			QuotaPlanID:   100,
		}, nil)

		subscription := createTestSubscriptionWithPeriod("123", "", fmt.Sprintf("%d", pricingPlanPeriodID))
		mockSubService := &MockSubscriptionRetriever{}
		mockSubService.SetupGetSuccess(&subscription)

		mockCredit.EXPECT().ValidateSubscriptionChange(mock.Anything, uint64(userID), pluginCore.ChangeTypeRenewal, mock.Anything).Return(nil)
		mockCredit.EXPECT().IssueCreditWithIdempotency(
			mock.Anything,
			uint64(userID),
			pluginCore.TransactionTypeCharge,
			mock.Anything,
			pluginCore.ReferenceTypeStripeInvoice,
			invoiceID,
			mock.AnythingOfType("string"),
			uint64(0),
		).Return(nil).Times(1)

		mockCredit.EXPECT().GetUserBalance(mock.Anything, uint64(userID)).Return(decimal.NewFromFloat(19.99), nil).Times(1)

		mockCredit.EXPECT().IssueUsageCredit(
			mock.Anything,
			uint64(userID),
			pluginCore.TransactionTypeTime,
			mock.Anything,
			invoiceID,
			mock.AnythingOfType("string"),
			uint64(0),
		).Return(nil).Times(1)

		mockQuota.EXPECT().AssignUserToPlan(mock.Anything, userID, uint(100)).Return(nil).Maybe()
		mockBilling.On("CreateOrUpdateSubscriber", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()

		gw := NewWithConfig(ctx.Logger(), ctx, testConfig(), mockQuota, mockUsers, mockBilling, mockPricing, mockCredit)
		gw.subService = mockSubService

		event := createTestInvoiceEvent(invoiceID, customerID, subscriptionID, 19.99)

		err := gw.handleInvoicePaid(ctx, event)
		assert.NoError(t, err)
	})
}

func TestStripeGateway_HandleWebhook_InvoicePaid_RaceWithCheckout_ActiveSubFallback(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockQuota, mockUsers, mockBilling, mockPricing := setupMockServices(ctx)
		mockCredit := core.GetService[*pluginCore.MockCreditService](ctx, pluginCore.CREDIT_SERVICE)

		subscriptionID := TestSubscriptionID
		customerID := TestCustomerID
		invoiceID := "in_test_race_active"
		pricingPlanPeriodID := uint(200)
		userID := TestUserID

		planID := uint(pricingPlanPeriodID)

		// Simulate race: GetSubscriberBySubscriptionID returns nil
		mockBilling.EXPECT().GetSubscriberBySubscriptionID(mock.Anything, subscriptionID, "stripe").Return(nil, nil)

		// Fallback 1: GetSubscriberByExternalID also returns nil
		mockBilling.EXPECT().GetSubscriberByExternalID(mock.Anything, customerID, "stripe").Return(nil, nil)

		// Fallback 2: GetActiveSubscription returns an existing active subscriber
		activeSubscriber := &pluginCore.Subscriber{
			UserID:              userID,
			GatewayType:         "stripe",
			ExternalID:          customerID,
			SubscriptionID:      subscriptionID,
			IsActive:            true,
			PricingPlanPeriodID: &planID,
		}
		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, userID).Return(activeSubscriber, nil)

		mockUsers.EXPECT().AccountExists(mock.Anything, userID).Return(true, createTestUser(userID), nil).Maybe()

		mockPricing.EXPECT().GetPricingPlanPeriod(mock.Anything, pricingPlanPeriodID).Return(&billingModels.PricingPlanPeriod{
			Model:         gorm.Model{ID: pricingPlanPeriodID},
			PricingPlanID: 1,
			Cadence:       "monthly",
			PriceUSD:      19.99,
			QuotaPlanID:   100,
		}, nil)

		subscription := createTestSubscriptionWithPeriod("123", "", fmt.Sprintf("%d", pricingPlanPeriodID))
		mockSubService := &MockSubscriptionRetriever{}
		mockSubService.SetupGetSuccess(&subscription)

		mockCredit.EXPECT().ValidateSubscriptionChange(mock.Anything, uint64(userID), pluginCore.ChangeTypeRenewal, mock.Anything).Return(nil)
		mockCredit.EXPECT().IssueCreditWithIdempotency(
			mock.Anything,
			uint64(userID),
			pluginCore.TransactionTypeCharge,
			mock.Anything,
			pluginCore.ReferenceTypeStripeInvoice,
			invoiceID,
			mock.AnythingOfType("string"),
			uint64(0),
		).Return(nil).Times(1)

		mockCredit.EXPECT().GetUserBalance(mock.Anything, uint64(userID)).Return(decimal.NewFromFloat(19.99), nil).Times(1)

		mockCredit.EXPECT().IssueUsageCredit(
			mock.Anything,
			uint64(userID),
			pluginCore.TransactionTypeTime,
			mock.Anything,
			invoiceID,
			mock.AnythingOfType("string"),
			uint64(0),
		).Return(nil).Times(1)

		mockQuota.EXPECT().AssignUserToPlan(mock.Anything, userID, uint(100)).Return(nil).Maybe()
		mockBilling.On("CreateOrUpdateSubscriber", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()

		gw := NewWithConfig(ctx.Logger(), ctx, testConfig(), mockQuota, mockUsers, mockBilling, mockPricing, mockCredit)
		gw.subService = mockSubService

		event := createTestInvoiceEvent(invoiceID, customerID, subscriptionID, 19.99)

		err := gw.handleInvoicePaid(ctx, event)
		assert.NoError(t, err)
	})
}

func TestStripeGateway_HandleWebhook_InvoicePaid_RaceWithCheckout_ExternalIDFallback(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockQuota, mockUsers, mockBilling, mockPricing := setupMockServices(ctx)
		mockCredit := core.GetService[*pluginCore.MockCreditService](ctx, pluginCore.CREDIT_SERVICE)

		subscriptionID := TestSubscriptionID
		customerID := TestCustomerID
		invoiceID := "in_test_race_ext"
		pricingPlanPeriodID := uint(200)
		userID := TestUserID

		planID := uint(pricingPlanPeriodID)
		mockSubscriber := &pluginCore.Subscriber{
			UserID:              userID,
			GatewayType:         "stripe",
			ExternalID:          customerID,
			SubscriptionID:      "sub_other",
			IsActive:            false,
			PricingPlanPeriodID: &planID,
		}

		// Simulate race: GetSubscriberBySubscriptionID returns nil
		mockBilling.EXPECT().GetSubscriberBySubscriptionID(mock.Anything, subscriptionID, "stripe").Return(nil, nil)

		// Fallback 1: GetSubscriberByExternalID finds the subscriber
		mockBilling.EXPECT().GetSubscriberByExternalID(mock.Anything, customerID, "stripe").Return(mockSubscriber, nil)

		mockUsers.EXPECT().AccountExists(mock.Anything, userID).Return(true, createTestUser(userID), nil).Maybe()

		mockPricing.EXPECT().GetPricingPlanPeriod(mock.Anything, pricingPlanPeriodID).Return(&billingModels.PricingPlanPeriod{
			Model:         gorm.Model{ID: pricingPlanPeriodID},
			PricingPlanID: 1,
			Cadence:       "monthly",
			PriceUSD:      19.99,
			QuotaPlanID:   100,
		}, nil)

		subscription := createTestSubscriptionWithPeriod("123", "", fmt.Sprintf("%d", pricingPlanPeriodID))
		mockSubService := &MockSubscriptionRetriever{}
		mockSubService.SetupGetSuccess(&subscription)

		mockCredit.EXPECT().ValidateSubscriptionChange(mock.Anything, uint64(userID), pluginCore.ChangeTypeRenewal, mock.Anything).Return(nil)
		mockCredit.EXPECT().IssueCreditWithIdempotency(
			mock.Anything,
			uint64(userID),
			pluginCore.TransactionTypeCharge,
			mock.Anything,
			pluginCore.ReferenceTypeStripeInvoice,
			invoiceID,
			mock.AnythingOfType("string"),
			uint64(0),
		).Return(nil).Times(1)

		mockCredit.EXPECT().GetUserBalance(mock.Anything, uint64(userID)).Return(decimal.NewFromFloat(19.99), nil).Times(1)

		mockCredit.EXPECT().IssueUsageCredit(
			mock.Anything,
			uint64(userID),
			pluginCore.TransactionTypeTime,
			mock.Anything,
			invoiceID,
			mock.AnythingOfType("string"),
			uint64(0),
		).Return(nil).Times(1)

		mockQuota.EXPECT().AssignUserToPlan(mock.Anything, userID, uint(100)).Return(nil).Maybe()
		mockBilling.On("CreateOrUpdateSubscriber", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()

		gw := NewWithConfig(ctx.Logger(), ctx, testConfig(), mockQuota, mockUsers, mockBilling, mockPricing, mockCredit)
		gw.subService = mockSubService

		event := createTestInvoiceEvent(invoiceID, customerID, subscriptionID, 19.99)

		err := gw.handleInvoicePaid(ctx, event)
		assert.NoError(t, err)
	})
}

func createTestInvoiceEvent(invoiceID string, customerID string, subscriptionID string, amount float64) stripe.Event {
	lineItemData := fmt.Sprintf(`{
		"id": "il_test_123",
		"period": {"start": 1704067200, "end": 1706745600},
		"amount": %d,
		"quantity": 1,
		"price": {
			"id": "price_200",
			"unit_amount": %d,
			"currency": "usd",
			"recurring": {"interval": "month", "usage_type": "licensed"},
			"product": "200"
		},
		"parent": {
			"type": "subscription_item_details",
			"subscription_item_details": {
				"subscription": "%s",
				"subscription_item": "si_test",
				"proration": false
			}
		}
	}`, int(amount*100), int(amount*100), subscriptionID)

	invoiceData := fmt.Sprintf(`{
		"id": "%s",
		"object": "invoice",
		"customer": {"id": "%s"},
		"status": "paid",
		"amount_paid": %d,
		"currency": "usd",
		"lines": {
			"data": [%s]
		},
		"total": %d,
		"subtotal": %d,
		"parent": {
			"type": "subscription_details",
			"subscription_details": {
				"subscription": {"id": "%s"}
			}
		}
	}`, invoiceID, customerID, int(amount*100), lineItemData, int(amount*100), int(amount*100), subscriptionID)
	
	return createTestEvent("invoice.paid", []byte(invoiceData))
}

func TestStripeGateway_GetManagementURL_Cancel_DeepLink(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)
		mockStripeClient := &MockStripeClient{}

		userID := uint(123)
		mockSubscriber := &pluginCore.Subscriber{
			UserID:         userID,
			GatewayType:    "stripe",
			ExternalID:     "cus_123",
			SubscriptionID: "sub_abc",
			IsActive:       true,
		}
		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, userID).Return(mockSubscriber, nil)

		mockSession := &stripe.BillingPortalSession{
			URL: "https://billing.stripe.com/p/session_cancel_deep",
		}
		mockStripeClient.BillingPortalSessionsService = &MockBillingPortalSessions{}
		mockStripeClient.BillingPortalSessionsService.
			On("Create", mock.Anything, mock.AnythingOfType("*stripe.BillingPortalSessionCreateParams")).
			Run(func(args mock.Arguments) {
				params := args.Get(1).(*stripe.BillingPortalSessionCreateParams)
				assert.NotNil(t, params.FlowData, "flow_data should be set for cancel operation")
				assert.Equal(t, string(stripe.BillingPortalSessionFlowTypeSubscriptionCancel), stripe.StringValue(params.FlowData.Type), "flow type should be subscription_cancel")
				assert.NotNil(t, params.FlowData.SubscriptionCancel, "subscription_cancel config should be set")
				assert.Equal(t, "sub_abc", stripe.StringValue(params.FlowData.SubscriptionCancel.Subscription), "subscription ID should match")
			}).
			Return(mockSession, nil)

		gw := NewWithConfig(ctx.Logger(), ctx, testConfigWithSecrets(TestWebhookSecret, "test_api_key"), nil, nil, mockBilling, nil, nil)
		gw.stripeClient = mockStripeClient

		result, err := gw.GetManagementURL(context.Background(), userID, pluginCore.OperationCancel)

		assert.NoError(t, err)
		assert.Equal(t, pluginCore.ActionRedirect, result.Action)
		assert.Equal(t, "https://billing.stripe.com/p/session_cancel_deep", result.URL)
	})
}

func TestStripeGateway_GetManagementURL_ChangePlan_DeepLink(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)
		mockStripeClient := &MockStripeClient{}

		userID := uint(456)
		mockSubscriber := &pluginCore.Subscriber{
			UserID:         userID,
			GatewayType:    "stripe",
			ExternalID:     "cus_456",
			SubscriptionID: "sub_xyz",
			IsActive:       true,
		}
		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, userID).Return(mockSubscriber, nil)

		mockSession := &stripe.BillingPortalSession{
			URL: "https://billing.stripe.com/p/session_update_deep",
		}
		mockStripeClient.BillingPortalSessionsService = &MockBillingPortalSessions{}
		mockStripeClient.BillingPortalSessionsService.
			On("Create", mock.Anything, mock.AnythingOfType("*stripe.BillingPortalSessionCreateParams")).
			Run(func(args mock.Arguments) {
				params := args.Get(1).(*stripe.BillingPortalSessionCreateParams)
				assert.NotNil(t, params.FlowData, "flow_data should be set for change_plan operation")
				assert.Equal(t, string(stripe.BillingPortalSessionFlowTypeSubscriptionUpdate), stripe.StringValue(params.FlowData.Type), "flow type should be subscription_update")
				assert.NotNil(t, params.FlowData.SubscriptionUpdate, "subscription_update config should be set")
				assert.Equal(t, "sub_xyz", stripe.StringValue(params.FlowData.SubscriptionUpdate.Subscription), "subscription ID should match")
			}).
			Return(mockSession, nil)

		gw := NewWithConfig(ctx.Logger(), ctx, testConfigWithSecrets(TestWebhookSecret, "test_api_key"), nil, nil, mockBilling, nil, nil)
		gw.stripeClient = mockStripeClient

		result, err := gw.GetManagementURL(context.Background(), userID, pluginCore.OperationChangePlan)

		assert.NoError(t, err)
		assert.Equal(t, pluginCore.ActionRedirect, result.Action)
		assert.Equal(t, "https://billing.stripe.com/p/session_update_deep", result.URL)
	})
}

func TestStripeGateway_GetManagementURL_Pause_GenericPortal(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)
		mockStripeClient := &MockStripeClient{}

		userID := uint(789)
		mockSubscriber := &pluginCore.Subscriber{
			UserID:         userID,
			GatewayType:    "stripe",
			ExternalID:     "cus_789",
			SubscriptionID: "sub_pause",
			IsActive:       true,
		}
		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, userID).Return(mockSubscriber, nil)

		mockSession := &stripe.BillingPortalSession{
			URL: "https://billing.stripe.com/p/session_generic",
		}
		mockStripeClient.BillingPortalSessionsService = &MockBillingPortalSessions{}
		mockStripeClient.BillingPortalSessionsService.
			On("Create", mock.Anything, mock.AnythingOfType("*stripe.BillingPortalSessionCreateParams")).
			Run(func(args mock.Arguments) {
				params := args.Get(1).(*stripe.BillingPortalSessionCreateParams)
				assert.Nil(t, params.FlowData, "pause has no Stripe deep link type; flow_data should be nil")
			}).
			Return(mockSession, nil)

		gw := NewWithConfig(ctx.Logger(), ctx, testConfigWithSecrets(TestWebhookSecret, "test_api_key"), nil, nil, mockBilling, nil, nil)
		gw.stripeClient = mockStripeClient

		result, err := gw.GetManagementURL(context.Background(), userID, pluginCore.OperationPause)

		assert.NoError(t, err)
		assert.Equal(t, pluginCore.ActionRedirect, result.Action)
		assert.Equal(t, "https://billing.stripe.com/p/session_generic", result.URL)
	})
}

func TestStripeGateway_GetManagementURL_Resume_GenericPortal(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)
		mockStripeClient := &MockStripeClient{}

		userID := uint(999)
		mockSubscriber := &pluginCore.Subscriber{
			UserID:         userID,
			GatewayType:    "stripe",
			ExternalID:     "cus_999",
			SubscriptionID: "sub_resume",
			IsActive:       true,
		}
		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, userID).Return(mockSubscriber, nil)

		mockSession := &stripe.BillingPortalSession{
			URL: "https://billing.stripe.com/p/session_generic",
		}
		mockStripeClient.BillingPortalSessionsService = &MockBillingPortalSessions{}
		mockStripeClient.BillingPortalSessionsService.
			On("Create", mock.Anything, mock.AnythingOfType("*stripe.BillingPortalSessionCreateParams")).
			Run(func(args mock.Arguments) {
				params := args.Get(1).(*stripe.BillingPortalSessionCreateParams)
				assert.Nil(t, params.FlowData, "resume has no Stripe deep link type; flow_data should be nil")
			}).
			Return(mockSession, nil)

		gw := NewWithConfig(ctx.Logger(), ctx, testConfigWithSecrets(TestWebhookSecret, "test_api_key"), nil, nil, mockBilling, nil, nil)
		gw.stripeClient = mockStripeClient

		result, err := gw.GetManagementURL(context.Background(), userID, pluginCore.OperationResume)

		assert.NoError(t, err)
		assert.Equal(t, pluginCore.ActionRedirect, result.Action)
		assert.Equal(t, "https://billing.stripe.com/p/session_generic", result.URL)
	})
}

func TestStripeGateway_GetLogo_EmbeddedFile(t *testing.T) {
	ctx, _ := coreTesting.NewTestContext(t)
	gw := NewWithConfig(ctx.Logger(), ctx, testConfig(), nil, nil, nil, nil, nil)

	// This test verifies that the embedded logo file can be read
	// Will fail if the file is not named correctly (should be "stripe.svg" not "logo.svg")
	logo, err := gw.GetLogo(context.Background())
	assert.NoError(t, err, "GetLogo should not return an error - check that assets/stripe.svg exists")
	assert.NotNil(t, logo)
	assert.True(t, len(logo) > 0, "logo should contain valid SVG data")
}
