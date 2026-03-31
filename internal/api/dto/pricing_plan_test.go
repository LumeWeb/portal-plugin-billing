package dto

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	"go.lumeweb.com/portal-plugin-billing/internal/db/models"
	"go.lumeweb.com/portal-plugin-billing/pkg/subscription"
)

func TestPricingPlanPeriodDTO(t *testing.T) {
	// Test that PricingPlanPeriodDTO struct exists and has correct fields
	period := PricingPlanPeriodDTO{
		ID:             1,
		PricingPlanID:  100,
		Cadence:        string(subscription.CadenceMonthly),
		PriceUSD:       19.99,
		QuotaPlanID:    200,
		RollingDays:    intPtr(30),
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	assert.Equal(t, uint(1), period.ID)
	assert.Equal(t, uint(100), period.PricingPlanID)
	assert.Equal(t, string(subscription.CadenceMonthly), period.Cadence)
	assert.Equal(t, 19.99, period.PriceUSD)
	assert.Equal(t, uint(200), period.QuotaPlanID)
	assert.NotNil(t, period.RollingDays)
	assert.Equal(t, 30, *period.RollingDays)
}

func intPtr(i int) *int {
	return &i
}

func TestPricingPlanResponse_WithPricingPeriods(t *testing.T) {
	// Test that PricingPlanResponse has PricingPeriods field instead of MonthlyPrice/YearlyPrice
	response := PricingPlanResponse{
		ID:              1,
		Name:            "Basic Plan",
		Description:     "Basic subscription plan",
		Currency:        "USD",
		IsActive:        true,
		IsPublic:        true,
		PricingPeriods: []PricingPlanPeriodDTO{
			{
				ID:             1,
				PricingPlanID:  1,
				Cadence:        string(subscription.CadenceMonthly),
				PriceUSD:       19.99,
				QuotaPlanID:    100,
				RollingDays:    nil,
				CreatedAt:      time.Now(),
				UpdatedAt:      time.Now(),
			},
			{
				ID:             2,
				PricingPlanID:  1,
				Cadence:        string(subscription.CadenceYearly),
				PriceUSD:       199.99,
				QuotaPlanID:    101,
				RollingDays:    nil,
				CreatedAt:      time.Now(),
				UpdatedAt:      time.Now(),
			},
		},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	assert.Equal(t, "Basic Plan", response.Name)
	assert.Len(t, response.PricingPeriods, 2)
	assert.Equal(t, 19.99, response.PricingPeriods[0].PriceUSD)
	assert.Equal(t, 199.99, response.PricingPeriods[1].PriceUSD)
}

func TestPricingPlanCreateRequest_WithPricingPeriods(t *testing.T) {
	// Test that PricingPlanCreateRequest accepts PricingPeriods array
	isActive := true
	isPublic := true
	request := PricingPlanCreateRequest{
		Name:        "Basic Plan",
		Description: "Basic subscription plan",
		PricingPeriods: []PricingPlanPeriodDTO{
			{
				Cadence:     string(subscription.CadenceMonthly),
				PriceUSD:    19.99,
				QuotaPlanID: 100,
				RollingDays: nil,
			},
		},
		Currency: "USD",
		IsActive: &isActive,
		IsPublic: &isPublic,
	}

	assert.Equal(t, "Basic Plan", request.Name)
	assert.Len(t, request.PricingPeriods, 1)
	assert.Equal(t, 19.99, request.PricingPeriods[0].PriceUSD)
	assert.NotNil(t, request.IsActive)
	assert.True(t, *request.IsActive)
}

func TestPricingPlanCreateRequest_Validation(t *testing.T) {
	// Test validation requires at least one pricing period
	request := PricingPlanCreateRequest{
		Name:           "Test Plan",
		Description:    "Test description",
		PricingPeriods: []PricingPlanPeriodDTO{}, // Empty array should fail
		Currency:       "USD",
	}

	schema := request.Schema()
	var result PricingPlanCreateRequest
	issues := schema.Parse(request, &result)

	// Should have validation error for empty pricing periods
	assert.NotEmpty(t, issues)
}

func TestPricingPlanUpdateRequest_WithPricingPeriods(t *testing.T) {
	// Test that PricingPlanUpdateRequest accepts PricingPeriods array
	isActive := true
	isPublic := true
	request := PricingPlanUpdateRequest{
		Name:        "Updated Plan",
		Description: "Updated description",
		PricingPeriods: []PricingPlanPeriodDTO{
			{
				Cadence:     string(subscription.CadenceMonthly),
				PriceUSD:    29.99,
				QuotaPlanID: 100,
				RollingDays: nil,
			},
		},
		Currency: "USD",
		IsActive: &isActive,
		IsPublic: &isPublic,
	}

	assert.Equal(t, "Updated Plan", request.Name)
	assert.Len(t, request.PricingPeriods, 1)
	assert.Equal(t, 29.99, request.PricingPeriods[0].PriceUSD)
}

func TestPricingPlanResponse_FromModel_WithPricingPeriods(t *testing.T) {
	// Test FromModel populates pricing periods from models
	now := time.Now()
	planModel := &models.PricingPlan{
		Name:       "Basic Plan",
		Description: "Basic subscription plan",
		Currency:   "USD",
		IsActive:   true,
		IsPublic:   true,
	}
	planModel.ID = 1
	planModel.CreatedAt = now
	planModel.UpdatedAt = now

	var response PricingPlanResponse
	err := response.FromModel(planModel)

	require.NoError(t, err)
	assert.Equal(t, "Basic Plan", response.Name)
	assert.Equal(t, uint(1), response.ID)
}

func TestPricingPlanCreateRequest_ToModel_WithPricingPeriods(t *testing.T) {
	// Test ToModel converts pricing periods to model
	isActive := true
	isPublic := true
	request := &PricingPlanCreateRequest{
		Name:        "Basic Plan",
		Description: "Basic subscription plan",
		PricingPeriods: []PricingPlanPeriodDTO{
			{
				Cadence:     string(subscription.CadenceMonthly),
				PriceUSD:    19.99,
				QuotaPlanID: 100,
				RollingDays: nil,
			},
		},
		Currency: "USD",
		IsActive: &isActive,
		IsPublic: &isPublic,
	}

	model, err := request.ToModel()

	require.NoError(t, err)
	assert.NotNil(t, model)
	assert.Equal(t, "Basic Plan", model.Name)
	assert.Equal(t, "USD", model.Currency)
	assert.True(t, model.IsActive)
}

func TestPricingPlanUpdateRequest_ToModel_WithPricingPeriods(t *testing.T) {
	// Test ToModel converts pricing periods to model for update
	request := &PricingPlanUpdateRequest{
		Name:        "Updated Plan",
		Description: "Updated description",
		PricingPeriods: []PricingPlanPeriodDTO{
			{
				Cadence:     string(subscription.CadenceMonthly),
				PriceUSD:    29.99,
				QuotaPlanID: 100,
				RollingDays: nil,
			},
		},
		Currency: "USD",
	}

	model, err := request.ToModel()

	require.NoError(t, err)
	assert.NotNil(t, model)
	assert.Equal(t, "Updated Plan", model.Name)
	assert.Equal(t, "USD", model.Currency)
}

func TestSubscriptionStatusResponse_WithPricingPlanPeriodID(t *testing.T) {
	// Test that SubscriptionStatusResponse uses PricingPlanPeriodID instead of PlanID
	now := time.Now()
	periodID := uint(100)
	response := SubscriptionStatusResponse{
		IsSubscribed:        true,
		GatewayType:         "stripe",
		PricingPlanPeriodID: &periodID,
		CreatedAt:           &now,
		UpdatedAt:           &now,
	}

	assert.True(t, response.IsSubscribed)
	assert.Equal(t, "stripe", response.GatewayType)
	assert.NotNil(t, response.PricingPlanPeriodID)
	assert.Equal(t, uint(100), *response.PricingPlanPeriodID)
}

func TestSubscriptionStatusResponse_FromModel_WithPricingPlanPeriodID(t *testing.T) {
	// Test FromModel maps PricingPlanPeriodID from Subscriber model
	now := time.Now()
	periodID := uint(100)
	subscriber := &pluginCore.Subscriber{
		UserID:              1,
		GatewayType:         "stripe",
		ExternalID:          "ext_123",
		SubscriptionID:      "sub_123",
		IsActive:            true,
		PricingPlanPeriodID: &periodID,
	}
	subscriber.CreatedAt = now
	subscriber.UpdatedAt = now

	var response SubscriptionStatusResponse
	err := response.FromModel(subscriber)

	require.NoError(t, err)
	assert.True(t, response.IsSubscribed)
	assert.Equal(t, "stripe", response.GatewayType)
	assert.NotNil(t, response.PricingPlanPeriodID)
	assert.Equal(t, uint(100), *response.PricingPlanPeriodID)
}
