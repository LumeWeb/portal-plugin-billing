package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/datatypes"
)

// CreditActiveView represents the billing_credits_active database view.
// This view contains only non-deleted credits.
type CreditActiveView struct {
	ID            uuid.UUID
	UserID        uint64
	Amount        decimal.Decimal
	Type          string
	Direction     string
	ReferenceID   string
	ReferenceType string
	Description   string
	Metadata      datatypes.JSON
	CreatedBy     uint64
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// TableName specifies GORM table name for the view.
func (CreditActiveView) TableName() string {
	return "billing_credits_active"
}

// CreditsBalanceView represents the billing_credits_balance database view.
type CreditsBalanceView struct {
	UserID  uint64
	Balance decimal.Decimal
}

// TableName specifies GORM table name for the view.
func (CreditsBalanceView) TableName() string {
	return "billing_credits_balance"
}
