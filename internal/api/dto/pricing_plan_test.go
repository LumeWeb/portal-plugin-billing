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
		PricingPeriods: []PricingPeriodCreateInput{
			{
				Cadence:     string(subscription.CadenceMonthly),
				PriceUSD:    new(19.99),
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
	assert.Equal(t, 19.99, *request.PricingPeriods[0].PriceUSD)
	assert.NotNil(t, request.IsActive)
	assert.True(t, *request.IsActive)
}

func TestPricingPlanCreateRequest_Validation(t *testing.T) {
	// Test validation requires at least one pricing period
	request := PricingPlanCreateRequest{
		Name:           "Test Plan",
		Description:    "Test description",
		PricingPeriods: []PricingPeriodCreateInput{}, // Empty array should fail
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
		PricingPeriods: []PricingPeriodInput{
			{
				Cadence:     string(subscription.CadenceMonthly),
				PriceUSD:    new(29.99),
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
	assert.Equal(t, 29.99, *request.PricingPeriods[0].PriceUSD)
}

func TestPricingPlanCreateRequest_WithFeatures(t *testing.T) {
	isActive := true
	isPublic := true
	request := PricingPlanCreateRequest{
		Name:        "Basic Plan",
		Description: "Basic subscription plan",
		Features:    []string{"Storage 100GB", "Bandwidth 1TB"},
		PricingPeriods: []PricingPeriodCreateInput{
			{
				Cadence:     string(subscription.CadenceMonthly),
				PriceUSD:    new(19.99),
				QuotaPlanID: 100,
			},
		},
		Currency: "USD",
		IsActive: &isActive,
		IsPublic: &isPublic,
	}

	assert.Equal(t, "Basic Plan", request.Name)
	assert.Len(t, request.Features, 2)
	assert.Equal(t, "Storage 100GB", request.Features[0])
	assert.Equal(t, "Bandwidth 1TB", request.Features[1])
}

func TestPricingPlanCreateRequest_ToModel_WithFeatures(t *testing.T) {
	isActive := true
	isPublic := true
	request := &PricingPlanCreateRequest{
		Name:        "Basic Plan",
		Description: "Basic subscription plan",
		Features:    []string{"Storage 100GB", "Bandwidth 1TB"},
		PricingPeriods: []PricingPeriodCreateInput{
			{
				Cadence:     string(subscription.CadenceMonthly),
				PriceUSD:    new(19.99),
				QuotaPlanID: 100,
			},
		},
		Currency: "USD",
		IsActive: &isActive,
		IsPublic: &isPublic,
	}

	model, err := request.ToModel()

	require.NoError(t, err)
	assert.NotNil(t, model)
	assert.Equal(t, `["Storage 100GB","Bandwidth 1TB"]`, *model.FeaturesJSON)
}

func TestPricingPlanUpdateRequest_WithFeatures(t *testing.T) {
	request := PricingPlanUpdateRequest{
		Name:        "Updated Plan",
		Description: "Updated description",
		Features:    &[]string{"Unlimited Storage", "Priority Support"},
		PricingPeriods: []PricingPeriodInput{
			{
				Cadence:     string(subscription.CadenceMonthly),
				PriceUSD:    new(29.99),
				QuotaPlanID: 100,
			},
		},
		Currency: "USD",
	}

	assert.Equal(t, "Updated Plan", request.Name)
	require.NotNil(t, request.Features)
	assert.Len(t, *request.Features, 2)
	assert.Equal(t, "Unlimited Storage", (*request.Features)[0])
}

func TestPricingPlanUpdateRequest_ToModel_WithFeatures(t *testing.T) {
	request := &PricingPlanUpdateRequest{
		Name:        "Updated Plan",
		Description: "Updated description",
		Features:    &[]string{"Unlimited Storage", "Priority Support"},
		PricingPeriods: []PricingPeriodInput{
			{
				Cadence:     string(subscription.CadenceMonthly),
				PriceUSD:    new(29.99),
				QuotaPlanID: 100,
			},
		},
		Currency: "USD",
	}

	model, err := request.ToModel()

	require.NoError(t, err)
	assert.NotNil(t, model)
	assert.Equal(t, `["Unlimited Storage","Priority Support"]`, *model.FeaturesJSON)
}

func TestPricingPlanUpdateRequest_ToModel_ClearFeatures(t *testing.T) {
	request := &PricingPlanUpdateRequest{
		Name:        "Updated Plan",
		Description: "Updated description",
		Features:    &[]string{},
		PricingPeriods: []PricingPeriodInput{
			{
				Cadence:     string(subscription.CadenceMonthly),
				PriceUSD:    new(29.99),
				QuotaPlanID: 100,
			},
		},
		Currency: "USD",
	}

	model, err := request.ToModel()

	require.NoError(t, err)
	assert.NotNil(t, model)
	require.NotNil(t, model.FeaturesJSON)
	assert.Equal(t, "", *model.FeaturesJSON)
}

func TestPricingPlanResponse_FromModel_WithFeatures(t *testing.T) {
	now := time.Now()
	planModel := &models.PricingPlan{
		Name:         "Basic Plan",
		Description:  "Basic subscription plan",
		FeaturesJSON: new(`["Storage 100GB","Bandwidth 1TB"]`),
		Currency:     "USD",
		IsActive:     true,
		IsPublic:     true,
	}
	planModel.ID = 1
	planModel.CreatedAt = now
	planModel.UpdatedAt = now

	var response PricingPlanResponse
	err := response.FromModel(planModel)

	require.NoError(t, err)
	assert.Equal(t, "Basic Plan", response.Name)
	assert.Equal(t, []string{"Storage 100GB", "Bandwidth 1TB"}, response.Features)
}

func TestPublicPricingPlanResponse_FromModel_WithFeatures(t *testing.T) {
	planModel := &models.PricingPlan{
		Name:         "Basic Plan",
		Description:  "Basic subscription plan",
		FeaturesJSON: new(`["Storage 100GB","Bandwidth 1TB"]`),
		Currency:     "USD",
		IsActive:     true,
		IsPublic:     true,
	}
	planModel.ID = 1

	var response PublicPricingPlanResponse
	err := response.FromModel(planModel)

	require.NoError(t, err)
	assert.Equal(t, []string{"Storage 100GB", "Bandwidth 1TB"}, response.Features)
}

func TestPricingPlanCreateRequest_ToModel_WithoutFeatures(t *testing.T) {
	isActive := true
	isPublic := true
	request := &PricingPlanCreateRequest{
		Name:        "Basic Plan",
		Description: "Basic subscription plan",
		PricingPeriods: []PricingPeriodCreateInput{
			{
				Cadence:     string(subscription.CadenceMonthly),
				PriceUSD:    new(19.99),
				QuotaPlanID: 100,
			},
		},
		Currency: "USD",
		IsActive: &isActive,
		IsPublic: &isPublic,
	}

	model, err := request.ToModel()

	require.NoError(t, err)
	assert.NotNil(t, model)
	require.NotNil(t, model.FeaturesJSON)
	assert.Equal(t, "", *model.FeaturesJSON)
}

func TestPricingPlanUpdateRequest_ToModel_WithoutFeatures(t *testing.T) {
	request := &PricingPlanUpdateRequest{
		Name:        "Updated Plan",
		Description: "Updated description",
		PricingPeriods: []PricingPeriodInput{
			{
				Cadence:     string(subscription.CadenceMonthly),
				PriceUSD:    new(29.99),
				QuotaPlanID: 100,
			},
		},
		Currency: "USD",
	}

	model, err := request.ToModel()

	require.NoError(t, err)
	assert.NotNil(t, model)
	assert.Nil(t, model.FeaturesJSON)
}

func TestPricingPlanUpdateRequest_ToModel_NilFeaturesNoOp(t *testing.T) {
	request := &PricingPlanUpdateRequest{
		Name:        "Updated Plan",
		Description: "Updated description",
		Features:    nil,
		PricingPeriods: []PricingPeriodInput{
			{
				Cadence:     string(subscription.CadenceMonthly),
				PriceUSD:    new(29.99),
				QuotaPlanID: 100,
			},
		},
		Currency: "USD",
	}

	model, err := request.ToModel()

	require.NoError(t, err)
	assert.NotNil(t, model)
	assert.Nil(t, model.FeaturesJSON)
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
		PricingPeriods: []PricingPeriodCreateInput{
			{
				Cadence:     string(subscription.CadenceMonthly),
				PriceUSD:    new(19.99),
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
		PricingPeriods: []PricingPeriodInput{
			{
				Cadence:     string(subscription.CadenceMonthly),
				PriceUSD:    new(29.99),
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

func TestPricingPlanPeriodCreateRequest_AllowFree_Success(t *testing.T) {
	allowFree := true
	req := PricingPlanPeriodCreateRequest{
		PricingPlanID: 1,
		Cadence:       "monthly",
		PriceUSD:      new(0.0),
		QuotaPlanID:   123,
		AllowFree:     &allowFree,
	}

	model, err := req.ToModel()

	require.NoError(t, err)
	assert.Equal(t, float64(0), model.PriceUSD)
}

func TestPricingPlanPeriodCreateRequest_ZeroPrice_WithoutAllowFree_Fails(t *testing.T) {
	req := PricingPlanPeriodCreateRequest{
		PricingPlanID: 1,
		Cadence:       "monthly",
		PriceUSD:      new(0.0),
		QuotaPlanID:   123,
	}

	_, err := req.ToModel()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "use allow_free for $0 plans")
}

func TestPricingPlanPeriodCreateRequest_ZeroPrice_WithAllowFreeFalse_Fails(t *testing.T) {
	allowFree := false
	req := PricingPlanPeriodCreateRequest{
		PricingPlanID: 1,
		Cadence:       "monthly",
		PriceUSD:      new(0.0),
		QuotaPlanID:   123,
		AllowFree:     &allowFree,
	}

	_, err := req.ToModel()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "use allow_free for $0 plans")
}

func TestPricingPlanPeriodCreateRequest_NegativePrice_Fails(t *testing.T) {
	req := PricingPlanPeriodCreateRequest{
		PricingPlanID: 1,
		Cadence:       "monthly",
		PriceUSD:      new(-5.0),
		QuotaPlanID:   123,
	}

	_, err := req.ToModel()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "price must not be negative")
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

func TestSubscriptionStatusResponse_FromModel_PausedSubscriber(t *testing.T) {
	now := time.Now()
	periodID := uint(100)
	subscriber := &pluginCore.Subscriber{
		UserID:              1,
		GatewayType:         "stripe",
		ExternalID:          "ext_123",
		SubscriptionID:      "sub_123",
		IsActive:            false,
		PricingPlanPeriodID: &periodID,
		PausedAt:            &now,
	}
	subscriber.CreatedAt = now
	subscriber.UpdatedAt = now

	var response SubscriptionStatusResponse
	err := response.FromModel(subscriber)

	require.NoError(t, err)
	assert.False(t, response.IsSubscribed)
	assert.Equal(t, "stripe", response.GatewayType)
	assert.NotNil(t, response.PricingPlanPeriodID)
	assert.Equal(t, uint(100), *response.PricingPlanPeriodID)
	assert.NotNil(t, response.PausedAt)
	assert.Equal(t, now, *response.PausedAt)
	assert.NotNil(t, response.CreatedAt)
	assert.NotNil(t, response.UpdatedAt)
}
