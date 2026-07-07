package atlos

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	pluginConfig "go.lumeweb.com/portal-plugin-billing/internal/config"
	billingModels "go.lumeweb.com/portal-plugin-billing/internal/db/models"
	"go.lumeweb.com/portal-plugin-billing/pkg/subscription"
	core "go.lumeweb.com/portal/core"
	coreTesting "go.lumeweb.com/portal/core/testing"
	portalModels "go.lumeweb.com/portal/db/models"
	"gorm.io/gorm"
)

// Test Plan Change Calculation (Business Logic)

func TestCalculatePlanChangeProration_UpgradeWithPayment(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockPricing := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)

		gateway := &AtlosGateway{
			coreCtx: ctx,
			config:  pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID, Endpoint: newMockAtlosServer(t)},

			pricing: mockPricing,
			billing: mockBilling,
		}
		userID := uint(1)
		newPeriodID := uint(2)

		oldPeriodID := uint(1)
		now := time.Now().UTC()
		endTime := now.AddDate(0, 1, 0)
		currentSub := &billingModels.Subscriber{
			Model:               gorm.Model{ID: 1},
			UserID:              userID,
			GatewayType:         GatewayID,
			PricingPlanPeriodID: &oldPeriodID,
			BillingPeriodStart:  &now,
			BillingPeriodEnd:    &endTime,
			SubscriptionID:      "sub-123",
			ExternalID:          "ext-123",
		}
		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, userID).Return(currentSub, nil).Once()

		newPeriod := &billingModels.PricingPlanPeriod{
			Model:         gorm.Model{ID: 2},
			PricingPlanID: 2,
			PriceUSD:      45.00,
			Cadence:       "monthly",
		}
		mockPricing.EXPECT().GetPricingPlanPeriod(mock.Anything, newPeriodID).Return(newPeriod, nil).Once()

		plan := &billingModels.PricingPlan{
			Model:       gorm.Model{ID: 2},
			Name:        "Pro Plan",
			Description: "Pro Plan Description",
			IsActive:    true,
		}
		mockPricing.EXPECT().GetPricingPlan(mock.Anything, uint(2)).Return(plan, nil).Once()

		oldPeriod := &billingModels.PricingPlanPeriod{
			Model:         gorm.Model{ID: 1},
			PricingPlanID: 1,
			PriceUSD:      30.00,
			Cadence:       "monthly",
		}
		mockPricing.EXPECT().GetPricingPlanPeriod(mock.Anything, oldPeriodID).Return(oldPeriod, nil).Once()

		calc, err := gateway.calculatePlanChangeProration(context.Background(), userID, newPeriodID)

		assert.NoError(t, err)
		assert.NotNil(t, calc)
		assert.Equal(t, newPeriod, calc.NewPeriod)
		assert.Equal(t, oldPeriod, calc.OldPeriod)
		assert.Equal(t, plan, calc.NewPlan)
		assert.Equal(t, currentSub, calc.CurrentSub)
		assert.Equal(t, PlanChangeActionCheckoutRequired, calc.ActionType)
		assert.True(t, calc.ProrationResult.UnusedCredit.GreaterThan(decimal.Zero))
		assert.True(t, calc.NetAmountDue.GreaterThan(decimal.Zero))
		assert.Equal(t, PlanChangeActionCheckoutRequired, calc.ActionType)
	})
}

func TestCalculatePlanChangeProration_UpgradeWithCredit(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockPricing := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)

		gateway := &AtlosGateway{
			coreCtx: ctx,
			config:  pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID, Endpoint: newMockAtlosServer(t)},

			pricing: mockPricing,
			billing: mockBilling,
		}
		userID := uint(1)
		newPeriodID := uint(2)

		oldPeriodID := uint(1)
		now := time.Now().UTC()
		endTime := now.AddDate(0, 1, 0)
		currentSub := &billingModels.Subscriber{
			Model:               gorm.Model{ID: 1},
			UserID:              userID,
			GatewayType:         GatewayID,
			PricingPlanPeriodID: &oldPeriodID,
			BillingPeriodStart:  &now,
			BillingPeriodEnd:    &endTime,
			SubscriptionID:      "sub-123",
			ExternalID:          "ext-123",
		}
		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, userID).Return(currentSub, nil).Once()

		newPeriod := &billingModels.PricingPlanPeriod{
			Model:         gorm.Model{ID: 2},
			PricingPlanID: 2,
			PriceUSD:      25.00,
			Cadence:       "monthly",
		}
		mockPricing.EXPECT().GetPricingPlanPeriod(mock.Anything, newPeriodID).Return(newPeriod, nil).Once()

		plan := &billingModels.PricingPlan{
			Model:       gorm.Model{ID: 2},
			Name:        "Basic Plan",
			Description: "Basic Plan",
			IsActive:    true,
		}
		mockPricing.EXPECT().GetPricingPlan(mock.Anything, uint(2)).Return(plan, nil).Once()

		oldPeriod := &billingModels.PricingPlanPeriod{
			Model:         gorm.Model{ID: 1},
			PricingPlanID: 1,
			PriceUSD:      45.00,
			Cadence:       "monthly",
		}
		mockPricing.EXPECT().GetPricingPlanPeriod(mock.Anything, oldPeriodID).Return(oldPeriod, nil).Once()

		calc, err := gateway.calculatePlanChangeProration(context.Background(), userID, newPeriodID)

		assert.NoError(t, err)
		assert.NotNil(t, calc)
		assert.True(t, calc.NetAmountDue.LessThan(decimal.Zero))
		assert.True(t, calc.CreditToIssue.GreaterThan(decimal.Zero))
		assert.Equal(t, PlanChangeActionCreditOnly, calc.ActionType)
	})
}

func TestCalculatePlanChangeProration_ZeroAmount(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockPricing := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)

		gateway := &AtlosGateway{
			coreCtx: ctx,
			config:  pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID, Endpoint: newMockAtlosServer(t)},

			pricing: mockPricing,
			billing: mockBilling,
		}
		userID := uint(1)
		newPeriodID := uint(2)

		oldPeriodID := uint(1)
		now := time.Now().UTC()
		endTime := now.AddDate(0, 1, 0)
		currentSub := &billingModels.Subscriber{
			Model:               gorm.Model{ID: 1},
			UserID:              userID,
			GatewayType:         GatewayID,
			PricingPlanPeriodID: &oldPeriodID,
			BillingPeriodStart:  &now,
			BillingPeriodEnd:    &endTime,
			SubscriptionID:      "sub-123",
			ExternalID:          "ext-123",
		}
		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, userID).Return(currentSub, nil).Once()

		newPeriod := &billingModels.PricingPlanPeriod{
			Model:         gorm.Model{ID: 2},
			PricingPlanID: 2,
			PriceUSD:      30.00,
			Cadence:       "monthly",
		}
		mockPricing.EXPECT().GetPricingPlanPeriod(mock.Anything, newPeriodID).Return(newPeriod, nil).Once()

		plan := &billingModels.PricingPlan{
			Model:       gorm.Model{ID: 2},
			Name:        "Standard Plan",
			Description: "Standard Plan",
			IsActive:    true,
		}
		mockPricing.EXPECT().GetPricingPlan(mock.Anything, uint(2)).Return(plan, nil).Once()

		oldPeriod := &billingModels.PricingPlanPeriod{
			Model:         gorm.Model{ID: 1},
			PricingPlanID: 1,
			PriceUSD:      30.00,
			Cadence:       "monthly",
		}
		mockPricing.EXPECT().GetPricingPlanPeriod(mock.Anything, oldPeriodID).Return(oldPeriod, nil).Once()

		calc, err := gateway.calculatePlanChangeProration(context.Background(), userID, newPeriodID)

		assert.NoError(t, err)
		assert.NotNil(t, calc)
		assert.True(t, calc.NetAmountDue.IsZero())
		assert.Equal(t, PlanChangeActionZeroAmount, calc.ActionType)
	})
}

func TestCalculatePlanChangeProration_InvalidNewPeriod(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockPricing := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)

		gateway := &AtlosGateway{
			coreCtx: ctx,
			config:  pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID, Endpoint: newMockAtlosServer(t)},

			pricing: mockPricing,
			billing: mockBilling,
		}
		userID := uint(1)
		newPeriodID := uint(999)

		oldPeriodID := uint(1)
		now := time.Now().UTC()
		endTime := now.AddDate(0, 1, 0)
		currentSub := &billingModels.Subscriber{
			Model:               gorm.Model{ID: 1},
			UserID:              userID,
			GatewayType:         GatewayID,
			PricingPlanPeriodID: &oldPeriodID,
			BillingPeriodStart:  &now,
			BillingPeriodEnd:    &endTime,
			SubscriptionID:      "sub-123",
			ExternalID:          "ext-123",
		}
		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, userID).Return(currentSub, nil).Once()

		mockPricing.EXPECT().GetPricingPlanPeriod(mock.Anything, newPeriodID).Return(nil, nil).Once()

		calc, err := gateway.calculatePlanChangeProration(context.Background(), userID, newPeriodID)

		assert.Error(t, err)
		assert.Nil(t, calc)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestCalculatePlanChangeProration_InactivePlan(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockPricing := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)

		gateway := &AtlosGateway{
			coreCtx: ctx,
			config:  pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID, Endpoint: newMockAtlosServer(t)},

			pricing: mockPricing,
			billing: mockBilling,
		}
		userID := uint(1)
		newPeriodID := uint(2)

		oldPeriodID := uint(1)
		now := time.Now().UTC()
		endTime := now.AddDate(0, 1, 0)
		currentSub := &billingModels.Subscriber{
			Model:               gorm.Model{ID: 1},
			UserID:              userID,
			GatewayType:         GatewayID,
			PricingPlanPeriodID: &oldPeriodID,
			BillingPeriodStart:  &now,
			BillingPeriodEnd:    &endTime,
			SubscriptionID:      "sub-123",
			ExternalID:          "ext-123",
		}
		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, userID).Return(currentSub, nil).Once()

		newPeriod := &billingModels.PricingPlanPeriod{
			Model:         gorm.Model{ID: 2},
			PricingPlanID: 2,
			PriceUSD:      45.00,
			Cadence:       "monthly",
		}
		mockPricing.EXPECT().GetPricingPlanPeriod(mock.Anything, newPeriodID).Return(newPeriod, nil).Once()

		plan := &billingModels.PricingPlan{
			Model:       gorm.Model{ID: 2},
			Name:        "Inactive Plan",
			Description: "",
			IsActive:    false,
		}
		mockPricing.EXPECT().GetPricingPlan(mock.Anything, uint(2)).Return(plan, nil).Once()

		calc, err := gateway.calculatePlanChangeProration(context.Background(), userID, newPeriodID)

		assert.Error(t, err)
		assert.Nil(t, calc)
		assert.Contains(t, err.Error(), "not active")
	})
}

func TestExecutePlanChange_ProratedFlow(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockPricing := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)
		mockUsers := core.GetService[*coreTesting.MockUserService](ctx, core.USER_SERVICE)

		gateway := &AtlosGateway{
			coreCtx: ctx,
			config:  pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID, Endpoint: newMockAtlosServer(t)},

			pricing: mockPricing,
			billing: mockBilling,
			users:   mockUsers,
		}
		userID := uint(1)
		newPeriodID := uint(2)

		setupProrationMocks(mockPricing, mockBilling, userID, newPeriodID, 30.00, 45.00)

		user := &portalModels.User{
			Model:     gorm.Model{ID: userID},
			FirstName: "John",
			LastName:  "Doe",
			Email:     "john@example.com",
		}
		mockUsers.EXPECT().AccountExists(mock.Anything, userID).Return(true, user, nil).Once()

		mockBilling.EXPECT().DeactivateSubscriber(mock.Anything, userID, GatewayID).Return(nil).Once()

		result, err := gateway.ExecutePlanChange(context.Background(), userID, newPeriodID)

		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, pluginCore.PlanChangeActionCheckoutRequired, result.Action)
		assert.NotEmpty(t, result.CheckoutLink)
		assert.True(t, result.CreditApplied.GreaterThan(decimal.Zero))
		assert.True(t, result.ChargeDue.GreaterThan(decimal.Zero))
	})
}

func TestExecutePlanChange_CreditOnlyFlow(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockPricing := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)
		mockCredit := core.GetService[*pluginCore.MockCreditService](ctx, pluginCore.CREDIT_SERVICE)

		gateway := &AtlosGateway{
			coreCtx: ctx,
			config:  pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID, Endpoint: newMockAtlosServer(t)},

			pricing: mockPricing,
			billing: mockBilling,
			credit:  mockCredit,
		}
		userID := uint(1)
		newPeriodID := uint(2)

		oldPeriod := &billingModels.PricingPlanPeriod{
			Model:         gorm.Model{ID: 1},
			PricingPlanID: 1,
			PriceUSD:      45.00,
			Cadence:       "monthly",
		}
		newPeriod := &billingModels.PricingPlanPeriod{
			Model:         gorm.Model{ID: 2},
			PricingPlanID: 2,
			PriceUSD:      30.00,
			Cadence:       "monthly",
		}

		oldPeriodID := uint(1)
		now := time.Now().UTC()
		endTime := now.AddDate(0, 1, 0)
		currentSub := &billingModels.Subscriber{
			Model:               gorm.Model{ID: 1},
			UserID:              userID,
			GatewayType:         GatewayID,
			PricingPlanPeriodID: &oldPeriodID,
			BillingPeriodStart:  &now,
			BillingPeriodEnd:    &endTime,
			SubscriptionID:      "sub-123",
			ExternalID:          "ext-123",
		}
		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, userID).Return(currentSub, nil).Once()

		mockPricing.EXPECT().GetPricingPlanPeriod(mock.Anything, newPeriodID).Return(newPeriod, nil).Once()

		plan := &billingModels.PricingPlan{
			Model:       gorm.Model{ID: 2},
			Name:        "Basic Plan",
			Description: "",
			IsActive:    true,
		}
		mockPricing.EXPECT().GetPricingPlan(mock.Anything, uint(2)).Return(plan, nil).Once()

		mockPricing.EXPECT().GetPricingPlanPeriod(mock.Anything, oldPeriodID).Return(oldPeriod, nil).Once()

		var issuedCredit decimal.Decimal
		mockCredit.EXPECT().IssueCreditWithIdempotency(
			mock.Anything, mock.Anything, mock.Anything, mock.Anything,
			mock.Anything, mock.Anything, mock.Anything, mock.Anything,
		).Run(func(ctx context.Context, userID uint64, transactionType string, amount decimal.Decimal, referenceType string, referenceID string, description string, createdBy uint64) {
			issuedCredit = amount
		}).Return(nil).Once()

		mockBilling.EXPECT().DeactivateSubscriber(mock.Anything, userID, GatewayID).Return(nil).Once()
		mockBilling.EXPECT().CreateOrUpdateSubscriber(mock.Anything, userID, GatewayID, "", "", true, &newPeriodID, mock.Anything, mock.Anything).Return(nil).Once()

		result, err := gateway.ExecutePlanChange(context.Background(), userID, newPeriodID)

		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, pluginCore.PlanChangeActionComplete, result.Action)
		assert.Empty(t, result.CheckoutLink)
		assert.True(t, result.ChargeDue.LessThan(decimal.Zero))
		assert.True(t, issuedCredit.GreaterThan(decimal.Zero))
	})
}

func setupProrationMocks(mockPricing *pluginCore.MockPricingService, mockBilling *pluginCore.MockBillingService, userID uint, newPeriodID uint, oldPriceUSD, newPriceUSD float64) {
	oldPeriodID := uint(1)
	now := time.Now().UTC()
	endTime := now.AddDate(0, 1, 0)
	currentSub := &billingModels.Subscriber{
		Model:               gorm.Model{ID: 1},
		UserID:              userID,
		GatewayType:         GatewayID,
		PricingPlanPeriodID: &oldPeriodID,
		BillingPeriodStart:  &now,
		BillingPeriodEnd:    &endTime,
		SubscriptionID:      "sub-123",
		ExternalID:          "ext-123",
	}
	mockBilling.EXPECT().GetActiveSubscription(mock.Anything, userID).Return(currentSub, nil).Once()

	newPeriod := &billingModels.PricingPlanPeriod{
		Model:         gorm.Model{ID: 2},
		PricingPlanID: 2,
		PriceUSD:      newPriceUSD,
		Cadence:       "monthly",
	}
	mockPricing.EXPECT().GetPricingPlanPeriod(mock.Anything, newPeriodID).Return(newPeriod, nil).Once()

	plan := &billingModels.PricingPlan{
		Model:       gorm.Model{ID: 2},
		Name:        "Pro Plan",
		Description: "",
		IsActive:    true,
	}
	mockPricing.EXPECT().GetPricingPlan(mock.Anything, uint(2)).Return(plan, nil).Once()

	oldPeriod := &billingModels.PricingPlanPeriod{
		Model:         gorm.Model{ID: 1},
		PricingPlanID: 1,
		PriceUSD:      oldPriceUSD,
		Cadence:       "monthly",
	}
	mockPricing.EXPECT().GetPricingPlanPeriod(mock.Anything, oldPeriodID).Return(oldPeriod, nil).Once()
}

func TestCalculatePlanChangeProration_UpgradeCoveredByExistingCredit(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockPricing := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)
		mockCredit := core.GetService[*pluginCore.MockCreditService](ctx, pluginCore.CREDIT_SERVICE)

		gateway := &AtlosGateway{
			coreCtx: ctx,
			config:  pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID, Endpoint: newMockAtlosServer(t)},

			pricing: mockPricing,
			billing: mockBilling,
			credit:  mockCredit,
		}
		userID := uint(1)
		// Scenario: user is on $1/m plan, upgrading to $2/m
		// This is a "reupgrade" — user previously had $2/m, downgraded to $1/m accumulating credit
		newPeriodID := uint(2)

		oldPeriodID := uint(1)
		now := time.Now().UTC()
		endTime := now.AddDate(0, 1, 0)
		currentSub := &billingModels.Subscriber{
			Model:               gorm.Model{ID: 1},
			UserID:              userID,
			GatewayType:         GatewayID,
			PricingPlanPeriodID: &oldPeriodID,
			BillingPeriodStart:  &now,
			BillingPeriodEnd:    &endTime,
			SubscriptionID:      "sub-123",
			ExternalID:          "ext-123",
		}
		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, userID).Return(currentSub, nil).Once()

		newPeriod := &billingModels.PricingPlanPeriod{
			Model:         gorm.Model{ID: 2},
			PricingPlanID: 2,
			PriceUSD:      2.00,
			Cadence:       "monthly",
		}
		mockPricing.EXPECT().GetPricingPlanPeriod(mock.Anything, newPeriodID).Return(newPeriod, nil).Once()

		plan := &billingModels.PricingPlan{
			Model:       gorm.Model{ID: 2},
			Name:        "Pro Plan",
			Description: "Pro Plan Description",
			IsActive:    true,
		}
		mockPricing.EXPECT().GetPricingPlan(mock.Anything, uint(2)).Return(plan, nil).Once()

		oldPeriod := &billingModels.PricingPlanPeriod{
			Model:         gorm.Model{ID: 1},
			PricingPlanID: 1,
			PriceUSD:      1.00,
			Cadence:       "monthly",
		}
		mockPricing.EXPECT().GetPricingPlanPeriod(mock.Anything, oldPeriodID).Return(oldPeriod, nil).Once()

		// User has existing credit balance that covers the upgrade cost
		mockCredit.EXPECT().GetUserBalance(mock.Anything, uint64(userID)).Return(decimal.NewFromFloat(1.50), nil).Once()

		calc, err := gateway.calculatePlanChangeProration(context.Background(), userID, newPeriodID)

		assert.NoError(t, err)
		assert.NotNil(t, calc)
		// The net amount due would be positive (upgrade from $1 to $2), but existing credit covers it
		assert.Equal(t, PlanChangeActionCreditOnly, calc.ActionType)
		assert.True(t, calc.ExistingCreditApplied.GreaterThan(decimal.Zero))
		assert.True(t, calc.NetAmountDue.GreaterThan(decimal.Zero))
	})
}

func TestCalculatePlanChangeProration_UpgradeNotCoveredByExistingCredit(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockPricing := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)
		mockCredit := core.GetService[*pluginCore.MockCreditService](ctx, pluginCore.CREDIT_SERVICE)

		gateway := &AtlosGateway{
			coreCtx: ctx,
			config:  pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID, Endpoint: newMockAtlosServer(t)},

			pricing: mockPricing,
			billing: mockBilling,
			credit:  mockCredit,
		}
		userID := uint(1)
		newPeriodID := uint(2)

		oldPeriodID := uint(1)
		now := time.Now().UTC()
		endTime := now.AddDate(0, 1, 0)
		currentSub := &billingModels.Subscriber{
			Model:               gorm.Model{ID: 1},
			UserID:              userID,
			GatewayType:         GatewayID,
			PricingPlanPeriodID: &oldPeriodID,
			BillingPeriodStart:  &now,
			BillingPeriodEnd:    &endTime,
			SubscriptionID:      "sub-123",
			ExternalID:          "ext-123",
		}
		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, userID).Return(currentSub, nil).Once()

		newPeriod := &billingModels.PricingPlanPeriod{
			Model:         gorm.Model{ID: 2},
			PricingPlanID: 2,
			PriceUSD:      45.00,
			Cadence:       "monthly",
		}
		mockPricing.EXPECT().GetPricingPlanPeriod(mock.Anything, newPeriodID).Return(newPeriod, nil).Once()

		plan := &billingModels.PricingPlan{
			Model:       gorm.Model{ID: 2},
			Name:        "Pro Plan",
			Description: "Pro Plan Description",
			IsActive:    true,
		}
		mockPricing.EXPECT().GetPricingPlan(mock.Anything, uint(2)).Return(plan, nil).Once()

		oldPeriod := &billingModels.PricingPlanPeriod{
			Model:         gorm.Model{ID: 1},
			PricingPlanID: 1,
			PriceUSD:      30.00,
			Cadence:       "monthly",
		}
		mockPricing.EXPECT().GetPricingPlanPeriod(mock.Anything, oldPeriodID).Return(oldPeriod, nil).Once()

		// User has some credit but not enough to cover the full upgrade
		mockCredit.EXPECT().GetUserBalance(mock.Anything, uint64(userID)).Return(decimal.NewFromFloat(5.00), nil).Once()

		calc, err := gateway.calculatePlanChangeProration(context.Background(), userID, newPeriodID)

		assert.NoError(t, err)
		assert.NotNil(t, calc)
		assert.Equal(t, PlanChangeActionCheckoutRequired, calc.ActionType)
		assert.True(t, calc.ExistingCreditApplied.Equal(decimal.NewFromFloat(5.00)))
		assert.True(t, calc.CheckoutAmount.Equal(decimal.NewFromFloat(10.00)))
		assert.True(t, calc.NetAmountDue.Equal(decimal.NewFromFloat(15.00)))
	})
}

func TestExecutePlanChange_ExistingBalanceCoversUpgrade(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockPricing := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)
		mockCredit := core.GetService[*pluginCore.MockCreditService](ctx, pluginCore.CREDIT_SERVICE)

		gateway := &AtlosGateway{
			coreCtx: ctx,
			config:  pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID, Endpoint: newMockAtlosServer(t)},

			pricing: mockPricing,
			billing: mockBilling,
			credit:  mockCredit,
		}
		userID := uint(1)
		// Upgrade from $1 to $2/month with existing credit covering the cost
		newPeriodID := uint(2)

		oldPeriodID := uint(1)
		now := time.Now().UTC()
		endTime := now.AddDate(0, 1, 0)
		currentSub := &billingModels.Subscriber{
			Model:               gorm.Model{ID: 1},
			UserID:              userID,
			GatewayType:         GatewayID,
			PricingPlanPeriodID: &oldPeriodID,
			BillingPeriodStart:  &now,
			BillingPeriodEnd:    &endTime,
			SubscriptionID:      "sub-123",
			ExternalID:          "ext-123",
		}
		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, userID).Return(currentSub, nil).Once()

		newPeriod := &billingModels.PricingPlanPeriod{
			Model:         gorm.Model{ID: 2},
			PricingPlanID: 2,
			PriceUSD:      2.00,
			Cadence:       "monthly",
		}
		mockPricing.EXPECT().GetPricingPlanPeriod(mock.Anything, newPeriodID).Return(newPeriod, nil).Once()

		plan := &billingModels.PricingPlan{
			Model:       gorm.Model{ID: 2},
			Name:        "Pro Plan",
			Description: "",
			IsActive:    true,
		}
		mockPricing.EXPECT().GetPricingPlan(mock.Anything, uint(2)).Return(plan, nil).Once()

		oldPeriod := &billingModels.PricingPlanPeriod{
			Model:         gorm.Model{ID: 1},
			PricingPlanID: 1,
			PriceUSD:      1.00,
			Cadence:       "monthly",
		}
		mockPricing.EXPECT().GetPricingPlanPeriod(mock.Anything, oldPeriodID).Return(oldPeriod, nil).Once()

		// Existing credit balance covers the upgrade
		mockCredit.EXPECT().GetUserBalance(mock.Anything, uint64(userID)).Return(decimal.NewFromFloat(2.00), nil).Once()

		// Remaining proration credit is issued (netAmountDue=1, balance=2, excess=1)
		var issuedProrationCredit decimal.Decimal
		mockCredit.EXPECT().IssueCreditWithIdempotency(
			mock.Anything, mock.Anything, mock.Anything, mock.Anything,
			mock.Anything, mock.Anything, mock.Anything, mock.Anything,
		).Run(func(ctx context.Context, userID uint64, transactionType string, amount decimal.Decimal, referenceType string, referenceID string, description string, createdBy uint64) {
			issuedProrationCredit = amount
		}).Return(nil).Once()

		// Existing credit balance is debited for the upgrade cost
		var debitedAmount decimal.Decimal
		mockCredit.EXPECT().IssueUsageCredit(
			mock.Anything,
			uint64(userID),
			pluginCore.TransactionTypeTime,
			mock.MatchedBy(func(amount decimal.Decimal) bool {
				debitedAmount = amount
				return amount.GreaterThan(decimal.Zero)
			}),
			mock.AnythingOfType("string"),
			mock.AnythingOfType("string"),
			uint64(0),
		).Return(nil).Once()

		mockBilling.EXPECT().DeactivateSubscriber(mock.Anything, userID, GatewayID).Return(nil).Once()
		mockBilling.EXPECT().CreateOrUpdateSubscriber(mock.Anything, userID, GatewayID, "", "", true, &newPeriodID, mock.Anything, mock.Anything).Return(nil).Once()

		result, err := gateway.ExecutePlanChange(context.Background(), userID, newPeriodID)

		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, pluginCore.PlanChangeActionComplete, result.Action)
		assert.Empty(t, result.CheckoutLink)
		assert.True(t, debitedAmount.GreaterThan(decimal.Zero), "should debit existing credit balance for upgrade cost")
		assert.True(t, issuedProrationCredit.GreaterThan(decimal.Zero), "should issue remaining proration credit")
	})
}

func TestUsesSubscriptionNetResult(t *testing.T) {
	prorationResult := subscription.ProrationResult{
		UnusedCredit: decimal.NewFromFloat(15.00),
		NewCharge:    decimal.NewFromFloat(45.00),
	}

	netResult := subscription.NetResult(prorationResult)
	expected := decimal.NewFromFloat(30.00)

	assert.True(t, netResult.Equal(expected))
}
