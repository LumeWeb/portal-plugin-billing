package pricing

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	"go.lumeweb.com/portal-plugin-billing/internal"
	"go.lumeweb.com/portal-plugin-billing/internal/config"
	"go.lumeweb.com/portal-plugin-billing/internal/db/migrations"
	"go.lumeweb.com/portal-plugin-billing/internal/db/models"
	"go.lumeweb.com/portal/core"
	goPortalCore "go.lumeweb.com/portal/core"
	coreTesting "go.lumeweb.com/portal/core/testing"
)

// ============================================================
// Test Entity Creation Helpers
// ============================================================

// createTestPricingPlanPeriod creates a test pricing plan period with reasonable defaults
func createTestPricingPlanPeriod(planID uint) *models.PricingPlanPeriod {
	price := 9.99
	rollingDays := 30
	return &models.PricingPlanPeriod{
		PricingPlanID: planID,
		Cadence:       "monthly",
		PriceUSD:      price,
		QuotaPlanID:   123,
		RollingDays:   &rollingDays,
	}
}

// createTestPricingPlanPeriodWithOptions creates a test pricing plan period with custom options
func createTestPricingPlanPeriodWithOptions(planID uint, cadence string, priceUSD float64, quotaPlanID uint, rollingDays *int) *models.PricingPlanPeriod {
	return &models.PricingPlanPeriod{
		PricingPlanID: planID,
		Cadence:       cadence,
		PriceUSD:      priceUSD,
		QuotaPlanID:   quotaPlanID,
		RollingDays:   rollingDays,
	}
}

// ============================================================
// Helper Functions
// ============================================================

// createPricingPlanForPeriods creates a pricing plan to be used for period tests
func createPricingPlanForPeriods(t *testing.T, ctx context.Context, service pluginCore.PricingService) uint {
	plan := &models.PricingPlan{
		Name:         "Test Plan for Periods",
		Description:  "A test pricing plan for periods",
		FeaturesJSON: new(`["Feature1","Feature2"]`),
		IsActive:     true,
		IsPublic:     true,
	}
	err := service.CreatePricingPlan(ctx, plan)
	assert.NoError(t, err)
	assert.NotZero(t, plan.ID)
	return plan.ID
}

// getPricingPeriodsTestOptions provides the standard test configuration for pricing period tests
func getPricingPeriodsTestOptions() coreTesting.TestContextBuilderOption {
	return coreTesting.CombineOptions(
		coreTesting.NewMockPluginBuilder(internal.PLUGIN_NAME).
			WithMigrations(core.DBMigration{core.DB_TYPE_SQLITE: migrations.GetSQLite()}).
			WithService(pluginCore.PRICING_SERVICE, NewPricingService).
			WithMockServiceFactory(pluginCore.BILLING_SERVICE, pluginCore.NewMockBillingService).
			WithServiceConfig(pluginCore.BILLING_SERVICE, &config.ServiceConfig{}).
			BuilderOption(),
	)
}

// setupPricingPeriodsTestContext provides setup for period tests
func setupPricingPeriodsTestContext(tb coreTesting.TB, ctx coreTesting.TestContext) pluginCore.PricingService {
	tb.Helper()
	
	// Setup CronService mock before getting the pricing service
	setupCronServiceMock(tb, ctx)
	
	return goPortalCore.GetService[pluginCore.PricingService](ctx, pluginCore.PRICING_SERVICE)
}

// ============================================================
// CreatePricingPlanPeriod Tests
// ============================================================

func TestPricingService_CreatePricingPlanPeriod(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingPeriodsTestContext(tb, ctx)
		testCtx := context.Background()

		// Create a pricing plan first
		planID := createPricingPlanForPeriods(t, testCtx, service)

		// Create a pricing plan period
		period := createTestPricingPlanPeriod(planID)
		err := service.CreatePricingPlanPeriod(testCtx, period)
		assert.NoError(t, err)
		assert.NotZero(t, period.ID)

		// Verify the period was created
		retrieved, err := service.GetPricingPlanPeriod(testCtx, period.ID)
		assert.NoError(t, err)
		assert.NotNil(t, retrieved)
		assert.Equal(t, planID, retrieved.PricingPlanID)
		assert.Equal(t, "monthly", retrieved.Cadence)
		assert.Equal(t, 9.99, retrieved.PriceUSD)
	}, getPricingPeriodsTestOptions())
}

func TestPricingService_CreatePricingPlanPeriod_WithNilPointer(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingPeriodsTestContext(tb, ctx)

		err := service.CreatePricingPlanPeriod(context.Background(), nil)
		assert.Error(t, err)
	}, getPricingPeriodsTestOptions())
}

func TestPricingService_CreatePricingPlanPeriod_WithInvalidPlanID(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingPeriodsTestContext(tb, ctx)

		// Try to create a period with a non-existent plan
		period := createTestPricingPlanPeriod(99999)
		err := service.CreatePricingPlanPeriod(context.Background(), period)
		// This might succeed if there's no foreign key constraint validation at the service level
		// or might fail depending on database setup
		assert.Error(t, err)
	}, getPricingPeriodsTestOptions())
}

// ============================================================
// UpdatePricingPlanPeriod Tests
// ============================================================

func TestPricingService_UpdatePricingPlanPeriod(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingPeriodsTestContext(tb, ctx)
		testCtx := context.Background()

		// Create a pricing plan and period
		planID := createPricingPlanForPeriods(t, testCtx, service)
		period := createTestPricingPlanPeriod(planID)
		err := service.CreatePricingPlanPeriod(testCtx, period)
		assert.NoError(t, err)

		// Update the period
		updatedPrice := 19.99
		updatedRollingDays := 60
		updatedPeriod := &models.PricingPlanPeriod{
			Cadence:     "yearly",
			PriceUSD:    updatedPrice,
			QuotaPlanID: 456,
			RollingDays: &updatedRollingDays,
		}
		err = service.UpdatePricingPlanPeriod(testCtx, period.ID, updatedPeriod)
		assert.NoError(t, err)

		// Verify the update
		retrieved, err := service.GetPricingPlanPeriod(testCtx, period.ID)
		assert.NoError(t, err)
		assert.Equal(t, "yearly", retrieved.Cadence)
		assert.Equal(t, updatedPrice, retrieved.PriceUSD)
		assert.Equal(t, uint(456), retrieved.QuotaPlanID)
		assert.Equal(t, 60, *retrieved.RollingDays)
	}, getPricingPeriodsTestOptions())
}

func TestPricingService_UpdatePricingPlanPeriod_NotFound(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingPeriodsTestContext(tb, ctx)

		updatedPeriod := &models.PricingPlanPeriod{
			Cadence:  "yearly",
			PriceUSD: 19.99,
		}

		err := service.UpdatePricingPlanPeriod(context.Background(), 99999, updatedPeriod)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	}, getPricingPeriodsTestOptions())
}

func TestPricingService_UpdatePricingPlanPeriod_WithNilPointer(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingPeriodsTestContext(tb, ctx)

		err := service.UpdatePricingPlanPeriod(context.Background(), 1, nil)
		assert.Error(t, err)
	}, getPricingPeriodsTestOptions())
}

// ============================================================
// DeletePricingPlanPeriod Tests
// ============================================================

func TestPricingService_DeletePricingPlanPeriod(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingPeriodsTestContext(tb, ctx)
		testCtx := context.Background()

		// Create a pricing plan and period
		planID := createPricingPlanForPeriods(t, testCtx, service)
		period := createTestPricingPlanPeriod(planID)
		err := service.CreatePricingPlanPeriod(testCtx, period)
		assert.NoError(t, err)

		// Delete the period
		err = service.DeletePricingPlanPeriod(testCtx, period.ID)
		assert.NoError(t, err)

		// Verify it's deleted (soft delete)
		_, err = service.GetPricingPlanPeriod(testCtx, period.ID)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	}, getPricingPeriodsTestOptions())
}

func TestPricingService_DeletePricingPlanPeriod_NotFound(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingPeriodsTestContext(tb, ctx)

		_ = service.DeletePricingPlanPeriod(context.Background(), 99999)
		// GORM Delete doesn't error on not found, but our implementation might check
	}, getPricingPeriodsTestOptions())
}

// ============================================================
// GetPricingPlanPeriod Tests
// ============================================================

func TestPricingService_GetPricingPlanPeriod(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingPeriodsTestContext(tb, ctx)
		testCtx := context.Background()

		// Create a pricing plan and period
		planID := createPricingPlanForPeriods(t, testCtx, service)
		period := createTestPricingPlanPeriod(planID)
		err := service.CreatePricingPlanPeriod(testCtx, period)
		assert.NoError(t, err)

		// Get the period
		retrieved, err := service.GetPricingPlanPeriod(testCtx, period.ID)
		assert.NoError(t, err)
		assert.NotNil(t, retrieved)
		assert.Equal(t, planID, retrieved.PricingPlanID)
		assert.Equal(t, "monthly", retrieved.Cadence)
		assert.Equal(t, 9.99, retrieved.PriceUSD)
		assert.Equal(t, uint(123), retrieved.QuotaPlanID)
		assert.Equal(t, 30, *retrieved.RollingDays)
	}, getPricingPeriodsTestOptions())
}

func TestPricingService_GetPricingPlanPeriod_NotFound(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingPeriodsTestContext(tb, ctx)

		_, err := service.GetPricingPlanPeriod(context.Background(), 99999)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	}, getPricingPeriodsTestOptions())
}

func TestPricingService_GetPricingPlanPeriod_WithNilRollingDays(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingPeriodsTestContext(tb, ctx)
		testCtx := context.Background()

		// Create a pricing plan and period without rolling days
		planID := createPricingPlanForPeriods(t, testCtx, service)
		period := createTestPricingPlanPeriodWithOptions(planID, "weekly", 4.99, 124, nil)
		err := service.CreatePricingPlanPeriod(testCtx, period)
		assert.NoError(t, err)

		// Get the period
		retrieved, err := service.GetPricingPlanPeriod(testCtx, period.ID)
		assert.NoError(t, err)
		assert.NotNil(t, retrieved)
		assert.Equal(t, "weekly", retrieved.Cadence)
		assert.Equal(t, 4.99, retrieved.PriceUSD)
		assert.Nil(t, retrieved.RollingDays)
	}, getPricingPeriodsTestOptions())
}

// ============================================================
// GetPricingPlanPeriods Tests
// ============================================================

func TestPricingService_GetPricingPlanPeriods(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingPeriodsTestContext(tb, ctx)
		testCtx := context.Background()

		// Create a pricing plan
		planID := createPricingPlanForPeriods(t, testCtx, service)

		// Create multiple periods for the same plan
		monthlyPrice := 9.99
		yearlyPrice := 99.99
		quarterlyPrice := 29.99
		rollingDays := 30

		monthlyPeriod := createTestPricingPlanPeriodWithOptions(planID, "monthly", monthlyPrice, 123, &rollingDays)
		err := service.CreatePricingPlanPeriod(testCtx, monthlyPeriod)
		assert.NoError(t, err)

		yearlyPeriod := createTestPricingPlanPeriodWithOptions(planID, "yearly", yearlyPrice, 124, &rollingDays)
		err = service.CreatePricingPlanPeriod(testCtx, yearlyPeriod)
		assert.NoError(t, err)

		quarterlyPeriod := createTestPricingPlanPeriodWithOptions(planID, "quarterly", quarterlyPrice, 125, &rollingDays)
		err = service.CreatePricingPlanPeriod(testCtx, quarterlyPeriod)
		assert.NoError(t, err)

		// Get all periods for the plan
		periods, err := service.GetPricingPlanPeriods(testCtx, planID)
		assert.NoError(t, err)
		assert.NotEmpty(t, periods)
		assert.Len(t, periods, 3)
	}, getPricingPeriodsTestOptions())
}

func TestPricingService_GetPricingPlanPeriods_EmptyResult(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingPeriodsTestContext(tb, ctx)

		// Create a pricing plan with no periods
		planID := createPricingPlanForPeriods(t, context.Background(), service)

		// Get periods for the plan
		periods, err := service.GetPricingPlanPeriods(context.Background(), planID)
		assert.NoError(t, err)
		assert.NotNil(t, periods)
		assert.Empty(t, periods)
	}, getPricingPeriodsTestOptions())
}

func TestPricingService_GetPricingPlanPeriods_NonExistentPlan(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingPeriodsTestContext(tb, ctx)

		// Get periods for a non-existent plan
		periods, err := service.GetPricingPlanPeriods(context.Background(), 99999)
		assert.NoError(t, err)
		assert.NotNil(t, periods)
		assert.Empty(t, periods)
	}, getPricingPeriodsTestOptions())
}

func TestPricingService_GetPricingPlanPeriods_WithVariedRollingDays(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingPeriodsTestContext(tb, ctx)
		testCtx := context.Background()

		// Create a pricing plan
		planID := createPricingPlanForPeriods(t, testCtx, service)

		// Create periods with varying rolling days
		rolling1 := 30
		rolling2 := 90
		rolling3 := 0

		period1 := createTestPricingPlanPeriodWithOptions(planID, "monthly", 9.99, 123, &rolling1)
		err := service.CreatePricingPlanPeriod(testCtx, period1)
		assert.NoError(t, err)

		period2 := createTestPricingPlanPeriodWithOptions(planID, "quarterly", 29.99, 124, &rolling2)
		err = service.CreatePricingPlanPeriod(testCtx, period2)
		assert.NoError(t, err)

		period3 := createTestPricingPlanPeriodWithOptions(planID, "weekly", 4.99, 125, &rolling3)
		err = service.CreatePricingPlanPeriod(testCtx, period3)
		assert.NoError(t, err)

		// Get all periods and verify rolling days
		periods, err := service.GetPricingPlanPeriods(testCtx, planID)
		assert.NoError(t, err)
		assert.Len(t, periods, 3)

		rollingDaysMap := make(map[int]bool)
		for _, p := range periods {
			if p.RollingDays != nil {
				rollingDaysMap[*p.RollingDays] = true
			}
		}
		// Should have 30, 90, and 0 rolling days
		assert.True(t, rollingDaysMap[30])
		assert.True(t, rollingDaysMap[90])
		assert.True(t, rollingDaysMap[0])
	}, getPricingPeriodsTestOptions())
}

// ============================================================
// Validation Tests
// ============================================================

func TestPricingService_CreatePricingPlanPeriod_WithInvalidCadence(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingPeriodsTestContext(tb, ctx)
		testCtx := context.Background()

		// Create a pricing plan first
		planID := createPricingPlanForPeriods(t, testCtx, service)

		// Try to create a period with an invalid cadence value
		// Service level validation allows: 'monthly', 'yearly', 'quarterly', 'weekly'
		// Note: 'rolling' is NOT in the allowed list
		rollingDays := 30
		period := &models.PricingPlanPeriod{
			PricingPlanID: planID,
			Cadence:       "rolling",
			PriceUSD:      19.99,
			QuotaPlanID:   123,
			RollingDays:   &rollingDays,
		}
		err := service.CreatePricingPlanPeriod(testCtx, period)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid cadence")
	}, getPricingPeriodsTestOptions())
}

func TestPricingService_CreatePricingPlanPeriod_WithInvalidCadence_ArbitraryString(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingPeriodsTestContext(tb, ctx)
		testCtx := context.Background()

		// Create a pricing plan first
		planID := createPricingPlanForPeriods(t, testCtx, service)

		// Try to create a period with an arbitrary invalid cadence value
		period := &models.PricingPlanPeriod{
			PricingPlanID: planID,
			Cadence:       "invalid_cadence",
			PriceUSD:      19.99,
			QuotaPlanID:   123,
		}
		err := service.CreatePricingPlanPeriod(testCtx, period)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid cadence")
	}, getPricingPeriodsTestOptions())
}

func TestPricingService_CreatePricingPlanPeriod_DuplicateCadenceInPlan(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingPeriodsTestContext(tb, ctx)
		testCtx := context.Background()

		// Create a pricing plan first
		planID := createPricingPlanForPeriods(t, testCtx, service)

		// Create first period with monthly cadence
		period1 := createTestPricingPlanPeriod(planID)
		err := service.CreatePricingPlanPeriod(testCtx, period1)
		assert.NoError(t, err)

		// Try to create second period with the same plan_id and monthly cadence
		// This should fail due to UNIQUE constraint on (pricing_plan_id, cadence)
		period2 := createTestPricingPlanPeriod(planID)
		period2.PriceUSD = 29.99
		period2.QuotaPlanID = 456
		err = service.CreatePricingPlanPeriod(testCtx, period2)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "UNIQUE constraint")
	}, getPricingPeriodsTestOptions())
}

func TestPricingService_CreatePricingPlanPeriod_WithNegativePrice(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingPeriodsTestContext(tb, ctx)
		testCtx := context.Background()

		// Create a pricing plan first
		planID := createPricingPlanForPeriods(t, testCtx, service)

		// Create a period with negative price
		// Note: No database constraint or service-level validation prevents negative prices,
		// so this should succeed
		rollingDays := 30
		period := &models.PricingPlanPeriod{
			PricingPlanID: planID,
			Cadence:       "monthly",
			PriceUSD:      -19.99,
			QuotaPlanID:   123,
			RollingDays:   &rollingDays,
		}
		err := service.CreatePricingPlanPeriod(testCtx, period)
		assert.NoError(t, err)
		assert.NotZero(t, period.ID)
		assert.Equal(t, -19.99, period.PriceUSD)
	}, getPricingPeriodsTestOptions())
}

func TestPricingService_CreatePricingPlanPeriod_WithZeroPrice(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingPeriodsTestContext(tb, ctx)
		testCtx := context.Background()

		// Create a pricing plan first
		planID := createPricingPlanForPeriods(t, testCtx, service)

		// Create a period with zero price
		// Note: No database constraint or service-level validation prevents zero prices,
		// so this should succeed (useful for free plans)
		rollingDays := 30
		period := &models.PricingPlanPeriod{
			PricingPlanID: planID,
			Cadence:       "monthly",
			PriceUSD:      0,
			QuotaPlanID:   123,
			RollingDays:   &rollingDays,
		}
		err := service.CreatePricingPlanPeriod(testCtx, period)
		assert.NoError(t, err)
		assert.NotZero(t, period.ID)
		assert.Equal(t, 0.0, period.PriceUSD)
	}, getPricingPeriodsTestOptions())
}

func TestPricingService_CreatePricingPlanPeriod_AllowedCadenceValues(t *testing.T) {
	allowedCadences := []string{"monthly", "yearly", "quarterly", "weekly"}

	for _, cadence := range allowedCadences {
		t.Run(cadence, func(t *testing.T) {
			coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
				service := setupPricingPeriodsTestContext(tb, ctx)
				testCtx := context.Background()

				// Create a pricing plan first
				planID := createPricingPlanForPeriods(t, testCtx, service)

				// Create a period with each allowed cadence value
				rollingDays := 30
				period := &models.PricingPlanPeriod{
					PricingPlanID: planID,
					Cadence:       cadence,
					PriceUSD:      19.99,
					QuotaPlanID:   123,
					RollingDays:   &rollingDays,
				}
				err := service.CreatePricingPlanPeriod(testCtx, period)
				assert.NoError(t, err)
				assert.NotZero(t, period.ID)
				assert.Equal(t, cadence, period.Cadence)
			}, getPricingPeriodsTestOptions())
		})
	}
}

// ============================================================
// Integration Tests
// ============================================================

func TestPricingService_PricingPlanPeriod_FullCrud(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingPeriodsTestContext(tb, ctx)
		testCtx := context.Background()

		// Create a pricing plan
		planID := createPricingPlanForPeriods(t, testCtx, service)

		// Create multiple periods with different cadences
		var periodIDs []uint
		cadences := []string{"monthly", "yearly", "quarterly"}
		for i := 0; i < 3; i++ {
			rollingDays := (i + 1) * 30
			period := createTestPricingPlanPeriodWithOptions(planID, cadences[i], float64(10+i), uint(100+i), &rollingDays)
			err := service.CreatePricingPlanPeriod(testCtx, period)
			assert.NoError(t, err)
			assert.NotZero(t, period.ID)
			periodIDs = append(periodIDs, period.ID)
		}

		// Get all periods
		periods, err := service.GetPricingPlanPeriods(testCtx, planID)
		assert.NoError(t, err)
		assert.Len(t, periods, 3)

		// Update second period (change from yearly to weekly)
		updatedPrice := 999.99
		updatedRollingDays := 999
		updatePeriod := &models.PricingPlanPeriod{
			Cadence:     "weekly",
			PriceUSD:    updatedPrice,
			QuotaPlanID: 888,
			RollingDays: &updatedRollingDays,
		}
		err = service.UpdatePricingPlanPeriod(testCtx, periodIDs[1], updatePeriod)
		assert.NoError(t, err)

		// Verify update
		updated, err := service.GetPricingPlanPeriod(testCtx, periodIDs[1])
		assert.NoError(t, err)
		assert.Equal(t, "weekly", updated.Cadence)
		assert.Equal(t, updatedPrice, updated.PriceUSD)
		assert.Equal(t, uint(888), updated.QuotaPlanID)

		// Delete first period
		err = service.DeletePricingPlanPeriod(testCtx, periodIDs[0])
		assert.NoError(t, err)

		// Verify deletion
		_, err = service.GetPricingPlanPeriod(testCtx, periodIDs[0])
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")

		// Verify remaining periods
		remainingPeriods, err := service.GetPricingPlanPeriods(testCtx, planID)
		assert.NoError(t, err)
		assert.Len(t, remainingPeriods, 2)
	}, getPricingPeriodsTestOptions())
}
