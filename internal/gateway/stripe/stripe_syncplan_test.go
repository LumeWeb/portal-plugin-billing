package stripe

import (
	"context"
	"testing"

	"github.com/stripe/stripe-go/v85"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	billingModels "go.lumeweb.com/portal-plugin-billing/internal/db/models"
	"go.lumeweb.com/portal-plugin-billing/pkg/subscription"
	"go.lumeweb.com/portal/core"
	coreTesting "go.lumeweb.com/portal/core/testing"
	"gorm.io/gorm"
)

// TestStripeGateway_SyncPlan_MonthlyCadence tests SyncPlan with monthly pricing period
func TestStripeGateway_SyncPlan_MonthlyCadence(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		_, _, _, mockPricing := setupMockServices(ctx)
		mockCredit := core.GetService[*pluginCore.MockCreditService](ctx, pluginCore.CREDIT_SERVICE)

		// Mock pricing service to return pricing plan periods
		periods := []*billingModels.PricingPlanPeriod{
			{
				Model:         gorm.Model{ID: 1},
				PricingPlanID: 1,
				Cadence:       "monthly",
				PriceUSD:      9.99,
				QuotaPlanID:   100,
			},
		}
		mockPricing.EXPECT().GetPricingPlanPeriods(mock.Anything, uint(1)).Return(periods, nil)
		mockPricing.EXPECT().GetPriceLinesForPlan(mock.Anything, uint(1)).Return([]*billingModels.PriceLinePlan{}, nil)
		mockPricing.EXPECT().GetGatewayProductMappingsByPlan(mock.Anything, uint(1)).Return([]*billingModels.GatewayProductMapping{}, nil)
		mockPricing.EXPECT().CreateGatewayProductMapping(mock.Anything, mock.Anything).Return(nil).Once()

		// Create gateway
		cfg := testConfig()
		gw := NewWithConfig(ctx.Logger(), ctx, cfg, nil, nil, nil, mockPricing, mockCredit)

		// Create Stripe mock
		mockClient := NewMockStripeClient()
		mockClient.V1ProductsService.On("Create", mock.Anything, mock.AnythingOfType("*stripe.ProductCreateParams")).Return(&stripe.Product{ID: "prod_test_123"}, nil)
		mockClient.V1PricesService.On("Create", mock.Anything, mock.AnythingOfType("*stripe.PriceCreateParams")).Return(&stripe.Price{ID: "price_monthly_test_123"}, nil)
		mockClient.V1ProductsService.On("Update", mock.Anything, "prod_test_123", mock.AnythingOfType("*stripe.ProductUpdateParams")).Return(&stripe.Product{ID: "prod_test_123"}, nil)
		gw.stripeClient = mockClient

		// Call SyncPlan
		planInfo := &pluginCore.PricingPlanInfo{
			ID:          1,
			Name:        "Test Plan",
			Description: "Test description",
			Currency:    "USD",
			IsActive:    true,
			IsPublic:    false,
		}

		result, err := gw.SyncPlan(context.Background(), planInfo)

		assert.NoError(t, err)
		assert.True(t, result.Success)
		assert.Equal(t, "prod_test_123", result.ProductID)
		assert.Len(t, result.RemotePriceIDs, 1)
		assert.Equal(t, uint(1), result.RemotePriceIDs[0].PricingPlanPeriodID)
		assert.Equal(t, "price_monthly_test_123", result.RemotePriceIDs[0].PriceID)

		// Verify Update was called to set default price
		mockClient.V1ProductsService.AssertCalled(t, "Update", mock.Anything, "prod_test_123", mock.AnythingOfType("*stripe.ProductUpdateParams"))
	})
}

// TestStripeGateway_SyncPlan_YearlyCadence tests SyncPlan with yearly pricing period
func TestStripeGateway_SyncPlan_YearlyCadence(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		_, _, _, mockPricing := setupMockServices(ctx)
		mockCredit := core.GetService[*pluginCore.MockCreditService](ctx, pluginCore.CREDIT_SERVICE)

		// Mock pricing service to return pricing plan periods
		periods := []*billingModels.PricingPlanPeriod{
			{
				Model:         gorm.Model{ID: 2},
				PricingPlanID: 1,
				Cadence:       "yearly",
				PriceUSD:      99.99,
				QuotaPlanID:   100,
			},
		}
		mockPricing.EXPECT().GetPricingPlanPeriods(mock.Anything, uint(1)).Return(periods, nil)
		mockPricing.EXPECT().GetPriceLinesForPlan(mock.Anything, uint(1)).Return([]*billingModels.PriceLinePlan{}, nil)
		mockPricing.EXPECT().GetGatewayProductMappingsByPlan(mock.Anything, uint(1)).Return([]*billingModels.GatewayProductMapping{}, nil)
		mockPricing.EXPECT().CreateGatewayProductMapping(mock.Anything, mock.Anything).Return(nil).Once()

		// Create gateway
		cfg := testConfig()
		gw := NewWithConfig(ctx.Logger(), ctx, cfg, nil, nil, nil, mockPricing, mockCredit)

		// Create Stripe mock
		mockClient := NewMockStripeClient()
		mockClient.V1ProductsService.On("Create", mock.Anything, mock.AnythingOfType("*stripe.ProductCreateParams")).Return(&stripe.Product{ID: "prod_test_123"}, nil)
		mockClient.V1PricesService.On("Create", mock.Anything, mock.AnythingOfType("*stripe.PriceCreateParams")).Return(&stripe.Price{ID: "price_yearly_test_123"}, nil)
		mockClient.V1ProductsService.On("Update", mock.Anything, "prod_test_123", mock.AnythingOfType("*stripe.ProductUpdateParams")).Return(&stripe.Product{ID: "prod_test_123"}, nil)
		gw.stripeClient = mockClient

		// Call SyncPlan
		planInfo := &pluginCore.PricingPlanInfo{
			ID:          1,
			Name:        "Test Plan",
			Description: "Test description",
			Currency:    "USD",
			IsActive:    true,
			IsPublic:    false,
		}

		result, err := gw.SyncPlan(context.Background(), planInfo)

		assert.NoError(t, err)
		assert.True(t, result.Success)
		assert.Equal(t, "prod_test_123", result.ProductID)
		assert.Len(t, result.RemotePriceIDs, 1)
		assert.Equal(t, uint(2), result.RemotePriceIDs[0].PricingPlanPeriodID)
		assert.Equal(t, "price_yearly_test_123", result.RemotePriceIDs[0].PriceID)
	})
}

// TestStripeGateway_SyncPlan_QuarterlyCadence tests SyncPlan with quarterly pricing period (interval_count=3)
func TestStripeGateway_SyncPlan_QuarterlyCadence(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		_, _, _, mockPricing := setupMockServices(ctx)
		mockCredit := core.GetService[*pluginCore.MockCreditService](ctx, pluginCore.CREDIT_SERVICE)

		// Mock pricing service to return pricing plan periods
		periods := []*billingModels.PricingPlanPeriod{
			{
				Model:         gorm.Model{ID: 3},
				PricingPlanID: 1,
				Cadence:       "quarterly",
				PriceUSD:      24.99,
				QuotaPlanID:   100,
			},
		}
		mockPricing.EXPECT().GetPricingPlanPeriods(mock.Anything, uint(1)).Return(periods, nil)
		mockPricing.EXPECT().GetPriceLinesForPlan(mock.Anything, uint(1)).Return([]*billingModels.PriceLinePlan{}, nil)
		mockPricing.EXPECT().GetGatewayProductMappingsByPlan(mock.Anything, uint(1)).Return([]*billingModels.GatewayProductMapping{}, nil)
		mockPricing.EXPECT().CreateGatewayProductMapping(mock.Anything, mock.Anything).Return(nil).Once()

		// Create gateway
		cfg := testConfig()
		gw := NewWithConfig(ctx.Logger(), ctx, cfg, nil, nil, nil, mockPricing, mockCredit)

		// Create Stripe mock
		mockClient := NewMockStripeClient()
		mockClient.V1ProductsService.On("Create", mock.Anything, mock.AnythingOfType("*stripe.ProductCreateParams")).Return(&stripe.Product{ID: "prod_test_123"}, nil)
		mockClient.V1PricesService.On("Create", mock.Anything, mock.AnythingOfType("*stripe.PriceCreateParams")).Return(&stripe.Price{ID: "price_quarterly_test_123"}, nil)
		mockClient.V1ProductsService.On("Update", mock.Anything, "prod_test_123", mock.AnythingOfType("*stripe.ProductUpdateParams")).Return(&stripe.Product{ID: "prod_test_123"}, nil)
		gw.stripeClient = mockClient

		// Call SyncPlan
		planInfo := &pluginCore.PricingPlanInfo{
			ID:          1,
			Name:        "Test Plan",
			Description: "Test description",
			Currency:    "USD",
			IsActive:    true,
			IsPublic:    false,
		}

		result, err := gw.SyncPlan(context.Background(), planInfo)

		assert.NoError(t, err)
		assert.True(t, result.Success)
		assert.Equal(t, "prod_test_123", result.ProductID)
		assert.Len(t, result.RemotePriceIDs, 1)
		assert.Equal(t, uint(3), result.RemotePriceIDs[0].PricingPlanPeriodID)
		assert.Equal(t, "price_quarterly_test_123", result.RemotePriceIDs[0].PriceID)
	})
}

// TestStripeGateway_SyncPlan_WeeklyCadence tests SyncPlan with weekly pricing period
func TestStripeGateway_SyncPlan_WeeklyCadence(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		_, _, _, mockPricing := setupMockServices(ctx)
		mockCredit := core.GetService[*pluginCore.MockCreditService](ctx, pluginCore.CREDIT_SERVICE)

		// Mock pricing service to return pricing plan periods
		periods := []*billingModels.PricingPlanPeriod{
			{
				Model:         gorm.Model{ID: 4},
				PricingPlanID: 1,
				Cadence:       "weekly",
				PriceUSD:      2.49,
				QuotaPlanID:   100,
			},
		}
		mockPricing.EXPECT().GetPricingPlanPeriods(mock.Anything, uint(1)).Return(periods, nil)
		mockPricing.EXPECT().GetPriceLinesForPlan(mock.Anything, uint(1)).Return([]*billingModels.PriceLinePlan{}, nil)
		mockPricing.EXPECT().GetGatewayProductMappingsByPlan(mock.Anything, uint(1)).Return([]*billingModels.GatewayProductMapping{}, nil)
		mockPricing.EXPECT().CreateGatewayProductMapping(mock.Anything, mock.Anything).Return(nil).Once()

		// Create gateway
		cfg := testConfig()
		gw := NewWithConfig(ctx.Logger(), ctx, cfg, nil, nil, nil, mockPricing, mockCredit)

		// Create Stripe mock
		mockClient := NewMockStripeClient()
		mockClient.V1ProductsService.On("Create", mock.Anything, mock.AnythingOfType("*stripe.ProductCreateParams")).Return(&stripe.Product{ID: "prod_test_123"}, nil)
		mockClient.V1PricesService.On("Create", mock.Anything, mock.AnythingOfType("*stripe.PriceCreateParams")).Return(&stripe.Price{ID: "price_weekly_test_123"}, nil)
		mockClient.V1ProductsService.On("Update", mock.Anything, "prod_test_123", mock.AnythingOfType("*stripe.ProductUpdateParams")).Return(&stripe.Product{ID: "prod_test_123"}, nil)
		gw.stripeClient = mockClient

		// Call SyncPlan
		planInfo := &pluginCore.PricingPlanInfo{
			ID:          1,
			Name:        "Test Plan",
			Description: "Test description",
			Currency:    "USD",
			IsActive:    true,
			IsPublic:    false,
		}

		result, err := gw.SyncPlan(context.Background(), planInfo)

		assert.NoError(t, err)
		assert.True(t, result.Success)
		assert.Equal(t, "prod_test_123", result.ProductID)
		assert.Len(t, result.RemotePriceIDs, 1)
		assert.Equal(t, uint(4), result.RemotePriceIDs[0].PricingPlanPeriodID)
		assert.Equal(t, "price_weekly_test_123", result.RemotePriceIDs[0].PriceID)
	})
}

// TestStripeGateway_SyncPlan_RollingCadence tests SyncPlan rejects rolling cadence
func TestStripeGateway_SyncPlan_RollingCadence(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		_, _, _, mockPricing := setupMockServices(ctx)
		mockCredit := core.GetService[*pluginCore.MockCreditService](ctx, pluginCore.CREDIT_SERVICE)

		// Mock pricing service to return pricing plan periods
		periods := []*billingModels.PricingPlanPeriod{
			{
				PricingPlanID: 1,
				Cadence:       "rolling",
				PriceUSD:      29.99,
				QuotaPlanID:   100,
				RollingDays:   new(int), // Rolling days
			},
		}
		mockPricing.EXPECT().GetPriceLinesForPlan(mock.Anything, uint(1)).Return([]*billingModels.PriceLinePlan{}, nil)
		mockPricing.EXPECT().GetPricingPlanPeriods(mock.Anything, uint(1)).Return(periods, nil)

		// Create gateway
		cfg := testConfig()
		gw := NewWithConfig(ctx.Logger(), ctx, cfg, nil, nil, nil, mockPricing, mockCredit)

		// Mock Stripe client to avoid real API calls
		mockClient := NewMockStripeClient()
		mockClient.V1ProductsService.On("Create", mock.Anything, mock.Anything).Return(&stripe.Product{ID: "prod_test_123"}, nil)
		gw.stripeClient = mockClient

		// Call SyncPlan
		planInfo := &pluginCore.PricingPlanInfo{
			ID:          1,
			Name:        "Test Plan",
			Description: "Test description",
			Currency:    "USD",
			IsActive:    true,
			IsPublic:    false,
		}

		result, err := gw.SyncPlan(context.Background(), planInfo)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "rolling periods not supported by Stripe")
		assert.False(t, result.Success)
	})
}

// TestStripeGateway_SyncPlan_MultiplePeriods tests SyncPlan with multiple pricing periods
func TestStripeGateway_SyncPlan_MultiplePeriods(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		_, _, _, mockPricing := setupMockServices(ctx)
		mockCredit := core.GetService[*pluginCore.MockCreditService](ctx, pluginCore.CREDIT_SERVICE)

		// Mock pricing service to return pricing plan periods
		periods := []*billingModels.PricingPlanPeriod{
			{
				Model:         gorm.Model{ID: 10},
				PricingPlanID: 1,
				Cadence:       "monthly",
				PriceUSD:      9.99,
				QuotaPlanID:   100,
			},
			{
				Model:         gorm.Model{ID: 11},
				PricingPlanID: 1,
				Cadence:       "yearly",
				PriceUSD:      99.99,
				QuotaPlanID:   100,
			},
			{
				Model:         gorm.Model{ID: 12},
				PricingPlanID: 1,
				Cadence:       "quarterly",
				PriceUSD:      24.99,
				QuotaPlanID:   100,
			},
		}
		mockPricing.EXPECT().GetPricingPlanPeriods(mock.Anything, uint(1)).Return(periods, nil)
		mockPricing.EXPECT().GetPriceLinesForPlan(mock.Anything, uint(1)).Return([]*billingModels.PriceLinePlan{}, nil)
		mockPricing.EXPECT().GetGatewayProductMappingsByPlan(mock.Anything, uint(1)).Return([]*billingModels.GatewayProductMapping{}, nil)
		mockPricing.EXPECT().CreateGatewayProductMapping(mock.Anything, mock.Anything).Return(nil).Times(3)

		// Create gateway
		cfg := testConfig()
		gw := NewWithConfig(ctx.Logger(), ctx, cfg, nil, nil, nil, mockPricing, mockCredit)

		// Create Stripe mock
		mockClient := NewMockStripeClient()
		mockClient.V1ProductsService.On("Create", mock.Anything, mock.AnythingOfType("*stripe.ProductCreateParams")).Return(&stripe.Product{ID: "prod_test_123"}, nil)
		mockClient.V1PricesService.On("Create", mock.Anything, mock.AnythingOfType("*stripe.PriceCreateParams")).Return(&stripe.Price{ID: "price_test_1"}, nil)
		mockClient.V1PricesService.On("Create", mock.Anything, mock.AnythingOfType("*stripe.PriceCreateParams")).Return(&stripe.Price{ID: "price_test_2"}, nil)
		mockClient.V1PricesService.On("Create", mock.Anything, mock.AnythingOfType("*stripe.PriceCreateParams")).Return(&stripe.Price{ID: "price_test_3"}, nil)
		mockClient.V1ProductsService.On("Update", mock.Anything, "prod_test_123", mock.AnythingOfType("*stripe.ProductUpdateParams")).Return(&stripe.Product{ID: "prod_test_123"}, nil)
		gw.stripeClient = mockClient

		// Call SyncPlan
		planInfo := &pluginCore.PricingPlanInfo{
			ID:          1,
			Name:        "Test Plan",
			Description: "Test description",
			Currency:    "USD",
			IsActive:    true,
			IsPublic:    false,
		}

		result, err := gw.SyncPlan(context.Background(), planInfo)

		assert.NoError(t, err)
		assert.True(t, result.Success)
		assert.Equal(t, "prod_test_123", result.ProductID)
		assert.Len(t, result.RemotePriceIDs, 3)
	})
}

// TestStripeGateway_SyncPlan_NoPeriods tests SyncPlan with no pricing periods
func TestStripeGateway_SyncPlan_NoPeriods(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		_, _, _, mockPricing := setupMockServices(ctx)
		mockCredit := core.GetService[*pluginCore.MockCreditService](ctx, pluginCore.CREDIT_SERVICE)

		// Mock pricing service to return no pricing plan periods
		periods := []*billingModels.PricingPlanPeriod{}
		mockPricing.EXPECT().GetPriceLinesForPlan(mock.Anything, uint(1)).Return([]*billingModels.PriceLinePlan{}, nil)
		mockPricing.EXPECT().GetPricingPlanPeriods(mock.Anything, uint(1)).Return(periods, nil)

		// Create gateway
		cfg := testConfig()
		gw := NewWithConfig(ctx.Logger(), ctx, cfg, nil, nil, nil, mockPricing, mockCredit)

		// Create Stripe mock
		mockClient := NewMockStripeClient()
		mockClient.V1ProductsService.On("Create", mock.Anything, mock.AnythingOfType("*stripe.ProductCreateParams")).Return(&stripe.Product{ID: "prod_test_123"}, nil)
		gw.stripeClient = mockClient

		// Call SyncPlan
		planInfo := &pluginCore.PricingPlanInfo{
			ID:          1,
			Name:        "Test Plan",
			Description: "Test description",
			Currency:    "USD",
			IsActive:    true,
			IsPublic:    false,
		}

		result, err := gw.SyncPlan(context.Background(), planInfo)

		assert.NoError(t, err)
		assert.True(t, result.Success)
		assert.Equal(t, "prod_test_123", result.ProductID)
		assert.Len(t, result.RemotePriceIDs, 0)

		// Verify Update was NOT called (no prices to set as default)
		mockClient.V1ProductsService.AssertNotCalled(t, "Update", mock.Anything, "prod_test_123", mock.AnythingOfType("*stripe.ProductUpdateParams"))
	})
}

// TestStripeGateway_SyncPlan_PricingServiceError tests SyncPlan when pricing service is not configured
func TestStripeGateway_SyncPlan_PricingServiceError(t *testing.T) {
	ctx, _ := coreTesting.NewTestContext(t)

	// Create gateway without pricing service
	cfg := testConfig()
	gw := NewWithConfig(ctx.Logger(), ctx, cfg, nil, nil, nil, nil, nil)

	// Call SyncPlan
	planInfo := &pluginCore.PricingPlanInfo{
		ID:          1,
		Name:        "Test Plan",
		Description: "Test description",
		Currency:    "USD",
		IsActive:    true,
		IsPublic:    false,
	}

	result, err := gw.SyncPlan(context.Background(), planInfo)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "pricing service not configured")
	assert.False(t, result.Success)
}

// TestStripeGateway_SyncPlan_ExistingMappingUpdates tests SyncPlan updates existing gateway product mapping
func TestStripeGateway_SyncPlan_ExistingMappingUpdates(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		_, _, _, mockPricing := setupMockServices(ctx)
		mockCredit := core.GetService[*pluginCore.MockCreditService](ctx, pluginCore.CREDIT_SERVICE)

		periodID := uint(1)
		// Mock pricing service to return pricing plan periods
		periods := []*billingModels.PricingPlanPeriod{
			{
				Model:         gorm.Model{ID: periodID},
				PricingPlanID: 1,
				Cadence:       "monthly",
				PriceUSD:      9.99,
				QuotaPlanID:   100,
			},
		}
		mockPricing.EXPECT().GetPricingPlanPeriods(mock.Anything, uint(1)).Return(periods, nil)
		mockPricing.EXPECT().GetPriceLinesForPlan(mock.Anything, uint(1)).Return([]*billingModels.PriceLinePlan{}, nil)

		// Mock existing mapping
		existingMapping := &billingModels.GatewayProductMapping{
			Model:              gorm.Model{ID: 1},
			PricingPlanPeriodID: &periodID,
			GatewayType:        GatewayID,
			RemotePriceID:      "price_old",
			SyncStatus:         "pending",
		}
		mockPricing.EXPECT().GetGatewayProductMappingsByPlan(mock.Anything, uint(1)).Return([]*billingModels.GatewayProductMapping{existingMapping}, nil)
		mockPricing.EXPECT().UpdateGatewayProductMapping(mock.Anything, uint(1), mock.Anything).Return(nil).Once()

		// Create gateway
		cfg := testConfig()
		gw := NewWithConfig(ctx.Logger(), ctx, cfg, nil, nil, nil, mockPricing, mockCredit)

		// Create Stripe mock
		mockClient := NewMockStripeClient()
		mockClient.V1ProductsService.On("Create", mock.Anything, mock.AnythingOfType("*stripe.ProductCreateParams")).Return(&stripe.Product{ID: "prod_new_123"}, nil)
		mockClient.V1PricesService.On("Create", mock.Anything, mock.AnythingOfType("*stripe.PriceCreateParams")).Return(&stripe.Price{ID: "price_monthly_new_123"}, nil)
		mockClient.V1ProductsService.On("Update", mock.Anything, "prod_new_123", mock.AnythingOfType("*stripe.ProductUpdateParams")).Return(&stripe.Product{ID: "prod_new_123"}, nil)
		gw.stripeClient = mockClient

		// Call SyncPlan
		planInfo := &pluginCore.PricingPlanInfo{
			ID:          1,
			Name:        "Test Plan",
			Description: "Test description",
			Currency:    "USD",
			IsActive:    true,
			IsPublic:    false,
		}

		result, err := gw.SyncPlan(context.Background(), planInfo)

		assert.NoError(t, err)
		assert.True(t, result.Success)
		assert.Equal(t, "prod_new_123", result.ProductID)
		assert.Len(t, result.RemotePriceIDs, 1)
	})
}

// TestStripeGateway_PickDefaultPrice_MonthlyPreference tests pickDefaultPrice with monthly preference
func TestStripeGateway_PickDefaultPrice_MonthlyPreference(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		cfg := testConfig()
		cfg.DefaultPriceCadence = string(subscription.CadenceMonthly)
		gw := NewWithConfig(ctx.Logger(), ctx, cfg, nil, nil, nil, nil, nil)

		periods := []*billingModels.PricingPlanPeriod{
			{Model: gorm.Model{ID: 1}, Cadence: "yearly"},
			{Model: gorm.Model{ID: 2}, Cadence: "monthly"},
			{Model: gorm.Model{ID: 3}, Cadence: "quarterly"},
		}
		priceIDs := []pluginCore.RemotePriceMapping{
			{PricingPlanPeriodID: 1, PriceID: "price_yearly"},
			{PricingPlanPeriodID: 2, PriceID: "price_monthly"},
			{PricingPlanPeriodID: 3, PriceID: "price_quarterly"},
		}

		result := gw.pickDefaultPrice(periods, priceIDs)
		assert.Equal(t, "price_monthly", result)
	})
}

// TestStripeGateway_PickDefaultPrice_YearlyPreference tests pickDefaultPrice with yearly preference
func TestStripeGateway_PickDefaultPrice_YearlyPreference(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		cfg := testConfig()
		cfg.DefaultPriceCadence = string(subscription.CadenceYearly)
		gw := NewWithConfig(ctx.Logger(), ctx, cfg, nil, nil, nil, nil, nil)

		periods := []*billingModels.PricingPlanPeriod{
			{Model: gorm.Model{ID: 1}, Cadence: "monthly"},
			{Model: gorm.Model{ID: 2}, Cadence: "yearly"},
			{Model: gorm.Model{ID: 3}, Cadence: "quarterly"},
		}
		priceIDs := []pluginCore.RemotePriceMapping{
			{PricingPlanPeriodID: 1, PriceID: "price_monthly"},
			{PricingPlanPeriodID: 2, PriceID: "price_yearly"},
			{PricingPlanPeriodID: 3, PriceID: "price_quarterly"},
		}

		result := gw.pickDefaultPrice(periods, priceIDs)
		assert.Equal(t, "price_yearly", result)
	})
}

// TestStripeGateway_PickDefaultPrice_FallbackToFirst tests pickDefaultPrice fallback to first price
func TestStripeGateway_PickDefaultPrice_FallbackToFirst(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		cfg := testConfig()
		cfg.DefaultPriceCadence = string(subscription.CadenceMonthly)
		gw := NewWithConfig(ctx.Logger(), ctx, cfg, nil, nil, nil, nil, nil)

		// No monthly period - should fallback to first
		periods := []*billingModels.PricingPlanPeriod{
			{Model: gorm.Model{ID: 1}, Cadence: "yearly"},
			{Model: gorm.Model{ID: 2}, Cadence: "quarterly"},
		}
		priceIDs := []pluginCore.RemotePriceMapping{
			{PricingPlanPeriodID: 1, PriceID: "price_yearly"},
			{PricingPlanPeriodID: 2, PriceID: "price_quarterly"},
		}

		result := gw.pickDefaultPrice(periods, priceIDs)
		assert.Equal(t, "price_yearly", result)
	})
}

// TestStripeGateway_PickDefaultPrice_EmptyConfig tests pickDefaultPrice with empty config (defaults to monthly)
func TestStripeGateway_PickDefaultPrice_EmptyConfig(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		cfg := testConfig()
		cfg.DefaultPriceCadence = "" // Empty - should default to monthly
		gw := NewWithConfig(ctx.Logger(), ctx, cfg, nil, nil, nil, nil, nil)

		periods := []*billingModels.PricingPlanPeriod{
			{Model: gorm.Model{ID: 1}, Cadence: "yearly"},
			{Model: gorm.Model{ID: 2}, Cadence: "monthly"},
		}
		priceIDs := []pluginCore.RemotePriceMapping{
			{PricingPlanPeriodID: 1, PriceID: "price_yearly"},
			{PricingPlanPeriodID: 2, PriceID: "price_monthly"},
		}

		result := gw.pickDefaultPrice(periods, priceIDs)
		assert.Equal(t, "price_monthly", result) // Should find monthly as default
	})
}

// TestStripeGateway_PickDefaultPrice_EmptyPrices tests pickDefaultPrice with empty prices
func TestStripeGateway_PickDefaultPrice_EmptyPrices(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		cfg := testConfig()
		gw := NewWithConfig(ctx.Logger(), ctx, cfg, nil, nil, nil, nil, nil)

		periods := []*billingModels.PricingPlanPeriod{}
		priceIDs := []pluginCore.RemotePriceMapping{}

		result := gw.pickDefaultPrice(periods, priceIDs)
		assert.Equal(t, "", result)
	})
}

// TestStripeGateway_SyncPlan_SetsDefaultPrice tests that SyncPlan sets the default price on the product
func TestStripeGateway_SyncPlan_SetsDefaultPrice(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		_, _, _, mockPricing := setupMockServices(ctx)
		mockCredit := core.GetService[*pluginCore.MockCreditService](ctx, pluginCore.CREDIT_SERVICE)

		// Mock pricing service to return pricing plan periods - monthly first to match preference
		periods := []*billingModels.PricingPlanPeriod{
			{
				Model:         gorm.Model{ID: 1},
				PricingPlanID: 1,
				Cadence:       "monthly",
				PriceUSD:      9.99,
				QuotaPlanID:   100,
			},
			{
				Model:         gorm.Model{ID: 2},
				PricingPlanID: 1,
				Cadence:       "yearly",
				PriceUSD:      99.99,
				QuotaPlanID:   100,
			},
		}
		mockPricing.EXPECT().GetPricingPlanPeriods(mock.Anything, uint(1)).Return(periods, nil)
		mockPricing.EXPECT().GetPriceLinesForPlan(mock.Anything, uint(1)).Return([]*billingModels.PriceLinePlan{}, nil)
		mockPricing.EXPECT().GetGatewayProductMappingsByPlan(mock.Anything, uint(1)).Return([]*billingModels.GatewayProductMapping{}, nil)
		mockPricing.EXPECT().CreateGatewayProductMapping(mock.Anything, mock.Anything).Return(nil).Times(2)

		// Create gateway with monthly as default cadence
		cfg := testConfig()
		cfg.DefaultPriceCadence = string(subscription.CadenceMonthly)
		gw := NewWithConfig(ctx.Logger(), ctx, cfg, nil, nil, nil, mockPricing, mockCredit)

		// Create Stripe mock
		mockClient := NewMockStripeClient()
		mockClient.V1ProductsService.On("Create", mock.Anything, mock.AnythingOfType("*stripe.ProductCreateParams")).Return(&stripe.Product{ID: "prod_test_123"}, nil)
		mockClient.V1PricesService.On("Create", mock.Anything, mock.AnythingOfType("*stripe.PriceCreateParams")).Return(&stripe.Price{ID: "price_monthly"}, nil).Once()
		mockClient.V1PricesService.On("Create", mock.Anything, mock.AnythingOfType("*stripe.PriceCreateParams")).Return(&stripe.Price{ID: "price_yearly"}, nil).Once()

		// Capture the Update call to verify DefaultPrice is set correctly
		var capturedParams *stripe.ProductUpdateParams
		mockClient.V1ProductsService.On("Update", mock.Anything, "prod_test_123", mock.AnythingOfType("*stripe.ProductUpdateParams")).
			Run(func(args mock.Arguments) {
				capturedParams = args.Get(2).(*stripe.ProductUpdateParams)
			}).
			Return(&stripe.Product{ID: "prod_test_123"}, nil)
		gw.stripeClient = mockClient

		// Call SyncPlan
		planInfo := &pluginCore.PricingPlanInfo{
			ID:          1,
			Name:        "Test Plan",
			Description: "Test description",
			Currency:    "USD",
			IsActive:    true,
			IsPublic:    false,
		}

		result, err := gw.SyncPlan(context.Background(), planInfo)

		assert.NoError(t, err)
		assert.True(t, result.Success)

		// Verify the default price was set to the monthly price
		assert.NotNil(t, capturedParams)
		assert.NotNil(t, capturedParams.DefaultPrice)
		assert.Equal(t, "price_monthly", *capturedParams.DefaultPrice)
	})
}
