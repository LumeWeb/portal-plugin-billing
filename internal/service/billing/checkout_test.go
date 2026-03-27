package billing

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	"go.lumeweb.com/portal-plugin-billing/internal/db/models"
	"go.lumeweb.com/portal-plugin-billing/internal/gateway"
	"go.lumeweb.com/portal/core"
	coreTesting "go.lumeweb.com/portal/core/testing"
)

// TestBillingService_GetCheckoutUI success scenarios

func TestBillingService_GetCheckoutUI_Success(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE)
		gateway.GetRegistry().Reset() // Clear state from other tests

		pricingSvc := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)

		mockGateway := pluginCore.NewMockPaymentGateway(tb)
		mockGateway.EXPECT().ID(mock.Anything).Return("stripe")
		mockGateway.EXPECT().GetCheckoutUI(mock.Anything, uint(1), uint(42)).Return(&pluginCore.CheckoutUIResponse{
			SessionID: "sess_test",
			ExpiresAt: *createTestExpirationTime(),
			Metadata:  map[string]any{"plan_id": uint(42)},
			Fragments: []pluginCore.CheckoutUIFragment{
				{Type: pluginCore.FragmentTypeLink, Link: "https://checkout.stripe.com/pay/sess_test"},
			},
		}, nil)

		pricingSvc.EXPECT().GetPricingPlan(mock.Anything, uint(42)).Return(&models.PricingPlan{
			Model:    gorm.Model{ID: 42},
			Name:     "Test Plan",
			IsActive: true,
			IsPublic: true,
		}, nil)

		err := service.RegisterGateway(ctx, mockGateway)
		if err != nil && !errors.Is(err, gateway.ErrGatewayAlreadyRegistered) {
			require.NoError(tb, err)
		}

		result, err := service.GetCheckoutUI(ctx, 1, 42, "stripe")

		require.NoError(tb, err)
		require.NotNil(tb, result)
		assert.Equal(tb, "sess_test", result.SessionID)
		assert.Equal(tb, uint(42), result.Metadata["plan_id"])
		assert.Len(tb, result.Fragments, 1)
		assert.Equal(tb, pluginCore.FragmentTypeLink, result.Fragments[0].Type)
	}, getBillingTestOptions())
}

func TestBillingService_GetCheckoutUI_DifferentGateways(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE)
		gateway.GetRegistry().Reset() // Clear state from other tests

		pricingSvc := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)

		mockStripeGateway := pluginCore.NewMockPaymentGateway(tb)
		mockPaypalGateway := pluginCore.NewMockPaymentGateway(tb)

		mockStripeGateway.EXPECT().ID(mock.Anything).Return("stripe")
		err := service.RegisterGateway(ctx, mockStripeGateway)
		if err != nil && !errors.Is(err, gateway.ErrGatewayAlreadyRegistered) {
			require.NoError(tb, err)
		}

		mockPaypalGateway.EXPECT().ID(mock.Anything).Return("paypal")
		err = service.RegisterGateway(ctx, mockPaypalGateway)
		if err != nil && !errors.Is(err, gateway.ErrGatewayAlreadyRegistered) {
			require.NoError(tb, err)
		}

		plan := &models.PricingPlan{Model: gorm.Model{ID: 42}, Name: "Test Plan", IsActive: true, IsPublic: true}

		pricingSvc.EXPECT().GetPricingPlan(mock.Anything, uint(42)).Return(plan, nil)
		stripeResponse := &pluginCore.CheckoutUIResponse{SessionID: "sess_stripe", Fragments: []pluginCore.CheckoutUIFragment{{Type: pluginCore.FragmentTypeLink}}}
		mockStripeGateway.EXPECT().GetCheckoutUI(mock.Anything, uint(1), uint(42)).Return(stripeResponse, nil)

		result, err := service.GetCheckoutUI(ctx, 1, 42, "stripe")
		require.NoError(tb, err)
		assert.Equal(tb, "sess_stripe", result.SessionID)

		pricingSvc.EXPECT().GetPricingPlan(mock.Anything, uint(42)).Return(plan, nil)
		paypalResponse := &pluginCore.CheckoutUIResponse{SessionID: "sess_paypal", Fragments: []pluginCore.CheckoutUIFragment{{Type: pluginCore.FragmentTypeScript}}}
		mockPaypalGateway.EXPECT().GetCheckoutUI(mock.Anything, uint(1), uint(42)).Return(paypalResponse, nil)

		result, err = service.GetCheckoutUI(ctx, 1, 42, "paypal")
		require.NoError(tb, err)
		assert.Equal(tb, "sess_paypal", result.SessionID)
	}, getBillingTestOptions())
}

func TestBillingService_GetCheckoutUI_UserAlreadySubscribed(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE)

		err := service.CreateOrUpdateSubscriber(ctx, 1, "any-gateway", "sub_test", true, nil)
		require.NoError(tb, err)

		_, err = service.GetCheckoutUI(ctx, 1, 42, "stripe")

		assert.Error(tb, err)
		assert.Contains(tb, err.Error(), "already has an active subscription")
	}, getBillingTestOptions())
}

func TestBillingService_GetCheckoutUI_PlanNotFound(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE)
		pricingSvc := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)

		pricingSvc.EXPECT().GetPricingPlan(mock.Anything, uint(999)).Return(nil, assert.AnError)

		result, err := service.GetCheckoutUI(ctx, 1, 999, "stripe")

		assert.Error(tb, err)
		assert.Nil(tb, result)
		assert.Contains(tb, err.Error(), "plan not found")
	}, getBillingTestOptions())
}

func TestBillingService_GetCheckoutUI_PlanNotActive(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE)
		pricingSvc := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)

		pricingSvc.EXPECT().GetPricingPlan(mock.Anything, uint(42)).Return(&models.PricingPlan{
			Model:    gorm.Model{ID: 42},
			Name:     "Inactive Plan",
			IsActive: false,
			IsPublic: true,
		}, nil)

		result, err := service.GetCheckoutUI(ctx, 1, 42, "stripe")

		assert.Error(tb, err)
		assert.Nil(tb, result)
		assert.Contains(tb, err.Error(), "plan is not active")
	}, getBillingTestOptions())
}

func TestBillingService_GetCheckoutUI_PlanNotPublic(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE)
		pricingSvc := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)

		pricingSvc.EXPECT().GetPricingPlan(mock.Anything, uint(42)).Return(&models.PricingPlan{
			Model:    gorm.Model{ID: 42},
			Name:     "Private Plan",
			IsActive: true,
			IsPublic: false,
		}, nil)

		result, err := service.GetCheckoutUI(ctx, 1, 42, "stripe")

		assert.Error(tb, err)
		assert.Nil(tb, result)
		assert.Contains(tb, err.Error(), "plan is not publicly available")
	}, getBillingTestOptions())
}

func TestBillingService_GetCheckoutUI_GatewayNotFound(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE)
		pricingSvc := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)

		pricingSvc.EXPECT().GetPricingPlan(mock.Anything, uint(42)).Return(&models.PricingPlan{
			Model:    gorm.Model{ID: 42},
			Name:     "Test Plan",
			IsActive: true,
			IsPublic: true,
		}, nil)

		result, err := service.GetCheckoutUI(ctx, 1, 42, "nonexistent")

		assert.Error(tb, err)
		assert.Nil(tb, result)
		assert.Contains(tb, err.Error(), "gateway not found")
	}, getBillingTestOptions())
}

func TestBillingService_GetCheckoutUI_GatewayError(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE)
		gateway.GetRegistry().Reset() // Clear state from other tests

		pricingSvc := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)

		mockGateway := pluginCore.NewMockPaymentGateway(tb)
		mockGateway.EXPECT().ID(mock.Anything).Return("stripe")
		mockGateway.EXPECT().GetCheckoutUI(mock.Anything, uint(1), uint(42)).Return(nil, assert.AnError)
		err := service.RegisterGateway(ctx, mockGateway)
		if err != nil && !errors.Is(err, gateway.ErrGatewayAlreadyRegistered) {
			require.NoError(tb, err)
		}

		pricingSvc.EXPECT().GetPricingPlan(mock.Anything, uint(42)).Return(&models.PricingPlan{
			Model:    gorm.Model{ID: 42},
			Name:     "Test Plan",
			IsActive: true,
			IsPublic: true,
		}, nil)

		result, err := service.GetCheckoutUI(ctx, 1, 42, "stripe")

		assert.Error(tb, err)
		assert.Nil(tb, result)
	}, getBillingTestOptions())
}

func TestBillingService_GetCheckoutUI_Response(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE)
		gateway.GetRegistry().Reset() // Clear state from other tests

		pricingSvc := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)

		mockGateway := pluginCore.NewMockPaymentGateway(tb)

		mockGateway.EXPECT().ID(mock.Anything).Return("stripe")
		err := service.RegisterGateway(ctx, mockGateway)
		if err != nil && !errors.Is(err, gateway.ErrGatewayAlreadyRegistered) {
			require.NoError(tb, err)
		}

		pricingSvc.EXPECT().GetPricingPlan(mock.Anything, uint(42)).Return(&models.PricingPlan{
			Model:    gorm.Model{ID: 42},
			Name:     "Test Plan",
			IsActive: true,
			IsPublic: true,
		}, nil)
		mockGateway.EXPECT().GetCheckoutUI(mock.Anything, uint(1), uint(42)).Return(&pluginCore.CheckoutUIResponse{
			SessionID: "sess_multi",
			ExpiresAt: *createTestExpirationTime(),
			Metadata: map[string]any{
				"plan_id":  uint(42),
				"gateway":  "stripe",
				"version":  "1.0",
			},
			Fragments: []pluginCore.CheckoutUIFragment{
				{Type: pluginCore.FragmentTypeHTML, HTML: "<button>Pay Now</button>"},
				{Type: pluginCore.FragmentTypeScript, Script: "https://js.stripe.com/v3/"},
			},
		}, nil)

		result, err := service.GetCheckoutUI(ctx, 1, 42, "stripe")

		require.NoError(tb, err)
		require.NotNil(tb, result)

		assert.Equal(tb, "sess_multi", result.SessionID)
		assert.Equal(tb, uint(42), result.Metadata["plan_id"])
		assert.Equal(tb, "stripe", result.Metadata["gateway"])
		assert.Equal(tb, "1.0", result.Metadata["version"])
		assert.Len(tb, result.Fragments, 2)
		assert.Equal(tb, pluginCore.FragmentTypeHTML, result.Fragments[0].Type)
		assert.Equal(tb, pluginCore.FragmentTypeScript, result.Fragments[1].Type)
	}, getBillingTestOptions())
}

func createTestExpirationTime() *time.Time {
	exp := time.Now().Add(30 * time.Minute)
	return &exp
}
