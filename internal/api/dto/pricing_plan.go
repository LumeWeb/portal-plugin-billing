package dto

import (
	"fmt"
	"time"

	z "github.com/Oudwins/zog"
	"go.lumeweb.com/httputil"
	"go.lumeweb.com/portal-plugin-billing/internal/db/models"
	"go.lumeweb.com/portal-plugin-billing/internal/service/pricing"
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
	AllowFree      bool      `json:"allow_free"` // Inferred: true when PriceUSD == 0
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
	r.AllowFree = period.PriceUSD == 0
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
			AllowFree:     period.PriceUSD == 0,
			IsActive:      period.DeletedAt.Time.IsZero(),
			CreatedAt:     period.CreatedAt,
			UpdatedAt:     period.UpdatedAt,
		}
	}
}

// PublicPricingPlanPeriodDTO represents a pricing plan period for public/user-facing API
// This excludes internal fields like is_active, created_at, updated_at, and pricing_plan_id
type PublicPricingPlanPeriodDTO struct {
	ID          uint    `json:"id"`
	Cadence     string  `json:"cadence"`
	PriceUSD    float64 `json:"price_usd"`
	QuotaPlanID uint    `json:"quota_plan_id"`
	RollingDays *int    `json:"rolling_days,omitempty"`
}

// FromModel converts a PricingPlanPeriod model to PublicPricingPlanPeriodDTO
func (r *PublicPricingPlanPeriodDTO) FromModel(period *models.PricingPlanPeriod) error {
	*r = PublicPricingPlanPeriodDTO{}
	if period == nil {
		return nil
	}

	r.ID = period.ID
	r.Cadence = period.Cadence
	r.PriceUSD = period.PriceUSD
	r.QuotaPlanID = period.QuotaPlanID
	r.RollingDays = period.RollingDays

	return nil
}

// PublicPricingPlanResponse represents a pricing plan response for public/user-facing API
// This excludes internal fields like is_active and is_public
type PublicPricingPlanResponse struct {
	ID             uint                        `json:"id"`
	Name           string                      `json:"name"`
	Description    string                      `json:"description"`
	Currency       string                      `json:"currency"`
	PricingPeriods []PublicPricingPlanPeriodDTO `json:"pricing_periods"`
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

	return nil
}

// SetPricingPeriods sets the pricing periods on the response
func (r *PublicPricingPlanResponse) SetPricingPeriods(periods []*models.PricingPlanPeriod) {
	r.PricingPeriods = make([]PublicPricingPlanPeriodDTO, len(periods))
	for i, period := range periods {
		r.PricingPeriods[i] = PublicPricingPlanPeriodDTO{
			ID:          period.ID,
			Cadence:     period.Cadence,
			PriceUSD:    period.PriceUSD,
			QuotaPlanID: period.QuotaPlanID,
			RollingDays: period.RollingDays,
		}
	}
}

// PublicPricingPlansListResponse is a swagger-only DTO that represents the paginated response for public pricing plans.
// It provides a concrete type for swagger documentation since queryutil.Response generics
// are not properly detected as array types.
//
// Note: This struct is only used for swagger documentation, not for actual encoding.
type PublicPricingPlansListResponse struct {
	Data  []PublicPricingPlanResponse `json:"data"`
	Total int64                       `json:"total"`
}

// PricingPeriodCreateInput represents a pricing period within a create plan request
type PricingPeriodCreateInput struct {
	Cadence     string   `json:"cadence"`
	PriceUSD    *float64 `json:"price_usd"`
	QuotaPlanID uint     `json:"quota_plan_id"`
	RollingDays *int     `json:"rolling_days,omitempty"`
	AllowFree   *bool    `json:"allow_free,omitempty"`
	IsActive    bool     `json:"is_active"`
}

// PricingPlanCreateRequest represents a request to create a pricing plan
type PricingPlanCreateRequest struct {
	Name           string                     `json:"name"`
	Description    string                     `json:"description"`
	PricingPeriods []PricingPeriodCreateInput `json:"pricing_periods"`
	Currency       string                     `json:"currency"`
	IsActive       *bool                      `json:"is_active"`
	IsPublic       *bool                      `json:"is_public"`
	PriceLineID    *uint                      `json:"priceline_id,omitempty"`
	Position       *int                       `json:"position,omitempty"`
}

func (r PricingPlanCreateRequest) Schema() *z.StructSchema {
	return z.Struct(z.Shape{
		"Name":           z.String().Required().Min(1).Max(255),
		"Description":    z.String().Required().Min(1).Max(500),
		"PricingPeriods": z.Slice(z.Struct(z.Shape{
			"Cadence":     z.String().Required(),
			"PriceUSD":    z.Ptr(z.Float64()).NotNil(),
			"QuotaPlanID": z.Uint().Required(),
			"RollingDays": z.Ptr(z.Int()),
			"AllowFree":   z.Ptr(z.Bool()),
			"IsActive":    z.Bool().Required(),
		})).Min(1),
		"Currency":    z.String().Default("USD").Min(3).Max(3),
		"IsActive":    z.Ptr(z.Bool()),
		"IsPublic":    z.Ptr(z.Bool()),
		"PriceLineID": z.Ptr(z.UintLike[uint]()),
		"Position":    z.Ptr(z.Int()),
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
func (r *PricingPlanCreateRequest) ToPricingPeriodModels(pricingPlanID uint) ([]models.PricingPlanPeriod, error) {
	periods := make([]models.PricingPlanPeriod, len(r.PricingPeriods))
	for i, period := range r.PricingPeriods {
		if err := validatePrice(*period.PriceUSD, period.AllowFree); err != nil {
			return nil, err
		}
		periods[i] = models.PricingPlanPeriod{
			PricingPlanID: pricingPlanID,
			Cadence:       period.Cadence,
			PriceUSD:      *period.PriceUSD,
			QuotaPlanID:   period.QuotaPlanID,
			RollingDays:   period.RollingDays,
		}
	}
	return periods, nil
}

// PricingPeriodInput represents a pricing period within an update plan request
type PricingPeriodInput struct {
	ID          uint     `json:"id,omitempty"`
	Cadence     string   `json:"cadence"`
	PriceUSD    *float64 `json:"price_usd"`
	QuotaPlanID uint     `json:"quota_plan_id"`
	RollingDays *int     `json:"rolling_days,omitempty"`
	AllowFree   *bool    `json:"allow_free,omitempty"`
	IsActive    bool     `json:"is_active"`
}

// PricingPlanUpdateRequest represents a request to update a pricing plan
type PricingPlanUpdateRequest struct {
	Name           string                    `json:"name"`
	Description    string                    `json:"description"`
	PricingPeriods []PricingPeriodInput      `json:"pricing_periods"`
	Currency       string                    `json:"currency"`
	IsActive       *bool                     `json:"is_active"`
	IsPublic       *bool                     `json:"is_public"`
}

func (r PricingPlanUpdateRequest) Schema() *z.StructSchema {
	return z.Struct(z.Shape{
		"Name":        z.String().Min(1).Max(255),
		"Description": z.String().Min(1).Max(500),
		"PricingPeriods": z.Slice(z.Struct(z.Shape{
			"ID":          z.Uint(),
			"Cadence":     z.String(),
			"PriceUSD":    z.Ptr(z.Float64()),
			"QuotaPlanID": z.Uint(),
			"RollingDays": z.Ptr(z.Int()),
			"AllowFree":   z.Ptr(z.Bool()),
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
func (r *PricingPlanUpdateRequest) ToPricingPeriodModels(pricingPlanID uint) ([]models.PricingPlanPeriod, error) {
	periods := make([]models.PricingPlanPeriod, len(r.PricingPeriods))
	for i, period := range r.PricingPeriods {
		periodModel := models.PricingPlanPeriod{
			PricingPlanID: pricingPlanID,
			Cadence:       period.Cadence,
			QuotaPlanID:   period.QuotaPlanID,
			RollingDays:   period.RollingDays,
		}
		if period.PriceUSD != nil {
			if err := validatePrice(*period.PriceUSD, period.AllowFree); err != nil {
				return nil, err
			}
			periodModel.PriceUSD = *period.PriceUSD
		}
		if period.ID > 0 {
			periodModel.ID = period.ID
		}
		periods[i] = periodModel
	}
	return periods, nil
}

// PricingPlanPeriodCreateRequest represents a request to create a pricing plan period
type PricingPlanPeriodCreateRequest struct {
	PricingPlanID uint     `json:"pricing_plan_id"`
	Cadence       string   `json:"cadence"`
	PriceUSD      *float64 `json:"price_usd"`
	QuotaPlanID   uint     `json:"quota_plan_id"`
	RollingDays   *int     `json:"rolling_days,omitempty"`
	AllowFree     *bool    `json:"allow_free,omitempty"`
}

func (r PricingPlanPeriodCreateRequest) Schema() *z.StructSchema {
	return z.Struct(z.Shape{
		"PricingPlanID": z.Uint().Required(),
		"Cadence":       z.String().Required().OneOf([]string{"monthly", "yearly", "quarterly", "weekly", "rolling"}),
		"PriceUSD":      z.Ptr(z.Float64()).NotNil(),
		"QuotaPlanID":   z.Uint().Required(),
		"RollingDays":   z.Ptr(z.Int()),
		"AllowFree":     z.Ptr(z.Bool()),
	})
}

func validatePrice(price float64, allowFree *bool) error {
	if price < 0 {
		return fmt.Errorf("price must not be negative")
	}
	if price == 0 && (allowFree == nil || !*allowFree) {
		return fmt.Errorf("price must be greater than 0 (use allow_free for $0 plans)")
	}
	return nil
}

func (r PricingPlanPeriodCreateRequest) ToModel() (*models.PricingPlanPeriod, error) {
	if r.RollingDays != nil && r.Cadence != "rolling" {
		return nil, fmt.Errorf("rolling_days can only be set for 'rolling' cadence")
	}
	if r.Cadence == "rolling" && r.RollingDays == nil {
		return nil, fmt.Errorf("rolling_days is required for 'rolling' cadence")
	}
	if err := validatePrice(*r.PriceUSD, r.AllowFree); err != nil {
		return nil, err
	}

	return &models.PricingPlanPeriod{
		PricingPlanID: r.PricingPlanID,
		Cadence:       r.Cadence,
		PriceUSD:      *r.PriceUSD,
		QuotaPlanID:   r.QuotaPlanID,
		RollingDays:   r.RollingDays,
	}, nil
}

// PricingPlanPeriodUpdateRequest represents a request to update a pricing plan period
type PricingPlanPeriodUpdateRequest struct {
	Cadence     string   `json:"cadence"`
	PriceUSD    *float64 `json:"price_usd"`
	QuotaPlanID uint     `json:"quota_plan_id"`
	RollingDays *int     `json:"rolling_days,omitempty"`
	AllowFree   *bool    `json:"allow_free,omitempty"`
}

func (r PricingPlanPeriodUpdateRequest) Schema() *z.StructSchema {
	return z.Struct(z.Shape{
		"Cadence":     z.String().OneOf([]string{"monthly", "yearly", "quarterly", "weekly", "rolling"}),
		"PriceUSD":    z.Ptr(z.Float64()),
		"QuotaPlanID": z.Uint(),
		"RollingDays": z.Ptr(z.Int()),
		"AllowFree":   z.Ptr(z.Bool()),
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

	if r.PriceUSD != nil {
		if err := validatePrice(*r.PriceUSD, r.AllowFree); err != nil {
			return nil, err
		}
		period.PriceUSD = *r.PriceUSD
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

// PricingPlanPeriodsListResponse is a swagger-only DTO that represents the paginated response for pricing plan periods.
// It provides a concrete type for swagger documentation since queryutil.Response generics
// are not properly detected as array types.
//
// Note: This struct is only used for swagger documentation, not for actual encoding.
type PricingPlanPeriodsListResponse struct {
	Data  []PricingPlanPeriodDTO `json:"data"`
	Total int64                  `json:"total"`
}

// PricingPlanPeriodFilterRequest represents filter options for listing pricing plan periods
type PricingPlanPeriodFilterRequest struct {
	PricingPlanID *uint   `json:"pricing_plan_id" filter:"true"`
	Cadence       *string `json:"cadence" filter:"true"`
}

// SyncStatus represents the status of a pricing plan sync operation
type SyncStatus string

const (
	SyncStatusSuccess SyncStatus = "success"
	SyncStatusPartial SyncStatus = "partial"
	SyncStatusError   SyncStatus = "error"
)

// GatewaySyncResult represents the sync result for a single gateway
type GatewaySyncResult struct {
	Success   bool   `json:"success"`
	ProductID string `json:"product_id,omitempty"`
	Error     string `json:"error,omitempty"`
}

// PricingPlanSyncResponse represents the sync result for a single pricing plan
type PricingPlanSyncResponse struct {
	PlanID         uint                         `json:"plan_id"`
	TotalGateways  int                          `json:"total_gateways"`
	SuccessCount   int                          `json:"success_count"`
	FailureCount   int                          `json:"failure_count"`
	Status         SyncStatus                   `json:"status"`
	GatewayResults map[string]GatewaySyncResult `json:"gateway_results"`
}

var _ httputil.DTOResponse[*pricing.SyncGatewayPlanResults] = (*PricingPlanSyncResponse)(nil)

// FromModel converts SyncGatewayPlanResults to PricingPlanSyncResponse
func (r *PricingPlanSyncResponse) FromModel(result *pricing.SyncGatewayPlanResults) error {
	if result == nil {
		return nil
	}

	r.PlanID = result.PlanID
	r.TotalGateways = result.TotalGateways
	r.SuccessCount = result.SuccessCount
	r.FailureCount = result.FailureCount

	if result.FailureCount > 0 && result.SuccessCount == 0 {
		r.Status = SyncStatusError
	} else if result.FailureCount > 0 {
		r.Status = SyncStatusPartial
	} else {
		r.Status = SyncStatusSuccess
	}

	r.GatewayResults = make(map[string]GatewaySyncResult, len(result.Results))
	for gwID, sr := range result.Results {
		r.GatewayResults[gwID] = GatewaySyncResult{
			Success:   sr.Success,
			ProductID: sr.ProductID,
		}
	}
	for gwID, gwErr := range result.Errors {
		r.GatewayResults[gwID] = GatewaySyncResult{
			Success: false,
			Error:   gwErr.Error(),
		}
	}

	return nil
}

// PricingPlanSyncAllResult represents the sync result summary for a single plan in a sync-all operation
type PricingPlanSyncAllResult struct {
	PlanID        uint       `json:"plan_id"`
	SuccessCount  int        `json:"success_count"`
	FailureCount  int        `json:"failure_count"`
	TotalGateways int        `json:"total_gateways"`
	Status        SyncStatus `json:"status"`
}

// PricingPlanSyncAllResponse represents the sync result for all pricing plans
type PricingPlanSyncAllResponse struct {
	TotalPlans    int                       `json:"total_plans"`
	TotalSuccess  int                       `json:"total_success"`
	TotalFailures int                       `json:"total_failures"`
	Results       []PricingPlanSyncAllResult `json:"results"`
}

// SyncAllResult represents the aggregated result of syncing all plans
type SyncAllResult struct {
	PlanResults []*pricing.SyncGatewayPlanResults
}

var _ httputil.DTOResponse[*SyncAllResult] = (*PricingPlanSyncAllResponse)(nil)

// FromModel converts SyncAllResult to PricingPlanSyncAllResponse
func (r *PricingPlanSyncAllResponse) FromModel(result *SyncAllResult) error {
	if result == nil {
		return nil
	}

	r.Results = make([]PricingPlanSyncAllResult, 0, len(result.PlanResults))
	r.TotalSuccess = 0
	r.TotalFailures = 0
	r.TotalPlans = len(result.PlanResults)

	for _, pr := range result.PlanResults {
		if pr == nil {
			continue
		}

		status := SyncStatusSuccess
		if pr.FailureCount > 0 && pr.SuccessCount == 0 {
			status = SyncStatusError
		} else if pr.FailureCount > 0 {
			status = SyncStatusPartial
		} else if pr.SuccessCount == 0 && pr.FailureCount == 0 {
			status = SyncStatusError
		}

		r.Results = append(r.Results, PricingPlanSyncAllResult{
			PlanID:        pr.PlanID,
			SuccessCount:  pr.SuccessCount,
			FailureCount:  pr.FailureCount,
			TotalGateways: pr.TotalGateways,
			Status:        status,
		})
		r.TotalSuccess += pr.SuccessCount
		r.TotalFailures += pr.FailureCount
	}

	return nil
}
