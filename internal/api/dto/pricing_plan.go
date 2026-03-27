package dto

import (
	"time"

	z "github.com/Oudwins/zog"
	"go.lumeweb.com/httputil"
	"go.lumeweb.com/portal-plugin-billing/internal/db/models"
)

var _ httputil.DTOResponse[*models.PricingPlan] = (*PricingPlanResponse)(nil)

// PricingPlanResponse represents a pricing plan response
type PricingPlanResponse struct {
	ID             uint       `json:"id"`
	Name           string     `json:"name"`
	Description    string     `json:"description"`
	MonthlyPrice   *float64   `json:"monthly_price,omitempty"`
	YearlyPrice    *float64   `json:"yearly_price,omitempty"`
	Currency       string     `json:"currency"`
	IsActive       bool       `json:"is_active"`
	IsPublic       bool       `json:"is_public"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
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
	r.MonthlyPrice = plan.MonthlyPriceUSD
	if plan.YearlyPriceUSD != nil {
		r.YearlyPrice = plan.YearlyPriceUSD
	} else if plan.MonthlyPriceUSD != nil {
		// Calculate yearly from monthly if not explicitly set
		yearlyPrice := *plan.MonthlyPriceUSD * 12
		r.YearlyPrice = &yearlyPrice
	}
	r.Currency = plan.Currency
	r.IsActive = plan.IsActive
	r.IsPublic = plan.IsPublic
	r.CreatedAt = plan.CreatedAt
	r.UpdatedAt = plan.UpdatedAt

	return nil
}

// PricingPlanCreateRequest represents a request to create a pricing plan
type PricingPlanCreateRequest struct {
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	MonthlyPrice  *float64 `json:"monthly_price"`
	YearlyPrice   *float64 `json:"yearly_price"`
	Currency      string   `json:"currency"`
	IsActive      *bool    `json:"is_active"`
	IsPublic      *bool    `json:"is_public"`
}

func (r PricingPlanCreateRequest) Schema() *z.StructSchema {
	return z.Struct(z.Shape{
		"Name":         z.String().Required().Min(1).Max(255),
		"Description":  z.String().Required().Min(1).Max(500),
		"MonthlyPrice": z.Ptr(z.Float().GTE(0)),
		"YearlyPrice":  z.Ptr(z.Float().GTE(0)),
		"Currency":     z.String().Default("USD").Min(3).Max(3),
		"IsActive":     z.Ptr(z.Bool()),
		"IsPublic":     z.Ptr(z.Bool()),
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
		Name:            r.Name,
		Description:     r.Description,
		MonthlyPriceUSD: r.MonthlyPrice,
		YearlyPriceUSD:  r.YearlyPrice,
		Currency:        r.Currency,
		IsActive:        isActive,
		IsPublic:        isPublic,
	}, nil
}

// PricingPlanUpdateRequest represents a request to update a pricing plan
type PricingPlanUpdateRequest struct {
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	MonthlyPrice  *float64 `json:"monthly_price"`
	YearlyPrice   *float64 `json:"yearly_price"`
	Currency      string   `json:"currency"`
	IsActive      *bool    `json:"is_active"`
	IsPublic      *bool    `json:"is_public"`
}

func (r PricingPlanUpdateRequest) Schema() *z.StructSchema {
	return z.Struct(z.Shape{
		"Name":         z.String().Min(1).Max(255),
		"Description":  z.String().Min(1).Max(500),
		"MonthlyPrice": z.Ptr(z.Float().GTE(0)),
		"YearlyPrice":  z.Ptr(z.Float().GTE(0)),
		"Currency":     z.String().Min(3).Max(3),
		"IsActive":     z.Ptr(z.Bool()),
		"IsPublic":     z.Ptr(z.Bool()),
	})
}

func (r *PricingPlanUpdateRequest) ToModel() (*models.PricingPlan, error) {
	return &models.PricingPlan{
		Name:            r.Name,
		Description:     r.Description,
		MonthlyPriceUSD: r.MonthlyPrice,
		YearlyPriceUSD:  r.YearlyPrice,
		Currency:        r.Currency,
	}, nil
}

// PricingPlanFilterRequest represents filter options for listing pricing plans
type PricingPlanFilterRequest struct {
	Name     string  `json:"name" filter:"true"`
	IsActive *bool   `json:"is_active" filter:"true"`
	IsPublic *bool   `json:"is_public" filter:"true"`
	Currency string  `json:"currency" filter:"true"`
}
