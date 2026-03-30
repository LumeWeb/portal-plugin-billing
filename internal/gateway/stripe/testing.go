package stripe

import (
	"context"
	"fmt"
	"io/fs"
	"time"

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
	V1ProductsService                   *MockProducts
	V1PricesService                     *MockPrices
	V1BillingPortalConfigurationsService *MockBillingPortalConfigurations
	BillingPortalSessionsService        *MockBillingPortalSessions
	CustomersService                    *MockCustomers
	SubscriptionsService                *MockSubscriptions
	V1CheckoutSessionsService            *MockCheckoutSessions
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

// V1Products returns the mock products service
func (m *MockStripeClient) V1Products() Products {
	if m.V1ProductsService == nil {
		panic("MockStripeClient.V1Products called but V1ProductsService is nil")
	}
	return m.V1ProductsService
}

// V1Prices returns the mock prices service
func (m *MockStripeClient) V1Prices() Prices {
	if m.V1PricesService == nil {
		panic("MockStripeClient.V1Prices called but V1PricesService is nil")
	}
	return m.V1PricesService
}

// V1BillingPortalConfigurations returns the mock billing portal configurations service
func (m *MockStripeClient) V1BillingPortalConfigurations() BillingPortalConfigurations {
	if m.V1BillingPortalConfigurationsService == nil {
		panic("MockStripeClient.V1BillingPortalConfigurations called but V1BillingPortalConfigurationsService is nil")
	}
	return m.V1BillingPortalConfigurationsService
}

// V1Subscriptions returns the mock subscriptions service
func (m *MockStripeClient) V1Subscriptions() Subscriptions {
	if m.SubscriptionsService == nil {
		panic("MockStripeClient.V1Subscriptions called but SubscriptionsService is nil")
	}
	return m.SubscriptionsService
}

// V1CheckoutSessions returns the mock checkout sessions service
func (m *MockStripeClient) V1CheckoutSessions() CheckoutSessions {
	if m.V1CheckoutSessionsService == nil {
		panic("MockStripeClient.V1CheckoutSessions called but V1CheckoutSessionsService is nil")
	}
	return m.V1CheckoutSessionsService
}

// NewMockStripeClient creates a new MockStripeClient with sensible default mock services
func NewMockStripeClient() *MockStripeClient {
	return &MockStripeClient{
		V1ProductsService:                   &MockProducts{},
		V1PricesService:                     &MockPrices{},
		V1BillingPortalConfigurationsService: &MockBillingPortalConfigurations{},
		BillingPortalSessionsService:        &MockBillingPortalSessions{},
		CustomersService:                    &MockCustomers{},
		SubscriptionsService:                &MockSubscriptions{},
		V1CheckoutSessionsService:            &MockCheckoutSessions{},
	}
}

// SetupCustomerCreate configures the mock to successfully create a customer
func (m *MockStripeClient) SetupCustomerCreate(customer *stripe.Customer) {
	m.CustomersService.On("Create", mock.Anything, mock.AnythingOfType("*stripe.CustomerCreateParams")).Return(customer, nil)
}

// SetupCustomerCreateError configures the mock to return an error when creating a customer
func (m *MockStripeClient) SetupCustomerCreateError(err error) {
	m.CustomersService.On("Create", mock.Anything, mock.AnythingOfType("*stripe.CustomerCreateParams")).Return((*stripe.Customer)(nil), err)
}

// SetupCheckoutSessionCreate configures the mock to successfully create a checkout session
func (m *MockStripeClient) SetupCheckoutSessionCreate(session *stripe.CheckoutSession) {
	m.V1CheckoutSessionsService.On("Create", mock.Anything, mock.AnythingOfType("*stripe.CheckoutSessionCreateParams")).Return(session, nil)
}

// SetupCheckoutSessionCreateError configures the mock to return an error when creating a checkout session
func (m *MockStripeClient) SetupCheckoutSessionCreateError(err error) {
	m.V1CheckoutSessionsService.On("Create", mock.Anything, mock.AnythingOfType("*stripe.CheckoutSessionCreateParams")).Return((*stripe.CheckoutSession)(nil), err)
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

// MockCheckoutSessions is a mock implementation of the checkout sessions service
type MockCheckoutSessions struct {
	mock.Mock
}

// Create mocks the Stripe checkout session creation
func (m *MockCheckoutSessions) Create(ctx context.Context, params *stripe.CheckoutSessionCreateParams) (*stripe.CheckoutSession, error) {
	args := m.Called(ctx, params)
	session, ok := args.Get(0).(*stripe.CheckoutSession)
	if !ok && args.Get(0) != nil {
		return nil, fmt.Errorf("mock setup error: expected *stripe.CheckoutSession, got %T", args.Get(0))
	}
	return session, args.Error(1)
}

// Retrieve mocks the Stripe checkout session retrieval
func (m *MockCheckoutSessions) Retrieve(ctx context.Context, id string, params *stripe.CheckoutSessionRetrieveParams) (*stripe.CheckoutSession, error) {
	args := m.Called(ctx, id, params)
	session, ok := args.Get(0).(*stripe.CheckoutSession)
	if !ok && args.Get(0) != nil {
		return nil, fmt.Errorf("mock setup error: expected *stripe.CheckoutSession, got %T", args.Get(0))
	}
	return session, args.Error(1)
}

// MockCustomers is a mock implementation of the customers service
type MockCustomers struct {
	mock.Mock
}

// Create mocks the Stripe customer creation
func (m *MockCustomers) Create(ctx context.Context, params *stripe.CustomerCreateParams) (*stripe.Customer, error) {
	args := m.Called(ctx, params)
	customer, ok := args.Get(0).(*stripe.Customer)
	if !ok && args.Get(0) != nil {
		return nil, fmt.Errorf("mock setup error: expected *stripe.Customer, got %T", args.Get(0))
	}
	return customer, args.Error(1)
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
		V1ProductsService:                   &MockProducts{},
		V1PricesService:                     &MockPrices{},
		V1BillingPortalConfigurationsService: &MockBillingPortalConfigurations{},
		BillingPortalSessionsService:        &MockBillingPortalSessions{},
		CustomersService:                    &MockCustomers{},
		SubscriptionsService:                &MockSubscriptions{},
		V1CheckoutSessionsService:           &MockCheckoutSessions{},
	}

	// Create mock retrievers
	mockSubRetriever := &MockSubscriptionRetriever{}
	mockCustomerRetriever := &MockCustomerRetriever{}

	// Create the gateway
	gateway := New(ctx.Logger(), webhookSecret, secretKey, mockQuota, mockUsers, mockBilling, nil, nil)

	// Replace the real client and retrievers with mocks
	gateway.stripeClient = mockStripeClient
	gateway.subService = mockSubRetriever
	gateway.customerService = mockCustomerRetriever

	return gateway, mockStripeClient, mockSubRetriever, mockCustomerRetriever
}

// MockProducts is a mock implementation of the products service
type MockProducts struct {
	mock.Mock
}

// Create mocks the Stripe product creation
func (m *MockProducts) Create(ctx context.Context, params *stripe.ProductCreateParams) (*stripe.Product, error) {
	args := m.Called(ctx, params)
	product, ok := args.Get(0).(*stripe.Product)
	if !ok && args.Get(0) != nil {
		return nil, fmt.Errorf("mock setup error: expected *stripe.Product, got %T", args.Get(0))
	}
	return product, args.Error(1)
}

// Retrieve mocks the Stripe product retrieval
func (m *MockProducts) Retrieve(ctx context.Context, id string, params *stripe.ProductRetrieveParams) (*stripe.Product, error) {
	args := m.Called(ctx, id, params)
	product, ok := args.Get(0).(*stripe.Product)
	if !ok && args.Get(0) != nil {
		return nil, fmt.Errorf("mock setup error: expected *stripe.Product, got %T", args.Get(0))
	}
	return product, args.Error(1)
}

// MockPrices is a mock implementation of the prices service
type MockPrices struct {
	mock.Mock
}

// Create mocks the Stripe price creation
func (m *MockPrices) Create(ctx context.Context, params *stripe.PriceCreateParams) (*stripe.Price, error) {
	args := m.Called(ctx, params)
	price, ok := args.Get(0).(*stripe.Price)
	if !ok && args.Get(0) != nil {
		return nil, fmt.Errorf("mock setup error: expected *stripe.Price, got %T", args.Get(0))
	}
	return price, args.Error(1)
}

// Retrieve mocks the Stripe price retrieval
func (m *MockPrices) Retrieve(ctx context.Context, id string, params *stripe.PriceRetrieveParams) (*stripe.Price, error) {
	args := m.Called(ctx, id, params)
	price, ok := args.Get(0).(*stripe.Price)
	if !ok && args.Get(0) != nil {
		return nil, fmt.Errorf("mock setup error: expected *stripe.Price, got %T", args.Get(0))
	}
	return price, args.Error(1)
}

// MockBillingPortalConfigurations is a mock implementation of the billing portal configurations service
type MockBillingPortalConfigurations struct {
	mock.Mock
}

// Create mocks the Stripe billing portal configuration creation
func (m *MockBillingPortalConfigurations) Create(ctx context.Context, params *stripe.BillingPortalConfigurationCreateParams) (*stripe.BillingPortalConfiguration, error) {
	args := m.Called(ctx, params)
	config, ok := args.Get(0).(*stripe.BillingPortalConfiguration)
	if !ok && args.Get(0) != nil {
		return nil, fmt.Errorf("mock setup error: expected *stripe.BillingPortalConfiguration, got %T", args.Get(0))
	}
	return config, args.Error(1)
}

// MockFS is a mock file system for testing purposes.
// It implements both fs.FS and fs.ReadFileFS interfaces.
type MockFS struct {
	Files map[string]string
}

// Open satisfies fs.FS interface
func (m *MockFS) Open(name string) (fs.File, error) {
	content, exists := m.Files[name]
	if !exists {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return &MockFile{name: name, data: []byte(content)}, nil
}

// ReadFile satisfies fs.ReadFileFS interface
func (m *MockFS) ReadFile(name string) ([]byte, error) {
	content, exists := m.Files[name]
	if !exists {
		return nil, &fs.PathError{Op: "readfile", Path: name, Err: fs.ErrNotExist}
	}
	return []byte(content), nil
}

// MockFile is a mock file implementation for testing
type MockFile struct {
	name  string
	data  []byte
	pos   int64
	isDir bool
}

func (m *MockFile) Stat() (fs.FileInfo, error) {
	return &MockFileInfo{name: m.name}, nil
}

func (m *MockFile) Read(p []byte) (int, error) {
	if m.pos >= int64(len(m.data)) {
		return 0, fmt.Errorf("EOF")
	}
	n := copy(p, m.data[m.pos:])
	m.pos += int64(n)
	return n, nil
}

func (m *MockFile) Close() error {
	return nil
}

// MockFileInfo is a mock file info for testing
type MockFileInfo struct {
	name string
}

func (m *MockFileInfo) Name() string       { return m.name }
func (m *MockFileInfo) Size() int64        { return 0 }
func (m *MockFileInfo) Mode() fs.FileMode  { return 0 }
func (m *MockFileInfo) ModTime() time.Time { return time.Time{} }
func (m *MockFileInfo) Sys() any           { return nil }
func (m *MockFileInfo) IsDir() bool        { return false }
