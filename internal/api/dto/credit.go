package dto

import (
	"encoding/json"
	"time"

	z "github.com/Oudwins/zog"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"go.lumeweb.com/httputil"
	"go.lumeweb.com/portal-plugin-billing/core"
	"go.lumeweb.com/portal-plugin-billing/internal/db/models"
)

var _ httputil.DTOResponse[*models.CreditModel] = (*CreditResponse)(nil)
var _ httputil.DTOResponse[*models.CreditModel] = (*UserCreditItem)(nil)
var _ httputil.DTOResponse[*models.CreditsBalanceView] = (*BalanceResponse)(nil)
var _ httputil.DTOResponse[*PurgeResult] = (*CreditPurgeResponse)(nil)

// CreditCreateRequest represents a request to create a credit entry
type CreditCreateRequest struct {
	UserID          uint64  `json:"user_id"`
	Amount          *string `json:"amount"` // Amount as string to support JSON unmarshaling
	TransactionType string  `json:"type"`
	Direction       string  `json:"direction"`
	Description     string  `json:"description,omitempty"`
	ReferenceID     string  `json:"reference_id,omitempty"`
	ReferenceType   string  `json:"reference_type,omitempty"`
}

// Schema defines the validation schema for CreditCreateRequest
func (r CreditCreateRequest) Schema() *z.StructSchema {
	return z.Struct(z.Shape{
		"UserID":          z.UintLike[uint64]().Required(),
		"Amount":          z.Ptr(z.String().Required().Min(1).Max(50)),
		"TransactionType": z.String().Required().Min(1).Max(50),
		"Direction":       z.String().Required().Min(1).Max(10),
		"Description":     z.String().Max(500),
		"ReferenceID":     z.String().Max(255),
		"ReferenceType":   z.String().Max(100),
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
		Type:          r.TransactionType,
		Direction:     r.Direction,
		Description:   r.Description,
		ReferenceID:   r.ReferenceID,
		ReferenceType: r.ReferenceType,
		// CreatedBy will be set by the service layer
	}, nil
}

// CreditResponse represents a credit entry response
type CreditResponse struct {
	ID              uuid.UUID              `json:"id"`
	UserID          uint64                 `json:"user_id"`
	Amount          decimal.Decimal        `json:"amount"`
	TransactionType string                 `json:"type"`
	Direction       string                 `json:"direction"`
	ReferenceID     string                 `json:"reference_id,omitempty"`
	ReferenceType   string                 `json:"reference_type,omitempty"`
	Description     string                 `json:"description,omitempty"`
	Metadata        map[string]interface{} `json:"metadata,omitempty"`
	CreatedBy       uint64                 `json:"created_by"`
	CreatedAt       time.Time              `json:"created_at"`
	UpdatedAt       time.Time              `json:"updated_at"`
	DeletedAt       *time.Time             `json:"deleted_at,omitempty"`
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
	r.TransactionType = credit.Type
	r.Direction = credit.Direction
	r.ReferenceID = credit.ReferenceID
	r.ReferenceType = credit.ReferenceType
	r.Description = credit.Description

	// Parse metadata if present
	if len(credit.Metadata) > 0 {
		if err := json.Unmarshal(credit.Metadata, &r.Metadata); err != nil {
			// If metadata parsing fails, leave as nil
			r.Metadata = nil
		}
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
	UserID          *uint64 `json:"user_id" filter:"true"`
	TransactionType *string `json:"type" filter:"true"`
	Direction       *string `json:"direction" filter:"true"`
}

// Schema defines the validation schema for CreditFilterRequest
func (r CreditFilterRequest) Schema() *z.StructSchema {
	return z.Struct(z.Shape{
		"UserID": z.Ptr(z.UintLike[uint64]()),
		"TransactionType": z.Ptr(z.String().OneOf([]string{
			core.TransactionTypeCharge,
			core.TransactionTypeRefund,
			core.TransactionTypeUsage,
			core.TransactionTypeManual,
			core.TransactionTypePromo,
			core.TransactionTypeTime,
			core.TransactionTypeChargeBack,
			core.TransactionTypeComp,
		})),
		"Direction": z.Ptr(z.String().OneOf([]string{
			core.DirectionCredit,
			core.DirectionDebit,
		})),
	})
}

// CreditItem represents a lightweight credit item for list responses
type CreditItem struct {
	ID              uuid.UUID       `json:"id"`
	UserID          uint64          `json:"user_id" filter:"true" sort:"true"`
	Amount          decimal.Decimal `json:"amount" filter:"true" sort:"true"`
	TransactionType string          `json:"type" filter:"true" sort:"true"`
	Direction       string          `json:"direction" filter:"true" sort:"true"`
	Description     string          `json:"description,omitempty"`
	CreatedBy       uint64          `json:"created_by" sort:"true"`
	CreatedAt       time.Time       `json:"created_at" sort:"true"`
	UpdatedAt       time.Time       `json:"updated_at" sort:"true"`
	DeletedAt       *time.Time      `json:"deleted_at,omitempty" sort:"true"`
}

// UserCreditItem represents a user-facing credit item without internal fields
type UserCreditItem struct {
	ID              uuid.UUID       `json:"id"`
	Amount          decimal.Decimal `json:"amount"`
	TransactionType string          `json:"type"`
	Direction       string          `json:"direction"`
	Description     string          `json:"description,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
}

// FromModel converts CreditModel to UserCreditItem (user-facing)
func (r *UserCreditItem) FromModel(credit *models.CreditModel) error {
	*r = UserCreditItem{}
	if credit == nil {
		return nil
	}

	r.ID = credit.ID
	r.Amount = credit.Amount
	r.TransactionType = credit.Type
	r.Direction = credit.Direction
	r.Description = credit.Description
	r.CreatedAt = credit.CreatedAt

	return nil
}

// CreditsListResponse is a swagger-only DTO that represents the paginated response for credits.
// It provides a concrete type for swagger documentation since queryutil.Response generics
// are not properly detected as array types.
//
// Note: This struct is only used for swagger documentation, not for actual encoding.
type CreditsListResponse struct {
	Data  []CreditItem `json:"data"`
	Total int64        `json:"total"`
}

// UserCreditsListResponse is a swagger-only DTO that represents the paginated response for user credits.
// It provides a concrete type for swagger documentation since queryutil.Response generics
// are not properly detected as array types.
//
// Note: This struct is only used for swagger documentation, not for actual encoding.
type UserCreditsListResponse struct {
	Data  []UserCreditItem `json:"data"`
	Total int64            `json:"total"`
}

// DeletedCreditsListResponse is a swagger-only DTO that represents the paginated response for deleted credits.
// It provides a concrete type for swagger documentation since queryutil.Response generics
// are not properly detected as array types.
//
// Note: This struct is only used for swagger documentation, not for actual encoding.
type DeletedCreditsListResponse struct {
	Data  []CreditItem `json:"data"`
	Total int64        `json:"total"`
}

// BalanceResponse represents a user's credit balance
type BalanceResponse struct {
	UserID  uint64          `json:"user_id"`
	Balance decimal.Decimal `json:"balance"`
}

// FromModel converts CreditsBalanceView to BalanceResponse
func (r *BalanceResponse) FromModel(view *models.CreditsBalanceView) error {
	if view == nil {
		return nil
	}

	*r = BalanceResponse{
		UserID:  view.UserID,
		Balance: view.Balance,
	}

	return nil
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

// CreditPurgeResponse represents the result of a credit purge operation
type CreditPurgeResponse struct {
	PurgedCount int64 `json:"purged_count"`
}

// PurgeResult represents the result of a purge operation for DTO conversion
type PurgeResult struct {
	Count int
}

// FromModel converts PurgeResult to CreditPurgeResponse
func (r *CreditPurgeResponse) FromModel(result *PurgeResult) error {
	if result == nil {
		return nil
	}
	*r = CreditPurgeResponse{
		PurgedCount: int64(result.Count),
	}
	return nil
}
