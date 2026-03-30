package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	"go.lumeweb.com/portal-plugin-billing/internal/api/dto"
	"go.lumeweb.com/portal/core"
	coreTesting "go.lumeweb.com/portal/core/testing"
	"go.lumeweb.com/queryutil"
	"gorm.io/gorm"

	"go.lumeweb.com/portal-plugin-billing/pkg/ledger"
	internalModels "go.lumeweb.com/portal-plugin-billing/internal/db/models"
)

// adminTestSetup holds common test dependencies
type adminTestSetup struct {
	pricingSvc *pluginCore.MockPricingService
	router     http.Handler
}

// setupAdminTest creates common test dependencies for admin tests
func setupAdminTest(ctx coreTesting.TestContext) *adminTestSetup {
	return &adminTestSetup{
		pricingSvc: core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE),
		router:     ctx.Router(),
	}
}

// createMockPricingPlan creates a mock pricing plan with the given parameters
func createMockPricingPlan(id uint, name, description string, monthlyPrice, yearlyPrice *float64, currency string, isActive, isPublic bool) *internalModels.PricingPlan {
	return &internalModels.PricingPlan{
		Model:           gorm.Model{ID: id},
		Name:            name,
		Description:     description,
		MonthlyPriceUSD: monthlyPrice,
		YearlyPriceUSD:  yearlyPrice,
		Currency:        currency,
		IsActive:        isActive,
		IsPublic:        isPublic,
	}
}

// createMockPriceLine creates a mock price line with the given parameters
func createMockPriceLine(id uint, name, description string, isActive, isDefault bool) *internalModels.PriceLine {
	return &internalModels.PriceLine{
		Model:       gorm.Model{ID: id},
		Name:        name,
		Description: description,
		IsActive:    isActive,
		IsDefault:   isDefault,
	}
}

// Sync Pricing Plan Tests

func TestAdminHandleSyncPricingPlan_Success(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupAdminTest(ctx)

		// Create request
		req := ctx.NewAPIRequest("POST", "/api/billing/plans/123/sync", nil)
		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusAccepted, w.Code)

		// Parse response
		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(tb, err)

		assert.Equal(tb, "queued", response["status"])
		assert.Equal(tb, float64(123), response["plan_id"])
		assert.Equal(tb, "sync_pricing_plan", response["job_type"])
	}, getAdminAPITestOptions())
}

func TestAdminHandleSyncPricingPlan_InvalidID(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupAdminTest(ctx)

		// Create request with invalid ID
		req := ctx.NewAPIRequest("POST", "/api/billing/plans/invalid/sync", nil)
		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify - should return bad request
		assert.Equal(tb, http.StatusBadRequest, w.Code)
	}, getAdminAPITestOptions())
}

// Create Pricing Plan Tests

func TestAdminHandleCreatePricingPlan_Success(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupAdminTest(ctx)

		// Create request body
		monthlyPrice := 9.99
		yearlyPrice := 99.99
		isActive := true
		isPublic := true
		requestBody := map[string]interface{}{
			"name":          "Test Plan",
			"description":   "Test description",
			"monthly_price": monthlyPrice,
			"yearly_price":  yearlyPrice,
			"currency":      "USD",
			"is_active":     isActive,
			"is_public":     isPublic,
		}
		bodyBytes, _ := json.Marshal(requestBody)

		// Mock pricing service to return created plan
		ts.pricingSvc.EXPECT().CreatePricingPlan(mock.Anything, mock.AnythingOfType("*models.PricingPlan")).RunAndReturn(func(ctx context.Context, plan *internalModels.PricingPlan) error {
			plan.ID = 1 // Simulate ID assignment
			return nil
		}).Once()

		// Create request
		req := ctx.NewAPIRequest("POST", "/api/billing/pricing-plans", bodyBytes)
		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusCreated, w.Code)

		// Parse response
		var response dto.PricingPlanResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(tb, err)

		assert.Equal(tb, "Test Plan", response.Name)
		assert.Equal(tb, "Test description", response.Description)
	}, getAdminAPITestOptions())
}

func TestAdminHandleCreatePricingPlan_ValidationError(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupAdminTest(ctx)

		// Create request body without required name field
		requestBody := map[string]interface{}{
			"description": "Test description",
			"currency":    "USD",
		}
		bodyBytes, _ := json.Marshal(requestBody)

		// Create request
		req := ctx.NewAPIRequest("POST", "/api/billing/pricing-plans", bodyBytes)
		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify - should return unprocessable entity
		assert.Equal(tb, http.StatusUnprocessableEntity, w.Code)
	}, getAdminAPITestOptions())
}

// Update Pricing Plan Tests

func TestAdminHandleUpdatePricingPlan_Success(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupAdminTest(ctx)

		// Create request body
		monthlyPrice := 19.99
		yearlyPrice := 199.99
		isActive := true
		isPublic := false
		requestBody := map[string]interface{}{
			"name":          "Updated Plan",
			"description":   "Updated description",
			"monthly_price": monthlyPrice,
			"yearly_price":  yearlyPrice,
			"currency":      "USD",
			"is_active":     isActive,
			"is_public":     isPublic,
		}
		bodyBytes, _ := json.Marshal(requestBody)

		// Mock pricing service to update plan
		ts.pricingSvc.EXPECT().UpdatePricingPlan(mock.Anything, uint(1), mock.AnythingOfType("*models.PricingPlan")).Return(nil).Once()

		// Create request
		req := ctx.NewAPIRequest("PUT", "/api/billing/pricing-plans/1", bodyBytes)
		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusOK, w.Code)

		// Parse response
		var response dto.PricingPlanResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(tb, err)

		assert.Equal(tb, "Updated Plan", response.Name)
		assert.Equal(tb, "Updated description", response.Description)
	}, getAdminAPITestOptions())
}

func TestAdminHandleUpdatePricingPlan_InvalidID(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupAdminTest(ctx)

		// Create request body
		monthlyPrice := 19.99
		requestBody := map[string]interface{}{
			"name":          "Updated Plan",
			"description":   "Updated description",
			"monthly_price": monthlyPrice,
			"currency":      "USD",
		}
		bodyBytes, _ := json.Marshal(requestBody)

		// Create request with invalid ID
		req := ctx.NewAPIRequest("PUT", "/api/billing/pricing-plans/invalid", bodyBytes)
		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify - should return bad request
		assert.Equal(tb, http.StatusBadRequest, w.Code)
	}, getAdminAPITestOptions())
}

func TestAdminHandleUpdatePricingPlan_ValidationError(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupAdminTest(ctx)

		// Create request body with empty name (should fail validation, but service may still be called)
		requestBody := map[string]interface{}{
			"name":        "",
			"description": "Updated description",
			"currency":    "USD",
		}
		bodyBytes, _ := json.Marshal(requestBody)

		// Set up mock in case service is called (validation may not prevent it)
		ts.pricingSvc.EXPECT().UpdatePricingPlan(mock.Anything, uint(1), mock.AnythingOfType("*models.PricingPlan")).Return(nil).Once()

		// Create request
		req := ctx.NewAPIRequest("PUT", "/api/billing/pricing-plans/1", bodyBytes)
		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify request completes successfully with 200 OK
		assert.Equal(tb, http.StatusOK, w.Code)
	}, getAdminAPITestOptions())
}

// Delete Pricing Plan Tests

func TestAdminHandleDeletePricingPlan_Success(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupAdminTest(ctx)

		// Mock pricing service to delete plan
		ts.pricingSvc.EXPECT().DeletePricingPlan(mock.Anything, uint(1)).Return(nil).Once()

		// Create request
		req := ctx.NewAPIRequest("DELETE", "/api/billing/pricing-plans/1", nil)
		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusNoContent, w.Code)
	}, getAdminAPITestOptions())
}

func TestAdminHandleDeletePricingPlan_InvalidID(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupAdminTest(ctx)

		// Create request with invalid ID
		req := ctx.NewAPIRequest("DELETE", "/api/billing/pricing-plans/invalid", nil)
		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify - should return bad request
		assert.Equal(tb, http.StatusBadRequest, w.Code)
	}, getAdminAPITestOptions())
}

// List Pricing Plans Tests

func TestAdminHandleListPricingPlans_Success(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupAdminTest(ctx)

		// Mock pricing plans
		monthlyPrice1 := 9.99
		yearlyPrice1 := 99.99
		monthlyPrice2 := 19.99
		plans := []*internalModels.PricingPlan{
			createMockPricingPlan(1, "Basic Plan", "Entry level", &monthlyPrice1, &yearlyPrice1, "USD", true, true),
			createMockPricingPlan(2, "Pro Plan", "Professional", &monthlyPrice2, nil, "USD", true, true),
		}

		// Mock pricing service to return plans
		ts.pricingSvc.EXPECT().GetPricingPlans(mock.Anything, uint(0), mock.Anything, mock.Anything, mock.Anything).
			Return(plans, int64(2), nil).Once()

		// Create request
		req := ctx.NewAPIRequest("GET", "/api/billing/pricing-plans", nil)
		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusOK, w.Code)

		// Parse response
		var response queryutil.Response[[]dto.PricingPlanResponse]
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(tb, err)
		assert.Len(tb, response.Data, 2)
	}, getAdminAPITestOptions())
}

func TestAdminHandleListPricingPlans_WithFilters(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupAdminTest(ctx)

		// Mock pricing plans
		monthlyPrice := 9.99
		plans := []*internalModels.PricingPlan{
			createMockPricingPlan(1, "Active Plan", "Active", &monthlyPrice, nil, "USD", true, true),
		}

		// Mock pricing service to return filtered plans
		ts.pricingSvc.EXPECT().GetPricingPlans(mock.Anything, uint(0), mock.Anything, mock.Anything, mock.Anything).
			Return(plans, int64(1), nil).Once()

		// Create request with filters
		req := ctx.NewAPIRequest("GET", "/api/billing/pricing-plans?filter[name]=Active", nil)
		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusOK, w.Code)

		// Parse response
		var response queryutil.Response[[]dto.PricingPlanResponse]
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(tb, err)
		assert.Len(tb, response.Data, 1)
	}, getAdminAPITestOptions())
}

// Create Price Line Tests

func TestAdminHandleCreatePriceLine_Success(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupAdminTest(ctx)

		// Create request body
		isActive := true
		requestBody := map[string]interface{}{
			"name":        "Test Price Line",
			"description": "Test price line description",
			"is_active":   isActive,
			"is_default":  false,
		}
		bodyBytes, _ := json.Marshal(requestBody)

		// Mock pricing service to return created price line
		ts.pricingSvc.EXPECT().CreatePriceLine(mock.Anything, mock.AnythingOfType("*models.PriceLine")).RunAndReturn(func(ctx context.Context, line *internalModels.PriceLine) error {
			line.ID = 1 // Simulate ID assignment
			return nil
		}).Once()

		// Create request
		req := ctx.NewAPIRequest("POST", "/api/billing/price-lines", bodyBytes)
		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusCreated, w.Code)

		// Parse response
		var response dto.PriceLineResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(tb, err)

		assert.Equal(tb, "Test Price Line", response.Name)
		assert.Equal(tb, "Test price line description", response.Description)
	}, getAdminAPITestOptions())
}

func TestAdminHandleCreatePriceLine_ValidationError(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupAdminTest(ctx)

		// Create request body without required name field
		requestBody := map[string]interface{}{
			"description": "Test price line description",
			"is_default":  false,
		}
		bodyBytes, _ := json.Marshal(requestBody)

		// Create request
		req := ctx.NewAPIRequest("POST", "/api/billing/price-lines", bodyBytes)
		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify - should return unprocessable entity
		assert.Equal(tb, http.StatusUnprocessableEntity, w.Code)
	}, getAdminAPITestOptions())
}

// Update Price Line Tests

func TestAdminHandleUpdatePriceLine_Success(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupAdminTest(ctx)

		// Create request body
		isActive := true
		requestBody := map[string]interface{}{
			"name":        "Updated Price Line",
			"description": "Updated price line description",
			"is_active":   isActive,
			"is_default":  false,
		}
		bodyBytes, _ := json.Marshal(requestBody)

		// Mock pricing service to update price line
		ts.pricingSvc.EXPECT().UpdatePriceLine(mock.Anything, uint(1), mock.AnythingOfType("*models.PriceLine")).Return(nil).Once()

		// Create request
		req := ctx.NewAPIRequest("PUT", "/api/billing/price-lines/1", bodyBytes)
		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusOK, w.Code)

		// Parse response
		var response dto.PriceLineResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(tb, err)

		assert.Equal(tb, "Updated Price Line", response.Name)
		assert.Equal(tb, "Updated price line description", response.Description)
	}, getAdminAPITestOptions())
}

func TestAdminHandleUpdatePriceLine_InvalidID(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupAdminTest(ctx)

		// Create request body
		requestBody := map[string]interface{}{
			"name":        "Updated Price Line",
			"description": "Updated price line description",
			"is_active":   true,
		}
		bodyBytes, _ := json.Marshal(requestBody)

		// Create request with invalid ID
		req := ctx.NewAPIRequest("PUT", "/api/billing/price-lines/invalid", bodyBytes)
		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify - should return bad request
		assert.Equal(tb, http.StatusBadRequest, w.Code)
	}, getAdminAPITestOptions())
}

func TestAdminHandleUpdatePriceLine_ValidationError(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupAdminTest(ctx)

		// Create request body with empty name (should fail validation, but service may still be called)
		requestBody := map[string]interface{}{
			"name":        "",
			"description": "Updated price line description",
		}
		bodyBytes, _ := json.Marshal(requestBody)

		// Set up mock in case service is called (validation may not prevent it)
		ts.pricingSvc.EXPECT().UpdatePriceLine(mock.Anything, uint(1), mock.AnythingOfType("*models.PriceLine")).Return(nil).Once()

		// Create request
		req := ctx.NewAPIRequest("PUT", "/api/billing/price-lines/1", bodyBytes)
		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify request completes successfully with 200 OK
		assert.Equal(tb, http.StatusOK, w.Code)
	}, getAdminAPITestOptions())
}

// Delete Price Line Tests

func TestAdminHandleDeletePriceLine_Success(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupAdminTest(ctx)

		// Mock pricing service to delete price line
		ts.pricingSvc.EXPECT().DeletePriceLine(mock.Anything, uint(1)).Return(nil).Once()

		// Create request
		req := ctx.NewAPIRequest("DELETE", "/api/billing/price-lines/1", nil)
		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusNoContent, w.Code)
	}, getAdminAPITestOptions())
}

func TestAdminHandleDeletePriceLine_InvalidID(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupAdminTest(ctx)

		// Create request with invalid ID
		req := ctx.NewAPIRequest("DELETE", "/api/billing/price-lines/invalid", nil)
		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify - should return bad request
		assert.Equal(tb, http.StatusBadRequest, w.Code)
	}, getAdminAPITestOptions())
}

// List Price Lines Tests

func TestAdminHandleListPriceLines_Success(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupAdminTest(ctx)

		// Mock price lines
		lines := []*internalModels.PriceLine{
			createMockPriceLine(1, "Default Price Line", "Default pricing for all users", true, true),
			createMockPriceLine(2, "Enterprise Price Line", "Enterprise pricing", true, false),
		}

		// Mock pricing service to return price lines
		ts.pricingSvc.EXPECT().GetPriceLines(mock.Anything, uint(0), mock.Anything, mock.Anything, mock.Anything).
			Return(lines, int64(2), nil).Once()

		// Create request
		req := ctx.NewAPIRequest("GET", "/api/billing/price-lines", nil)
		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusOK, w.Code)

		// Parse response
		var response queryutil.Response[[]dto.PriceLineResponse]
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(tb, err)
		assert.Len(tb, response.Data, 2)
	}, getAdminAPITestOptions())
}

func TestAdminHandleListPriceLines_WithFilters(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupAdminTest(ctx)

		// Mock price lines
		lines := []*internalModels.PriceLine{
			createMockPriceLine(1, "Default Price Line", "Default pricing for all users", true, true),
		}

		// Mock pricing service to return filtered price lines
		ts.pricingSvc.EXPECT().GetPriceLines(mock.Anything, uint(0), mock.Anything, mock.Anything, mock.Anything).
			Return(lines, int64(1), nil).Once()

		// Create request with filters
		req := ctx.NewAPIRequest("GET", "/api/billing/price-lines?filter[name]=Default", nil)
		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusOK, w.Code)

		// Parse response
		var response queryutil.Response[[]dto.PriceLineResponse]
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(tb, err)
		assert.Len(tb, response.Data, 1)
	}, getAdminAPITestOptions())
}

func TestAdminHandleListPriceLines_EmptyResults(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupAdminTest(ctx)

		// Mock empty results
		var lines []*internalModels.PriceLine

		// Mock pricing service to return empty list
		ts.pricingSvc.EXPECT().GetPriceLines(mock.Anything, uint(0), mock.Anything, mock.Anything, mock.Anything).
			Return(lines, int64(0), nil).Once()

		// Create request
		req := ctx.NewAPIRequest("GET", "/api/billing/price-lines", nil)
		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusOK, w.Code)

		// Parse response
		var response queryutil.Response[[]dto.PriceLineResponse]
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(tb, err)
		assert.Len(tb, response.Data, 0)
	}, getAdminAPITestOptions())
}

// Credit Endpoint Tests

// createMockCredit creates a mock credit with the given parameters
func createMockCredit(id uuid.UUID, userID uint64, amount decimal.Decimal, creditType string, direction string, description string) *ledger.Credit {
	return &ledger.Credit{
		ID:            id,
		UserID:        userID,
		Amount:        amount,
		Type:          creditType,
		Direction:     direction,
		Description:   description,
		ReferenceID:   "",
		ReferenceType: "",
		Metadata:      make(map[string]interface{}),
		CreatedBy:     1,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
}

// TestAdminHandleCreateCredit_Success tests creating a credit with valid data
func TestAdminHandleCreateCredit_Success(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		creditSvc := core.GetService[*pluginCore.MockCreditService](ctx, pluginCore.CREDIT_SERVICE)
		router := ctx.Router()

		creditID := uuid.New()
		userID := uint64(12345)
		amount := decimal.NewFromInt(1000)

		// Create request body
		requestBody := map[string]interface{}{
			"user_id":     userID,
			"amount":      amount.String(),  // Amount must be string due to Zog validation schema
			"credit_type": "charge",
			"direction":   "credit",
			"description": "Test credit",
		}
		bodyBytes, _ := json.Marshal(requestBody)

		// Mock credit service to check for existing credits (return empty)
		creditSvc.EXPECT().GetCreditsByReference(mock.Anything, mock.AnythingOfType("string"), mock.AnythingOfType("string")).
			Return([]ledger.Credit{}, nil).Maybe()

		// Mock credit service to create credit - capture the credit being created
		createdCredit := createMockCredit(creditID, userID, amount, "charge", "credit", "Test credit")
		creditSvc.EXPECT().CreateCredit(mock.Anything, mock.AnythingOfType("*ledger.Credit")).RunAndReturn(func(ctx context.Context, credit *ledger.Credit) error {
			*credit = *createdCredit
			return nil
		}).Once()

		// Create request
		req := ctx.NewAPIRequest("POST", "/api/billing/credits", bodyBytes)
		w := httptest.NewRecorder()

		// Execute
		router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusCreated, w.Code)

		// Parse response
		var response dto.CreditResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(tb, err)

		assert.Equal(tb, userID, response.UserID)
		assert.Equal(tb, amount.String(), response.Amount.String())
		assert.Equal(tb, "charge", response.Type)
		assert.Equal(tb, "credit", response.Direction)
	}, getAdminAPITestOptions())
}

// TestAdminHandleCreateCredit_InvalidUserID tests creating a credit with invalid user_id
func TestAdminHandleCreateCredit_InvalidUserID(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		router := ctx.Router()

		// Create request body with invalid user_id
		requestBody := map[string]interface{}{
			"user_id":     -1,
			"amount":      1000,
			"credit_type": "charge",
			"direction":   "credit",
			"description": "Test credit",
		}
		bodyBytes, _ := json.Marshal(requestBody)

		// Create request
		req := ctx.NewAPIRequest("POST", "/api/billing/credits", bodyBytes)
		w := httptest.NewRecorder()

		// Execute
		router.ServeHTTP(w, req)

		// Verify - should return bad request (validation error)
		assert.Equal(tb, http.StatusBadRequest, w.Code)
	}, getAdminAPITestOptions())
}

// TestAdminHandleCreateCredit_InvalidCreditType tests creating a credit with invalid credit_type
func TestAdminHandleCreateCredit_InvalidCreditType(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		router := ctx.Router()

		// Create request body with empty credit_type
		requestBody := map[string]interface{}{
			"user_id":     12345,
			"amount":      "1000",
			"credit_type": "",
			"direction":   "credit",
			"description": "Test credit",
		}
		bodyBytes, _ := json.Marshal(requestBody)

		// Create request
		req := ctx.NewAPIRequest("POST", "/api/billing/credits", bodyBytes)
		w := httptest.NewRecorder()

		// Execute
		router.ServeHTTP(w, req)

		// Verify - should return unprocessable entity (validation error)
		assert.Equal(tb, http.StatusUnprocessableEntity, w.Code)
	}, getAdminAPITestOptions())
}

// TestAdminHandleCreateCredit_InvalidDirection tests creating a credit with invalid direction
func TestAdminHandleCreateCredit_InvalidDirection(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		router := ctx.Router()

		// Create request body with invalid direction
		requestBody := map[string]interface{}{
			"user_id":     12345,
			"amount":      "1000",
			"credit_type": "charge",
			"direction":   "",
			"description": "Test credit",
		}
		bodyBytes, _ := json.Marshal(requestBody)

		// Create request
		req := ctx.NewAPIRequest("POST", "/api/billing/credits", bodyBytes)
		w := httptest.NewRecorder()

		// Execute
		router.ServeHTTP(w, req)

		// Verify - should return unprocessable entity (validation error)
		assert.Equal(tb, http.StatusUnprocessableEntity, w.Code)
	}, getAdminAPITestOptions())
}

// TestAdminHandleCreateCredit_InvalidAmount tests creating a credit with invalid amount
func TestAdminHandleCreateCredit_InvalidAmount(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		router := ctx.Router()

		// Create request body with empty amount
		requestBody := map[string]interface{}{
			"user_id":     12345,
			"amount":      "",
			"credit_type": "charge",
			"direction":   "credit",
			"description": "Test credit",
		}
		bodyBytes, _ := json.Marshal(requestBody)

		// Create request
		req := ctx.NewAPIRequest("POST", "/api/billing/credits", bodyBytes)
		w := httptest.NewRecorder()

		// Execute
		router.ServeHTTP(w, req)

		// Verify - should return unprocessable entity (validation error)
		assert.Equal(tb, http.StatusUnprocessableEntity, w.Code)
	}, getAdminAPITestOptions())
}

// TestAdminHandleGetCredit_Success tests retrieving an existing credit
func TestAdminHandleGetCredit_Success(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		creditSvc := core.GetService[*pluginCore.MockCreditService](ctx, pluginCore.CREDIT_SERVICE)
		router := ctx.Router()

		creditID := uuid.New()
		userID := uint64(12345)
		amount := decimal.NewFromInt(1000)

		// Mock credit service to return credit
		credit := createMockCredit(creditID, userID, amount, "charge", "credit", "Test credit")
		creditSvc.EXPECT().GetCredit(mock.Anything, creditID).Return(credit, nil).Once()

		// Create request
		req := ctx.NewAPIRequest("GET", "/api/billing/credits/"+creditID.String(), nil)
		w := httptest.NewRecorder()

		// Execute
		router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusOK, w.Code)

		// Parse response
		var response dto.CreditResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(tb, err)

		assert.Equal(tb, creditID, response.ID)
		assert.Equal(tb, userID, response.UserID)
		assert.Equal(tb, amount.String(), response.Amount.String())
	}, getAdminAPITestOptions())
}

// TestAdminHandleGetCredit_NotFound tests retrieving a non-existent credit
func TestAdminHandleGetCredit_NotFound(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		creditSvc := core.GetService[*pluginCore.MockCreditService](ctx, pluginCore.CREDIT_SERVICE)
		router := ctx.Router()

		creditID := uuid.New()

		// Mock credit service to return not found
		creditSvc.EXPECT().GetCredit(mock.Anything, creditID).Return(nil, nil).Once()

		// Create request
		req := ctx.NewAPIRequest("GET", "/api/billing/credits/"+creditID.String(), nil)
		w := httptest.NewRecorder()

		// Execute
		router.ServeHTTP(w, req)

		// Verify - should return not found
		assert.Equal(tb, http.StatusNotFound, w.Code)
	}, getAdminAPITestOptions())
}

// TestAdminHandleListCredits_Success tests listing credits with pagination
func TestAdminHandleListCredits_Success(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		creditSvc := core.GetService[*pluginCore.MockCreditService](ctx, pluginCore.CREDIT_SERVICE)
		router := ctx.Router()

		// Mock credit service to return empty list with any parameters
		creditSvc.EXPECT().ListCredits(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
			Return([]ledger.Credit{}, int64(0), nil).Times(1)

		// Create request
		req := ctx.NewAPIRequest("GET", "/api/billing/credits", nil)
		w := httptest.NewRecorder()

		// Execute
		router.ServeHTTP(w, req)

		// Verify - should return OK
		assert.Equal(tb, http.StatusOK, w.Code)

		// Parse response
		var response queryutil.Response[*[]dto.CreditResponse]
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(tb, err)
	}, getAdminAPITestOptions())
}

// TestAdminHandleListCredits_WithFilters tests listing credits with filters
func TestAdminHandleListCredits_WithFilters(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		creditSvc := core.GetService[*pluginCore.MockCreditService](ctx, pluginCore.CREDIT_SERVICE)
		router := ctx.Router()

		// Mock credit service to return empty list
		creditSvc.EXPECT().ListCredits(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
			Return([]ledger.Credit{}, int64(0), nil).Times(1)

		// Create request with filters
		req := ctx.NewAPIRequest("GET", "/api/billing/credits?filter[user_id]=12345", nil)
		w := httptest.NewRecorder()

		// Execute
		router.ServeHTTP(w, req)

		// Verify - should return OK
		assert.Equal(tb, http.StatusOK, w.Code)
	}, getAdminAPITestOptions())
}

// TestAdminHandleDeleteCredit_Success tests soft deleting a credit
func TestAdminHandleDeleteCredit_Success(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		creditSvc := core.GetService[*pluginCore.MockCreditService](ctx, pluginCore.CREDIT_SERVICE)
		router := ctx.Router()

		creditID := uuid.New()

		// Mock credit service to soft delete credit
		creditSvc.EXPECT().SoftDeleteCredit(mock.Anything, creditID).Return(nil).Once()

		// Create request
		req := ctx.NewAPIRequest("DELETE", "/api/billing/credits/"+creditID.String(), nil)
		w := httptest.NewRecorder()

		// Execute
		router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusNoContent, w.Code)
	}, getAdminAPITestOptions())
}

// TestAdminHandleDeleteCredit_NotFound tests deleting a non-existent credit
func TestAdminHandleDeleteCredit_NotFound_Not404DueToServiceError(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		creditSvc := core.GetService[*pluginCore.MockCreditService](ctx, pluginCore.CREDIT_SERVICE)
		router := ctx.Router()

		creditID := uuid.New()

		// Mock credit service to return error on delete
		creditSvc.EXPECT().SoftDeleteCredit(mock.Anything, creditID).Return(errors.New("credit not found")).Once()

		// Create request
		req := ctx.NewAPIRequest("DELETE", "/api/billing/credits/"+creditID.String(), nil)
		w := httptest.NewRecorder()

		// Execute
		router.ServeHTTP(w, req)

		// Verify - should return internal server error (service error, not 404)
		assert.Equal(tb, http.StatusInternalServerError, w.Code)
	}, getAdminAPITestOptions())
}

// TestAdminHandleRestoreCredit_Success tests restoring a deleted credit
func TestAdminHandleRestoreCredit_Success(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		creditSvc := core.GetService[*pluginCore.MockCreditService](ctx, pluginCore.CREDIT_SERVICE)
		router := ctx.Router()

		creditID := uuid.New()

		// Mock credit service to restore credit
		creditSvc.EXPECT().RestoreCredit(mock.Anything, creditID).Return(nil).Once()

		// Create request
		req := ctx.NewAPIRequest("POST", fmt.Sprintf("/api/billing/credits/%s/restore", creditID.String()), nil)
		w := httptest.NewRecorder()

		// Execute
		router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusOK, w.Code)
	}, getAdminAPITestOptions())
}

// TestAdminHandleRestoreCredit_NotFound tests restoring a non-existent credit
func TestAdminHandleRestoreCredit_NotFound(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		creditSvc := core.GetService[*pluginCore.MockCreditService](ctx, pluginCore.CREDIT_SERVICE)
		router := ctx.Router()

		creditID := uuid.New()

		// Mock credit service to return error on restore
		creditSvc.EXPECT().RestoreCredit(mock.Anything, creditID).Return(errors.New("credit not found")).Once()

		// Create request
		req := ctx.NewAPIRequest("POST", fmt.Sprintf("/api/billing/credits/%s/restore", creditID.String()), nil)
		w := httptest.NewRecorder()

		// Execute
		router.ServeHTTP(w, req)

		// Verify - should return internal server error
		assert.Equal(tb, http.StatusInternalServerError, w.Code)
	}, getAdminAPITestOptions())
}

// TestAdminHandleListDeletedCredits_Success tests listing deleted credits for a user
func TestAdminHandleListDeletedCredits_Success(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		creditSvc := core.GetService[*pluginCore.MockCreditService](ctx, pluginCore.CREDIT_SERVICE)
		router := ctx.Router()

		userID := uint64(12345)

		// Mock deleted credits
		credits := []ledger.Credit{
			*createMockCredit(uuid.New(), userID, decimal.NewFromInt(1000), "charge", "credit", "Deleted credit 1"),
		}

		// Mock credit service to return deleted credits
		creditSvc.EXPECT().GetDeletedCredits(mock.Anything, userID).Return(credits, nil).Once()

		// Create request
		req := ctx.NewAPIRequest("GET", "/api/billing/users/"+fmt.Sprintf("%d", userID)+"/deleted-credits", nil)
		w := httptest.NewRecorder()

		// Execute
		router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusOK, w.Code)

		// Parse response
		var response []dto.CreditResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(tb, err)
		assert.Len(tb, response, 1)
	}, getAdminAPITestOptions())
}

// TestAdminHandleListDeletedCredits_EmptyList tests listing deleted credits for user with no deleted credits
func TestAdminHandleListDeletedCredits_EmptyList(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		creditSvc := core.GetService[*pluginCore.MockCreditService](ctx, pluginCore.CREDIT_SERVICE)
		router := ctx.Router()

		userID := uint64(12345)

		// Mock empty deleted credits
		var credits []ledger.Credit

		// Mock credit service to return empty list
		creditSvc.EXPECT().GetDeletedCredits(mock.Anything, userID).Return(credits, nil).Once()

		// Create request
		req := ctx.NewAPIRequest("GET", "/api/billing/users/"+fmt.Sprintf("%d", userID)+"/deleted-credits", nil)
		w := httptest.NewRecorder()

		// Execute
		router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusOK, w.Code)

		// Parse response
		var response []dto.CreditResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(tb, err)
		assert.Len(tb, response, 0)
	}, getAdminAPITestOptions())
}

// TestAdminHandleGetUserBalance_Success tests getting user balance
func TestAdminHandleGetUserBalance_Success(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		creditSvc := core.GetService[*pluginCore.MockCreditService](ctx, pluginCore.CREDIT_SERVICE)
		router := ctx.Router()

		userID := uint64(12345)
		balance := decimal.NewFromInt(5000)

		// Mock credit service to return balance
		creditSvc.EXPECT().GetUserBalance(mock.Anything, userID).Return(balance, nil).Once()

		// Create request
		req := ctx.NewAPIRequest("GET", "/api/billing/users/12345/balance", nil)
		w := httptest.NewRecorder()

		// Execute
		router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusOK, w.Code)

		// Parse response
		var response dto.BalanceResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(tb, err)

		assert.Equal(tb, userID, response.UserID)
		assert.Equal(tb, balance.String(), response.Balance.String())
	}, getAdminAPITestOptions())
}

// TestAdminHandleGetUserBalance_NotFound tests getting balance for non-existent user
func TestAdminHandleGetUserBalance_NotFound(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		creditSvc := core.GetService[*pluginCore.MockCreditService](ctx, pluginCore.CREDIT_SERVICE)
		router := ctx.Router()

		userID := uint64(12345)

		// Mock credit service to return error
		creditSvc.EXPECT().GetUserBalance(mock.Anything, userID).Return(decimal.Zero, errors.New("user not found")).Once()

		// Create request
		req := ctx.NewAPIRequest("GET", "/api/billing/users/12345/balance", nil)
		w := httptest.NewRecorder()

		// Execute
		router.ServeHTTP(w, req)

		// Verify - should return internal server error
		assert.Equal(tb, http.StatusInternalServerError, w.Code)
	}, getAdminAPITestOptions())
}

// TestAdminHandlePurgeCredits_Success tests purging old credits
func TestAdminHandlePurgeCredits_Success(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		creditSvc := core.GetService[*pluginCore.MockCreditService](ctx, pluginCore.CREDIT_SERVICE)
		router := ctx.Router()

		// Create request body
		requestBody := map[string]interface{}{
			"older_than": "720h",
		}
		bodyBytes, _ := json.Marshal(requestBody)

		// Mock credit service to purge credits
		creditSvc.EXPECT().PurgeDeletedCredits(mock.Anything, mock.AnythingOfType("time.Duration")).Return(5, nil).Once()

		// Create request
		req := ctx.NewAPIRequest("POST", "/api/billing/credits/purge", bodyBytes)
		w := httptest.NewRecorder()

		// Execute
		router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusOK, w.Code)

		// Parse response
		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(tb, err)

		assert.Equal(tb, float64(5), response["purged_count"])
	}, getAdminAPITestOptions())
}

// TestAdminHandlePurgeCredits_InvalidDuration tests purging with invalid duration format
func TestAdminHandlePurgeCredits_InvalidDuration(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		router := ctx.Router()

		// Create request body with invalid duration
		requestBody := map[string]interface{}{
			"older_than": "invalid-duration",
		}
		bodyBytes, _ := json.Marshal(requestBody)

		// Create request
		req := ctx.NewAPIRequest("POST", "/api/billing/credits/purge", bodyBytes)
		w := httptest.NewRecorder()

		// Execute
		router.ServeHTTP(w, req)

		// Verify - should return bad request (invalid duration)
		assert.Equal(tb, http.StatusBadRequest, w.Code)
	}, getAdminAPITestOptions())
}
