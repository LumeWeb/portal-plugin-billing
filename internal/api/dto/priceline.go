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
var _ httputil.DTOResponse[*models.PriceLine] = (*PriceLineDetailResponse)(nil)

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

// FromModel converts a PriceLine model to PriceLineDetailResponse
func (r *PriceLineDetailResponse) FromModel(priceline *models.PriceLine) error {
	*r = PriceLineDetailResponse{}
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
	return &models.PriceLine{
		Name:        r.Name,
		Description: r.Description,
	}, nil
}

// AddPlanToPriceLineRequest represents a request to add a plan to a price line
type AddPlanToPriceLineRequest struct {
	PlanID   uint `json:"plan_id"`
	Position int  `json:"position"`
}

func (r AddPlanToPriceLineRequest) Schema() *z.StructSchema {
	return z.Struct(z.Shape{
		"PlanID":   z.UintLike[uint]().Required(),
		"Position": z.Int().Required(),
	})
}

func (r *AddPlanToPriceLineRequest) ToModel() (*AddPlanToPriceLineRequest, error) {
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

// PriceLineFilterRequest represents filter options for listing price lines
type PriceLineFilterRequest struct {
	Name      string `json:"name" filter:"true"`
	IsActive  *bool  `json:"is_active" filter:"true"`
	IsDefault *bool  `json:"is_default" filter:"true"`
}
