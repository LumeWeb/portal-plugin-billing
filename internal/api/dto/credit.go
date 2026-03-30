package dto

import (
	"encoding/json"
	"time"

	z "github.com/Oudwins/zog"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"go.lumeweb.com/httputil"
	"go.lumeweb.com/portal-plugin-billing/internal/db/models"
)

var _ httputil.DTOResponse[*models.CreditModel] = (*CreditResponse)(nil)

// CreditCreateRequest represents a request to create a credit entry
type CreditCreateRequest struct {
	UserID        uint64          `json:"user_id"`
	Amount        *string         `json:"amount"` // Amount as string to support JSON unmarshaling
	CreditType    string          `json:"credit_type"`
	Direction     string          `json:"direction"`
	Description   string          `json:"description,omitempty"`
	ReferenceID   string          `json:"reference_id,omitempty"`
	ReferenceType string          `json:"reference_type,omitempty"`
}

// Schema defines the validation schema for CreditCreateRequest
func (r CreditCreateRequest) Schema() *z.StructSchema {
	return z.Struct(z.Shape{
		"UserID":        z.UintLike[uint64]().Required(),
		"Amount":        z.Ptr(z.String().Required().Min(1).Max(50)),
		"CreditType":    z.String().Required().Min(1).Max(50),
		"Direction":     z.String().Required().Min(1).Max(10),
		"Description":   z.String().Max(500),
		"ReferenceID":   z.String().Max(255),
		"ReferenceType": z.String().Max(100),
	})
}

// ToModel converts CreditCreateRequest to CreditModel
func (r *CreditCreateRequest) ToModel() (*models.CreditModel, error) {
	amount := decimal.Zero
	if r.Amount != nil {
		parsedAmount, err := decimal.NewFromString(*r.Amount)
		if err != nil {
			return nil, err
		}
		amount = parsedAmount
	}
	return &models.CreditModel{
		UserID:        r.UserID,
		Amount:        amount,
		Type:          r.CreditType,
		Direction:     r.Direction,
		Description:   r.Description,
		ReferenceID:   r.ReferenceID,
		ReferenceType: r.ReferenceType,
		// CreatedBy will be set by the service layer
	}, nil
}

// CreditResponse represents a credit entry response
type CreditResponse struct {
	ID            uuid.UUID              `json:"id"`
	UserID        uint64                 `json:"user_id"`
	Amount        decimal.Decimal        `json:"amount"`
	Type          string                 `json:"type"`
	Direction     string                 `json:"direction"`
	ReferenceID   string                 `json:"reference_id,omitempty"`
	ReferenceType string                 `json:"reference_type,omitempty"`
	Description   string                 `json:"description,omitempty"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
	CreatedBy     uint64                 `json:"created_by"`
	CreatedAt     time.Time              `json:"created_at"`
	UpdatedAt     time.Time              `json:"updated_at"`
	DeletedAt     *time.Time             `json:"deleted_at,omitempty"`
}

// FromModel converts CreditModel to CreditResponse
func (r *CreditResponse) FromModel(credit *models.CreditModel) error {
	*r = CreditResponse{}
	if credit == nil {
		return nil
	}

	r.ID = credit.ID
	r.UserID = credit.UserID
	r.Amount = credit.Amount
	r.Type = credit.Type
	r.Direction = credit.Direction
	r.ReferenceID = credit.ReferenceID
	r.ReferenceType = credit.ReferenceType
	r.Description = credit.Description

	// Parse metadata if present
	if len(credit.Metadata) > 0 {
		if err := json.Unmarshal(credit.Metadata, &r.Metadata); err != nil {
			// If metadata parsing fails, leave as empty map
			r.Metadata = make(map[string]interface{})
		}
	} else {
		r.Metadata = make(map[string]interface{})
	}

	r.CreatedBy = credit.CreatedBy
	r.CreatedAt = credit.CreatedAt
	r.UpdatedAt = credit.UpdatedAt
	r.DeletedAt = credit.DeletedAt

	return nil
}

// CreditListResponse represents a list of credit entries
type CreditListResponse []CreditResponse

// CreditFilterRequest represents filter options for listing credits
type CreditFilterRequest struct {
	UserID      *uint64 `json:"user_id" filter:"true"`
	CreditType  *string `json:"credit_type" filter:"true"`
	Direction   *string `json:"direction" filter:"true"`
}

// Schema defines the validation schema for CreditFilterRequest
func (r CreditFilterRequest) Schema() *z.StructSchema {
	return z.Struct(z.Shape{
		"UserID": z.Ptr(z.UintLike[uint64]()),
		"CreditType": z.Ptr(z.String().OneOf([]string{
			"charge",
			"refund",
			"usage",
			"manual_adjustment",
			"promo",
			"time",
			"charge_back",
			"comp",
		})),
		"Direction": z.Ptr(z.String().OneOf([]string{
			"credit",
			"debit",
		})),
	})
}

// CreditItem represents a lightweight credit item for list responses
type CreditItem struct {
	ID            uuid.UUID       `json:"id"`
	UserID        uint64          `json:"user_id" filter:"true" sort:"true"`
	Amount        decimal.Decimal `json:"amount" filter:"true" sort:"true"`
	Type          string          `json:"type" filter:"true" sort:"true"`
	Direction     string          `json:"direction" filter:"true" sort:"true"`
	Description   string          `json:"description,omitempty"`
	CreatedBy     uint64          `json:"created_by" sort:"true"`
	CreatedAt     time.Time       `json:"created_at" sort:"true"`
	UpdatedAt     time.Time       `json:"updated_at" sort:"true"`
	DeletedAt     *time.Time      `json:"deleted_at,omitempty" sort:"true"`
}

// CreditsListResponse is a swagger-only DTO that represents the paginated response for credits.
// It provides a concrete type for swagger documentation since queryutil.Response generics
// are not properly detected as array types.
//
// Note: This struct is only used for swagger documentation, not for actual encoding.
type CreditsListResponse struct {
	Data []CreditItem `json:"data"`
}

// DeletedCreditsListResponse is a swagger-only DTO that represents the paginated response for deleted credits.
// It provides a concrete type for swagger documentation since queryutil.Response generics
// are not properly detected as array types.
//
// Note: This struct is only used for swagger documentation, not for actual encoding.
type DeletedCreditsListResponse struct {
	Data []CreditItem `json:"data"`
}

// BalanceResponse represents a user's credit balance
type BalanceResponse struct {
	UserID  uint64         `json:"user_id"`
	Balance decimal.Decimal `json:"balance"`
}

// CreditPurgeRequest represents a request to purge old credit entries
type CreditPurgeRequest struct {
	OlderThan string `json:"older_than"`
}

// Schema defines the validation schema for CreditPurgeRequest
func (r CreditPurgeRequest) Schema() *z.StructSchema {
	return z.Struct(z.Shape{
		"OlderThan": z.String().Required().Min(1).Max(50),
	})
}

// ToModel satisfies the DTORequest interface
// CreditPurgeRequest doesn't need to convert to a model, returning the struct itself
func (r CreditPurgeRequest) ToModel() (CreditPurgeRequest, error) {
	return r, nil
}


