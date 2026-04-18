package dto

import (
	"time"

	z "github.com/Oudwins/zog"
	"go.lumeweb.com/httputil"
	"go.lumeweb.com/portal-plugin-billing/internal/db/models"
	_ "go.lumeweb.com/queryutil"
)


// Initialize zog to satisfy import - the z alias is used throughout this file
var _ = z.Int()


var _ httputil.DTOResponse[*models.PriceLine] = (*PriceLineResponse)(nil)
var _ httputil.DTOResponse[*PriceLineDetail] = (*PriceLineDetailResponse)(nil)

// PriceLineDetail wraps a PriceLine with its associated plans
type PriceLineDetail struct {
	*models.PriceLine
	Plans []*models.PriceLinePlan
}

// PriceLineResponse represents a price line response
type PriceLineResponse struct {
	ID          uint      `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	IsActive    bool      `json:"is_active"`
	IsDefault   bool      `json:"is_default"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// FromModel converts a PriceLine model to PriceLineResponse
func (r *PriceLineResponse) FromModel(priceline *models.PriceLine) error {
	*r = PriceLineResponse{}
	if priceline == nil {
		return nil
	}

	r.ID = priceline.ID
	r.Name = priceline.Name
	r.Description = priceline.Description
	r.IsActive = priceline.IsActive
	r.IsDefault = priceline.IsDefault
	r.CreatedAt = priceline.CreatedAt
	r.UpdatedAt = priceline.UpdatedAt

	return nil
}

// PriceLineDetailResponse represents a detailed price line response with plans
type PriceLineDetailResponse struct {
	ID          uint                `json:"id"`
	Name        string              `json:"name"`
	Description string              `json:"description"`
	IsActive    bool                `json:"is_active"`
	IsDefault   bool                `json:"is_default"`
	CreatedAt   time.Time           `json:"created_at"`
	UpdatedAt   time.Time           `json:"updated_at"`
	Plans       []PricingPlanItem   `json:"plans,omitempty"`
}

// FromModel converts a PriceLineDetail to PriceLineDetailResponse
func (r *PriceLineDetailResponse) FromModel(detail *PriceLineDetail) error {
	*r = PriceLineDetailResponse{}
	if detail == nil || detail.PriceLine == nil {
		return nil
	}

	pl := detail.PriceLine

	r.ID = pl.ID
	r.Name = pl.Name
	r.Description = pl.Description
	r.IsActive = pl.IsActive
	r.IsDefault = pl.IsDefault
	r.CreatedAt = pl.CreatedAt
	r.UpdatedAt = pl.UpdatedAt

	r.SetPlans(detail.Plans)

	return nil
}

// SetPlans populates the plans field from PriceLinePlan associations
func (r *PriceLineDetailResponse) SetPlans(priceLinePlans []*models.PriceLinePlan) {
	r.Plans = make([]PricingPlanItem, 0, len(priceLinePlans))
	for _, plp := range priceLinePlans {
		if plp.PricingPlan == nil {
			continue
		}
		item := PricingPlanItem{
			ID:          plp.PricingPlan.ID,
			Name:        plp.PricingPlan.Name,
			Description: plp.PricingPlan.Description,
			Currency:    plp.PricingPlan.Currency,
			IsActive:    plp.PricingPlan.IsActive,
			Position:    plp.Position,
		}
		// Extract monthly/yearly prices from periods if available
		for _, period := range plp.PricingPlan.Periods {
			if period.DeletedAt.Time.IsZero() {
				switch period.Cadence {
				case "monthly":
					item.MonthlyPrice = &period.PriceUSD
				case "yearly":
					item.YearlyPrice = &period.PriceUSD
				}
			}
		}
		r.Plans = append(r.Plans, item)
	}
}

// PriceLineCreateRequest represents a request to create a price line
type PriceLineCreateRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	IsActive    *bool  `json:"is_active"`
	IsDefault   bool   `json:"is_default"`
}

func (r PriceLineCreateRequest) Schema() *z.StructSchema {
	return z.Struct(z.Shape{
		"Name":        z.String().Required().Min(1).Max(255),
		"Description": z.String().Required().Min(1).Max(500),
		"IsActive":    z.Ptr(z.Bool()),
		"IsDefault":   z.Bool(),
	})
}

func (r *PriceLineCreateRequest) ToModel() (*models.PriceLine, error) {
	isActive := true
	if r.IsActive != nil {
		isActive = *r.IsActive
	}

	return &models.PriceLine{
		Name:        r.Name,
		Description: r.Description,
		IsActive:    isActive,
		IsDefault:   r.IsDefault,
	}, nil
}

// PriceLineUpdateRequest represents a request to update a price line
type PriceLineUpdateRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	IsActive    *bool  `json:"is_active"`
	IsDefault   bool   `json:"is_default"`
}

func (r PriceLineUpdateRequest) Schema() *z.StructSchema {
	return z.Struct(z.Shape{
		"Name":        z.String().Min(1).Max(255),
		"Description": z.String().Min(1).Max(500),
		"IsActive":    z.Ptr(z.Bool()),
		"IsDefault":   z.Bool(),
	})
}

func (r *PriceLineUpdateRequest) ToModel() (*models.PriceLine, error) {
	line := &models.PriceLine{
		Name:        r.Name,
		Description: r.Description,
	}

	if r.IsActive != nil {
		line.IsActive = *r.IsActive
	}

	line.IsDefault = r.IsDefault

	return line, nil
}

// AddPlanToPriceLineRequest represents a request to add a plan to a price line
type AddPlanToPriceLineRequest struct {
	PlanID   uint  `json:"plan_id"`
	Position *int  `json:"position"`
}

func (r AddPlanToPriceLineRequest) Schema() *z.StructSchema {
	return z.Struct(z.Shape{
		"PlanID":   z.Uint().Required(),
		"Position": z.Ptr(z.Int()).NotNil(),
	})
}

func (r *AddPlanToPriceLineRequest) ToModel() (*AddPlanToPriceLineRequest, error) {
	return r, nil
}

// UpdatePlanPositionRequest represents a request to update a plan's position in a price line
type UpdatePlanPositionRequest struct {
	Position *int `json:"position"`
}

func (r UpdatePlanPositionRequest) Schema() *z.StructSchema {
	return z.Struct(z.Shape{
		"Position": z.Ptr(z.Int()).NotNil(),
	})
}

func (r *UpdatePlanPositionRequest) ToModel() (*UpdatePlanPositionRequest, error) {
	return r, nil
}

// PriceLineAssignmentRequest represents a request to assign a price line to a user
type PriceLineAssignmentRequest struct {
	UserID      uint `json:"user_id"`
	PriceLineID uint `json:"price_line_id"`
}

func (r PriceLineAssignmentRequest) Schema() *z.StructSchema {
	return z.Struct(z.Shape{
		"UserID":      z.UintLike[uint]().Required(),
		"PriceLineID": z.UintLike[uint]().Required(),
	})
}

func (r *PriceLineAssignmentRequest) ToModel() (*models.PriceLineAssignment, error) {
	return &models.PriceLineAssignment{
		UserID:      r.UserID,
		PriceLineID: r.PriceLineID,
	}, nil
}

// PricingPlanItem represents a pricing plan in a list response
type PricingPlanItem struct {
	ID            uint      `json:"id"`
	Name          string    `json:"name"`
	Description   string    `json:"description"`
	MonthlyPrice  *float64  `json:"monthly_price,omitempty"`
	YearlyPrice   *float64  `json:"yearly_price,omitempty"`
	Currency      string    `json:"currency"`
	IsActive      bool      `json:"is_active"`
	Position      int       `json:"position,omitempty"`
}

// PricingPlansListResponse is a swagger-only DTO that represents the paginated response for pricing plans.
// It merges the generic queryutil.Response[*dto.PricingPlanItem] for OpenAPI documentation.
//
// This struct exists due to a TODO bug where queryutil.Response generics are not getting detected
// properly as an array type in the swagger documentation generation. By providing a concrete struct,
// we ensure the swagger docs correctly show the data field as an array of PricingPlanItem items.
//
// Note: This struct is only used for swagger documentation, not for actual encoding.
type PricingPlansListResponse struct {
	Data  []PricingPlanItem `json:"data"`
	Total int64             `json:"total"`
}

// PriceLinesListResponse is a swagger-only DTO that represents the paginated response for price lines.
// It provides a concrete type for swagger documentation since queryutil.Response generics
// are not properly detected as array types.
//
// Note: This struct is only used for swagger documentation, not for actual encoding.
type PriceLinesListResponse struct {
	Data  []PriceLineResponse `json:"data"`
	Total int64               `json:"total"`
}

// PriceLineFilterRequest represents filter options for listing price lines
type PriceLineFilterRequest struct {
	Name      string `json:"name" filter:"true"`
	IsActive  *bool  `json:"is_active" filter:"true"`
	IsDefault *bool  `json:"is_default" filter:"true"`
}
