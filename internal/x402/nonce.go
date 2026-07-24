package x402

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// DefaultGatewayType is the hardcoded gateway for x402 payments.
const DefaultGatewayType = "atlos"

// NonceStore tracks issued challenges.
type NonceStore interface {
	Set(ctx context.Context, nonce string, userID uint, amount decimal.Decimal, gatewayType string, expiry time.Duration) error
	Get(ctx context.Context, nonce string) (userID uint, amount decimal.Decimal, gatewayType string, ok bool, err error)
	Delete(ctx context.Context, nonce string) error
}

// DBNonceStore uses the database for persistent nonce tracking.
type DBNonceStore struct {
	db *gorm.DB
}

// NewDBNonceStore creates a new DBNonceStore.
func NewDBNonceStore(db *gorm.DB) *DBNonceStore {
	return &DBNonceStore{db: db}
}

// X402Nonce represents a stored payment challenge nonce.
type X402Nonce struct {
	ID          uint            `gorm:"primaryKey"`
	Nonce       string          `gorm:"uniqueIndex;size:64;not null"`
	UserID      uint            `gorm:"not null"`
	Amount      decimal.Decimal `gorm:"type:decimal(20,10);not null"`
	GatewayType string          `gorm:"size:32;not null"`
	Status      string          `gorm:"size:16;not null;default:'pending'"`
	ExpiresAt   time.Time       `gorm:"not null"`
	CreatedAt   time.Time
	SettledAt   *time.Time
}

// Set stores a new nonce record.
func (s *DBNonceStore) Set(ctx context.Context, nonce string, userID uint, amount decimal.Decimal, gatewayType string, expiry time.Duration) error {
	record := X402Nonce{
		Nonce:       nonce,
		UserID:      userID,
		Amount:      amount,
		GatewayType: gatewayType,
		Status:      "pending",
		ExpiresAt:   time.Now().Add(expiry),
	}
	return s.db.WithContext(ctx).Create(&record).Error
}

// Get retrieves a pending, non-expired nonce record.
func (s *DBNonceStore) Get(ctx context.Context, nonce string) (uint, decimal.Decimal, string, bool, error) {
	var record X402Nonce
	err := s.db.WithContext(ctx).Where("nonce = ? AND status = ? AND expires_at > ?", nonce, "pending", time.Now()).First(&record).Error
	if err == gorm.ErrRecordNotFound {
		return 0, decimal.Zero, "", false, nil
	}
	if err != nil {
		return 0, decimal.Zero, "", false, err
	}
	return record.UserID, record.Amount, record.GatewayType, true, nil
}

// Delete removes a nonce record.
func (s *DBNonceStore) Delete(ctx context.Context, nonce string) error {
	return s.db.WithContext(ctx).Where("nonce = ?", nonce).Delete(&X402Nonce{}).Error
}
