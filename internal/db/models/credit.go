package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/datatypes"
)

// CreditModel maps credits table to credit ledger domain.
type CreditModel struct {
	ID            uuid.UUID         `gorm:"primaryKey;type:text"`
	UserID        uint64            `gorm:"not null;index"`
	Amount        decimal.Decimal   `gorm:"type:decimal;not null"`
	Type          string            `gorm:"type:text;not null"`
	Direction     string            `gorm:"type:text;not null"`
	ReferenceID   string            `gorm:"type:text"`
	ReferenceType string            `gorm:"type:text"`
	Description   string            `gorm:"type:text"`
	Metadata      datatypes.JSON    `gorm:"type:text"`
	CreatedBy     uint64            `gorm:"not null"`
	CreatedAt     time.Time         `gorm:"autoCreateTime"`
	UpdatedAt     time.Time         `gorm:"autoUpdateTime"`
	DeletedAt     *time.Time        `gorm:"index"`
}

// TableName specifies GORM table name.
func (CreditModel) TableName() string {
	return "billing_credits"
}
