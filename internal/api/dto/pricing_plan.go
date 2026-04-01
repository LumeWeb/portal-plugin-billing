package dto

import (
	"fmt"
	"time"

	z "github.com/Oudwins/zog"
	"go.lumeweb.com/httputil"
	"go.lumeweb.com/portal-plugin-billing/internal/db/models"
)

var _ httputil.DTOResponse[*models.PricingPlan] = (*PricingPlanResponse)(nil)
var _ httputil.DTOResponse[*models.PricingPlanPeriod] = (*PricingPlanPeriodDTO)(nil)

// PricingPlanPeriodDTO represents a pricing plan period with billing cadence information
type PricingPlanPeriodDTO struct {
	ID             uint      `json:"id"`
	PricingPlanID  uint      `json:"pricing_plan_id"`
	Cadence        string    `json:"cadence"`
	PriceUSD       float64   `json:"price_usd"`
	QuotaPlanID    uint      `json:"quota_plan_id"`
	RollingDays    *int      `json:"rolling_days,omitempty"`
	IsActive       bool      `json:"is_active"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// FromModel converts a PricingPlanPeriod model to PricingPlanPeriodDTO
func (r *PricingPlanPeriodDTO) FromModel(period *models.PricingPlanPeriod) error {
	*r = PricingPlanPeriodDTO{}
	if period == nil {
		return nil
	}

	r.ID = period.ID
	r.PricingPlanID = period.PricingPlanID
	r.Cadence = period.Cadence
	r.PriceUSD = period.PriceUSD
	r.QuotaPlanID = period.QuotaPlanID
	r.RollingDays = period.RollingDays
	r.IsActive = period.DeletedAt.Time.IsZero()
	r.CreatedAt = period.CreatedAt
	r.UpdatedAt = period.UpdatedAt

	return nil
}

// PricingPlanResponse represents a pricing plan response (admin/internal use)
type PricingPlanResponse struct {
	ID              uint                     `json:"id"`
	Name            string                   `json:"name"`
	Description     string                   `json:"description"`
	Currency        string                   `json:"currency"`
	IsActive        bool                     `json:"is_active"`
	IsPublic        bool                     `json:"is_public"`
	PricingPeriods  []PricingPlanPeriodDTO   `json:"pricing_periods"`
	CreatedAt       time.Time                `json:"created_at"`
	UpdatedAt       time.Time                `json:"updated_at"`
}

// FromModel converts a PricingPlan model to PricingPlanResponse
func (r *PricingPlanResponse) FromModel(plan *models.PricingPlan) error {
	*r = PricingPlanResponse{}
	if plan == nil {
		return nil
	}

	r.ID = plan.ID
	r.Name = plan.Name
	r.Description = plan.Description
	r.Currency = plan.Currency
	r.IsActive = plan.IsActive
	r.IsPublic = plan.IsPublic
	r.CreatedAt = plan.CreatedAt
	r.UpdatedAt = plan.UpdatedAt

	return nil
}

// SetPricingPeriods sets the pricing periods on the response
func (r *PricingPlanResponse) SetPricingPeriods(periods []*models.PricingPlanPeriod) {
	r.PricingPeriods = make([]PricingPlanPeriodDTO, len(periods))
	for i, period := range periods {
		r.PricingPeriods[i] = PricingPlanPeriodDTO{
			ID:            period.ID,
			PricingPlanID: period.PricingPlanID,
			Cadence:       period.Cadence,
			PriceUSD:      period.PriceUSD,
			QuotaPlanID:   period.QuotaPlanID,
			RollingDays:   period.RollingDays,
			IsActive:      period.DeletedAt.Time.IsZero(),
			CreatedAt:     period.CreatedAt,
			UpdatedAt:     period.UpdatedAt,
		}
	}
}

// PublicPricingPlanResponse represents a pricing plan response for public/user-facing API
// This excludes internal fields like is_active and is_public
type PublicPricingPlanResponse struct {
	ID             uint                       `json:"id"`
	Name           string                     `json:"name"`
	Description    string                     `json:"description"`
	Currency       string                     `json:"currency"`
	PricingPeriods []PricingPlanPeriodDTO     `json:"pricing_periods"`
	CreatedAt      time.Time                  `json:"created_at"`
	UpdatedAt      time.Time                  `json:"updated_at"`
}

// FromModel converts a PricingPlan model to PublicPricingPlanResponse
func (r *PublicPricingPlanResponse) FromModel(plan *models.PricingPlan) error {
	*r = PublicPricingPlanResponse{}
	if plan == nil {
		return nil
	}

	r.ID = plan.ID
	r.Name = plan.Name
	r.Description = plan.Description
	r.Currency = plan.Currency
	r.CreatedAt = plan.CreatedAt
	r.UpdatedAt = plan.UpdatedAt

	return nil
}

// SetPricingPeriods sets the pricing periods on the response
func (r *PublicPricingPlanResponse) SetPricingPeriods(periods []*models.PricingPlanPeriod) {
	r.PricingPeriods = make([]PricingPlanPeriodDTO, len(periods))
	for i, period := range periods {
		r.PricingPeriods[i] = PricingPlanPeriodDTO{
			ID:            period.ID,
			PricingPlanID: period.PricingPlanID,
			Cadence:       period.Cadence,
			PriceUSD:      period.PriceUSD,
			QuotaPlanID:   period.QuotaPlanID,
			RollingDays:   period.RollingDays,
			IsActive:      period.DeletedAt.Time.IsZero(),
			CreatedAt:     period.CreatedAt,
			UpdatedAt:     period.UpdatedAt,
		}
	}
}

// PricingPlanCreateRequest represents a request to create a pricing plan
type PricingPlanCreateRequest struct {
	Name           string                 `json:"name"`
	Description    string                 `json:"description"`
	PricingPeriods []PricingPlanPeriodDTO `json:"pricing_periods"`
	Currency       string                 `json:"currency"`
	IsActive       *bool                  `json:"is_active"`
	IsPublic       *bool                  `json:"is_public"`
}

func (r PricingPlanCreateRequest) Schema() *z.StructSchema {
	return z.Struct(z.Shape{
		"Name":           z.String().Required().Min(1).Max(255),
		"Description":    z.String().Required().Min(1).Max(500),
		"PricingPeriods": z.Slice(z.Struct(z.Shape{
			"Cadence":     z.String().Required(),
			"PriceUSD":    z.Float64().Required(),
			"QuotaPlanID": z.Uint().Required(),
			"RollingDays": z.Ptr(z.Int()),
			"IsActive":    z.Bool().Required(),
		})).Min(1),
		"Currency": z.String().Default("USD").Min(3).Max(3),
		"IsActive": z.Ptr(z.Bool()),
		"IsPublic": z.Ptr(z.Bool()),
	})
}

func (r *PricingPlanCreateRequest) ToModel() (*models.PricingPlan, error) {
	isActive := true
	if r.IsActive != nil {
		isActive = *r.IsActive
	}

	isPublic := false
	if r.IsPublic != nil {
		isPublic = *r.IsPublic
	}

	return &models.PricingPlan{
		Name:        r.Name,
		Description: r.Description,
		Currency:    r.Currency,
		IsActive:    isActive,
		IsPublic:    isPublic,
	}, nil
}

// ToPricingPeriodModels converts pricing periods from DTOs to models
func (r *PricingPlanCreateRequest) ToPricingPeriodModels(pricingPlanID uint) []models.PricingPlanPeriod {
	periods := make([]models.PricingPlanPeriod, len(r.PricingPeriods))
	for i, period := range r.PricingPeriods {
		periods[i] = models.PricingPlanPeriod{
			PricingPlanID: pricingPlanID,
			Cadence:       period.Cadence,
			PriceUSD:      period.PriceUSD,
			QuotaPlanID:   period.QuotaPlanID,
			RollingDays:   period.RollingDays,
		}
	}
	return periods
}

// PricingPlanUpdateRequest represents a request to update a pricing plan
type PricingPlanUpdateRequest struct {
	Name           string                 `json:"name"`
	Description    string                 `json:"description"`
	PricingPeriods []PricingPlanPeriodDTO `json:"pricing_periods"`
	Currency       string                 `json:"currency"`
	IsActive       *bool                  `json:"is_active"`
	IsPublic       *bool                  `json:"is_public"`
}

func (r PricingPlanUpdateRequest) Schema() *z.StructSchema {
	return z.Struct(z.Shape{
		"Name":        z.String().Min(1).Max(255),
		"Description": z.String().Min(1).Max(500),
		"PricingPeriods": z.Slice(z.Struct(z.Shape{
			"Cadence":     z.String(),
			"PriceUSD":    z.Float64(),
			"QuotaPlanID": z.Uint(),
			"RollingDays": z.Ptr(z.Int()),
			"IsActive":    z.Bool(),
		})),
		"Currency": z.String().Min(3).Max(3),
		"IsActive": z.Ptr(z.Bool()),
		"IsPublic": z.Ptr(z.Bool()),
	})
}

func (r *PricingPlanUpdateRequest) ToModel() (*models.PricingPlan, error) {
	plan := &models.PricingPlan{
		Name:        r.Name,
		Description: r.Description,
		Currency:    r.Currency,
	}

	if r.IsActive != nil {
		plan.IsActive = *r.IsActive
	}

	if r.IsPublic != nil {
		plan.IsPublic = *r.IsPublic
	}

	return plan, nil
}

// ToPricingPeriodModels converts pricing periods from DTOs to models
func (r *PricingPlanUpdateRequest) ToPricingPeriodModels(pricingPlanID uint) []models.PricingPlanPeriod {
	periods := make([]models.PricingPlanPeriod, len(r.PricingPeriods))
	for i, period := range r.PricingPeriods {
		periodModel := models.PricingPlanPeriod{
			PricingPlanID: pricingPlanID,
			Cadence:       period.Cadence,
			PriceUSD:      period.PriceUSD,
			QuotaPlanID:   period.QuotaPlanID,
			RollingDays:   period.RollingDays,
		}
		if period.ID > 0 {
			periodModel.ID = period.ID
		}
		periods[i] = periodModel
	}
	return periods
}

// PricingPlanPeriodCreateRequest represents a request to create a pricing plan period
type PricingPlanPeriodCreateRequest struct {
	PricingPlanID uint    `json:"pricing_plan_id"`
	Cadence       string  `json:"cadence"`
	PriceUSD      float64 `json:"price_usd"`
	QuotaPlanID   uint    `json:"quota_plan_id"`
	RollingDays   *int    `json:"rolling_days,omitempty"`
}

func (r PricingPlanPeriodCreateRequest) Schema() *z.StructSchema {
	return z.Struct(z.Shape{
		"PricingPlanID": z.Uint().Required(),
		"Cadence":       z.String().Required().OneOf([]string{"monthly", "yearly", "quarterly", "weekly", "rolling"}),
		"PriceUSD":      z.Float64().Required(),
		"QuotaPlanID":   z.Uint().Required(),
		"RollingDays":   z.Ptr(z.Int()),
	})
}

func (r PricingPlanPeriodCreateRequest) ToModel() (*models.PricingPlanPeriod, error) {
	if r.RollingDays != nil && r.Cadence != "rolling" {
		return nil, fmt.Errorf("rolling_days can only be set for 'rolling' cadence")
	}
	if r.Cadence == "rolling" && r.RollingDays == nil {
		return nil, fmt.Errorf("rolling_days is required for 'rolling' cadence")
	}
	if r.PriceUSD <= 0 {
		return nil, fmt.Errorf("price must be greater than 0")
	}

	return &models.PricingPlanPeriod{
		PricingPlanID: r.PricingPlanID,
		Cadence:       r.Cadence,
		PriceUSD:      r.PriceUSD,
		QuotaPlanID:   r.QuotaPlanID,
		RollingDays:   r.RollingDays,
	}, nil
}

// PricingPlanPeriodUpdateRequest represents a request to update a pricing plan period
type PricingPlanPeriodUpdateRequest struct {
	Cadence     string  `json:"cadence"`
	PriceUSD    float64 `json:"price_usd"`
	QuotaPlanID uint    `json:"quota_plan_id"`
	RollingDays *int    `json:"rolling_days,omitempty"`
}

func (r PricingPlanPeriodUpdateRequest) Schema() *z.StructSchema {
	return z.Struct(z.Shape{
		"Cadence":     z.String().OneOf([]string{"monthly", "yearly", "quarterly", "weekly", "rolling"}),
		"PriceUSD":    z.Float64(),
		"QuotaPlanID": z.Uint(),
		"RollingDays": z.Ptr(z.Int()),
	})
}

func (r PricingPlanPeriodUpdateRequest) ToModel() (*models.PricingPlanPeriod, error) {
	period := &models.PricingPlanPeriod{}

	if r.Cadence != "" {
		if r.RollingDays != nil && r.Cadence != "rolling" {
			return nil, fmt.Errorf("rolling_days can only be set for 'rolling' cadence")
		}
		if r.Cadence == "rolling" && r.RollingDays == nil {
			return nil, fmt.Errorf("rolling_days is required for 'rolling' cadence")
		}
		period.Cadence = r.Cadence
	}

	if r.PriceUSD != 0 {
		if r.PriceUSD <= 0 {
			return nil, fmt.Errorf("price must be greater than 0")
		}
		period.PriceUSD = r.PriceUSD
	}

	if r.QuotaPlanID > 0 {
		period.QuotaPlanID = r.QuotaPlanID
	}

	if r.RollingDays != nil {
		period.RollingDays = r.RollingDays
	}

	return period, nil
}

// PricingPlanFilterRequest represents filter options for listing pricing plans
type PricingPlanFilterRequest struct {
	Name     string  `json:"name" filter:"true"`
	IsActive *bool   `json:"is_active" filter:"true"`
	IsPublic *bool   `json:"is_public" filter:"true"`
	Currency string  `json:"currency" filter:"true"`
}

// PricingPlanPeriodFilterRequest represents filter options for listing pricing plan periods
type PricingPlanPeriodFilterRequest struct {
	PricingPlanID *uint   `json:"pricing_plan_id" filter:"true"`
	Cadence       *string `json:"cadence" filter:"true"`
}
