package stripe

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stripe/stripe-go/v83"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	billingModels "go.lumeweb.com/portal-plugin-billing/internal/db/models"
	quotaCore "go.lumeweb.com/portal-plugin-quota/core"
	coreTesting "go.lumeweb.com/portal/core/testing"
)

func TestStripeGateway_SyncPlan_Success(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockStripeClient := &MockStripeClient{
			V1ProductsService:                   &MockProducts{},
			V1PricesService:                     &MockPrices{},
			V1BillingPortalConfigurationsService: &MockBillingPortalConfigurations{},
		}

		monthlyPrice := 19.99
		yearlyPrice := 199.99
		planInfo := &pluginCore.PricingPlanInfo{
			ID:              1,
			Name:            "Test Plan",
			Description:     "Test Description",
			Currency:        "usd",
			MonthlyPriceUSD: &monthlyPrice,
			YearlyPriceUSD:  &yearlyPrice,
			IsActive:        true,
			IsPublic:        true,
		}

		mockPricingService := &pluginCore.MockPricingService{}

		mockStripeClient.V1ProductsService.
			On("Create", mock.Anything, mock.AnythingOfType("*stripe.ProductCreateParams")).
			Return(&stripe.Product{ID: "prod_123", Name: "Test Plan"}, nil)

		mockStripeClient.V1PricesService.
			On("Create", mock.Anything, mock.AnythingOfType("*stripe.PriceCreateParams")).
			Return(&stripe.Price{ID: "price_monthly_123"}, nil).
			Twice()

		mockPricingService.
			On("GetPriceLinesForPlan", mock.Anything, planInfo.ID).
			Return([]*billingModels.PriceLinePlan{}, nil)

		mockQuota := &quotaCore.MockQuotaService{}
		mockUsers := &coreTesting.MockUserService{}
		mockBilling := &pluginCore.MockBillingService{}
		mockCredit := &pluginCore.MockCreditService{}

		gateway := New(ctx.Logger(), TestWebhookSecret, "test_key", mockQuota, mockUsers, mockBilling, mockPricingService, mockCredit)
		gateway.stripeClient = mockStripeClient

		result, err := gateway.SyncPlan(context.Background(), planInfo)

		assert.NoError(t, err)
		assert.True(t, result.Success)
		assert.Equal(t, "prod_123", result.ProductID)
		assert.NotEmpty(t, result.MonthlyPriceID)
		assert.NotEmpty(t, result.YearlyPriceID)

		mockStripeClient.V1ProductsService.AssertExpectations(t)
		mockStripeClient.V1PricesService.AssertExpectations(t)
	})
}

func TestStripeGateway_SyncPlan_NilPricingService(t *testing.T) {
	ctx, _ := coreTesting.NewTestContext(t)
	gateway := New(ctx.Logger(), TestWebhookSecret, "test_key", nil, nil, nil, nil, nil)

	planInfo := &pluginCore.PricingPlanInfo{
		ID:       1,
		Name:     "Test Plan",
		Currency: "usd",
	}

	result, err := gateway.SyncPlan(context.Background(), planInfo)

	assert.Error(t, err)
	assert.False(t, result.Success)
	assert.Contains(t, err.Error(), "pricing service not configured")
}

func TestStripeGateway_SupportsProductSync(t *testing.T) {
	ctx, _ := coreTesting.NewTestContext(t)
	gateway := New(ctx.Logger(), TestWebhookSecret, "", nil, nil, nil, nil, nil)

	assert.True(t, gateway.SupportsProductSync())
}

func TestStripeGateway_SupportsPriceUpdates(t *testing.T) {
	ctx, _ := coreTesting.NewTestContext(t)
	gateway := New(ctx.Logger(), TestWebhookSecret, "", nil, nil, nil, nil, nil)

	assert.True(t, gateway.SupportsPriceUpdates())
}

func TestStripeGateway_SupportsPlanDeletion(t *testing.T) {
	ctx, _ := coreTesting.NewTestContext(t)
	gateway := New(ctx.Logger(), TestWebhookSecret, "", nil, nil, nil, nil, nil)

	assert.False(t, gateway.SupportsPlanDeletion())
}

func TestStripeGateway_RequiredPricingFields(t *testing.T) {
	ctx, _ := coreTesting.NewTestContext(t)
	gateway := New(ctx.Logger(), TestWebhookSecret, "", nil, nil, nil, nil, nil)

	fields := gateway.RequiredPricingFields()
	assert.Equal(t, []string{"name", "amount", "currency"}, fields)
}

func TestStripeGateway_GetName(t *testing.T) {
	ctx, _ := coreTesting.NewTestContext(t)
	gateway := New(ctx.Logger(), TestWebhookSecret, "", nil, nil, nil, nil, nil)

	name := gateway.GetName(context.Background())
	assert.Equal(t, "Stripe", name)
}

func TestStripeGateway_GetDescription(t *testing.T) {
	ctx, _ := coreTesting.NewTestContext(t)
	gateway := New(ctx.Logger(), TestWebhookSecret, "", nil, nil, nil, nil, nil)

	description := gateway.GetDescription(context.Background())
	assert.Equal(t, "Industry-leading payment processor", description)
}

func TestStripeGateway_GetCheckoutUI(t *testing.T) {
	ctx, _ := coreTesting.NewTestContext(t)
	gateway := New(ctx.Logger(), TestWebhookSecret, "", nil, nil, nil, nil, nil)

	ui, err := gateway.GetCheckoutUI(context.Background(), 123, 456)

	// Should fail with service not configured error since no services are provided
	assert.Error(t, err)
	assert.Nil(t, ui)
	assert.Contains(t, err.Error(), "not configured")
}

func TestStripeGateway_GetCustomerPortalMetadata(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		gateway := New(ctx.Logger(), TestWebhookSecret, "", nil, nil, nil, nil, nil)

		metadata, err := gateway.GetCustomerPortalMetadata(context.Background(), 123)

		assert.NoError(t, err)
		assert.NotNil(t, metadata)
		// Expect empty metadata as currently implemented
		assert.Equal(t, map[string]any{}, metadata)
	})
}


