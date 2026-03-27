package stripe

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stripe/stripe-go/v83"
	"github.com/stripe/stripe-go/v83/webhook"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	pluginConfig "go.lumeweb.com/portal-plugin-billing/internal/config"
	quotaCore "go.lumeweb.com/portal-plugin-quota/core"
	"go.lumeweb.com/portal/core"
	coreTesting "go.lumeweb.com/portal/core/testing"
	billingModels "go.lumeweb.com/portal-plugin-billing/internal/db/models"
	portalModels "go.lumeweb.com/portal/db/models"
	"gorm.io/gorm"
)

const (
	// StripeAPIVersion is the API version that matches the stripe-go library version
	StripeAPIVersion = "2025-09-30.clover"

	// Test constants for commonly used values
	TestUserID         = uint(123)
	TestCustomerID     = "cus_123"
	TestSubscriptionID = "sub_123"
	TestPlanID         = uint(1)
	TestWebhookSecret  = "wh_test_secret"
)

func TestMain(m *testing.M) {
	coreTesting.WithOptions(m,
		coreTesting.WithMockServiceFactory(quotaCore.QUOTA_SERVICE, quotaCore.NewMockQuotaService, &quotaCore.QuotaConfig{}),
		coreTesting.WithMockServiceFactory(pluginCore.BILLING_SERVICE, pluginCore.NewMockBillingService, &pluginConfig.ServiceConfig{}),
		coreTesting.WithMockServiceFactory(pluginCore.PRICING_SERVICE, pluginCore.NewMockPricingService, coreTesting.NewConfigBuilder().Build()),
	)
}

func TestStripeGateway_ID(t *testing.T) {
	ctx, _ := coreTesting.NewTestContext(t)
	gw := New(ctx.Logger(), TestWebhookSecret, "", nil, nil, nil, nil)
	assert.Equal(t, "stripe", gw.ID(context.Background()))
}

func TestStripeGateway_SignatureHeader(t *testing.T) {
	ctx, _ := coreTesting.NewTestContext(t)
	gw := New(ctx.Logger(), TestWebhookSecret, "", nil, nil, nil, nil)
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
			gw := New(ctx.Logger(), tt.secret, "", nil, nil, nil, nil)

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

	if planID != "" {
		subscription.Items = &stripe.SubscriptionItemList{
			Data: []*stripe.SubscriptionItem{
				{
					Price: &stripe.Price{
						ID: "price_123",
						Product: &stripe.Product{
							ID: "prod_123",
							Metadata: map[string]string{
								PlanIDMetadataKey: planID,
							},
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
func setupSubscriptionActivationMocks(mockQuota *quotaCore.MockQuotaService, mockUsers *coreTesting.MockUserService, mockBilling *pluginCore.MockBillingService, mockPricing *pluginCore.MockPricingService, userID uint, pricingPlanID, quotaPlanID uint) {
	mockUsers.EXPECT().AccountExists(mock.Anything, userID).Return(true, createTestUser(userID), nil)
	
	// Mock pricing service to return PricingPlan with QuotaPlanID
	pricingPlan := &billingModels.PricingPlan{
		Model:      gorm.Model{ID: pricingPlanID},
		QuotaPlanID: &quotaPlanID,
		Name:       "Test Plan",
		Description: "Test Description",
	}
	mockPricing.EXPECT().GetPricingPlan(mock.Anything, pricingPlanID).Return(pricingPlan, nil)
	
	// Mock quota service calls with QuotaPlanID (not PricingPlanID)
	mockQuota.EXPECT().GetQuotaPlan(mock.Anything, quotaPlanID).Return(&quotaCore.QuotaPlan{}, nil)
	mockQuota.EXPECT().AssignUserToPlan(mock.Anything, userID, quotaPlanID).Return(nil)
	
	// Billing service still tracks with PricingPlanID
	mockBilling.EXPECT().CreateOrUpdateSubscriber(mock.Anything, userID, "stripe", TestCustomerID, true, mock.AnythingOfType("*uint")).Return(nil)
}

// Helper function to setup mocks for subscription deactivation scenarios
func setupSubscriptionDeactivationMocks(mockQuota *quotaCore.MockQuotaService, mockUsers *coreTesting.MockUserService, mockBilling *pluginCore.MockBillingService, userID uint) {
	mockUsers.EXPECT().AccountExists(mock.Anything, userID).Return(true, createTestUser(userID), nil)
	mockQuota.EXPECT().RemoveUserFromPlan(mock.Anything, userID).Return(nil)
	mockBilling.EXPECT().DeactivateSubscriber(mock.Anything, userID, "stripe").Return(nil)
}

// Helper function to run a subscription activation test scenario
func runSubscriptionActivationTest(t *testing.T, eventType string, pricingPlanID, quotaPlanID uint) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockQuota, mockUsers, mockBilling, mockPricing := setupMockServices(ctx)
		mockSubService := &MockSubscriptionRetriever{}

		subscription := createTestSubscription("123", strconv.FormatUint(uint64(pricingPlanID), 10))
		rawData, _ := json.Marshal(subscription)
		event := createTestEvent(eventType, rawData)
		payload, _ := json.Marshal(event)

		mockSubService.SetupGetSuccess(&subscription)
		setupSubscriptionActivationMocks(mockQuota, mockUsers, mockBilling, mockPricing, TestUserID, pricingPlanID, quotaPlanID)

		gw := New(ctx.Logger(), TestWebhookSecret, "", mockQuota, mockUsers, mockBilling, mockPricing)
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

		gw := New(ctx.Logger(), TestWebhookSecret, "", mockQuota, mockUsers, mockBilling, nil)
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

	gw := New(ctx.Logger(), TestWebhookSecret, "", nil, nil, nil, nil)
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
			gw := New(ctx.Logger(), TestWebhookSecret, "", nil, nil, nil, nil)

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
			gw := New(ctx.Logger(), TestWebhookSecret, "", nil, nil, nil, nil)

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
	gw := New(ctx.Logger(), secret, "", nil, nil, nil, nil)
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

		gw := New(ctx.Logger(), TestWebhookSecret, "", mockQuota, mockUsers, nil, nil)
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

		gw := New(ctx.Logger(), TestWebhookSecret, "", mockQuota, mockUsers, mockBilling, nil)
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

		gw := New(ctx.Logger(), TestWebhookSecret, "", mockQuota, mockUsers, mockBilling, nil)
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

		gw := New(ctx.Logger(), TestWebhookSecret, "", mockQuota, mockUsers, mockBilling, nil)
		gw.subService = mockSubService
		err := gw.HandleWebhook(context.Background(), payload)

		// Should not make any changes for cancellation requests
		assert.NoError(t, err)

		// Verify that no quota or billing operations were called
		mockQuota.AssertNotCalled(t, "AssignUserToPlan")
		mockQuota.AssertNotCalled(t, "RemoveUserFromPlan")
		mockBilling.AssertNotCalled(t, "CreateOrUpdateSubscriber")
		mockBilling.AssertNotCalled(t, "DeactivateSubscriber")
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

		gw := New(ctx.Logger(), TestWebhookSecret, "", mockQuota, mockUsers, mockBilling, nil)
		gw.subService = mockSubService
		err := gw.HandleWebhook(context.Background(), payload)

		// Should not return error but should proceed with deactivation
		assert.NoError(t, err)
	})
}

func TestStripeGateway_HandleWebhook_SubscriptionPaused(t *testing.T) {
	runSubscriptionDeactivationTest(t, EventTypeSubscriptionPaused)
}

func TestStripeGateway_HandleWebhook_SubscriptionResumed(t *testing.T) {
	quotaPlanID := uint(20) // Different ID to test the mapping
	runSubscriptionActivationTest(t, EventTypeSubscriptionResumed, TestPlanID, quotaPlanID)
}

func TestStripeGateway_GetCustomerPortalURL_Success(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)
		mockStripeClient := &MockStripeClient{}

		// Mock active subscription
		planID := uint(42)
		mockSubscriber := &pluginCore.Subscriber{
			UserID:      123,
			GatewayType: "stripe",
			GatewayID:   "cus_123",
			IsActive:    true,
			PlanID:      &planID,
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

		gw := New(ctx.Logger(), TestWebhookSecret, "test_api_key", nil, nil, mockBilling, nil)
		gw.stripeClient = mockStripeClient

		url, err := gw.GetCustomerPortalURL(context.Background(), 123, "https://example.com/return")

		assert.NoError(t, err)
		assert.Equal(t, "https://billing.stripe.com/p/session/test_session_id", url)
	})
}

func TestStripeGateway_HandleWebhook_CheckoutSessionCompleted(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockQuota, mockUsers, mockBilling, mockPricing := setupMockServices(ctx)
		mockSubService := &MockSubscriptionRetriever{}

		pricingPlanID := uint(3)
		quotaPlanID := uint(30) // Different ID to test the mapping
		userID := uint(456)
		customerID := "cus_456"

		checkoutSession := stripe.CheckoutSession{
			ID:                "cs_test_123",
			ClientReferenceID: strconv.FormatUint(uint64(userID), 10),
			Subscription:      &stripe.Subscription{ID: "sub_456"},
			Mode:              stripe.CheckoutSessionModeSubscription,
		}
		rawData, _ := json.Marshal(checkoutSession)
		event := createTestEvent(EventTypeCheckoutSessionCompleted, rawData)
		payload, _ := json.Marshal(event)

		// Setup subscription mock for the expanded subscription call
		testSubscription := &stripe.Subscription{
			ID: "sub_456",
			Customer: &stripe.Customer{
				ID: customerID,
			},
			Items: &stripe.SubscriptionItemList{
				Data: []*stripe.SubscriptionItem{
					{
						Price: &stripe.Price{
							ID: "price_456",
							Product: &stripe.Product{
								ID: "prod_456",
								Metadata: map[string]string{
									PlanIDMetadataKey: strconv.FormatUint(uint64(pricingPlanID), 10),
								},
							},
						},
					},
				},
			},
		}
		mockSubService.On("Get", mock.Anything, "sub_456", mock.MatchedBy(func(params *stripe.SubscriptionRetrieveParams) bool {
			if params == nil {
				return false
			}
			for _, expand := range params.Expand {
				if *expand == "items.data.price.product" {
					return true
				}
			}
			return false
		})).Return(testSubscription, nil)

		// Setup mocks
		mockUsers.EXPECT().AccountExists(mock.Anything, userID).Return(true, createTestUser(userID), nil)
		
		// Mock pricing service to return PricingPlan with QuotaPlanID
		pricingPlan := &billingModels.PricingPlan{
			Model:      gorm.Model{ID: pricingPlanID},
			QuotaPlanID: &quotaPlanID,
			Name:       "Test Plan",
			Description: "Test Description",
		}
		mockPricing.EXPECT().GetPricingPlan(mock.Anything, pricingPlanID).Return(pricingPlan, nil)
		
		// Mock quota service calls with QuotaPlanID (not PricingPlanID)
		mockQuota.EXPECT().GetQuotaPlan(mock.Anything, quotaPlanID).Return(&quotaCore.QuotaPlan{}, nil)
		mockQuota.EXPECT().AssignUserToPlan(mock.Anything, userID, quotaPlanID).Return(nil)
		
		// Billing service still tracks with PricingPlanID
		mockBilling.EXPECT().CreateOrUpdateSubscriber(mock.Anything, userID, "stripe", customerID, true, mock.AnythingOfType("*uint")).Return(nil)

		gw := New(ctx.Logger(), TestWebhookSecret, "test_api_key", mockQuota, mockUsers, mockBilling, mockPricing)
		gw.subService = mockSubService

		err := gw.HandleWebhook(context.Background(), payload)

		assert.NoError(t, err)
	})
}

func TestStripeGateway_GetCustomerPortalURL_NoActiveSubscription(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)

		// Mock no active subscription
		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, uint(123)).Return((*pluginCore.Subscriber)(nil), nil)

		gw := New(ctx.Logger(), TestWebhookSecret, "", nil, nil, mockBilling, nil)

		url, err := gw.GetCustomerPortalURL(context.Background(), 123, "https://example.com/return")

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no active stripe subscription found")
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
		gw := New(ctx.Logger(), TestWebhookSecret, "", nil, mockUser, mockBilling, nil)
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
		mockSubscriber := &pluginCore.Subscriber{
			UserID:      789,
			GatewayType: "stripe",
			GatewayID:   "cus_test_456",
			IsActive:    true,
		}
		mockBilling.EXPECT().GetSubscriberByGatewayID(mock.Anything, "cus_test_456", "stripe").Return(mockSubscriber, nil)

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
		mockBilling.EXPECT().GetSubscriberByGatewayID(mock.Anything, "cus_test_789", "stripe").Return(nil, nil)

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
			UserID: 123, GatewayType: "stripe", GatewayID: "cus_123", IsActive: true, PlanID: &planID,
		}
		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, uint(123)).Return(mockSubscriber, nil)

		mockStripeClient.BillingPortalSessionsService = &MockBillingPortalSessions{}
		mockStripeClient.BillingPortalSessionsService.
			On("Create", mock.Anything, mock.AnythingOfType("*stripe.BillingPortalSessionCreateParams")).
			Return((*stripe.BillingPortalSession)(nil), assert.AnError)

		gw := New(ctx.Logger(), TestWebhookSecret, "test_api_key", nil, nil, mockBilling, nil)
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
			UserID:      123,
			GatewayType: "paypal", // Different gateway
			GatewayID:   "cus_123",
			IsActive:    true,
			PlanID:      &planID,
		}
		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, uint(123)).Return(mockSubscriber, nil)

		gw := New(ctx.Logger(), TestWebhookSecret, "", nil, nil, mockBilling, nil)

		url, err := gw.GetCustomerPortalURL(context.Background(), 123, "https://example.com/return")

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no active stripe subscription found")
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

		gw := New(ctx.Logger(), TestWebhookSecret, "", nil, nil, mockBilling, nil)

		url, err := gw.GetCustomerPortalURL(context.Background(), 123, "https://example.com/return")

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get active subscription")
		assert.Empty(t, url)
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
		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, uint(123)).Return(mockSubscriber, nil)

		gw := New(ctx.Logger(), TestWebhookSecret, "", nil, nil, mockBilling, nil)

		url, err := gw.GetCustomerPortalURL(context.Background(), 123, "https://example.com/return")

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid GatewayID: must be a Stripe customer ID starting with 'cus_'")
		assert.Empty(t, url)
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
		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, uint(123)).Return(mockSubscriber, nil)

		gw := New(ctx.Logger(), TestWebhookSecret, "", nil, nil, mockBilling, nil)

		url, err := gw.GetCustomerPortalURL(context.Background(), 123, "https://example.com/return")

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "subscriber GatewayID is empty")
		assert.Empty(t, url)
	})
}
