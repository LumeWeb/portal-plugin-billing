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
	// SetGatewayPaymentID associates a gateway payment ID with a nonce for webhook correlation.
	SetGatewayPaymentID(ctx context.Context, nonce string, paymentID string) error
	// GetByGatewayPaymentID looks up a nonce by gateway payment ID.
	GetByGatewayPaymentID(ctx context.Context, paymentID string) (nonce string, userID uint, amount decimal.Decimal, ok bool, err error)
	// Settle marks a nonce as settled and records the transaction reference.
	Settle(ctx context.Context, nonce string, reference string) error
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
	ID               uint            `gorm:"primaryKey"`
	Nonce            string          `gorm:"uniqueIndex;size:64;not null"`
	GatewayPaymentID *string         `gorm:"size:64;index;default:null"` // gateway payment ID for webhook correlation
	UserID           uint            `gorm:"not null"`
	Amount           decimal.Decimal `gorm:"type:decimal(20,10);not null"`
	GatewayType      string          `gorm:"size:32;not null"`
	Status           string          `gorm:"size:16;not null;default:'pending'"`
	Reference        string          `gorm:"size:128;default:null"` // transaction reference when settled
	ExpiresAt        time.Time       `gorm:"not null"`
	CreatedAt        time.Time
	SettledAt        *time.Time
}

// TableName sets the table name for X402Nonce
func (X402Nonce) TableName() string {
	return "billing_x402_nonces"
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

// SetGatewayPaymentID associates a gateway payment ID with a nonce for webhook correlation.
func (s *DBNonceStore) SetGatewayPaymentID(ctx context.Context, nonce string, paymentID string) error {
	return s.db.WithContext(ctx).
		Model(&X402Nonce{}).
		Where("nonce = ?", nonce).
		Update("gateway_payment_id", paymentID).Error
}

// GetByGatewayPaymentID looks up a nonce by gateway payment ID.
func (s *DBNonceStore) GetByGatewayPaymentID(ctx context.Context, paymentID string) (string, uint, decimal.Decimal, bool, error) {
	var record X402Nonce
	err := s.db.WithContext(ctx).Where("gateway_payment_id = ? AND status = ? AND expires_at > ?", paymentID, "pending", time.Now()).First(&record).Error
	if err == gorm.ErrRecordNotFound {
		return "", 0, decimal.Zero, false, nil
	}
	if err != nil {
		return "", 0, decimal.Zero, false, err
	}
	return record.Nonce, record.UserID, record.Amount, true, nil
}

// Settle marks a nonce as settled with a transaction reference.
func (s *DBNonceStore) Settle(ctx context.Context, nonce string, reference string) error {
	now := time.Now()
	return s.db.WithContext(ctx).
		Model(&X402Nonce{}).
		Where("nonce = ? AND status = ?", nonce, "pending").
		Updates(map[string]interface{}{
			"status":     "settled",
			"settled_at": now,
			"reference":  reference,
		}).Error
}
