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
	ID            uuid.UUID         `gorm:"primaryKey;type:text"`
	UserID        uint64            `gorm:"not null;index"`
	Amount        decimal.Decimal   `gorm:"type:decimal;not null"`
	Type          string            `gorm:"type:text;not null"`
	Direction     string            `gorm:"type:text;not null"`
	ReferenceID   string            `gorm:"type;text"`
	ReferenceType string            `gorm:"type:text"`
	Description   string            `gorm:"type:text"`
	Metadata      datatypes.JSON    `gorm:"type:text"`
	CreatedBy     uint64            `gorm:"not null"`
	CreatedAt     time.Time         `gorm:"autoCreateTime"`
	UpdatedAt     time.Time         `gorm:"autoUpdateTime"`
}

// TableName specifies GORM table name for the view.
func (CreditActiveView) TableName() string {
	return "billing_credits_active"
}

// CreditsBalanceView represents the billing_credits_balance database view.
type CreditsBalanceView struct {
	UserID  uint64          `gorm:"primaryKey"`
	Balance decimal.Decimal `gorm:"type:decimal;not null"`
}

// TableName specifies GORM table name for the view.
func (CreditsBalanceView) TableName() string {
	return "billing_credits_balance"
}
