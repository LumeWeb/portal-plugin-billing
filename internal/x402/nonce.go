package x402

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

const DefaultGatewayType = "atlos"

// NonceStatus is the lifecycle state of an x402 nonce.
type NonceStatus string

const (
	NonceStatusPending  NonceStatus = "pending"
	NonceStatusSettled  NonceStatus = "settled"
	NonceStatusExpired  NonceStatus = "expired"
	NonceStatusMismatch NonceStatus = "mismatch" // ATLOS paid a different amount than challenged
)

type NonceStore interface {
	Set(ctx context.Context, nonce string, userID uint, wallet string, amount decimal.Decimal, gatewayType string, challengeAccepts string, expiry time.Duration) error
	Get(ctx context.Context, nonce string) (userID uint, amount decimal.Decimal, gatewayType string, ok bool, err error)
	GetForConfirmation(ctx context.Context, nonce string) (userID uint, wallet string, amount decimal.Decimal, gatewayType string, challengeAccepts string, ok bool, err error)
	Delete(ctx context.Context, nonce string) error
	Consume(ctx context.Context, nonce string) (bool, error)
	SetGatewayPaymentID(ctx context.Context, nonce string, paymentID string) error
	GetByGatewayPaymentID(ctx context.Context, paymentID string) (nonce string, userID uint, amount decimal.Decimal, ok bool, err error)
	Settle(ctx context.Context, nonce string, reference string) error
}

type DBNonceStore struct {
	db *gorm.DB
}

func NewDBNonceStore(db *gorm.DB) *DBNonceStore {
	return &DBNonceStore{db: db}
}

type X402Nonce struct {
	ID               uint             `gorm:"primaryKey"`
	Nonce            string           `gorm:"uniqueIndex;size:66;not null"`
	GatewayPaymentID *string          `gorm:"size:64;index;default:null"` // gateway payment ID for webhook correlation
	UserID           uint             `gorm:"not null"`
	Amount           decimal.Decimal  `gorm:"type:decimal(20,10);not null"`
	Wallet           string           `gorm:"size:64;not null"` // challenge wallet (lowercased) — signer must match
	GatewayType      string           `gorm:"size:32;not null"`
	Status           NonceStatus      `gorm:"size:16;not null;default:'pending'"`
	Reference        string           `gorm:"size:128;default:null"` // transaction reference when settled
	ChallengeAccepts string           `gorm:"type:text;default:null"` // JSON of accepted payment requirements from challenge
	ExpiresAt        time.Time        `gorm:"not null"`
	CreatedAt        time.Time
	SettledAt        *time.Time
}

func (X402Nonce) TableName() string {
	return "billing_x402_nonces"
}

func (s *DBNonceStore) Set(ctx context.Context, nonce string, userID uint, wallet string, amount decimal.Decimal, gatewayType string, challengeAccepts string, expiry time.Duration) error {
	record := X402Nonce{
		Nonce:            nonce,
		UserID:           userID,
		Wallet:           wallet,
		Amount:           amount,
		GatewayType:      gatewayType,
		ChallengeAccepts: challengeAccepts,
		Status:           NonceStatusPending,
		ExpiresAt:        time.Now().Add(expiry),
	}
	return s.db.WithContext(ctx).Create(&record).Error
}

func (s *DBNonceStore) Get(ctx context.Context, nonce string) (uint, decimal.Decimal, string, bool, error) {
	var record X402Nonce
	err := s.db.WithContext(ctx).Where("nonce = ? AND status = ? AND expires_at > ?", nonce, NonceStatusPending, time.Now()).First(&record).Error
	if err == gorm.ErrRecordNotFound {
		return 0, decimal.Zero, "", false, nil
	}
	if err != nil {
		return 0, decimal.Zero, "", false, err
	}
	return record.UserID, record.Amount, record.GatewayType, true, nil
}

// GetForConfirmation looks up a nonce for payment confirmation.
// Unlike Get, it accepts both pending and settled nonces, since the ATLOS
// webhook may settle the nonce before the client calls back with the proof.
func (s *DBNonceStore) GetForConfirmation(ctx context.Context, nonce string) (uint, string, decimal.Decimal, string, string, bool, error) {
	var record X402Nonce
	err := s.db.WithContext(ctx).
		Where("nonce = ? AND status IN (?, ?) AND expires_at > ?", nonce, NonceStatusPending, NonceStatusSettled, time.Now()).
		First(&record).Error
	if err == gorm.ErrRecordNotFound {
		return 0, "", decimal.Zero, "", "", false, nil
	}
	if err != nil {
		return 0, "", decimal.Zero, "", "", false, err
	}
	return record.UserID, record.Wallet, record.Amount, record.GatewayType, record.ChallengeAccepts, true, nil
}

func (s *DBNonceStore) Delete(ctx context.Context, nonce string) error {
	return s.db.WithContext(ctx).Where("nonce = ?", nonce).Delete(&X402Nonce{}).Error
}

// Consume atomically deletes a pending nonce. Returns false if the nonce
// was already settled by the webhook or doesn't exist, preventing double-credit.
func (s *DBNonceStore) Consume(ctx context.Context, nonce string) (bool, error) {
	// Only delete pending nonces. If the webhook already settled the nonce,
	// RowsAffected will be 0 and the caller knows not to issue credit.
	result := s.db.WithContext(ctx).
		Where("nonce = ? AND status = ?", nonce, NonceStatusPending).
		Delete(&X402Nonce{})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func (s *DBNonceStore) SetGatewayPaymentID(ctx context.Context, nonce string, paymentID string) error {
	return s.db.WithContext(ctx).
		Model(&X402Nonce{}).
		Where("nonce = ?", nonce).
		Update("gateway_payment_id", paymentID).Error
}

func (s *DBNonceStore) GetByGatewayPaymentID(ctx context.Context, paymentID string) (string, uint, decimal.Decimal, bool, error) {
	var record X402Nonce
	err := s.db.WithContext(ctx).Where("gateway_payment_id = ? AND status = ? AND expires_at > ?", paymentID, NonceStatusPending, time.Now()).First(&record).Error
	if err == gorm.ErrRecordNotFound {
		return "", 0, decimal.Zero, false, nil
	}
	if err != nil {
		return "", 0, decimal.Zero, false, err
	}
	return record.Nonce, record.UserID, record.Amount, true, nil
}

func (s *DBNonceStore) Settle(ctx context.Context, nonce string, reference string) error {
	now := time.Now()
	return s.db.WithContext(ctx).
		Model(&X402Nonce{}).
		Where("nonce = ? AND status = ?", nonce, NonceStatusPending).
		Updates(map[string]interface{}{
			"status":     NonceStatusSettled,
			"settled_at": now,
			"reference":  reference,
		}).Error
}
