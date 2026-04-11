package pricing

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	"go.lumeweb.com/portal-plugin-billing/internal"
	"go.lumeweb.com/portal-plugin-billing/internal/config"
	"go.lumeweb.com/portal-plugin-billing/internal/db/migrations"
	"go.lumeweb.com/portal-plugin-billing/internal/db/models"
	"go.lumeweb.com/portal/core"
	coreTesting "go.lumeweb.com/portal/core/testing"
	mocks "go.lumeweb.com/portal/core/testing/mocks"
	"go.lumeweb.com/queryutil"
)

const (
	// Test constants
	TestUserID      = uint(123)
	TestPlanID      = uint(1)
	TestPriceLineID = uint(1)
)

// ============================================================
// Test Configuration and Setup Helpers
// ============================================================

// getPricingTestOptions provides the standard test configuration for pricing tests
// This base configuration includes plugin setup, migrations, and essential services
func getPricingTestOptions() coreTesting.TestContextBuilderOption {
	return coreTesting.CombineOptions(
		coreTesting.NewMockPluginBuilder(internal.PLUGIN_NAME).
			WithMigrations(core.DBMigration{core.DB_TYPE_SQLITE: migrations.GetSQLite()}).
			WithService(pluginCore.PRICING_SERVICE, NewPricingService).
			WithMockServiceFactory(pluginCore.BILLING_SERVICE, pluginCore.NewMockBillingService).
			WithServiceConfig(pluginCore.BILLING_SERVICE, &config.ServiceConfig{}).
			BuilderOption(),
	)
}

// setupPricingTestContext provides all common test setup in one call
// Returns the pricing service and performs common setup like CronService mocking
func setupPricingTestContext(tb coreTesting.TB, ctx coreTesting.TestContext, withCronMock bool) pluginCore.PricingService {
	tb.Helper()

	// Setup CronService mock if requested
	if withCronMock {
		setupCronServiceMock(tb, ctx)
	}

	// Get and return the pricing service
	return core.GetService[pluginCore.PricingService](ctx, pluginCore.PRICING_SERVICE)
}

// setupPaginationHelper creates a default pagination for tests
func setupPaginationHelper(offset int, limit int) (queryutil.Pagination, error) {
	return queryutil.NewPagination(offset, limit)
}

// setupCronServiceMock sets up the CronService mocks to handle sync triggers
// This must be called before executing pricing service methods that trigger sync
func setupCronServiceMock(tb coreTesting.TB, ctx coreTesting.TestContext) {
	tb.Helper()

	// Get the CronService mock from test context
	cronSvc := core.GetService[*mocks.MockCronService](ctx, core.CRON_SERVICE)

	t := tb.(*testing.T)
	mockJobFactory := mocks.NewMockCronJobFactory(t)
	mockJob := mocks.NewMockCronJob(t)

	// Setup expectations to handle triggerPlanSync calls
	// These use Maybe() since not all tests trigger sync and some may trigger it multiple times

	// JobFactory() should return our mock factory
	cronSvc.EXPECT().JobFactory().Return(mockJobFactory).Maybe()

	// CreateJob() should return our mock job with no error
	mockJobFactory.EXPECT().CreateJob(mock.Anything, mock.Anything).Return(mockJob, nil).Maybe()

	// SetArgs() can be called on the job to set the planID
	mockJob.EXPECT().SetArgs(mock.Anything).Maybe()

	// RegisterJob() should succeed
	cronSvc.EXPECT().RegisterJob(mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
}

// ============================================================
// Test Entity Creation Helpers
// ============================================================

// createTestPricingPlan creates a test pricing plan with reasonable defaults
func createTestPricingPlan() *models.PricingPlan {
	return &models.PricingPlan{
		Name:         "Test Plan",
		Description:  "A test pricing plan",
		FeaturesJSON: `[{"name":"Feature1"},{"name":"Feature2"}]`,
		IsActive:     true,
		IsPublic:     true,
	}
}

// createTestPricingPlanWithOptions creates a test pricing plan with custom options
func createTestPricingPlanWithOptions(name string, description string, monthlyPrice *float64) *models.PricingPlan {
	_ = monthlyPrice // Not used in current PricingPlan model
	return &models.PricingPlan{
		Name:         name,
		Description:  description,
		FeaturesJSON: `[{"name":"Feature1"},{"name":"Feature2"}]`,
		IsActive:     true,
		IsPublic:     true,
	}
}

// createTestPriceLine creates a test price line with reasonable defaults
func createTestPriceLine() *models.PriceLine {
	return &models.PriceLine{
		Name:        "Test Price Line",
		Description: "A test price line",
		IsActive:    true,
	}
}

// createTestGatewayProductMapping creates a test gateway product mapping
func createTestGatewayProductMapping(planPeriodID *uint) *models.GatewayProductMapping {
	return &models.GatewayProductMapping{
		PricingPlanPeriodID: planPeriodID,
		GatewayType:         "stripe",
		RemoteProductID:     "prod_test_123",
		SyncStatus:          "pending",
	}
}

// ============================================================
// Test Workflow Helpers
// ============================================================

// createAndVerifyPlan creates a plan and verifies it was created successfully
// Returns the created plan for further testing
func createAndVerifyPlan(t *testing.T, service pluginCore.PricingService, plan *models.PricingPlan) *models.PricingPlan {
	err := service.CreatePricingPlan(context.Background(), plan)
	assert.NoError(t, err)
	assert.NotZero(t, plan.ID)

	// Verify creation
	retrieved, err := service.GetPricingPlan(context.Background(), plan.ID)
	assert.NoError(t, err)
	assert.Equal(t, plan.Name, retrieved.Name)

	return plan
}

// createAndVerifyPriceLine creates a price line and verifies it was created successfully
// Returns the created price line for further testing
func createAndVerifyPriceLine(t *testing.T, service pluginCore.PricingService, line *models.PriceLine) *models.PriceLine {
	err := service.CreatePriceLine(context.Background(), line)
	assert.NoError(t, err)
	assert.NotZero(t, line.ID)

	return line
}

// createAndVerifyGatewayMapping creates a gateway mapping and verifies it was created successfully
// Returns the created mapping for further testing
func createAndVerifyGatewayMapping(t *testing.T, service pluginCore.PricingService, mapping *models.GatewayProductMapping, planPeriodID uint) *models.GatewayProductMapping {
	err := service.CreateGatewayProductMapping(context.Background(), mapping)
	assert.NoError(t, err)
	assert.NotZero(t, mapping.ID)

	// Verify creation
	retrieved, err := service.GetGatewayProductMapping(context.Background(), planPeriodID, mapping.GatewayType)
	assert.NoError(t, err)
	assert.Equal(t, mapping.RemoteProductID, retrieved.RemoteProductID)

	return mapping
}

// ============================================================
// Service Tests
// ============================================================

func TestPricingService_ID(t *testing.T) {
	svc, _, err := NewPricingService()
	assert.NoError(t, err)
	service := svc.(pluginCore.PricingService)
	assert.Equal(t, pluginCore.PRICING_SERVICE, service.ID())
}

func TestPricingService_GetConfig(t *testing.T) {
	svc, _, err := NewPricingService()
	assert.NoError(t, err)
	service := svc.(pluginCore.PricingService)

	config, err := service.GetConfig()
	assert.NoError(t, err)
	assert.NotNil(t, config)
}

// ============================================================
// CreatePricingPlan tests
// ============================================================

func TestPricingService_CreatePricingPlan(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingTestContext(tb, ctx, true)

		plan := createTestPricingPlan()
		createAndVerifyPlan(t, service, plan)
	}, getPricingTestOptions())
}

func TestPricingService_CreatePricingPlan_WithInvalidData(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingTestContext(tb, ctx, true)

		// Plan without required fields
		plan := &models.PricingPlan{
			Description: "Missing name",
		}
		err := service.CreatePricingPlan(context.Background(), plan)
		assert.Error(t, err)
	}, getPricingTestOptions())
}

// ============================================================
// UpdatePricingPlan tests
// ============================================================

func TestPricingService_UpdatePricingPlan(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingTestContext(tb, ctx, true)

		plan := createAndVerifyPlan(t, service, createTestPricingPlan())

		// Update the plan
		updatedName := "Updated Plan Name"
		updatedPlan := &models.PricingPlan{
			Name:        updatedName,
			Description: "Updated description",
		}
		err := service.UpdatePricingPlan(context.Background(), plan.ID, updatedPlan)
		assert.NoError(t, err)

		// Verify the update
		retrieved, err := service.GetPricingPlan(context.Background(), plan.ID)
		assert.NoError(t, err)
		assert.Equal(t, updatedName, retrieved.Name)
		assert.Equal(t, "Updated description", retrieved.Description)
	}, getPricingTestOptions())
}

func TestPricingService_UpdatePricingPlan_NotFound(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingTestContext(tb, ctx, false)

		updatedPlan := &models.PricingPlan{
			Name:        "Updated Plan",
			Description: "Updated description",
		}

		err := service.UpdatePricingPlan(context.Background(), 99999, updatedPlan)
		assert.Error(t, err)
	}, getPricingTestOptions())
}

// ============================================================
// DeletePricingPlan tests
// ============================================================

func TestPricingService_DeletePricingPlan(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingTestContext(tb, ctx, true)

		plan := createAndVerifyPlan(t, service, createTestPricingPlan())

		// Delete the plan
		err := service.DeletePricingPlan(context.Background(), plan.ID)
		assert.NoError(t, err)

		// Verify it's deleted (soft delete)
		_, err = service.GetPricingPlan(context.Background(), plan.ID)
		assert.Error(t, err)
	}, getPricingTestOptions())
}

func TestPricingService_DeletePricingPlan_NotFound(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingTestContext(tb, ctx, false)

		err := service.DeletePricingPlan(context.Background(), 99999)
		assert.NoError(t, err) // GORM Delete doesn't error on not found
	}, getPricingTestOptions())
}

// ============================================================
// GetPricingPlan tests
// ============================================================

func TestPricingService_GetPricingPlan(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingTestContext(tb, ctx, true)

		plan := createAndVerifyPlan(t, service, createTestPricingPlan())

		// Get the plan
		retrieved, err := service.GetPricingPlan(context.Background(), plan.ID)
		assert.NoError(t, err)
		assert.NotNil(t, retrieved)
		assert.Equal(t, plan.Name, retrieved.Name)
		assert.Equal(t, plan.Description, retrieved.Description)
	}, getPricingTestOptions())
}

func TestPricingService_GetPricingPlan_NotFound(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingTestContext(tb, ctx, false)

		_, err := service.GetPricingPlan(context.Background(), 99999)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	}, getPricingTestOptions())
}

// ============================================================
// GetPricingPlans tests
// ============================================================

func TestPricingService_GetPricingPlans(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingTestContext(tb, ctx, true)

		// Create multiple plans
		for i := 1; i <= 5; i++ {
			plan := createTestPricingPlan()
			plan.Name = "Test Plan " + string(rune('0'+i))
			err := service.CreatePricingPlan(context.Background(), plan)
			assert.NoError(t, err)
		}

		// Get all plans
		pagination, _ := queryutil.NewPagination(0, 10)
		plans, total, err := service.GetPricingPlans(context.Background(), 0, nil, nil, pagination)
		assert.NoError(t, err)
		assert.NotZero(t, total)
		assert.NotEmpty(t, plans)
		assert.LessOrEqual(t, len(plans), 10)
	}, getPricingTestOptions())
}

func TestPricingService_GetPricingPlans_WithPagination(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingTestContext(tb, ctx, true)

		// Create 15 plans
		for i := 0; i < 15; i++ {
			plan := createTestPricingPlan()
			plan.Name = "Plan " + string(rune('0'+i))
			err := service.CreatePricingPlan(context.Background(), plan)
			assert.NoError(t, err)
		}

		// Get first page
		page1, _ := queryutil.NewPagination(0, 10)
		plans1, total1, err := service.GetPricingPlans(context.Background(), 0, nil, nil, page1)
		assert.NoError(t, err)
		assert.LessOrEqual(t, len(plans1), 10)

		// Get second page
		page2, _ := queryutil.NewPagination(10, 10)
		plans2, total2, err := service.GetPricingPlans(context.Background(), 0, nil, nil, page2)
		assert.NoError(t, err)
		assert.Equal(t, total1, total2)
		assert.Less(t, len(plans2), 11) // At most 1 more on page 2
	}, getPricingTestOptions())
}

// ============================================================
// CreatePriceLine tests
// ============================================================

func TestPricingService_CreatePriceLine(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingTestContext(tb, ctx, false)

		line := createTestPriceLine()
		err := service.CreatePriceLine(context.Background(), line)
		assert.NoError(t, err)
		assert.NotZero(t, line.ID)

		// Verify the line was created
		pagination, _ := queryutil.NewPagination(0, 10)
		lines, total, err := service.GetPriceLines(context.Background(), 0, nil, nil, pagination)
		assert.NoError(t, err)
		assert.NotZero(t, total)
		assert.NotEmpty(t, lines)
	}, getPricingTestOptions())
}

func TestPricingService_CreatePriceLine_PreventsMultipleDefaults(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingTestContext(tb, ctx, false)

		// Create first default line
		line1 := createTestPriceLine()
		line1.IsDefault = true
		err := service.CreatePriceLine(context.Background(), line1)
		assert.NoError(t, err)

		// Try to create another default line - should fail
		line2 := createTestPriceLine()
		line2.Name = "Second Default"
		line2.IsDefault = true
		err = service.CreatePriceLine(context.Background(), line2)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "a default price line already exists")
	}, getPricingTestOptions())
}

func TestPricingService_CreatePriceLine_InvalidData(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingTestContext(tb, ctx, false)

		// Line without required fields
		line := &models.PriceLine{
			Description: "Missing name",
		}
		err := service.CreatePriceLine(context.Background(), line)
		assert.Error(t, err)
	}, getPricingTestOptions())
}

// ============================================================
// UpdatePriceLine tests
// ============================================================

func TestPricingService_UpdatePriceLine(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingTestContext(tb, ctx, false)

		line := createAndVerifyPriceLine(t, service, createTestPriceLine())

		// Update the line
		updatedName := "Updated Price Line"
		updatedLine := &models.PriceLine{
			Name:        updatedName,
			Description: "Updated description",
		}
		err := service.UpdatePriceLine(context.Background(), line.ID, updatedLine)
		assert.NoError(t, err)

		// Verify the update
		pagination, _ := queryutil.NewPagination(0, 10)
		lines, _, err := service.GetPriceLines(context.Background(), 0, nil, nil, pagination)
		assert.NoError(t, err)
		assert.Equal(t, updatedName, lines[0].Name)
	}, getPricingTestOptions())
}

func TestPricingService_UpdatePriceLine_PreventsMultipleDefaults(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingTestContext(tb, ctx, false)

		// Create first default line
		line1 := createTestPriceLine()
		line1.IsDefault = true
		err := service.CreatePriceLine(context.Background(), line1)
		assert.NoError(t, err)

		// Create second non-default line
		line2 := createTestPriceLine()
		line2.Name = "Second Line"
		line2.IsDefault = false
		err = service.CreatePriceLine(context.Background(), line2)
		assert.NoError(t, err)

		// Try to update second line to default - should fail
		updatedLine := &models.PriceLine{
			IsDefault: true,
		}
		err = service.UpdatePriceLine(context.Background(), line2.ID, updatedLine)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "a default price line already exists")
	}, getPricingTestOptions())
}

// ============================================================
// DeletePriceLine tests
// ============================================================

func TestPricingService_DeletePriceLine(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingTestContext(tb, ctx, false)

		line := createAndVerifyPriceLine(t, service, createTestPriceLine())

		// Delete the line
		err := service.DeletePriceLine(context.Background(), line.ID)
		assert.NoError(t, err)

		// Get all lines - deleted line should not appear
		pagination, _ := queryutil.NewPagination(0, 10)
		lines, _, err := service.GetPriceLines(context.Background(), 0, nil, nil, pagination)
		assert.NoError(t, err)
		// Verify deleted line is not in results
		for _, l := range lines {
			assert.NotEqual(t, line.ID, l.ID)
		}
	}, getPricingTestOptions())
}

// ============================================================
// AddPlanToPriceLine tests
// ============================================================

func TestPricingService_AddPlanToPriceLine(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingTestContext(tb, ctx, true)

		// Create plan and line
		plan := createAndVerifyPlan(t, service, createTestPricingPlan())
		line := createAndVerifyPriceLine(t, service, createTestPriceLine())

		// Add plan to line
		err := service.AddPlanToPriceLine(context.Background(), line.ID, plan.ID, 0)
		assert.NoError(t, err)

		// Verify the association
		plans, err := service.GetPlansForPriceLine(context.Background(), line.ID)
		assert.NoError(t, err)
		assert.Len(t, plans, 1)
		assert.Equal(t, plan.ID, plans[0].ID)
	}, getPricingTestOptions())
}

func TestPricingService_AddPlanToPriceLine_Duplicate(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingTestContext(tb, ctx, true)

		// Create plan and line
		plan := createAndVerifyPlan(t, service, createTestPricingPlan())
		line := createAndVerifyPriceLine(t, service, createTestPriceLine())

		// Add plan to line twice
		err := service.AddPlanToPriceLine(context.Background(), line.ID, plan.ID, 0)
		assert.NoError(t, err)

		// Second call should not error (idempotent)
		err = service.AddPlanToPriceLine(context.Background(), line.ID, plan.ID, 0)
		assert.NoError(t, err)

		// Should only have one association
		plans, err := service.GetPlansForPriceLine(context.Background(), line.ID)
		assert.NoError(t, err)
		assert.Len(t, plans, 1)
	}, getPricingTestOptions())
}

// ============================================================
// RemovePlanFromPriceLine tests
// ============================================================

func TestPricingService_RemovePlanFromPriceLine(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingTestContext(tb, ctx, true)

		// Create plans and line
		plan1 := createAndVerifyPlan(t, service, createTestPricingPlan())
		plan1.Name = "Plan 1"

		plan2 := createAndVerifyPlan(t, service, createTestPricingPlan())
		plan2.Name = "Plan 2"

		plan3 := createAndVerifyPlan(t, service, createTestPricingPlan())
		plan3.Name = "Plan 3"

		line := createAndVerifyPriceLine(t, service, createTestPriceLine())

		// Add all plans to line with positions
		err := service.AddPlanToPriceLine(context.Background(), line.ID, plan1.ID, 0)
		assert.NoError(t, err)
		err = service.AddPlanToPriceLine(context.Background(), line.ID, plan2.ID, 1)
		assert.NoError(t, err)
		err = service.AddPlanToPriceLine(context.Background(), line.ID, plan3.ID, 2)
		assert.NoError(t, err)

		// Remove plan2 from the middle
		err = service.RemovePlanFromPriceLine(context.Background(), line.ID, plan2.ID)
		assert.NoError(t, err)

		// Verify plans are reordered
		_, err = service.GetPriceLinesForPlan(context.Background(), plan1.ID)
		assert.NoError(t, err)

		// Check positions are consecutive
		plans, err := service.GetPlansForPriceLine(context.Background(), line.ID)
		assert.NoError(t, err)
		assert.Len(t, plans, 2)
	}, getPricingTestOptions())
}

func TestPricingService_RemovePlanFromPriceLine_NotFound(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingTestContext(tb, ctx, true)

		// Try to remove non-existent association
		err := service.RemovePlanFromPriceLine(context.Background(), 99999, 99999)
		assert.NoError(t, err) // GORM doesn't error on delete with non-existent ID
	}, getPricingTestOptions())
}

// ============================================================
// AssignPriceLineToUser tests
// ============================================================

func TestPricingService_AssignPriceLineToUser(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingTestContext(tb, ctx, false)

		userID := uint(123)

		// Create a price line
		line := createAndVerifyPriceLine(t, service, createTestPriceLine())

		// Assign line to user
		err := service.AssignPriceLineToUser(context.Background(), userID, line.ID)
		assert.NoError(t, err)

		// Verify assignment by getting effective line
		effectiveLine, err := service.GetEffectivePriceLineForUser(context.Background(), userID)
		assert.NoError(t, err)
		assert.NotNil(t, effectiveLine)
		assert.Equal(t, line.ID, effectiveLine.ID)
	}, getPricingTestOptions())
}

func TestPricingService_AssignPriceLineToUser_UpdateExisting(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingTestContext(tb, ctx, true)

		userID := uint(123)

		// Create two price lines
		line1 := createTestPriceLine()
		line1.Name = "Line 1"
		err := service.CreatePriceLine(context.Background(), line1)
		assert.NoError(t, err)

		line2 := createTestPriceLine()
		line2.Name = "Line 2"
		err = service.CreatePriceLine(context.Background(), line2)
		assert.NoError(t, err)

		// Assign first line to user
		err = service.AssignPriceLineToUser(context.Background(), userID, line1.ID)
		assert.NoError(t, err)

		// Update to second line
		err = service.AssignPriceLineToUser(context.Background(), userID, line2.ID)
		assert.NoError(t, err)

		// Verify update
		effectiveLine, err := service.GetEffectivePriceLineForUser(context.Background(), userID)
		assert.NoError(t, err)
		assert.NotNil(t, effectiveLine)
		assert.Equal(t, line2.ID, effectiveLine.ID)
	}, getPricingTestOptions())
}

// ============================================================
// GetEffectivePriceLineForUser tests
// ============================================================

func TestPricingService_GetEffectivePriceLineForUser_WithAssignment(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingTestContext(tb, ctx, false)

		userID := uint(123)

		// Create a price line
		line := createTestPriceLine()
		err := service.CreatePriceLine(context.Background(), line)
		assert.NoError(t, err)

		// Assign line to user
		err = service.AssignPriceLineToUser(context.Background(), userID, line.ID)
		assert.NoError(t, err)

		// Get effective line
		effectiveLine, err := service.GetEffectivePriceLineForUser(context.Background(), userID)
		assert.NoError(t, err)
		assert.NotNil(t, effectiveLine)
		assert.Equal(t, line.ID, effectiveLine.ID)
		assert.Equal(t, line.Name, effectiveLine.Name)
	}, getPricingTestOptions())
}

func TestPricingService_GetEffectivePriceLineForUser_WithDefault(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingTestContext(tb, ctx, false)

		userID := uint(123)

		// Create default price line
		defaultLine := createTestPriceLine()
		defaultLine.Name = "Default Line"
		defaultLine.IsDefault = true
		err := service.CreatePriceLine(context.Background(), defaultLine)
		assert.NoError(t, err)

		// Get effective line for user without assignment
		effectiveLine, err := service.GetEffectivePriceLineForUser(context.Background(), userID)
		assert.NoError(t, err)
		assert.NotNil(t, effectiveLine)
		assert.Equal(t, defaultLine.ID, effectiveLine.ID)
		assert.Equal(t, defaultLine.Name, effectiveLine.Name)
	}, getPricingTestOptions())
}

func TestPricingService_GetEffectivePriceLineForUser_NoDefaultLine(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingTestContext(tb, ctx, false)

		userID := uint(123)

		// Try to get effective line without default or assignment
		_, err := service.GetEffectivePriceLineForUser(context.Background(), userID)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "default price line not found")
	}, getPricingTestOptions())
}

// ============================================================
// GetDefaultPriceLine tests
// ============================================================

func TestPricingService_GetDefaultPriceLine_Success(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingTestContext(tb, ctx, false)

		// Create default price line
		defaultLine := createTestPriceLine()
		defaultLine.Name = "Default Line"
		defaultLine.IsDefault = true
		err := service.CreatePriceLine(context.Background(), defaultLine)
		assert.NoError(t, err)

		// Get default line
		retrievedLine, err := service.GetDefaultPriceLine(context.Background())
		assert.NoError(t, err)
		assert.NotNil(t, retrievedLine)
		assert.Equal(t, defaultLine.ID, retrievedLine.ID)
		assert.Equal(t, defaultLine.Name, retrievedLine.Name)
		assert.True(t, retrievedLine.IsDefault)
	}, getPricingTestOptions())
}

func TestPricingService_GetDefaultPriceLine_NotFound(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingTestContext(tb, ctx, false)

		// Try to get default line when none exists
		_, err := service.GetDefaultPriceLine(context.Background())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "default price line not found")
	}, getPricingTestOptions())
}

func TestPricingService_GetDefaultPriceLine_MultipleLines(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingTestContext(tb, ctx, false)

		// Create non-default price lines
		for i := 0; i < 3; i++ {
			line := createTestPriceLine()
			line.Name = "Line " + string(rune('0'+i))
			line.IsDefault = false
			err := service.CreatePriceLine(context.Background(), line)
			assert.NoError(t, err)
		}

		// Create default price line
		defaultLine := createTestPriceLine()
		defaultLine.Name = "Default Line"
		defaultLine.IsDefault = true
		err := service.CreatePriceLine(context.Background(), defaultLine)
		assert.NoError(t, err)

		// Get default line (should return the one marked as default)
		retrievedLine, err := service.GetDefaultPriceLine(context.Background())
		assert.NoError(t, err)
		assert.NotNil(t, retrievedLine)
		assert.Equal(t, defaultLine.ID, retrievedLine.ID)
		assert.Equal(t, defaultLine.Name, retrievedLine.Name)
		assert.True(t, retrievedLine.IsDefault)
	}, getPricingTestOptions())
}

// ============================================================
// GetPriceLines tests
// ============================================================

func TestPricingService_GetPriceLines(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingTestContext(tb, ctx, false)

		// Create multiple price lines
		for i := 0; i < 5; i++ {
			line := createTestPriceLine()
			line.Name = "Line " + string(rune('0'+i))
			err := service.CreatePriceLine(context.Background(), line)
			assert.NoError(t, err)
		}

		// Get all price lines
		pagination, _ := queryutil.NewPagination(0, 10)
		lines, total, err := service.GetPriceLines(context.Background(), 0, nil, nil, pagination)
		assert.NoError(t, err)
		assert.NotZero(t, total)
		assert.NotEmpty(t, lines)
	}, getPricingTestOptions())
}

func TestPricingService_GetPriceLines_WithPagination(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingTestContext(tb, ctx, false)

		// Create 15 price lines
		for i := 0; i < 15; i++ {
			line := createTestPriceLine()
			line.Name = "Line " + string(rune('0'+i))
			err := service.CreatePriceLine(context.Background(), line)
			assert.NoError(t, err)
		}

		// Get first page
		page1, _ := queryutil.NewPagination(0, 10)
		lines1, total1, err := service.GetPriceLines(context.Background(), 0, nil, nil, page1)
		assert.NoError(t, err)
		assert.LessOrEqual(t, len(lines1), 10)

		// Get second page
		page2, _ := queryutil.NewPagination(10, 10)
		lines2, total2, err := service.GetPriceLines(context.Background(), 0, nil, nil, page2)
		assert.NoError(t, err)
		assert.Equal(t, total1, total2)
		assert.Less(t, len(lines2), 11)
	}, getPricingTestOptions())
}

// ============================================================
// GetPriceLine tests
// ============================================================

func TestPricingService_GetPriceLine(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingTestContext(tb, ctx, false)

		// Create a test price line
		line := createTestPriceLine()
		line.Name = "Test Price Line"
		line.Description = "Test Description"
		err := service.CreatePriceLine(context.Background(), line)
		assert.NoError(t, err)

		// Get the price line by ID
		retrieved, err := service.GetPriceLine(context.Background(), line.ID)
		assert.NoError(t, err)
		assert.NotNil(t, retrieved)
		assert.Equal(t, line.Name, retrieved.Name)
		assert.Equal(t, line.Description, retrieved.Description)
	}, getPricingTestOptions())
}

func TestPricingService_GetPriceLine_NotFound(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingTestContext(tb, ctx, false)

		// Try to get a non-existent price line
		_, err := service.GetPriceLine(context.Background(), 99999)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	}, getPricingTestOptions())
}

// ============================================================
// GetUpgradeDowngradePlans tests
// ============================================================

func TestPricingService_GetUpgradeDowngradePlans(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingTestContext(tb, ctx, true)

		// Create 3 plans with prices to indicate positioning
		plan1 := createTestPricingPlan()
		plan1.Name = "Basic"
		err := service.CreatePricingPlan(context.Background(), plan1)
		assert.NoError(t, err)

		plan2 := createTestPricingPlan()
		plan2.Name = "Pro"
		err = service.CreatePricingPlan(context.Background(), plan2)
		assert.NoError(t, err)

		plan3 := createTestPricingPlan()
		plan3.Name = "Premium"
		err = service.CreatePricingPlan(context.Background(), plan3)
		assert.NoError(t, err)

		// Create price line
		line := createTestPriceLine()
		err = service.CreatePriceLine(context.Background(), line)
		assert.NoError(t, err)

		// Add plans in specific order
		err = service.AddPlanToPriceLine(context.Background(), line.ID, plan1.ID, 0) // Base
		assert.NoError(t, err)
		err = service.AddPlanToPriceLine(context.Background(), line.ID, plan2.ID, 1) // First upgrade
		assert.NoError(t, err)
		err = service.AddPlanToPriceLine(context.Background(), line.ID, plan3.ID, 2) // Second upgrade
		assert.NoError(t, err)

		// Test from middle plan (Pro)
		paths, err := service.GetUpgradeDowngradePlans(context.Background(), plan2.ID, line.ID)
		assert.NoError(t, err)
		assert.NotNil(t, paths)

		// Should have 1 upgrade (Premium)
		assert.Len(t, paths.Upgrades, 1)
		assert.Equal(t, "Premium", paths.Upgrades[0].Name)

		// Should have 1 downgrade (Basic)
		assert.Len(t, paths.Downgrades, 1)
		assert.Equal(t, "Basic", paths.Downgrades[0].Name)
	}, getPricingTestOptions())
}

func TestPricingService_GetUpgradeDowngradePlans_FromBase(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingTestContext(tb, ctx, true)

		// Create plans
		plan1 := createTestPricingPlan()
		plan1.Name = "Basic"
		err := service.CreatePricingPlan(context.Background(), plan1)
		assert.NoError(t, err)

		plan2 := createTestPricingPlan()
		plan2.Name = "Pro"
		err = service.CreatePricingPlan(context.Background(), plan2)
		assert.NoError(t, err)

		plan3 := createTestPricingPlan()
		plan3.Name = "Premium"
		err = service.CreatePricingPlan(context.Background(), plan3)
		assert.NoError(t, err)

		// Create price line
		line := createTestPriceLine()
		err = service.CreatePriceLine(context.Background(), line)
		assert.NoError(t, err)

		// Add plans
		err = service.AddPlanToPriceLine(context.Background(), line.ID, plan1.ID, 0)
		assert.NoError(t, err)
		err = service.AddPlanToPriceLine(context.Background(), line.ID, plan2.ID, 1)
		assert.NoError(t, err)
		err = service.AddPlanToPriceLine(context.Background(), line.ID, plan3.ID, 2)
		assert.NoError(t, err)

		// Test from base plan (Basic)
		paths, err := service.GetUpgradeDowngradePlans(context.Background(), plan1.ID, line.ID)
		assert.NoError(t, err)

		// Should have 2 upgrades, 0 downgrades
		assert.Len(t, paths.Upgrades, 2)
		assert.Len(t, paths.Downgrades, 0)
	}, getPricingTestOptions())
}

func TestPricingService_GetUpgradeDowngradePlans_PlanNotFound(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingTestContext(tb, ctx, false)

		// Create price line
		line := createTestPriceLine()
		err := service.CreatePriceLine(context.Background(), line)
		assert.NoError(t, err)

		// Try with non-existent plan
		_, err = service.GetUpgradeDowngradePlans(context.Background(), 99999, line.ID)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "plan not found")
	}, getPricingTestOptions())
}

// ============================================================
// GetPlansForPriceLine tests
// ============================================================

func TestPricingService_GetPlansForPriceLine(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingTestContext(tb, ctx, true)

		// Create plans
		plan1 := createTestPricingPlan()
		plan1.Name = "Plan 1"
		err := service.CreatePricingPlan(context.Background(), plan1)
		assert.NoError(t, err)

		plan2 := createTestPricingPlan()
		plan2.Name = "Plan 2"
		err = service.CreatePricingPlan(context.Background(), plan2)
		assert.NoError(t, err)

		// Create price line
		line := createTestPriceLine()
		err = service.CreatePriceLine(context.Background(), line)
		assert.NoError(t, err)

		// Add plans to line
		err = service.AddPlanToPriceLine(context.Background(), line.ID, plan1.ID, 0)
		assert.NoError(t, err)
		err = service.AddPlanToPriceLine(context.Background(), line.ID, plan2.ID, 1)
		assert.NoError(t, err)

		// Get plans for line
		plans, err := service.GetPlansForPriceLine(context.Background(), line.ID)
		assert.NoError(t, err)
		assert.Len(t, plans, 2)

		// Verify order is correct
		assert.Equal(t, "Plan 1", plans[0].Name)
		assert.Equal(t, "Plan 2", plans[1].Name)
	}, getPricingTestOptions())
}

func TestPricingService_GetPlansForPriceLine_Empty(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingTestContext(tb, ctx, false)

		// Create price line without plans
		line := createTestPriceLine()
		err := service.CreatePriceLine(context.Background(), line)
		assert.NoError(t, err)

		// Get plans for line
		plans, err := service.GetPlansForPriceLine(context.Background(), line.ID)
		assert.NoError(t, err)
		assert.Len(t, plans, 0)
	}, getPricingTestOptions())
}

// ============================================================
// CreateGatewayProductMapping tests
// ============================================================

func TestPricingService_CreateGatewayProductMapping(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingTestContext(tb, ctx, true)

		// Create a plan first
		plan := createAndVerifyPlan(t, service, createTestPricingPlan())

		// Create a plan period
		period := createTestPricingPlanPeriod(plan.ID)
		err := service.CreatePricingPlanPeriod(context.Background(), period)
		assert.NoError(t, err)

		// Create gateway mapping
		mapping := createTestGatewayProductMapping(&period.ID)
		err = service.CreateGatewayProductMapping(context.Background(), mapping)
		assert.NoError(t, err)
		assert.NotZero(t, mapping.ID)

		// Verify mapping
		retrieved, err := service.GetGatewayProductMapping(context.Background(), period.ID, "stripe")
		assert.NoError(t, err)
		assert.Equal(t, mapping.RemoteProductID, retrieved.RemoteProductID)
	}, getPricingTestOptions())
}

func TestPricingService_CreateGatewayProductMapping_InvalidData(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingTestContext(tb, ctx, false)

		// Create mapping without required fields
		planPeriodID := uint(99999)
		mapping := &models.GatewayProductMapping{
			PricingPlanPeriodID: &planPeriodID,
			GatewayType:         "", // Missing
		}
		err := service.CreateGatewayProductMapping(context.Background(), mapping)
		assert.Error(t, err)
	}, getPricingTestOptions())
}

// ============================================================
// UpdateGatewayProductMapping tests
// ============================================================

func TestPricingService_UpdateGatewayProductMapping(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingTestContext(tb, ctx, true)

		// Create a plan
		plan := createAndVerifyPlan(t, service, createTestPricingPlan())

		// Create a plan period
		period := createTestPricingPlanPeriod(plan.ID)
		err := service.CreatePricingPlanPeriod(context.Background(), period)
		assert.NoError(t, err)

		// Create mapping
		mapping := createTestGatewayProductMapping(&period.ID)
		err = service.CreateGatewayProductMapping(context.Background(), mapping)
		assert.NoError(t, err)

		// Update mapping
		updatedRemoteProductID := "prod_updated_456"
		updatedMapping := &models.GatewayProductMapping{
			RemoteProductID: updatedRemoteProductID,
			GatewayType:     "stripe",
			SyncStatus:      "synced",
		}
		err = service.UpdateGatewayProductMapping(context.Background(), mapping.ID, updatedMapping)
		assert.NoError(t, err)

		// Verify update
		retrieved, err := service.GetGatewayProductMapping(context.Background(), period.ID, "stripe")
		assert.NoError(t, err)
		assert.Equal(t, updatedRemoteProductID, retrieved.RemoteProductID)
		assert.Equal(t, "synced", retrieved.SyncStatus)
	}, getPricingTestOptions())
}

// ============================================================
// GetGatewayProductMapping tests
// ============================================================

func TestPricingService_GetGatewayProductMapping(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingTestContext(tb, ctx, true)

		// Create a plan
		plan := createAndVerifyPlan(t, service, createTestPricingPlan())

		// Create a plan period
		period := createTestPricingPlanPeriod(plan.ID)
		err := service.CreatePricingPlanPeriod(context.Background(), period)
		assert.NoError(t, err)

		// Create mapping
		mapping := createTestGatewayProductMapping(&period.ID)
		err = service.CreateGatewayProductMapping(context.Background(), mapping)
		assert.NoError(t, err)

		// Get mapping
		retrieved, err := service.GetGatewayProductMapping(context.Background(), period.ID, "stripe")
		assert.NoError(t, err)
		assert.NotNil(t, retrieved)
		assert.Equal(t, mapping.RemoteProductID, retrieved.RemoteProductID)
		assert.Equal(t, "stripe", retrieved.GatewayType)
	}, getPricingTestOptions())
}

func TestPricingService_GetGatewayProductMapping_NotFound(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingTestContext(tb, ctx, false)

		_, err := service.GetGatewayProductMapping(context.Background(), 99999, "stripe")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	}, getPricingTestOptions())
}

// ============================================================
// GetGatewayProductMappingsByPlan tests
// ============================================================

func TestPricingService_GetGatewayProductMappingsByPlan(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingTestContext(tb, ctx, true)

		// Create a plan
		plan := createAndVerifyPlan(t, service, createTestPricingPlan())

		// Create periods for the plan with different cadences
		monthlyRollingDays := 30
		yearlyRollingDays := 365

		period1 := &models.PricingPlanPeriod{
			PricingPlanID: plan.ID,
			Cadence:       "monthly",
			PriceUSD:      9.99,
			QuotaPlanID:   123,
			RollingDays:   &monthlyRollingDays,
		}
		err := service.CreatePricingPlanPeriod(context.Background(), period1)
		assert.NoError(t, err)

		period2 := &models.PricingPlanPeriod{
			PricingPlanID: plan.ID,
			Cadence:       "yearly",
			PriceUSD:      99.99,
			QuotaPlanID:   124,
			RollingDays:   &yearlyRollingDays,
		}
		err = service.CreatePricingPlanPeriod(context.Background(), period2)
		assert.NoError(t, err)

		// Create mappings for multiple gateways
		mapping1 := createTestGatewayProductMapping(&period1.ID)
		mapping1.GatewayType = "stripe"
		err = service.CreateGatewayProductMapping(context.Background(), mapping1)
		assert.NoError(t, err)

		mapping2 := createTestGatewayProductMapping(&period2.ID)
		mapping2.GatewayType = "paypal"
		err = service.CreateGatewayProductMapping(context.Background(), mapping2)
		assert.NoError(t, err)

		// Get all mappings for plan
		mappings, err := service.GetGatewayProductMappingsByPlan(context.Background(), plan.ID)
		assert.NoError(t, err)
		assert.Len(t, mappings, 2)
	}, getPricingTestOptions())
}

func TestPricingService_GetGatewayProductMappingsByPlan_NotFound(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingTestContext(tb, ctx, false)

		mappings, err := service.GetGatewayProductMappingsByPlan(context.Background(), 99999)
		assert.NoError(t, err)
		assert.Len(t, mappings, 0)
	}, getPricingTestOptions())
}

// ============================================================
// UpdateGatewaySyncStatus tests
// ============================================================

func TestPricingService_UpdateGatewaySyncStatus(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingTestContext(tb, ctx, true)

		// Create a plan
		plan := createAndVerifyPlan(t, service, createTestPricingPlan())

		// Create a plan period
		period := createTestPricingPlanPeriod(plan.ID)
		err := service.CreatePricingPlanPeriod(context.Background(), period)
		assert.NoError(t, err)

		// Create mapping
		mapping := createTestGatewayProductMapping(&period.ID)
		mapping.SyncStatus = "pending"
		err = service.CreateGatewayProductMapping(context.Background(), mapping)
		assert.NoError(t, err)

		// Update sync status
		syncResult := pluginCore.SyncResult{
			ProductID: "prod_synced_789",
		}
		err = service.UpdateGatewaySyncStatus(context.Background(), period.ID, "stripe", syncResult)
		assert.NoError(t, err)

		// Verify update
		retrieved, err := service.GetGatewayProductMapping(context.Background(), period.ID, "stripe")
		assert.NoError(t, err)
		assert.Equal(t, "synced", retrieved.SyncStatus)
		assert.Equal(t, "prod_synced_789", retrieved.RemoteProductID)
		assert.NotNil(t, retrieved.LastSyncedAt)
		assert.Equal(t, 0, retrieved.Retries)
		assert.Empty(t, retrieved.ErrorMessage)
	}, getPricingTestOptions())
}

// ============================================================
// RecordGatewaySyncError tests
// ============================================================

func TestPricingService_RecordGatewaySyncError(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingTestContext(tb, ctx, true)

		// Create a plan
		plan := createAndVerifyPlan(t, service, createTestPricingPlan())

		// Create a plan period
		period := createTestPricingPlanPeriod(plan.ID)
		err := service.CreatePricingPlanPeriod(context.Background(), period)
		assert.NoError(t, err)

		// Create mapping
		mapping := createTestGatewayProductMapping(&period.ID)
		err = service.CreateGatewayProductMapping(context.Background(), mapping)
		assert.NoError(t, err)

		// Record sync error
		testErr := errors.New("test sync error")
		err = service.RecordGatewaySyncError(context.Background(), period.ID, "stripe", testErr)
		assert.NoError(t, err)

		// Verify error recorded
		retrieved, err := service.GetGatewayProductMapping(context.Background(), period.ID, "stripe")
		assert.NoError(t, err)
		assert.Equal(t, "error", retrieved.SyncStatus)
		assert.NotEmpty(t, retrieved.ErrorMessage)
		assert.Equal(t, 1, retrieved.Retries)
		assert.NotNil(t, retrieved.LastSyncedAt)
	}, getPricingTestOptions())
}

func TestPricingService_RecordGatewaySyncError_IncrementsRetries(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingTestContext(tb, ctx, true)

		// Create a plan
		plan := createAndVerifyPlan(t, service, createTestPricingPlan())

		// Create a plan period
		period := createTestPricingPlanPeriod(plan.ID)
		err := service.CreatePricingPlanPeriod(context.Background(), period)
		assert.NoError(t, err)

		// Create mapping with existing retries
		mapping := createTestGatewayProductMapping(&period.ID)
		mapping.Retries = 3
		err = service.CreateGatewayProductMapping(context.Background(), mapping)
		assert.NoError(t, err)

		// Record first error
		testErr := errors.New("test sync error")
		err = service.RecordGatewaySyncError(context.Background(), period.ID, "stripe", testErr)
		assert.NoError(t, err)

		// Verify retries incremented
		retrieved, err := service.GetGatewayProductMapping(context.Background(), period.ID, "stripe")
		assert.NoError(t, err)
		assert.Equal(t, 4, retrieved.Retries)

		// Record another error
		err = service.RecordGatewaySyncError(context.Background(), period.ID, "stripe", testErr)
		assert.NoError(t, err)

		retrieved, err = service.GetGatewayProductMapping(context.Background(), period.ID, "stripe")
		assert.NoError(t, err)
		assert.Equal(t, 5, retrieved.Retries)
	}, getPricingTestOptions())
}

// ============================================================
// DeleteGatewayProductMapping tests
// ============================================================

func TestPricingService_DeleteGatewayProductMapping(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingTestContext(tb, ctx, true)

		// Create a plan
		plan := createAndVerifyPlan(t, service, createTestPricingPlan())

		// Create a plan period
		period := createTestPricingPlanPeriod(plan.ID)
		err := service.CreatePricingPlanPeriod(context.Background(), period)
		assert.NoError(t, err)

		// Create mapping
		mapping := createTestGatewayProductMapping(&period.ID)
		err = service.CreateGatewayProductMapping(context.Background(), mapping)
		assert.NoError(t, err)

		// Delete mapping
		err = service.DeleteGatewayProductMapping(context.Background(), mapping.ID)
		assert.NoError(t, err)

		// Verify deletion (should be not found)
		_, err = service.GetGatewayProductMapping(context.Background(), period.ID, "stripe")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	}, getPricingTestOptions())
}

// ============================================================
// GetPendingSyncMappings tests
// ============================================================

func TestPricingService_GetPendingSyncMappings(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingTestContext(tb, ctx, true)

		// Create plans and mappings
		for i := 0; i < 3; i++ {
			plan := createTestPricingPlan()
			plan.Name = "Plan " + string(rune('0'+i))
			err := service.CreatePricingPlan(context.Background(), plan)
			assert.NoError(t, err)

			period := createTestPricingPlanPeriod(plan.ID)
			err = service.CreatePricingPlanPeriod(context.Background(), period)
			assert.NoError(t, err)

			mapping := createTestGatewayProductMapping(&period.ID)
			mapping.RemoteProductID = "prod_test_" + string(rune('0'+i))
			mapping.SyncStatus = "pending"
			err = service.CreateGatewayProductMapping(context.Background(), mapping)
			assert.NoError(t, err)
		}

		// Get pending mappings
		mappings, err := service.GetPendingSyncMappings(context.Background(), "")
		assert.NoError(t, err)
		assert.Len(t, mappings, 3)
	}, getPricingTestOptions())
}

func TestPricingService_GetPendingSyncMappings_WithGatewayType(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingTestContext(tb, ctx, true)

		// Create one stripe mapping
		plan1 := createTestPricingPlan()
		err := service.CreatePricingPlan(context.Background(), plan1)
		assert.NoError(t, err)

		period1 := createTestPricingPlanPeriod(plan1.ID)
		err = service.CreatePricingPlanPeriod(context.Background(), period1)
		assert.NoError(t, err)

		mapping1 := createTestGatewayProductMapping(&period1.ID)
		mapping1.GatewayType = "stripe"
		mapping1.SyncStatus = "pending"
		err = service.CreateGatewayProductMapping(context.Background(), mapping1)
		assert.NoError(t, err)

		// Create one paypal mapping
		plan2 := createTestPricingPlan()
		err = service.CreatePricingPlan(context.Background(), plan2)
		assert.NoError(t, err)

		period2 := createTestPricingPlanPeriod(plan2.ID)
		err = service.CreatePricingPlanPeriod(context.Background(), period2)
		assert.NoError(t, err)

		mapping2 := createTestGatewayProductMapping(&period2.ID)
		mapping2.GatewayType = "paypal"
		mapping2.SyncStatus = "pending"
		err = service.CreateGatewayProductMapping(context.Background(), mapping2)
		assert.NoError(t, err)

		// Get pending stripe mappings only
		mappings, err := service.GetPendingSyncMappings(context.Background(), "stripe")
		assert.NoError(t, err)
		assert.Len(t, mappings, 1)
		assert.Equal(t, "stripe", mappings[0].GatewayType)
	}, getPricingTestOptions())
}

func TestPricingService_GetPendingSyncMappings_IncludesErrorState(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingTestContext(tb, ctx, true)

		// Create pending mapping
		plan1 := createTestPricingPlan()
		err := service.CreatePricingPlan(context.Background(), plan1)
		assert.NoError(t, err)

		period1 := createTestPricingPlanPeriod(plan1.ID)
		err = service.CreatePricingPlanPeriod(context.Background(), period1)
		assert.NoError(t, err)

		mapping1 := createTestGatewayProductMapping(&period1.ID)
		mapping1.SyncStatus = "pending"
		err = service.CreateGatewayProductMapping(context.Background(), mapping1)
		assert.NoError(t, err)

		// Create error mapping
		plan2 := createTestPricingPlan()
		err = service.CreatePricingPlan(context.Background(), plan2)
		assert.NoError(t, err)

		period2 := createTestPricingPlanPeriod(plan2.ID)
		err = service.CreatePricingPlanPeriod(context.Background(), period2)
		assert.NoError(t, err)

		mapping2 := createTestGatewayProductMapping(&period2.ID)
		mapping2.SyncStatus = "error"
		err = service.CreateGatewayProductMapping(context.Background(), mapping2)
		assert.NoError(t, err)

		// Create synced mapping (not included)
		plan3 := createTestPricingPlan()
		err = service.CreatePricingPlan(context.Background(), plan3)
		assert.NoError(t, err)

		period3 := createTestPricingPlanPeriod(plan3.ID)
		err = service.CreatePricingPlanPeriod(context.Background(), period3)
		assert.NoError(t, err)

		mapping3 := createTestGatewayProductMapping(&period3.ID)
		mapping3.SyncStatus = "synced"
		err = service.CreateGatewayProductMapping(context.Background(), mapping3)
		assert.NoError(t, err)

		// Get pending mappings (should include pending and error)
		mappings, err := service.GetPendingSyncMappings(context.Background(), "")
		assert.NoError(t, err)
		assert.Len(t, mappings, 2)

		statuses := make(map[string]bool)
		for _, m := range mappings {
			statuses[m.SyncStatus] = true
		}
		assert.True(t, statuses["pending"])
		assert.True(t, statuses["error"])
		assert.False(t, statuses["synced"])
	}, getPricingTestOptions())
}

// ============================================================
// GetPriceLinesForPlan tests
// ============================================================

func TestPricingService_GetPriceLinesForPlan(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingTestContext(tb, ctx, true)

		// Create plan and multiple price lines
		plan := createAndVerifyPlan(t, service, createTestPricingPlan())

		line1 := createTestPriceLine()
		line1.Name = "Line 1"
		err := service.CreatePriceLine(context.Background(), line1)
		assert.NoError(t, err)

		line2 := createTestPriceLine()
		line2.Name = "Line 2"
		err = service.CreatePriceLine(context.Background(), line2)
		assert.NoError(t, err)

		// Add plan to both lines
		err = service.AddPlanToPriceLine(context.Background(), line1.ID, plan.ID, 0)
		assert.NoError(t, err)
		err = service.AddPlanToPriceLine(context.Background(), line2.ID, plan.ID, 0)
		assert.NoError(t, err)

		// Get price lines for plan
		priceLines, err := service.GetPriceLinesForPlan(context.Background(), plan.ID)
		assert.NoError(t, err)
		assert.Len(t, priceLines, 2)
	}, getPricingTestOptions())
}

func TestPricingService_GetPriceLinesForPlan_NotFound(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingTestContext(tb, ctx, false)

		priceLines, err := service.GetPriceLinesForPlan(context.Background(), 99999)
		assert.NoError(t, err)
		assert.Len(t, priceLines, 0)
	}, getPricingTestOptions())
}
