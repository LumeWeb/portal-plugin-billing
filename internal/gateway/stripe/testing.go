package stripe

import (
	"context"
	"fmt"

	"github.com/stretchr/testify/mock"
	"github.com/stripe/stripe-go/v83"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	quotaCore "go.lumeweb.com/portal-plugin-quota/core"
	"go.lumeweb.com/portal/core"
	coreTesting "go.lumeweb.com/portal/core/testing"
)

// MockStripeClient is a mock implementation of the Client for testing purposes.
// It allows tests to control the responses from Stripe API calls without making actual
// API requests.
type MockStripeClient struct {
	mock.Mock
	BillingPortalSessionsService *MockBillingPortalSessions
	CustomersService             *MockCustomers
	SubscriptionsService         *MockSubscriptions
}

// V1BillingPortalSessions returns the mock billing portal sessions service
func (m *MockStripeClient) V1BillingPortalSessions() BillingPortalSessions {
	if m.BillingPortalSessionsService == nil {
		panic("MockStripeClient.V1BillingPortalSessions called but BillingPortalSessionsService is nil")
	}
	return m.BillingPortalSessionsService
}

// V1Customers returns the mock customers service
func (m *MockStripeClient) V1Customers() Customers {
	if m.CustomersService == nil {
		panic("MockStripeClient.V1Customers called but CustomersService is nil")
	}
	return m.CustomersService
}

// V1Subscriptions returns the mock subscriptions service
func (m *MockStripeClient) V1Subscriptions() Subscriptions {
	if m.SubscriptionsService == nil {
		panic("MockStripeClient.V1Subscriptions called but SubscriptionsService is nil")
	}
	return m.SubscriptionsService
}

// NewMockStripeClient creates a new MockStripeClient with sensible default mock services
func NewMockStripeClient() *MockStripeClient {
	return &MockStripeClient{
		BillingPortalSessionsService: &MockBillingPortalSessions{},
		CustomersService:             &MockCustomers{},
		SubscriptionsService:         &MockSubscriptions{},
	}
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

// Retrieve mocks the Stripe customer retrieval
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

// Retrieve mocks the Stripe subscription retrieval
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
	m.On("Get", mock.Anything, subscription.ID, mock.MatchedBy(func(params *stripe.SubscriptionRetrieveParams) bool {
		if params == nil {
			return false
		}
		for _, expand := range params.Expand {
			if expand != nil && *expand == "items.data.price.product" {
				return true
			}
		}
		return false
	})).Return(subscription, nil)
}

// SetupGetError configures the mock to return an error for subscription retrieval.
// This helper method simplifies test setup for error handling scenarios.
//
// Parameters:
// - subscriptionID: The ID of the subscription that should fail
// - err: The error to return
func (m *MockSubscriptionRetriever) SetupGetError(subscriptionID string, err error) {
	m.On("Get", mock.Anything, subscriptionID, mock.AnythingOfType("*stripe.SubscriptionRetrieveParams")).Return((*stripe.Subscription)(nil), err)
}

// MockCustomerRetriever is a mock implementation of the CustomerRetriever interface
// for testing purposes. It allows tests to control the customer data returned without
// making actual API calls to Stripe.
type MockCustomerRetriever struct {
	mock.Mock
}

// Get is the mock implementation of the CustomerRetriever.Get method.
// It records the call and returns predefined values set up by the test.
func (m *MockCustomerRetriever) Get(ctx context.Context, id string, params *stripe.CustomerRetrieveParams) (*stripe.Customer, error) {
	args := m.Called(ctx, id, params)
	customer, ok := args.Get(0).(*stripe.Customer)
	if !ok && args.Get(0) != nil {
		return nil, fmt.Errorf("mock setup error: expected *stripe.Customer, got %T", args.Get(0))
	}
	return customer, args.Error(1)
}

// SetupGetSuccess configures the mock to return a successful customer retrieval.
func (m *MockCustomerRetriever) SetupGetSuccess(customer *stripe.Customer) {
	m.On("Get", mock.Anything, customer.ID, mock.AnythingOfType("*stripe.CustomerRetrieveParams")).Return(customer, nil)
}

// SetupGetError configures the mock to return an error for customer retrieval.
func (m *MockCustomerRetriever) SetupGetError(customerID string, err error) {
	m.On("Get", mock.Anything, customerID, mock.AnythingOfType("*stripe.CustomerRetrieveParams")).Return((*stripe.Customer)(nil), err)
}

// CreateMockStripeGateway creates a fully configured mock Stripe gateway for testing
// This factory function sets up all necessary mock services and returns a ready-to-use gateway
func CreateMockStripeGateway(
	ctx coreTesting.TestContext,
	webhookSecret string,
	secretKey string,
) (*StripeGateway, *MockStripeClient, *MockSubscriptionRetriever, *MockCustomerRetriever) {
	// Create mock services
	mockQuota := core.GetService[quotaCore.QuotaService](ctx, quotaCore.QUOTA_SERVICE)
	mockUsers := core.GetService[core.UserService](ctx, core.USER_SERVICE)
	mockBilling := core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE)

	// Add explicit nil checks for mock services
	if mockQuota == nil {
		panic("CreateMockStripeGateway: missing QuotaService in test context")
	}
	if mockUsers == nil {
		panic("CreateMockStripeGateway: missing UserService in test context")
	}
	if mockBilling == nil {
		panic("CreateMockStripeGateway: missing BillingService in test context")
	}

	// Create mock Stripe client and services
	mockStripeClient := &MockStripeClient{
		BillingPortalSessionsService: &MockBillingPortalSessions{},
		CustomersService:             &MockCustomers{},
		SubscriptionsService:         &MockSubscriptions{},
	}

	// Create mock retrievers
	mockSubRetriever := &MockSubscriptionRetriever{}
	mockCustomerRetriever := &MockCustomerRetriever{}

	// Create the gateway
	gateway := New(ctx.Logger(), webhookSecret, secretKey, mockQuota, mockUsers, mockBilling)

	// Replace the real client and retrievers with mocks
	gateway.stripeClient = mockStripeClient
	gateway.subService = mockSubRetriever
	gateway.customerService = mockCustomerRetriever

	return gateway, mockStripeClient, mockSubRetriever, mockCustomerRetriever
}
