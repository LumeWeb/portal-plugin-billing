package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/datatypes"
)

// CreditModel maps credits table to credit ledger domain.
type CreditModel struct {
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
	DeletedAt     *time.Time
}

// TableName specifies GORM table name.
func (CreditModel) TableName() string {
	return "billing_credits"
}
