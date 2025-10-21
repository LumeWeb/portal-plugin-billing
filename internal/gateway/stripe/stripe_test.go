package stripe

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
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
	
	// Test constants for commonly used values
	TestUserID        = uint(123)
	TestCustomerID    = "cus_123"
	TestSubscriptionID = "sub_123"
	TestPlanID        = uint(1)
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
					Plan: &stripe.Plan{
						ID: "plan_123",
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

// Helper function to setup common mock services
func setupMockServices(ctx coreTesting.TestContext) (*quotaCore.MockQuotaService, *coreMocks.MockUserService, *pluginCore.MockBillingService) {
	mockQuota := core.GetService[*quotaCore.MockQuotaService](ctx, quotaCore.QUOTA_SERVICE)
	mockUsers := core.GetService[*coreMocks.MockUserService](ctx, core.USER_SERVICE)
	mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)
	return mockQuota, mockUsers, mockBilling
}

// Helper function to setup mocks for subscription activation scenarios
func setupSubscriptionActivationMocks(mockQuota *quotaCore.MockQuotaService, mockUsers *coreMocks.MockUserService, mockBilling *pluginCore.MockBillingService, userID uint, planID uint) {
	mockUsers.On("AccountExists", userID).Return(true, createTestUser(userID), nil)
	mockQuota.On("GetQuotaPlan", planID).Return(&quotaCore.QuotaPlan{}, nil)
	mockQuota.On("AssignUserToPlan", userID, planID).Return(nil)
	mockBilling.On("CreateOrUpdateSubscriber", userID, "stripe", TestCustomerID, true, mock.AnythingOfType("*uint")).Return(nil)
}

// Helper function to setup mocks for subscription deactivation scenarios
func setupSubscriptionDeactivationMocks(mockQuota *quotaCore.MockQuotaService, mockUsers *coreMocks.MockUserService, mockBilling *pluginCore.MockBillingService, userID uint) {
	mockUsers.On("AccountExists", userID).Return(true, createTestUser(userID), nil)
	mockQuota.On("RemoveUserFromPlan", userID).Return(nil)
	mockBilling.On("DeactivateSubscriber", userID, "stripe").Return(nil)
}

// Helper function to run a subscription activation test scenario
func runSubscriptionActivationTest(t *testing.T, eventType string, planID uint) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockQuota, mockUsers, mockBilling := setupMockServices(ctx)
		mockSubService := &MockSubscriptionRetriever{}

		subscription := createTestSubscription("123", strconv.FormatUint(uint64(planID), 10))
		rawData, _ := json.Marshal(subscription)
		event := createTestEvent(eventType, rawData)
		payload, _ := json.Marshal(event)

		mockSubService.SetupGetSuccess(&subscription)
		setupSubscriptionActivationMocks(mockQuota, mockUsers, mockBilling, TestUserID, planID)

		gw := New(ctx.Logger(), "test_secret", "", mockQuota, mockUsers, mockBilling)
		gw.subService = mockSubService
		err := gw.HandleWebhook(context.Background(), payload)

		assert.NoError(t, err)
	})
}

// Helper function to run a subscription deactivation test scenario
func runSubscriptionDeactivationTest(t *testing.T, eventType string) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockQuota, mockUsers, mockBilling := setupMockServices(ctx)
		mockSubService := &MockSubscriptionRetriever{}

		subscription := createTestSubscription("123", "")
		rawData, _ := json.Marshal(subscription)
		event := createTestEvent(eventType, rawData)
		payload, _ := json.Marshal(event)

		mockSubService.SetupGetSuccess(&subscription)
		setupSubscriptionDeactivationMocks(mockQuota, mockUsers, mockBilling, TestUserID)

		gw := New(ctx.Logger(), "test_secret", "", mockQuota, mockUsers, mockBilling)
		gw.subService = mockSubService
		err := gw.HandleWebhook(context.Background(), payload)

		assert.NoError(t, err)
	})
}



func TestStripeGateway_HandleWebhook_SubscriptionDeleted(t *testing.T) {
	runSubscriptionDeactivationTest(t, EventTypeSubscriptionDeleted)
}

func TestStripeGateway_HandleWebhook_SubscriptionUpdated(t *testing.T) {
	planID := uint(2)
	runSubscriptionActivationTest(t, EventTypeSubscriptionUpdated, planID)
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
			expectError: true,
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
		mockQuota, mockUsers, _ := setupMockServices(ctx)
		mockSubService := &MockSubscriptionRetriever{}

		subscription := createTestSubscription("123", "1")
		rawData, _ := json.Marshal(subscription)
		event := createTestEvent(EventTypeSubscriptionUpdated, rawData)
		payload, _ := json.Marshal(event)

		mockSubService.SetupGetSuccess(&subscription)
		mockUsers.On("AccountExists", TestUserID).Return(false, nil, nil)

		gw := New(ctx.Logger(), "test_secret", "", mockQuota, mockUsers, nil)
		gw.subService = mockSubService
		err := gw.HandleWebhook(context.Background(), payload)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "user with ID 123 not found")
	})
}

func TestStripeGateway_HandleWebhook_MissingPlanID(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockQuota, mockUsers, mockBilling := setupMockServices(ctx)
		mockSubService := &MockSubscriptionRetriever{}

		subscription := createTestSubscription("123", "")
		rawData, _ := json.Marshal(subscription)
		event := createTestEvent(EventTypeSubscriptionUpdated, rawData)
		payload, _ := json.Marshal(event)

		mockSubService.SetupGetSuccess(&subscription)
		setupSubscriptionDeactivationMocks(mockQuota, mockUsers, mockBilling, TestUserID)

		gw := New(ctx.Logger(), "test_secret", "", mockQuota, mockUsers, mockBilling)
		gw.subService = mockSubService
		err := gw.HandleWebhook(context.Background(), payload)

		// Should proceed with deactivation when no plan ID is found
		assert.NoError(t, err)
	})
}

func TestStripeGateway_HandleWebhook_NilSubscriptionItems(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockQuota, mockUsers, mockBilling := setupMockServices(ctx)
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

		gw := New(ctx.Logger(), "test_secret", "", mockQuota, mockUsers, mockBilling)
		gw.subService = mockSubService
		err := gw.HandleWebhook(context.Background(), payload)

		// Should proceed with deactivation when subscription items are nil
		assert.NoError(t, err)
	})
}

func TestStripeGateway_HandleWebhook_SubscriptionUpdated_AllPricesNil(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockQuota, mockUsers, mockBilling := setupMockServices(ctx)
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
						Plan: &stripe.Plan{
							Product: &stripe.Product{}, // Empty product with no plan_id
						},
					},
					{
						Price: nil,
						Plan: &stripe.Plan{
							Product: &stripe.Product{}, // Empty product with no plan_id
						},
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

		gw := New(ctx.Logger(), "test_secret", "", mockQuota, mockUsers, mockBilling)
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
	runSubscriptionActivationTest(t, EventTypeSubscriptionResumed, TestPlanID)
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
		mockBilling.On("GetActiveSubscription", uint(123)).Return(mockSubscriber, nil)

		// Mock successful billing portal session creation
		mockSession := &stripe.BillingPortalSession{
			URL: "https://billing.stripe.com/p/session/test_session_id",
		}
		mockStripeClient.billingPortalSessionsService = &MockBillingPortalSessions{}
		mockStripeClient.billingPortalSessionsService.On("Create", mock.Anything, mock.AnythingOfType("*stripe.BillingPortalSessionCreateParams")).Return(mockSession, nil)

		gw := New(ctx.Logger(), "test_secret", "test_api_key", nil, nil, mockBilling)
		gw.stripeClient = mockStripeClient

		url, err := gw.GetCustomerPortalURL(context.Background(), 123, "https://example.com/return")

		assert.NoError(t, err)
		assert.Equal(t, "https://billing.stripe.com/p/session/test_session_id", url)

		// Verify the billing service was called correctly
		mockBilling.AssertExpectations(t)
		mockStripeClient.AssertExpectations(t)
	})
}

func TestStripeGateway_HandleWebhook_CheckoutSessionCompleted(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockQuota, mockUsers, mockBilling := setupMockServices(ctx)
		mockSubService := &MockSubscriptionRetriever{}

		planID := uint(3)
		userID := uint(456)
		customerID := "cus_456"
		
		checkoutSession := stripe.CheckoutSession{
			ID:                 "cs_test_123",
			ClientReferenceID:  strconv.FormatUint(uint64(userID), 10),
			Subscription:       &stripe.Subscription{ID: "sub_456"},
			Mode:               "subscription",
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
									PlanIDMetadataKey: strconv.FormatUint(uint64(planID), 10),
								},
							},
						},
						Plan: &stripe.Plan{
							ID: "plan_456",
						},
					},
				},
			},
		}
		mockSubService.On("Get", mock.Anything, "sub_456", mock.AnythingOfType("*stripe.SubscriptionRetrieveParams")).Return(testSubscription, nil)

		// Setup mocks
		mockUsers.On("AccountExists", userID).Return(true, createTestUser(userID), nil)
		mockQuota.On("GetQuotaPlan", planID).Return(&quotaCore.QuotaPlan{}, nil)
		mockQuota.On("AssignUserToPlan", userID, planID).Return(nil)
		mockBilling.On("CreateOrUpdateSubscriber", userID, "stripe", customerID, true, &planID).Return(nil)
		

		gw := New(ctx.Logger(), "test_secret", "test_api_key", mockQuota, mockUsers, mockBilling)
		gw.subService = mockSubService
		
		err := gw.HandleWebhook(context.Background(), payload)

		assert.NoError(t, err)
		
		mockSubService.AssertExpectations(t)
	})
}

// MockStripeClient is a mock implementation of the Client for testing purposes.
// It allows tests to control the responses from Stripe API calls without making actual
// API requests.
type MockStripeClient struct {
	mock.Mock
	billingPortalSessionsService *MockBillingPortalSessions
	customersService             *MockCustomers
	subscriptionsService         *MockSubscriptions
}

// V1BillingPortalSessions returns the mock billing portal sessions service
func (m *MockStripeClient) V1BillingPortalSessions() BillingPortalSessions {
	return m.billingPortalSessionsService
}

// V1Customers returns the mock customers service
func (m *MockStripeClient) V1Customers() Customers {
	return m.customersService
}

// V1Subscriptions returns the mock subscriptions service
func (m *MockStripeClient) V1Subscriptions() Subscriptions {
	return m.subscriptionsService
}

// MockBillingPortalSessions is a mock implementation of the billing portal sessions service
type MockBillingPortalSessions struct {
	mock.Mock
}

// Create mocks the Stripe billing portal session creation
func (m *MockBillingPortalSessions) Create(ctx context.Context, params *stripe.BillingPortalSessionCreateParams) (*stripe.BillingPortalSession, error) {
	args := m.Called(ctx, params)
	session, ok := args.Get(0).(*stripe.BillingPortalSession)
	if !ok && args.Get(0) != nil {
		return nil, fmt.Errorf("mock setup error: expected *stripe.BillingPortalSession, got %T", args.Get(0))
	}
	return session, args.Error(1)
}

// MockCustomers is a mock implementation of the customers service
type MockCustomers struct {
	mock.Mock
}

// Get mocks the Stripe customer retrieval
func (m *MockCustomers) Retrieve(ctx context.Context, id string, params *stripe.CustomerRetrieveParams) (*stripe.Customer, error) {
	args := m.Called(ctx, id, params)
	customer, ok := args.Get(0).(*stripe.Customer)
	if !ok && args.Get(0) != nil {
		return nil, fmt.Errorf("mock setup error: expected *stripe.Customer, got %T", args.Get(0))
	}
	return customer, args.Error(1)
}

// Update mocks the Stripe customer update
func (m *MockCustomers) Update(ctx context.Context, id string, params *stripe.CustomerUpdateParams) (*stripe.Customer, error) {
	args := m.Called(ctx, id, params)
	customer, ok := args.Get(0).(*stripe.Customer)
	if !ok && args.Get(0) != nil {
		return nil, fmt.Errorf("mock setup error: expected *stripe.Customer, got %T", args.Get(0))
	}
	return customer, args.Error(1)
}

// MockSubscriptions is a mock implementation of the subscriptions service
type MockSubscriptions struct {
	mock.Mock
}

// Get mocks the Stripe subscription retrieval
func (m *MockSubscriptions) Retrieve(ctx context.Context, id string, params *stripe.SubscriptionRetrieveParams) (*stripe.Subscription, error) {
	args := m.Called(ctx, id, params)
	subscription, ok := args.Get(0).(*stripe.Subscription)
	if !ok && args.Get(0) != nil {
		return nil, fmt.Errorf("mock setup error: expected *stripe.Subscription, got %T", args.Get(0))
	}
	return subscription, args.Error(1)
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
	m.On("Get", mock.Anything, subscription.ID, mock.AnythingOfType("*stripe.SubscriptionRetrieveParams")).Return(subscription, nil)
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
