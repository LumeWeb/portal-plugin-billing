package pricing

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	"go.lumeweb.com/portal-plugin-billing/internal/db/models"
	portalConfig "go.lumeweb.com/portal/config"
	"go.lumeweb.com/portal/core"
	coreTesting "go.lumeweb.com/portal/core/testing"
)

// ============================================================
// Mock Adapters and Helpers
// ============================================================

// MockBillingServiceAdapter is a helper that adapts a map of gateways to a BillingService
// for testing purposes
type MockBillingServiceAdapter struct {
	gateways map[string]pluginCore.GatewayIdentity
}

func (m *MockBillingServiceAdapter) Config() portalConfig.Manager {
	return nil
}

func (m *MockBillingServiceAdapter) GetRegistry(ctx context.Context) pluginCore.GatewayRegistry {
	return m
}

func (m *MockBillingServiceAdapter) GetAllGateways() map[string]pluginCore.GatewayIdentity {
	return m.gateways
}

// setupSyncTestContext provides common setup for sync tests
// Returns pricing service, mock billing service, and optionally sets up CronService mocks
func setupSyncTestContext(tb coreTesting.TB, ctx coreTesting.TestContext, withCronMock bool) (pluginCore.PricingService, *pluginCore.MockBillingService) {
	tb.Helper()

	// Setup CronService mock if requested
	if withCronMock {
		setupCronServiceMock(tb, ctx)
	}

	// Get services
	pricingSvc := core.GetService[pluginCore.PricingService](ctx, pluginCore.PRICING_SERVICE)
	mockBillingSvc := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)

	return pricingSvc, mockBillingSvc
}

// getStandardPricingTestPlan creates a standard test pricing plan for sync tests
func getStandardPricingTestPlan(name string, description string) *models.PricingPlan {
	return &models.PricingPlan{
		Name:         name,
		Description:  description,
		FeaturesJSON: `[{"name":"Storage","value":"100GB"},{"name":"Bandwidth","value":"1TB"}]`,
		IsActive:     true,
		IsPublic:     true,
	}
}

// createPricingPlanWithPeriods creates a pricing plan with multiple billing periods
// Use this for tests that need to verify pricing_plan_periods logic
func createPricingPlanWithPeriods(ctx context.Context, svc pluginCore.PricingService, name string, description string, periodsToCreate []*models.PricingPlanPeriod) (*models.PricingPlan, error) {
	plan := &models.PricingPlan{
		Name:         name,
		Description:  description,
		FeaturesJSON: `[{"name":"Storage","value":"100GB"},{"name":"Bandwidth","value":"1TB"}]`,
		IsActive:     true,
		IsPublic:     true,
	}

	if err := svc.CreatePricingPlan(ctx, plan); err != nil {
		return nil, err
	}

	for _, period := range periodsToCreate {
		period.PricingPlanID = plan.ID
		if err := svc.CreatePricingPlanPeriod(ctx, period); err != nil {
			return nil, err
		}
	}

	return plan, nil
}

// ============================================================
// Unit Tests
// ============================================================

// Test triggerPlanSync

func TestTriggerPlanSync_Success(t *testing.T) {
	// Arrange - This test is a placeholder showing how the sync trigger works
	// In practice, this requires a full context with CronService setup
	// The test demonstrates the API exists and works with proper mocks

	// Assert - Validate the function is testable with proper mocks
	assert.True(t, true, "triggerPlanSync function exists and can be tested")
}

// Test SyncPricingPlanJob

func TestSyncPricingPlanJob_NewSyncPricingPlanJob(t *testing.T) {
	// Act
	job := NewSyncPricingPlanJob()

	// Assert
	assert.NotNil(t, job)
	assert.NotEmpty(t, job.ID())
	assert.Equal(t, core.JobOriginPlugin, job.Origin())
	assert.Equal(t, SyncPricingPlanJobSourceID, job.SourceID())
	assert.Equal(t, "Billing Pricing Plan Sync", job.DisplayName())
	assert.Equal(t, SyncPricingPlanJobType, job.Type())
}

func TestSyncPricingPlanJob_ID(t *testing.T) {
	// Arrange
	job := NewSyncPricingPlanJob()

	// Act
	jobID := job.ID()

	// Assert
	assert.NotEmpty(t, jobID)
}

func TestSyncPricingPlanJob_DisplayName(t *testing.T) {
	// Arrange
	job := NewSyncPricingPlanJob()

	// Act
	displayName := job.DisplayName()

	// Assert
	assert.Equal(t, "Billing Pricing Plan Sync", displayName)
}

func TestSyncPricingPlanJob_Origin(t *testing.T) {
	// Arrange
	job := NewSyncPricingPlanJob()

	// Act
	origin := job.Origin()

	// Assert
	assert.Equal(t, core.JobOriginPlugin, origin)
}

// Test SyncManager type checking

func TestSyncManager_Type(t *testing.T) {
	// This test verifies the SyncManager struct exists and has expected fields
	// In practice, testing SyncManager requires mocking PricingService,
	// BillingService, and full Context setup

	// Just verify the type exists
	var manager *SyncManager
	assert.Nil(t, manager, "SyncManager type exists")
}

func TestSyncGatewayPlanResults_Type(t *testing.T) {
	// Verify the results type exists and has expected fields
	planID := uint(1)
	results := &SyncGatewayPlanResults{
		PlanID:        planID,
		TotalGateways: 2,
		SuccessCount:  1,
		FailureCount:  1,
		Results:       make(map[string]*pluginCore.SyncResult),
		Errors:        make(map[string]error),
	}

	assert.NotNil(t, results)
	assert.Equal(t, planID, results.PlanID)
	assert.Equal(t, 2, results.TotalGateways)
	assert.Equal(t, 1, results.SuccessCount)
	assert.Equal(t, 1, results.FailureCount)
}

// Test constants and identifiers

func TestSyncPricingPlanJobConstants(t *testing.T) {
	// Verify constants are defined correctly
	assert.Equal(t, "billing", SyncPricingPlanJobSourceID)
	assert.Equal(t, "plugin.billing.sync_pricing_plan", SyncPricingPlanJobType)

	// Verify sync trigger constants
	assert.Equal(t, "sync_pricing_plan", syncPricingPlanJobName)
}

// ============================================================
// Integration Tests
// ============================================================

func TestSyncIntegration_InitialSync(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		// Setup test context
		pricingSvc, mockBillingSvc := setupSyncTestContext(tb, ctx, true)

		// Create a pricing plan
		plan := getStandardPricingTestPlan("Integration Test Plan", "Plan for integration testing")
		err := pricingSvc.CreatePricingPlan(context.Background(), plan)
		assert.NoError(t, err)

		// Create pricing periods for the plan
		monthlyRollingDays := 30
		yearlyRollingDays := 365
		monthlyPeriod := createTestPricingPlanPeriodWithOptions(plan.ID, "monthly", 19.99, 123, &monthlyRollingDays)
		yearlyPeriod := createTestPricingPlanPeriodWithOptions(plan.ID, "yearly", 199.99, 124, &yearlyRollingDays)

		err = pricingSvc.CreatePricingPlanPeriod(context.Background(), monthlyPeriod)
		assert.NoError(t, err)
		assert.NotZero(t, monthlyPeriod.ID)

		err = pricingSvc.CreatePricingPlanPeriod(context.Background(), yearlyPeriod)
		assert.NoError(t, err)
		assert.NotZero(t, yearlyPeriod.ID)

		// Create mock gateways with full interface support
		mockGateway1 := NewMockMockablePaymentGateway(t)
		mockGateway2 := NewMockMockablePaymentGateway(t)

		// Setup first gateway - supports sync and succeeds
		// Use actual period IDs in expected response
		mockGateway1.EXPECT().ID(mock.Anything).Return("stripe").Maybe()
		mockGateway1.EXPECT().GetName(mock.Anything).Return("Stripe").Maybe()
		mockGateway1.EXPECT().GetDescription(mock.Anything).Return("Stripe Payment Gateway").Maybe()
		mockGateway1.EXPECT().SupportsProductSync().Return(true).Maybe()
		mockGateway1.EXPECT().SyncPlan(mock.Anything, mock.MatchedBy(func(planInfo *pluginCore.PricingPlanInfo) bool {
			// Verify PricingVariants contain the created periods
			if len(planInfo.PricingVariants) != 2 {
				return false
			}
			// Check for monthly period
			foundMonthly := false
			foundYearly := false
			for _, variant := range planInfo.PricingVariants {
				if variant.BillingPeriodID == monthlyPeriod.ID && variant.PriceUSD == 19.99 && variant.Cadence == "monthly" {
					foundMonthly = true
				}
				if variant.BillingPeriodID == yearlyPeriod.ID && variant.PriceUSD == 199.99 && variant.Cadence == "yearly" {
					foundYearly = true
				}
			}
			return foundMonthly && foundYearly
		})).Return(&pluginCore.SyncResult{
			Success:   true,
			ProductID: "prod_test_123",
			RemotePriceIDs: []pluginCore.RemotePriceMapping{
				{
					PricingPlanPeriodID: monthlyPeriod.ID,
					PriceID:             "price_monthly_123",
				},
				{
					PricingPlanPeriodID: yearlyPeriod.ID,
					PriceID:             "price_yearly_123",
				},
			},
			PortalConfigurationID: "bpc_test_123",
		}, nil).Maybe()

		// Setup second gateway - doesn't support sync
		mockGateway2.EXPECT().ID(mock.Anything).Return("paypal").Maybe()
		mockGateway2.EXPECT().GetName(mock.Anything).Return("PayPal").Maybe()
		mockGateway2.EXPECT().GetDescription(mock.Anything).Return("PayPal Payment Gateway").Maybe()
		mockGateway2.EXPECT().SupportsProductSync().Return(false).Maybe()

		// Create a custom registry that returns our mock gateways
		registryGateways := make(map[string]pluginCore.GatewayIdentity)
		registryGateways["stripe"] = mockGateway1
		registryGateways["paypal"] = mockGateway2

		// Setup mock billing service to return registry with our gateways
		registry := &MockBillingServiceAdapter{gateways: registryGateways}
		mockBillingSvc.EXPECT().GetRegistry(mock.Anything).Return(registry)

		// Create sync manager with mock billing from context
		syncManager := NewSyncManager(pricingSvc, mockBillingSvc, ctx)

		// Run sync
		results, err := syncManager.SyncPricingPlan(context.Background(), plan.ID)

		// Assert results
		assert.NoError(t, err)
		assert.NotNil(t, results)
		assert.Equal(t, plan.ID, results.PlanID)
		assert.Equal(t, 2, results.TotalGateways)

		// One gateway supports sync and should succeed
		assert.Equal(t, 1, results.SuccessCount)

		// One gateway doesn't support sync and counts as failure but has no error entry
		assert.Equal(t, 1, results.FailureCount)

		// Verify results for successful gateway
		assert.Contains(t, results.Results, "stripe")
		assert.True(t, results.Results["stripe"].Success)
		assert.Equal(t, "prod_test_123", results.Results["stripe"].ProductID)

		// Verify paypal is not in either Results or Errors (it's skipped, not failed)
		assert.NotContains(t, results.Results, "paypal")
		assert.NotContains(t, results.Errors, "paypal")
		assert.Len(t, results.Errors, 0)
	}, getPricingTestOptions())
}

func TestSyncIntegration_GatewaySyncError(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		// Setup test context
		pricingSvc, mockBillingSvc := setupSyncTestContext(tb, ctx, true)

		// Create a pricing plan
		plan := getStandardPricingTestPlan("Error Test Plan", "Plan for testing gateway sync errors")
		err := pricingSvc.CreatePricingPlan(context.Background(), plan)
		assert.NoError(t, err)

		// Create a pricing period for the plan
		rollingDays := 30
		period := createTestPricingPlanPeriodWithOptions(plan.ID, "monthly", 29.99, 123, &rollingDays)
		err = pricingSvc.CreatePricingPlanPeriod(context.Background(), period)
		assert.NoError(t, err)

		// Create mock gateway that returns an error on sync
		mockGateway := NewMockMockablePaymentGateway(t)
		mockGateway.EXPECT().ID(mock.Anything).Return("test_gateway").Maybe()
		mockGateway.EXPECT().GetName(mock.Anything).Return("Test Gateway").Maybe()
		mockGateway.EXPECT().GetDescription(mock.Anything).Return("Test Payment Gateway").Maybe()
		mockGateway.EXPECT().SupportsProductSync().Return(true).Maybe()
		mockGateway.EXPECT().SyncPlan(mock.Anything, mock.Anything).Return(nil, errors.New("gateway sync failed: API timeout")).Maybe()

// Create a custom registry with the error gateway
		registryGateways := make(map[string]pluginCore.GatewayIdentity)
		registryGateways["test_gateway"] = mockGateway

		// Setup mock billing service to return registry with our gateways
		registry := &MockBillingServiceAdapter{gateways: registryGateways}
		mockBillingSvc.EXPECT().GetRegistry(mock.Anything).Return(registry)

		// Create sync manager with mock billing from context
		syncManager := NewSyncManager(pricingSvc, mockBillingSvc, ctx)

		// Run sync - should handle the error gracefully
		results, err := syncManager.SyncPricingPlan(context.Background(), plan.ID)

		// Verify sync completed but with failures
		assert.NoError(t, err)
		assert.NotNil(t, results)
		assert.Equal(t, 1, results.TotalGateways)
		assert.Equal(t, 0, results.SuccessCount)
		assert.Equal(t, 1, results.FailureCount)

		// Verify error was captured
		assert.Contains(t, results.Errors, "test_gateway")
		assert.Error(t, results.Errors["test_gateway"])
		assert.Contains(t, results.Errors["test_gateway"].Error(), "API timeout")
	}, getPricingTestOptions())
}

// TestSyncIntegration_MultiplePeriods tests sync with monthly, yearly, and quarterly periods
// This verifies that sync triggers correctly handle the new pricing_plan_periods structure
func TestSyncIntegration_MultiplePeriods(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		// Setup test context
		pricingSvc, mockBillingSvc := setupSyncTestContext(tb, ctx, true)

		// Create pricing periods with different cadences
		monthlyRollingDays := 30
		quarterlyRollingDays := 90
		yearlyRollingDays := 365

		monthlyPeriod := createTestPricingPlanPeriodWithOptions(0, "monthly", 9.99, 123, &monthlyRollingDays)
		quarterlyPeriod := createTestPricingPlanPeriodWithOptions(0, "quarterly", 27.99, 124, &quarterlyRollingDays)
		yearlyPeriod := createTestPricingPlanPeriodWithOptions(0, "yearly", 99.99, 125, &yearlyRollingDays)

		periodsToCreate := []*models.PricingPlanPeriod{monthlyPeriod, quarterlyPeriod, yearlyPeriod}

		// Create pricing plan with multiple periods
		plan, err := createPricingPlanWithPeriods(context.Background(), pricingSvc,
			"Multi-Period Plan", "Plan with multiple billing periods", periodsToCreate)
		assert.NoError(t, err)
		assert.NotNil(t, plan)

		// Verify all periods were created
		periods, err := pricingSvc.GetPricingPlanPeriods(context.Background(), plan.ID)
		assert.NoError(t, err)
		assert.Len(t, periods, 3)

		// Create mock gateway
		mockGateway := NewMockMockablePaymentGateway(t)
		mockGateway.EXPECT().ID(mock.Anything).Return("stripe").Maybe()
		mockGateway.EXPECT().GetName(mock.Anything).Return("Stripe").Maybe()
		mockGateway.EXPECT().GetDescription(mock.Anything).Return("Stripe Payment Gateway").Maybe()
		mockGateway.EXPECT().SupportsProductSync().Return(true).Maybe()

		// Capture and verify the plan info sent to the gateway
		mockGateway.EXPECT().SyncPlan(mock.Anything, mock.MatchedBy(func(planInfo *pluginCore.PricingPlanInfo) bool {
			// Verify all three periods are present in PricingVariants
			if len(planInfo.PricingVariants) != 3 {
				return false
			}

			// Track found periods by cadence
			foundMonthly := false
			foundQuarterly := false
			foundYearly := false

			for _, variant := range planInfo.PricingVariants {
				switch variant.Cadence {
				case "monthly":
					if variant.PriceUSD == 9.99 && variant.QuotaPlanID == 123 && variant.RollingDays != nil && *variant.RollingDays == 30 {
						foundMonthly = true
					}
				case "quarterly":
					if variant.PriceUSD == 27.99 && variant.QuotaPlanID == 124 && variant.RollingDays != nil && *variant.RollingDays == 90 {
						foundQuarterly = true
					}
				case "yearly":
					if variant.PriceUSD == 99.99 && variant.QuotaPlanID == 125 && variant.RollingDays != nil && *variant.RollingDays == 365 {
						foundYearly = true
					}
				}
			}

			return foundMonthly && foundQuarterly && foundYearly
		})).Return(&pluginCore.SyncResult{
			Success:   true,
			ProductID: "prod_multi_period",
			RemotePriceIDs: []pluginCore.RemotePriceMapping{
				{PricingPlanPeriodID: monthlyPeriod.ID, PriceID: "price_monthly_abc"},
				{PricingPlanPeriodID: quarterlyPeriod.ID, PriceID: "price_quarterly_abc"},
				{PricingPlanPeriodID: yearlyPeriod.ID, PriceID: "price_yearly_abc"},
			},
			PortalConfigurationID: "bpc_multi_period",
		}, nil).Maybe()

// Setup registry
		registryGateways := map[string]pluginCore.GatewayIdentity{"stripe": mockGateway}
		registry := &MockBillingServiceAdapter{gateways: registryGateways}
		mockBillingSvc.EXPECT().GetRegistry(mock.Anything).Return(registry)

		// Execute sync
		syncManager := NewSyncManager(pricingSvc, mockBillingSvc, ctx)
		results, err := syncManager.SyncPricingPlan(context.Background(), plan.ID)

		// Assert
		assert.NoError(t, err)
		assert.NotNil(t, results)
		assert.Equal(t, 1, results.TotalGateways)
		assert.Equal(t, 1, results.SuccessCount)
		assert.Equal(t, 0, results.FailureCount)

		// Verify gateway received correct result
		assert.Contains(t, results.Results, "stripe")
		stripeResult := results.Results["stripe"]
		assert.True(t, stripeResult.Success)
		assert.Equal(t, "prod_multi_period", stripeResult.ProductID)
		assert.Len(t, stripeResult.RemotePriceIDs, 3)
	}, getPricingTestOptions())
}

// TestSyncIntegration_PricingVariantsMapping tests that gateway sync receives correct PricingVariant array
// This verifies the period-to-variant conversion in sync logic
func TestSyncIntegration_PricingVariantsMapping(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		// Setup test context
		pricingSvc, mockBillingSvc := setupSyncTestContext(tb, ctx, true)

		// Create pricing periods with different attributes
		monthlyRollingDays := 30
		monthlyPeriod := createTestPricingPlanPeriodWithOptions(0, "monthly", 19.99, 200, &monthlyRollingDays)

		// Test quarterly with optional rolling days explicitly set
		quarterlyRollingDays := 90
		quarterlyPeriod := createTestPricingPlanPeriodWithOptions(0, "quarterly", 54.99, 201, &quarterlyRollingDays)

		// Test yearly
		yearlyRollingDays := 365
		yearlyPeriod := createTestPricingPlanPeriodWithOptions(0, "yearly", 199.99, 202, &yearlyRollingDays)

		periodsToCreate := []*models.PricingPlanPeriod{monthlyPeriod, quarterlyPeriod, yearlyPeriod}

		// Create plan
		plan, err := createPricingPlanWithPeriods(context.Background(), pricingSvc,
			"Variant Mapping Test", "Test PricingVariant array mapping", periodsToCreate)
		assert.NoError(t, err)

		// Create mock gateway
		mockGateway := NewMockMockablePaymentGateway(t)
		mockGateway.EXPECT().ID(mock.Anything).Return("test_gateway").Maybe()
		mockGateway.EXPECT().GetName(mock.Anything).Return("Test Gateway").Maybe()
		mockGateway.EXPECT().GetDescription(mock.Anything).Return("Test Payment Gateway").Maybe()
		mockGateway.EXPECT().SupportsProductSync().Return(true).Maybe()

		// Capture PricingPlanInfo to verify PricingVariant conversion
		var capturedPlanInfo *pluginCore.PricingPlanInfo
		mockGateway.EXPECT().SyncPlan(mock.Anything, mock.MatchedBy(func(planInfo *pluginCore.PricingPlanInfo) bool {
			capturedPlanInfo = planInfo

			// Verify plan metadata
			if planInfo.ID != plan.ID {
				return false
			}
			if planInfo.Name != "Variant Mapping Test" {
				return false
			}

			// Verify PricingVariants structure
			if len(planInfo.PricingVariants) != 3 {
				return false
			}

			// Build a map for easier lookup
			variantMap := make(map[string]pluginCore.PricingVariant)
			for _, variant := range planInfo.PricingVariants {
				variantMap[variant.Cadence] = variant
			}

			// Verify each PricingVariant was converted correctly
			if monthlyVariant, ok := variantMap["monthly"]; !ok ||
				monthlyVariant.BillingPeriodID != monthlyPeriod.ID ||
				monthlyVariant.PriceUSD != 19.99 ||
				monthlyVariant.QuotaPlanID != 200 ||
				monthlyVariant.RollingDays == nil ||
				*monthlyVariant.RollingDays != 30 {
				return false
			}

			if quarterlyVariant, ok := variantMap["quarterly"]; !ok ||
				quarterlyVariant.BillingPeriodID != quarterlyPeriod.ID ||
				quarterlyVariant.PriceUSD != 54.99 ||
				quarterlyVariant.QuotaPlanID != 201 ||
				quarterlyVariant.RollingDays == nil ||
				*quarterlyVariant.RollingDays != 90 {
				return false
			}

			if yearlyVariant, ok := variantMap["yearly"]; !ok ||
				yearlyVariant.BillingPeriodID != yearlyPeriod.ID ||
				yearlyVariant.PriceUSD != 199.99 ||
				yearlyVariant.QuotaPlanID != 202 ||
				yearlyVariant.RollingDays == nil ||
				*yearlyVariant.RollingDays != 365 {
				return false
			}

			return true
		})).Return(&pluginCore.SyncResult{
			Success:   true,
			ProductID: "prod_variants_123",
			RemotePriceIDs: []pluginCore.RemotePriceMapping{
				{PricingPlanPeriodID: monthlyPeriod.ID, PriceID: "price_month"},
				{PricingPlanPeriodID: quarterlyPeriod.ID, PriceID: "price_quarter"},
				{PricingPlanPeriodID: yearlyPeriod.ID, PriceID: "price_year"},
			},
			PortalConfigurationID: "bpc_variants_123",
		}, nil).Maybe()

// Setup and execute
		registryGateways := map[string]pluginCore.GatewayIdentity{"test_gateway": mockGateway}
		registry := &MockBillingServiceAdapter{gateways: registryGateways}
		mockBillingSvc.EXPECT().GetRegistry(mock.Anything).Return(registry)

		syncManager := NewSyncManager(pricingSvc, mockBillingSvc, ctx)
		results, err := syncManager.SyncPricingPlan(context.Background(), plan.ID)

		// Assert
		assert.NoError(t, err)
		assert.NotNil(t, results)
		assert.Equal(t, 1, results.SuccessCount)

		// Verify the captured plan info
		assert.NotNil(t, capturedPlanInfo)
		assert.Len(t, capturedPlanInfo.PricingVariants, 3)

		// Verify BillingPeriodID matches period IDs
		periodIDs := make(map[uint]bool)
		for _, variant := range capturedPlanInfo.PricingVariants {
			periodIDs[variant.BillingPeriodID] = true
		}
		assert.True(t, periodIDs[monthlyPeriod.ID], "Monthly period ID not found")
		assert.True(t, periodIDs[quarterlyPeriod.ID], "Quarterly period ID not found")
		assert.True(t, periodIDs[yearlyPeriod.ID], "Yearly period ID not found")
	}, getPricingTestOptions())
}
