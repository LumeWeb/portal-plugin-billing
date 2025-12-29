package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/docker/go-units"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/stripe/stripe-go/v83"
	"github.com/stripe/stripe-go/v83/webhook"
	plugin "go.lumeweb.com/portal-plugin-billing"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	billingDTO "go.lumeweb.com/portal-plugin-billing/internal/api/dto"
	"go.lumeweb.com/portal-plugin-billing/internal/config"
	billingModels "go.lumeweb.com/portal-plugin-billing/internal/db/models"
	"go.lumeweb.com/portal-plugin-billing/internal/gateway"
	pluginGatewayStripe "go.lumeweb.com/portal-plugin-billing/internal/gateway/stripe"
	corePlugin "go.lumeweb.com/portal-plugin-core"
	dashboardPlugin "go.lumeweb.com/portal-plugin-dashboard"
	quotaPlugin "go.lumeweb.com/portal-plugin-quota"
	quotaCore "go.lumeweb.com/portal-plugin-quota/core"
	"go.lumeweb.com/portal/core"
	coreTesting "go.lumeweb.com/portal/core/testing"
	"go.lumeweb.com/portal/event"
	"go.lumeweb.com/portal/service"
	"gorm.io/gorm"
)

func TestMain(m *testing.M) {
	// Use the new framework's TestMain helper to set up the shared environment.
	coreTesting.WithOptions(m,
		coreTesting.WithConfig("plugin.dashboard.api.subdomain", "account"),
		coreTesting.WithPlugins(corePlugin.GetPluginInfo()),
		coreTesting.WithPlugins(dashboardPlugin.GetPluginInfo()), // Fixed typo in variable name
		coreTesting.WithPlugins(quotaPlugin.GetPluginInfo()),
		coreTesting.WithPlugins(plugin.GetPluginInfo()),
		coreTesting.WithServiceFactory(core.USER_SERVICE, service.NewUserService),
		coreTesting.WithServiceFactory(core.AUTH_SERVICE, service.NewAuthService),
		coreTesting.WithServiceFactory(core.STORAGE_SERVICE, service.NewStorageService),
		coreTesting.WithMockS3(),
		coreTesting.WithConfig("plugin.billing.service.billing.stripe.webhook_secret", "1234567890"),
		coreTesting.WithConfig("plugin.billing.service.billing.stripe.secret_key", "sk_test_1234567890"),
		// Configure the domain for the API
		coreTesting.WithSQLite(),
		coreTesting.WithAPIID("dashboard"),
		coreTesting.WithTestMainContextSimple(func(ctx coreTesting.TestContext) []coreTesting.TestContextBuilderOption {
			return []coreTesting.TestContextBuilderOption{
				func(c coreTesting.TestContext) (coreTesting.TestContext, error) {
					event.OnBootCompleted(ctx, func(_ core.Context, ctx context.Context) error {
						registry := gateway.GetRegistry()
						registry.Reset()

						// Register Stripe gateway if webhook secret is configured
						if secret := strings.TrimSpace(getStripeWebhookSecret(c)); secret != "" {
							// Use mock factory to create a fully configured mock Stripe gateway
							mockGateway, _, _, _ := pluginGatewayStripe.CreateMockStripeGateway(c, secret, getStripeSecretKey(c))

							if err := registry.Register(context.Background(), mockGateway); err != nil {
								return fmt.Errorf("failed to register mock stripe gateway: %w", err)
							}
						}

						c.OnExit(func(context core.Context) error {
							registry.Reset()
							return nil
						})

						return nil
					})
					return ctx, nil
				},
			}
		}),
	)
}

// createTestUserAndLogin creates a test user and returns auth token and user ID
func createTestUserAndLogin(ctx coreTesting.TestContext) (string, uint) {
	// Register user via HTTP API
	registerReq := map[string]interface{}{
		"email":      TestUserEmail,
		"password":   TestUserPassword,
		"first_name": "Test",
		"last_name":  "User",
	}
	body, err := json.Marshal(registerReq)
	if err != nil {
		ctx.T().Fatalf("failed to marshal register request: %v", err)
	}

	req := ctx.NewAPIRequest(http.MethodPost, "/api/auth/register", body)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	ctx.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		ctx.T().Fatalf("failed to register test user: status %d, body: %s", rec.Code, rec.Body.String())
	}

	// Login via HTTP API
	loginReq := map[string]any{
		"email":    TestUserEmail,
		"password": TestUserPassword,
		"remember": false,
	}
	body, err = json.Marshal(loginReq)
	if err != nil {
		ctx.T().Fatalf("failed to marshal login request: %v", err)
	}

	req = ctx.NewAPIRequest(http.MethodPost, "/api/auth/login", body)
	req.Header.Set("Content-Type", "application/json")

	rec = httptest.NewRecorder()
	ctx.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		ctx.T().Fatalf("failed to login test user: status %d, body: %s", rec.Code, rec.Body.String())
	}

	// Follow redirect to get the actual response
	location := rec.Header().Get("Location")
	if location == "" {
		ctx.T().Fatalf("login returned redirect but no Location header")
	}

	// Make request to the redirect location
	req = ctx.NewAPIRequest(http.MethodGet, location, nil)
	req.Header.Set("Content-Type", "application/json")

	rec = httptest.NewRecorder()
	ctx.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		ctx.T().Fatalf("failed to get login response after redirect: status %d, body: %s", rec.Code, rec.Body.String())
	}

	// Extract token from response
	var loginResponse struct {
		Token string `json:"token"`
	}
	err = json.Unmarshal(rec.Body.Bytes(), &loginResponse)
	if err != nil {
		ctx.T().Fatalf("failed to parse login response: %v", err)
	}

	if loginResponse.Token == "" {
		ctx.T().Fatalf("no token returned from login")
	}

	// Get user ID from account info
	req = ctx.NewAPIRequest(http.MethodGet, "/api/account", nil)
	req.Header.Set("Authorization", "Bearer "+loginResponse.Token)

	rec = httptest.NewRecorder()
	ctx.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		ctx.T().Fatalf("failed to get account info: status %d, body: %s", rec.Code, rec.Body.String())
	}

	var accountResponse struct {
		ID uint `json:"id"`
	}
	err = json.Unmarshal(rec.Body.Bytes(), &accountResponse)
	if err != nil {
		ctx.T().Fatalf("failed to parse account response: %v", err)
	}

	return loginResponse.Token, accountResponse.ID
}

// Local test helpers based on actual gateway package

// TestSetupHelpers contains common setup patterns for billing tests
type TestSetupHelpers struct {
	ctx               coreTesting.TestContext
	userID            uint
	token             string
	mockStripeGateway *pluginGatewayStripe.StripeGateway
	mockSubRetriever  *pluginGatewayStripe.MockSubscriptionRetriever
	mockStripeClient  *pluginGatewayStripe.MockStripeClient
}

// NewTestSetupHelpers creates a new test setup helper with user and gateway configured
func NewTestSetupHelpers(ctx coreTesting.TestContext) *TestSetupHelpers {
	token, userID := createTestUserAndLogin(ctx)

	// Get and configure mock gateway
	registry := gateway.GetRegistry()
	stripeGateway, exists := registry.Get(pluginGatewayStripe.GatewayID)
	if !exists {
		ctx.T().Fatalf("Stripe gateway not found in registry")
	}

	mockStripeGateway, ok := stripeGateway.(*pluginGatewayStripe.StripeGateway)
	if !ok {
		ctx.T().Fatalf("Gateway is not a StripeGateway")
	}

	// Get mock services
	mockSubRetriever, ok := mockStripeGateway.GetSubscriptionRetriever().(*pluginGatewayStripe.MockSubscriptionRetriever)
	if !ok {
		ctx.T().Fatalf("Subscription retriever is not a mock")
	}

	mockStripeClient, ok := mockStripeGateway.GetStripeClient().(*pluginGatewayStripe.MockStripeClient)
	if !ok {
		ctx.T().Fatalf("Stripe client is not a mock")
	}

	return &TestSetupHelpers{
		ctx:               ctx,
		userID:            userID,
		token:             token,
		mockStripeGateway: mockStripeGateway,
		mockSubRetriever:  mockSubRetriever,
		mockStripeClient:  mockStripeClient,
	}
}

// CreateMockSubscription creates a mock subscription with the given parameters
func (h *TestSetupHelpers) CreateMockSubscription(subscriptionID, customerID string, planID *uint) *stripe.Subscription {
	subscription := &stripe.Subscription{
		ID: subscriptionID,
		Customer: &stripe.Customer{
			ID: customerID,
			Metadata: map[string]string{
				"user_id": fmt.Sprintf("%d", h.userID),
			},
		},
		Items: &stripe.SubscriptionItemList{
			Data: []*stripe.SubscriptionItem{
				{
					Price: &stripe.Price{
						Product: &stripe.Product{
							Metadata: map[string]string{},
						},
					},
				},
			},
		},
	}

	// Add plan ID to metadata if provided
	if planID != nil {
		subscription.Items.Data[0].Price.Product.Metadata["plan_id"] = fmt.Sprintf("%d", *planID)
	}

	return subscription
}

// SetupMockSubscription configures the mock subscription retriever with the given subscription
func (h *TestSetupHelpers) SetupMockSubscription(subscription *stripe.Subscription) {
	h.mockSubRetriever.Mock = mock.Mock{} // Reset the mock
	h.mockSubRetriever.SetupGetSuccess(subscription)
}

// SetupMockCustomer configures the mock customer service to handle update calls
func (h *TestSetupHelpers) SetupMockCustomer(customerID string) {
	mockCustomer := &stripe.Customer{
		ID: customerID,
		Metadata: map[string]string{
			"user_id": fmt.Sprintf("%d", h.userID),
		},
	}

	h.mockStripeClient.CustomersService.On("Update", mock.Anything, customerID, mock.MatchedBy(func(params *stripe.CustomerUpdateParams) bool {
		return params != nil && params.Metadata != nil && params.Metadata["user_id"] == fmt.Sprintf("%d", h.userID)
	})).Return(mockCustomer, nil)
}

// SetupMockBillingPortal configures the mock billing portal service
func (h *TestSetupHelpers) SetupMockBillingPortal(customerID string) {
	h.mockStripeClient.BillingPortalSessionsService.On("Create", mock.Anything, mock.MatchedBy(func(params *stripe.BillingPortalSessionCreateParams) bool {
		return params != nil && params.Customer != nil && *params.Customer == customerID
	})).Return(&stripe.BillingPortalSession{
		URL: "https://billing.stripe.com/session/test",
	}, nil)
}

// createTestQuotaPlan creates a test quota plan in the database
func createTestQuotaPlan(ctx coreTesting.TestContext, planID uint, name string) error {
	// Get the quota service from the context
	quotaService := core.GetService[quotaCore.QuotaService](ctx, quotaCore.QUOTA_SERVICE)
	if quotaService == nil {
		return fmt.Errorf("quota service not available")
	}

	plan := &quotaCore.QuotaPlan{
		Model:              gorm.Model{ID: planID},
		Name:               name,
		Description:        fmt.Sprintf("Test plan: %s", name),
		StorageLimit:       units.GiB,       // 1GB
		UploadDailyLimit:   units.MiB * 100, // 100MB
		DownloadDailyLimit: units.MiB * 100, // 100MB
		UploadTotalLimit:   units.GiB,       // 1GB
		DownloadTotalLimit: units.GiB,       // 1GB
		IsDefault:          false,
	}

	return quotaService.CreateQuotaPlan(context.Background(), plan)
}

// createStripeCheckoutSessionEvent creates a Stripe checkout.session.completed event
func createStripeCheckoutSessionEvent(userID uint, subscriptionID string, planID uint) *stripe.Event {
	checkoutSession := stripe.CheckoutSession{
		ID:                fmt.Sprintf("cs_test_%d", time.Now().UnixNano()),
		Object:            "checkout.session",
		Mode:              stripe.CheckoutSessionModeSubscription,
		ClientReferenceID: fmt.Sprintf("%d", userID),
		Subscription: &stripe.Subscription{
			ID: subscriptionID,
		},
		Customer: &stripe.Customer{
			ID: fmt.Sprintf("cus_test_%d", userID),
		},
		Metadata: map[string]string{
			"plan_id": fmt.Sprintf("%d", planID),
		},
	}
	rawData, err := json.Marshal(checkoutSession)
	if err != nil {
		panic(fmt.Sprintf("test setup failed: %v", err))
	}

	return &stripe.Event{
		ID:         fmt.Sprintf("evt_test_%d", time.Now().UnixNano()),
		Object:     "event",
		APIVersion: stripe.APIVersion,
		Type:       pluginGatewayStripe.EventTypeCheckoutSessionCompleted,
		Data: &stripe.EventData{
			Raw: rawData,
		},
	}
}

// createStripeSubscriptionEvent creates a Stripe subscription event
func createStripeSubscriptionEvent(eventType, subscriptionID, customerID string, userID uint, planID *uint) *stripe.Event {
	subscription := stripe.Subscription{
		ID: subscriptionID,
		Customer: &stripe.Customer{
			ID: customerID,
		},
		Metadata: map[string]string{
			"user_id": fmt.Sprintf("%d", userID),
		},
	}

	// Add plan ID to metadata if provided
	if planID != nil {
		subscription.Metadata["plan_id"] = fmt.Sprintf("%d", *planID)
	}

	rawData, err := json.Marshal(subscription)
	if err != nil {
		panic(fmt.Sprintf("test setup failed: %v", err))
	}

	return &stripe.Event{
		ID:         fmt.Sprintf("evt_test_%d", time.Now().UnixNano()),
		Object:     "event",
		APIVersion: stripe.APIVersion,
		Type:       stripe.EventType(eventType),
		Data: &stripe.EventData{
			Raw: rawData,
		},
	}
}

// SendWebhook sends a webhook to the Stripe endpoint and returns the response
func (h *TestSetupHelpers) SendWebhook(event *stripe.Event) *httptest.ResponseRecorder {
	payload, signature, err := generateStripeWebhookPayload(event, getStripeWebhookSecret(h.ctx))
	if err != nil {
		h.ctx.T().Fatalf("Failed to generate webhook payload: %v", err)
	}

	req := h.ctx.NewAPIRequest(http.MethodPost, "/api/account/billing/webhooks/stripe", payload)
	req.Header.Set("Stripe-Signature", signature)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	h.ctx.Router().ServeHTTP(rec, req)

	return rec
}

// SendWebhookWithInvalidSignature sends a webhook with an invalid signature
func (h *TestSetupHelpers) SendWebhookWithInvalidSignature(event *stripe.Event) *httptest.ResponseRecorder {
	payload, _, err := generateStripeWebhookPayload(event, getStripeWebhookSecret(h.ctx))
	if err != nil {
		h.ctx.T().Fatalf("Failed to generate webhook payload: %v", err)
	}

	req := h.ctx.NewAPIRequest(http.MethodPost, "/api/account/billing/webhooks/stripe", payload)
	req.Header.Set("Stripe-Signature", "invalid_signature")
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	h.ctx.Router().ServeHTTP(rec, req)

	return rec
}

// GetSubscriptionStatus retrieves the current subscription status via API
func (h *TestSetupHelpers) GetSubscriptionStatus() billingDTO.SubscriptionStatusResponse {
	req := h.ctx.NewAPIRequest(http.MethodGet, "/api/account/billing/subscription", nil)
	req.Header.Set("Authorization", "Bearer "+h.token)

	rec := httptest.NewRecorder()
	h.ctx.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		h.ctx.T().Fatalf("Failed to get subscription status: status %d, body: %s", rec.Code, rec.Body.String())
	}

	var response billingDTO.SubscriptionStatusResponse
	err := json.Unmarshal(rec.Body.Bytes(), &response)
	if err != nil {
		h.ctx.T().Fatalf("Failed to parse subscription status response: %v", err)
	}

	return response
}

// RequestCustomerPortal requests a customer portal URL
func (h *TestSetupHelpers) RequestCustomerPortal(returnURL string) billingDTO.CustomerPortalResponse {
	requestBody := billingDTO.CustomerPortalRequest{
		ReturnURL: returnURL,
	}
	bodyBytes, err := json.Marshal(requestBody)
	if err != nil {
		h.ctx.T().Fatalf("failed to marshal customer portal request: %v", err)
	}

	req := h.ctx.NewAPIRequest(http.MethodPost, "/api/account/billing/customer-portal", bodyBytes)
	req.Header.Set("Authorization", "Bearer "+h.token)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	h.ctx.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		h.ctx.T().Fatalf("Failed to request customer portal: status %d, body: %s", rec.Code, rec.Body.String())
	}

	var response billingDTO.CustomerPortalResponse
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	if err != nil {
		h.ctx.T().Fatalf("Failed to parse customer portal response: %v", err)
	}

	return response
}

// generateStripeWebhookPayload generates a signed Stripe webhook payload
func generateStripeWebhookPayload(event *stripe.Event, secret string) ([]byte, string, error) {
	payload, err := json.Marshal(event)
	if err != nil {
		return nil, "", err
	}

	// Generate a valid signature
	unsignedPayload := &webhook.UnsignedPayload{
		Payload:   payload,
		Secret:    secret,
		Timestamp: time.Now(),
	}
	signedPayload := webhook.GenerateTestSignedPayload(unsignedPayload)

	return payload, signedPayload.Header, nil
}

// getStripeWebhookSecret retrieves the Stripe webhook secret from the service configuration
func getStripeWebhookSecret(ctx coreTesting.TestContext) string {
	return core.GetServiceConfig[*config.ServiceConfig](ctx, pluginCore.BILLING_SERVICE).Stripe.WebhookSecret
}

// getStripeSecretKey retrieves the Stripe secret key from the service configuration
func getStripeSecretKey(ctx coreTesting.TestContext) string {
	return core.GetServiceConfig[*config.ServiceConfig](ctx, pluginCore.BILLING_SERVICE).Stripe.SecretKey
}

// assertSubscriberExists checks if a subscriber exists and returns it
func assertSubscriberExists(ctx coreTesting.TestContext, userID uint, gatewayType string, isActive bool) (*billingModels.Subscriber, error) {
	// Get the billing service from the context
	billingService := core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE)
	if billingService == nil {
		return nil, fmt.Errorf("billing service not available")
	}

	// Use the billing service to get the active subscriber
	subscriber, err := billingService.GetActiveSubscriber(context.Background(), userID, gatewayType)
	if err != nil {
		return nil, err
	}

	// Check if the subscriber's active status matches what we expect
	if subscriber != nil && subscriber.IsActive != isActive {
		return nil, fmt.Errorf("subscriber active status mismatch: expected %v, got %v", isActive, subscriber.IsActive)
	}

	return subscriber, nil
}

// TestSubscriptionSignup_NewUserSubscription tests new user subscription signup flow via HTTP API
func TestSubscriptionSignup_NewUserSubscription(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		planID := uint(42)
		subscriptionID := "sub_signup_test"
		customerID := "cus_test_" + subscriptionID

		// Create test setup helper and quota plan
		helper := NewTestSetupHelpers(ctx)
		err := createTestQuotaPlan(ctx, planID, "Signup Test Plan")
		require.NoError(t, err)

		// Set up mock subscription and customer
		mockSubscription := helper.CreateMockSubscription(subscriptionID, customerID, &planID)
		helper.SetupMockSubscription(mockSubscription)
		helper.SetupMockCustomer(customerID)

		// Create and send checkout session completed webhook
		evt := createStripeCheckoutSessionEvent(helper.userID, subscriptionID, planID)
		rec := helper.SendWebhook(evt)

		// Verify webhook was processed successfully
		assert.Equal(t, http.StatusNoContent, rec.Code)

		// Verify subscription status via API
		response := helper.GetSubscriptionStatus()
		assert.True(t, response.IsSubscribed)
		assert.Equal(t, "stripe", response.GatewayType)
		require.NotNil(t, response.PlanID)
		assert.Equal(t, planID, *response.PlanID)
	})
}

// TestSubscriptionSignup_DuplicatePrevention tests duplicate subscription signup prevention via HTTP API
func TestSubscriptionSignup_DuplicatePrevention(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		planID := uint(42)
		subscriptionID := "sub_signup_test"
		customerID := "cus_signup_test"

		// Create test setup helper and quota plan
		helper := NewTestSetupHelpers(ctx)
		err := createTestQuotaPlan(ctx, planID, "Signup Test Plan")
		require.NoError(t, err)

		// Set up mock subscription and customer
		mockSubscription := helper.CreateMockSubscription(subscriptionID, customerID, &planID)
		helper.SetupMockSubscription(mockSubscription)
		helper.SetupMockCustomer(customerID)

		// First create the initial subscription
		event := createStripeCheckoutSessionEvent(helper.userID, subscriptionID, planID)
		rec := helper.SendWebhook(event)
		assert.Equal(t, http.StatusNoContent, rec.Code)

		// Send the same webhook again (should be deduplicated)
		rec = helper.SendWebhook(event)
		assert.Equal(t, http.StatusNoContent, rec.Code)

		// Verify only one subscriber exists
		subscriber, err := assertSubscriberExists(ctx, helper.userID, "stripe", true)
		require.NoError(t, err)
		assert.Equal(t, customerID, subscriber.GatewayID)
	})
}

// TestSubscriptionSignup_InvalidWebhookSignature tests invalid webhook signature handling via HTTP API
func TestSubscriptionSignup_InvalidWebhookSignature(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		// Create test setup helper
		helper := NewTestSetupHelpers(ctx)

		event := createStripeCheckoutSessionEvent(helper.userID, "sub_invalid_test", 42)
		rec := helper.SendWebhookWithInvalidSignature(event)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
}

// TestSubscriptionSignup_MissingPlanMetadata tests missing plan metadata handling via HTTP API
func TestSubscriptionSignup_MissingPlanMetadata(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		// Create test setup helper
		helper := NewTestSetupHelpers(ctx)

		// Set up mock subscription data without plan metadata
		mockSubscription := helper.CreateMockSubscription("sub_noplan_test", "cus_noplan_test", nil)
		helper.SetupMockSubscription(mockSubscription)

		// Create subscription event without plan metadata
		event := createStripeSubscriptionEvent(
			pluginGatewayStripe.EventTypeSubscriptionUpdated,
			"sub_noplan_test",
			"cus_noplan_test",
			helper.userID,
			nil, // No plan ID
		)

		rec := helper.SendWebhook(event)
		assert.Equal(t, http.StatusNoContent, rec.Code)

		// Verify subscriber is inactive (no active subscriber should exist)
		subscriber, err := assertSubscriberExists(ctx, helper.userID, "stripe", false)
		require.NoError(t, err)
		assert.Nil(t, subscriber) // Should be nil since we expect no active subscriber
	})
}

// TestPlanUpgrade_PlanUpgradeFlow tests plan upgrade flow via HTTP API
func TestPlanUpgrade_PlanUpgradeFlow(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		initialPlanID := uint(50)
		upgradePlanID := uint(100)
		subscriptionID := "sub_upgrade_test"
		customerID := "cus_upgrade_test"

		// Create test setup helper and quota plans
		helper := NewTestSetupHelpers(ctx)
		err := createTestQuotaPlan(ctx, initialPlanID, "Initial Plan")
		require.NoError(t, err)
		err = createTestQuotaPlan(ctx, upgradePlanID, "Upgrade Plan")
		require.NoError(t, err)

		// Set up initial mock subscription and customer
		mockSubscription := helper.CreateMockSubscription(subscriptionID, customerID, &initialPlanID)
		helper.SetupMockSubscription(mockSubscription)
		helper.SetupMockCustomer(customerID)

		// Create initial subscription
		event := createStripeCheckoutSessionEvent(helper.userID, subscriptionID, initialPlanID)
		rec := helper.SendWebhook(event)
		assert.Equal(t, http.StatusNoContent, rec.Code)

		// Verify initial plan
		response := helper.GetSubscriptionStatus()
		require.NotNil(t, response.PlanID)
		assert.Equal(t, initialPlanID, *response.PlanID)

		// Set up mock subscription for upgrade scenario
		upgradeMockSubscription := helper.CreateMockSubscription(subscriptionID, customerID, &upgradePlanID)
		helper.SetupMockSubscription(upgradeMockSubscription)

		// Send upgrade webhook
		upgradeEvent := createStripeSubscriptionEvent(
			pluginGatewayStripe.EventTypeSubscriptionUpdated,
			subscriptionID,
			customerID,
			helper.userID,
			&upgradePlanID,
		)
		rec = helper.SendWebhook(upgradeEvent)
		assert.Equal(t, http.StatusNoContent, rec.Code)

		// Verify plan was upgraded
		response = helper.GetSubscriptionStatus()
		require.NotNil(t, response.PlanID)
		assert.Equal(t, upgradePlanID, *response.PlanID)
	})
}

// TestPlanDowngrade_PlanDowngradeFlow tests plan downgrade flow via HTTP API
func TestPlanDowngrade_PlanDowngradeFlow(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		initialPlanID := uint(200)
		downgradePlanID := uint(50)
		subscriptionID := "sub_downgrade_test"
		customerID := "cus_downgrade_test"

		// Create test setup helper and quota plans
		helper := NewTestSetupHelpers(ctx)
		err := createTestQuotaPlan(ctx, initialPlanID, "Premium Plan")
		require.NoError(t, err)
		err = createTestQuotaPlan(ctx, downgradePlanID, "Basic Plan")
		require.NoError(t, err)

		// Set up initial mock subscription and customer
		mockSubscription := helper.CreateMockSubscription(subscriptionID, customerID, &initialPlanID)
		helper.SetupMockSubscription(mockSubscription)
		helper.SetupMockCustomer(customerID)

		// Create initial subscription with premium plan
		event := createStripeCheckoutSessionEvent(helper.userID, subscriptionID, initialPlanID)
		rec := helper.SendWebhook(event)
		assert.Equal(t, http.StatusNoContent, rec.Code)

		// Verify initial plan
		response := helper.GetSubscriptionStatus()
		require.NotNil(t, response.PlanID)
		assert.Equal(t, initialPlanID, *response.PlanID)

		// Set up mock subscription for downgrade scenario
		downgradeMockSubscription := helper.CreateMockSubscription(subscriptionID, customerID, &downgradePlanID)
		helper.SetupMockSubscription(downgradeMockSubscription)

		// Send downgrade webhook
		downgradeEvent := createStripeSubscriptionEvent(
			pluginGatewayStripe.EventTypeSubscriptionUpdated,
			subscriptionID,
			customerID,
			helper.userID,
			&downgradePlanID,
		)
		rec = helper.SendWebhook(downgradeEvent)
		assert.Equal(t, http.StatusNoContent, rec.Code)

		// Verify plan was downgraded
		response = helper.GetSubscriptionStatus()
		require.NotNil(t, response.PlanID)
		assert.Equal(t, downgradePlanID, *response.PlanID)
	})
}

// TestSubscriptionCancellation_SubscriptionCancellationFlow tests subscription cancellation flow via HTTP API
func TestSubscriptionCancellation_SubscriptionCancellationFlow(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		planID := uint(75)
		subscriptionID := "sub_cancel_test"
		customerID := "cus_cancel_test"

		// Create test setup helper and quota plan
		helper := NewTestSetupHelpers(ctx)
		err := createTestQuotaPlan(ctx, planID, "Cancel Test Plan")
		require.NoError(t, err)

		// Set up initial mock subscription and customer
		mockSubscription := helper.CreateMockSubscription(subscriptionID, customerID, &planID)
		helper.SetupMockSubscription(mockSubscription)
		helper.SetupMockCustomer(customerID)

		// Create active subscription
		event := createStripeCheckoutSessionEvent(helper.userID, subscriptionID, planID)
		rec := helper.SendWebhook(event)
		assert.Equal(t, http.StatusNoContent, rec.Code)

		// Verify active subscription
		response := helper.GetSubscriptionStatus()
		assert.True(t, response.IsSubscribed)

		// Set up mock subscription for cancellation scenario
		cancelMockSubscription := helper.CreateMockSubscription(subscriptionID, customerID, &planID)
		helper.SetupMockSubscription(cancelMockSubscription)

		// Send cancellation webhook
		cancelEvent := createStripeSubscriptionEvent(
			pluginGatewayStripe.EventTypeSubscriptionDeleted,
			subscriptionID,
			customerID,
			helper.userID,
			nil,
		)
		rec = helper.SendWebhook(cancelEvent)
		assert.Equal(t, http.StatusNoContent, rec.Code)

		// Verify subscription is cancelled
		cancelResponse := helper.GetSubscriptionStatus()
		assert.False(t, cancelResponse.IsSubscribed)
		assert.Equal(t, "", cancelResponse.GatewayType)
		assert.Nil(t, cancelResponse.PlanID)
	})
}

// TestSubscriptionCancellation_SubscriptionPauseFlow tests subscription pause flow via HTTP API
func TestSubscriptionCancellation_SubscriptionPauseFlow(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		planID := uint(76)
		subscriptionID := "sub_pause_test"
		customerID := "cus_pause_test"

		// Create test setup helper and quota plan
		helper := NewTestSetupHelpers(ctx)
		err := createTestQuotaPlan(ctx, planID, "Pause Test Plan")
		require.NoError(t, err)

		// Set up initial mock subscription and customer
		mockSubscription := helper.CreateMockSubscription(subscriptionID, customerID, &planID)
		helper.SetupMockSubscription(mockSubscription)
		helper.SetupMockCustomer(customerID)

		// Create active subscription
		event := createStripeCheckoutSessionEvent(helper.userID, subscriptionID, planID)
		rec := helper.SendWebhook(event)
		assert.Equal(t, http.StatusNoContent, rec.Code)

		// Verify active subscription
		response := helper.GetSubscriptionStatus()
		assert.True(t, response.IsSubscribed)

		// Set up mock subscription for pause scenario
		pauseMockSubscription := helper.CreateMockSubscription(subscriptionID, customerID, &planID)
		helper.SetupMockSubscription(pauseMockSubscription)

		// Send pause webhook
		pauseEvent := createStripeSubscriptionEvent(
			pluginGatewayStripe.EventTypeSubscriptionPaused,
			subscriptionID,
			customerID,
			helper.userID,
			nil,
		)
		rec = helper.SendWebhook(pauseEvent)
		assert.Equal(t, http.StatusNoContent, rec.Code)

		// Verify subscription is paused
		response = helper.GetSubscriptionStatus()
		assert.False(t, response.IsSubscribed)
	})
}

// TestSubscriptionReactivation_SubscriptionReactivationFlow tests subscription reactivation flow via HTTP API
func TestSubscriptionReactivation_SubscriptionReactivationFlow(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		planID := uint(85)
		subscriptionID := "sub_resume_test"
		customerID := "cus_resume_test"

		// Create test setup helper and quota plan
		helper := NewTestSetupHelpers(ctx)
		err := createTestQuotaPlan(ctx, planID, "Resume Test Plan")
		require.NoError(t, err)

		// Set up initial mock subscription and customer
		mockSubscription := helper.CreateMockSubscription(subscriptionID, customerID, &planID)
		helper.SetupMockSubscription(mockSubscription)
		helper.SetupMockCustomer(customerID)

		// Create and then pause subscription
		event := createStripeCheckoutSessionEvent(helper.userID, subscriptionID, planID)
		rec := helper.SendWebhook(event)
		assert.Equal(t, http.StatusNoContent, rec.Code)

		// Set up mock subscription for pause scenario
		pauseMockSubscription := helper.CreateMockSubscription(subscriptionID, customerID, &planID)
		helper.SetupMockSubscription(pauseMockSubscription)

		// Pause the subscription
		pauseEvent := createStripeSubscriptionEvent(
			pluginGatewayStripe.EventTypeSubscriptionPaused,
			subscriptionID,
			customerID,
			helper.userID,
			nil,
		)
		rec = helper.SendWebhook(pauseEvent)
		assert.Equal(t, http.StatusNoContent, rec.Code)

		// Verify subscription is paused
		response := helper.GetSubscriptionStatus()
		assert.False(t, response.IsSubscribed)

		// Set up mock subscription for resume scenario
		resumeMockSubscription := helper.CreateMockSubscription(subscriptionID, customerID, &planID)
		helper.SetupMockSubscription(resumeMockSubscription)

		// Send resume webhook
		resumeEvent := createStripeSubscriptionEvent(
			pluginGatewayStripe.EventTypeSubscriptionResumed,
			subscriptionID,
			customerID,
			helper.userID,
			&planID,
		)
		rec = helper.SendWebhook(resumeEvent)
		assert.Equal(t, http.StatusNoContent, rec.Code)

		// Verify subscription is reactivated
		response = helper.GetSubscriptionStatus()
		assert.True(t, response.IsSubscribed)
		assert.Equal(t, "stripe", response.GatewayType)
		require.NotNil(t, response.PlanID)
		assert.Equal(t, planID, *response.PlanID)
	})
}

// TestSubscriptionCancellation_CancellationRequestIgnored tests that subscription cancellation requests are ignored until deletion
func TestSubscriptionCancellation_CancellationRequestIgnored(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		planID := uint(77)
		subscriptionID := "sub_cancel_request_test"
		customerID := "cus_cancel_request_test"

		// Create test setup helper and quota plan
		helper := NewTestSetupHelpers(ctx)
		err := createTestQuotaPlan(ctx, planID, "Cancellation Request Test Plan")
		require.NoError(t, err)

		// Set up initial mock subscription and customer
		mockSubscription := helper.CreateMockSubscription(subscriptionID, customerID, &planID)
		helper.SetupMockSubscription(mockSubscription)
		helper.SetupMockCustomer(customerID)

		// Create active subscription
		event := createStripeCheckoutSessionEvent(helper.userID, subscriptionID, planID)
		rec := helper.SendWebhook(event)
		assert.Equal(t, http.StatusNoContent, rec.Code)

		// Verify active subscription
		response := helper.GetSubscriptionStatus()
		assert.True(t, response.IsSubscribed)

		// Create mock subscription with cancellation request (cancel_at set)
		cancelRequestSubscription := helper.CreateMockSubscription(subscriptionID, customerID, &planID)
		// Add cancellation request fields to simulate Stripe's cancellation request
		cancelRequestSubscription.CancelAt = time.Now().Add(30 * 24 * time.Hour).Unix() // 30 days from now
		cancelRequestSubscription.CancellationDetails = &stripe.SubscriptionCancellationDetails{
			Reason: "cancellation_requested",
		}
		helper.SetupMockSubscription(cancelRequestSubscription)

		// Send cancellation request webhook (should be ignored)
		cancelRequestEvent := createStripeSubscriptionEvent(
			pluginGatewayStripe.EventTypeSubscriptionUpdated,
			subscriptionID,
			customerID,
			helper.userID,
			&planID,
		)
		rec = helper.SendWebhook(cancelRequestEvent)
		assert.Equal(t, http.StatusNoContent, rec.Code)

		// Verify subscription is still active (cancellation request was ignored)
		response = helper.GetSubscriptionStatus()
		assert.True(t, response.IsSubscribed)
		assert.Equal(t, "stripe", response.GatewayType)
		require.NotNil(t, response.PlanID)
		assert.Equal(t, planID, *response.PlanID)
	})
}

// TestSubscriptionCancellation_CancellationRequestWithCancelAtZero tests that subscriptions with cancel_at=0 are processed normally
func TestSubscriptionCancellation_CancellationRequestWithCancelAtZero(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		planID := uint(78)
		subscriptionID := "sub_cancel_at_zero_test"
		customerID := "cus_cancel_at_zero_test"

		// Create test setup helper and quota plan
		helper := NewTestSetupHelpers(ctx)
		err := createTestQuotaPlan(ctx, planID, "Cancel At Zero Test Plan")
		require.NoError(t, err)

		// Set up initial mock subscription and customer
		mockSubscription := helper.CreateMockSubscription(subscriptionID, customerID, &planID)
		helper.SetupMockSubscription(mockSubscription)
		helper.SetupMockCustomer(customerID)

		// Create active subscription
		event := createStripeCheckoutSessionEvent(helper.userID, subscriptionID, planID)
		rec := helper.SendWebhook(event)
		assert.Equal(t, http.StatusNoContent, rec.Code)

		// Verify active subscription
		response := helper.GetSubscriptionStatus()
		assert.True(t, response.IsSubscribed)

		// Create mock subscription with cancel_at=0 (not a cancellation request)
		normalUpdateSubscription := helper.CreateMockSubscription(subscriptionID, customerID, &planID)
		normalUpdateSubscription.CancelAt = 0 // Explicitly set to 0 (not a cancellation)
		helper.SetupMockSubscription(normalUpdateSubscription)

		// Send normal update webhook (should be processed)
		updateEvent := createStripeSubscriptionEvent(
			pluginGatewayStripe.EventTypeSubscriptionUpdated,
			subscriptionID,
			customerID,
			helper.userID,
			&planID,
		)
		rec = helper.SendWebhook(updateEvent)
		assert.Equal(t, http.StatusNoContent, rec.Code)

		// Verify subscription is still active (normal update was processed)
		response = helper.GetSubscriptionStatus()
		assert.True(t, response.IsSubscribed)
		assert.Equal(t, "stripe", response.GatewayType)
		require.NotNil(t, response.PlanID)
		assert.Equal(t, planID, *response.PlanID)
	})
}

// TestSubscriptionCancellation_CancellationRequestWithDifferentReason tests that only "cancellation_requested" reason is ignored
func TestSubscriptionCancellation_CancellationRequestWithDifferentReason(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		planID := uint(79)
		subscriptionID := "sub_different_reason_test"
		customerID := "cus_different_reason_test"

		// Create test setup helper and quota plan
		helper := NewTestSetupHelpers(ctx)
		err := createTestQuotaPlan(ctx, planID, "Different Reason Test Plan")
		require.NoError(t, err)

		// Set up initial mock subscription and customer
		mockSubscription := helper.CreateMockSubscription(subscriptionID, customerID, &planID)
		helper.SetupMockSubscription(mockSubscription)
		helper.SetupMockCustomer(customerID)

		// Create active subscription
		event := createStripeCheckoutSessionEvent(helper.userID, subscriptionID, planID)
		rec := helper.SendWebhook(event)
		assert.Equal(t, http.StatusNoContent, rec.Code)

		// Verify active subscription
		response := helper.GetSubscriptionStatus()
		assert.True(t, response.IsSubscribed)

		// Create mock subscription with different cancellation reason (should be processed)
		differentReasonSubscription := helper.CreateMockSubscription(subscriptionID, customerID, &planID)
		differentReasonSubscription.CancellationDetails = &stripe.SubscriptionCancellationDetails{
			Reason: "payment_failed", // Different reason, should be processed
		}
		helper.SetupMockSubscription(differentReasonSubscription)

		// Send update webhook with different reason (should be processed)
		updateEvent := createStripeSubscriptionEvent(
			pluginGatewayStripe.EventTypeSubscriptionUpdated,
			subscriptionID,
			customerID,
			helper.userID,
			&planID,
		)
		rec = helper.SendWebhook(updateEvent)
		assert.Equal(t, http.StatusNoContent, rec.Code)

		// Verify subscription is still active (different reason was processed normally)
		response = helper.GetSubscriptionStatus()
		assert.True(t, response.IsSubscribed)
		assert.Equal(t, "stripe", response.GatewayType)
		require.NotNil(t, response.PlanID)
		assert.Equal(t, planID, *response.PlanID)
	})
}

// TestSubscriptionCancellation_CancellationUndoFlow tests that cancellation requests can be undone before deletion
func TestSubscriptionCancellation_CancellationUndoFlow(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		planID := uint(80)
		subscriptionID := "sub_cancel_undo_test"
		customerID := "cus_cancel_undo_test"

		// Create test setup helper and quota plan
		helper := NewTestSetupHelpers(ctx)
		err := createTestQuotaPlan(ctx, planID, "Cancellation Undo Test Plan")
		require.NoError(t, err)

		// Set up initial mock subscription and customer
		mockSubscription := helper.CreateMockSubscription(subscriptionID, customerID, &planID)
		helper.SetupMockSubscription(mockSubscription)
		helper.SetupMockCustomer(customerID)

		// Create active subscription
		event := createStripeCheckoutSessionEvent(helper.userID, subscriptionID, planID)
		rec := helper.SendWebhook(event)
		assert.Equal(t, http.StatusNoContent, rec.Code)

		// Verify active subscription
		response := helper.GetSubscriptionStatus()
		assert.True(t, response.IsSubscribed)

		// Step 1: Send cancellation request (should be ignored)
		cancelRequestSubscription := helper.CreateMockSubscription(subscriptionID, customerID, &planID)
		cancelRequestSubscription.CancelAt = time.Now().Add(30 * 24 * time.Hour).Unix() // 30 days from now
		cancelRequestSubscription.CancellationDetails = &stripe.SubscriptionCancellationDetails{
			Reason: "cancellation_requested",
		}
		helper.SetupMockSubscription(cancelRequestSubscription)

		cancelRequestEvent := createStripeSubscriptionEvent(
			pluginGatewayStripe.EventTypeSubscriptionUpdated,
			subscriptionID,
			customerID,
			helper.userID,
			&planID,
		)
		rec = helper.SendWebhook(cancelRequestEvent)
		assert.Equal(t, http.StatusNoContent, rec.Code)

		// Verify subscription is still active after cancellation request
		response = helper.GetSubscriptionStatus()
		assert.True(t, response.IsSubscribed)

		// Step 2: Undo the cancellation request (remove cancel_at and cancellation details)
		undoSubscription := helper.CreateMockSubscription(subscriptionID, customerID, &planID)
		undoSubscription.CancelAt = 0 // No cancellation scheduled
		// No CancellationDetails - cancellation was undone
		helper.SetupMockSubscription(undoSubscription)

		undoEvent := createStripeSubscriptionEvent(
			pluginGatewayStripe.EventTypeSubscriptionUpdated,
			subscriptionID,
			customerID,
			helper.userID,
			&planID,
		)
		rec = helper.SendWebhook(undoEvent)
		assert.Equal(t, http.StatusNoContent, rec.Code)

		// Verify subscription is still active after undo
		response = helper.GetSubscriptionStatus()
		assert.True(t, response.IsSubscribed)
		assert.Equal(t, "stripe", response.GatewayType)
		require.NotNil(t, response.PlanID)
		assert.Equal(t, planID, *response.PlanID)
	})
}

// TestCustomerPortal_ActiveSubscriber tests customer portal access for active subscriber via HTTP API
func TestCustomerPortal_ActiveSubscriber(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		planID := uint(95)
		subscriptionID := "sub_portal_test"
		customerID := "cus_test_" + subscriptionID

		// Create test setup helper and quota plan
		helper := NewTestSetupHelpers(ctx)
		err := createTestQuotaPlan(ctx, planID, "Portal Test Plan")
		require.NoError(t, err)

		// Set up mock subscription and customer
		mockSubscription := helper.CreateMockSubscription(subscriptionID, customerID, &planID)
		helper.SetupMockSubscription(mockSubscription)
		helper.SetupMockCustomer(customerID)
		helper.SetupMockBillingPortal(customerID)

		// Create active subscription
		event := createStripeCheckoutSessionEvent(helper.userID, subscriptionID, planID)
		rec := helper.SendWebhook(event)
		assert.Equal(t, http.StatusNoContent, rec.Code)

		// Request customer portal URL
		response := helper.RequestCustomerPortal("https://example.com/return")
		assert.NotEmpty(t, response.URL)
		assert.Contains(t, response.URL, "billing.stripe.com")
	})
}

// TestCustomerPortal_InactiveUser tests customer portal access denied for inactive user via HTTP API
func TestCustomerPortal_InactiveUser(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		// Create test setup helper
		helper := NewTestSetupHelpers(ctx)

		// Request customer portal URL without active subscription
		requestBody := billingDTO.CustomerPortalRequest{
			ReturnURL: "https://example.com/return",
		}
		bodyBytes, err := json.Marshal(requestBody)
		require.NoError(t, err, "failed to marshal customer portal request")

		req := ctx.NewAPIRequest(http.MethodPost, "/api/account/billing/customer-portal", bodyBytes)
		req.Header.Set("Authorization", "Bearer "+helper.token)
		req.Header.Set("Content-Type", "application/json")

		rec := httptest.NewRecorder()
		ctx.Router().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)

		var response map[string]interface{}
		err = json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)

		// Safe type assertion for error field
		errorVal, ok := response["error"]
		require.True(t, ok, "expected error field in response")
		errorStr, ok := errorVal.(string)
		require.True(t, ok, "expected error to be a string")
		assert.Contains(t, errorStr, "no active subscription found")
	})
}

// TestCustomerPortal_MissingReturnURL tests customer portal missing return URL via HTTP API
func TestCustomerPortal_MissingReturnURL(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		// Create test setup helper
		helper := NewTestSetupHelpers(ctx)

		// Request customer portal URL without return_url
		requestBody := billingDTO.CustomerPortalRequest{
			ReturnURL: "", // Empty return URL to trigger validation error
		}
		bodyBytes, err := json.Marshal(requestBody)
		require.NoError(t, err, "failed to marshal customer portal request")

		req := ctx.NewAPIRequest(http.MethodPost, "/api/account/billing/customer-portal", bodyBytes)
		req.Header.Set("Authorization", "Bearer "+helper.token)
		req.Header.Set("Content-Type", "application/json")

		rec := httptest.NewRecorder()
		ctx.Router().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)

		var response map[string]interface{}
		err = json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)

		// Safe type assertion for error field
		errorVal, ok := response["error"]
		require.True(t, ok, "expected error field in response")
		errorStr, ok := errorVal.(string)
		require.True(t, ok, "expected error to be a string")
		assert.Contains(t, errorStr, "validation failed")
	})
}
