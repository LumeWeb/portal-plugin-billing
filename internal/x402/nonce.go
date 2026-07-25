package x402

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

const DefaultGatewayType = "atlos"

type NonceStore interface {
	Set(ctx context.Context, nonce string, userID uint, amount decimal.Decimal, gatewayType string, expiry time.Duration) error
	Get(ctx context.Context, nonce string) (userID uint, amount decimal.Decimal, gatewayType string, ok bool, err error)
	Delete(ctx context.Context, nonce string) error
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

func (X402Nonce) TableName() string {
	return "billing_x402_nonces"
}

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

func (s *DBNonceStore) Delete(ctx context.Context, nonce string) error {
	return s.db.WithContext(ctx).Where("nonce = ?", nonce).Delete(&X402Nonce{}).Error
}

func (s *DBNonceStore) SetGatewayPaymentID(ctx context.Context, nonce string, paymentID string) error {
	return s.db.WithContext(ctx).
		Model(&X402Nonce{}).
		Where("nonce = ?", nonce).
		Update("gateway_payment_id", paymentID).Error
}

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
