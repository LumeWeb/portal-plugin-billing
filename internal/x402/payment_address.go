package x402

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

type X402PaymentAddress struct {
	ID             uint            `gorm:"primaryKey"`
	Nonce           string          `gorm:"index;size:64;not null"`
	PaymentID       string          `gorm:"uniqueIndex;size:64;not null"`
	WalletAddress   string          `gorm:"size:128;not null"`
	AssetCode       string          `gorm:"size:16;not null"`
	BlockchainCode  int64           `gorm:"not null"`
	Amount          string          `gorm:"size:64;not null"` // smallest unit
	CreatedAt       time.Time
}

func (X402PaymentAddress) TableName() string {
	return "billing_x402_payment_addresses"
}

type PaymentAddressStore struct {
	db *gorm.DB
}

func NewPaymentAddressStore(db *gorm.DB) *PaymentAddressStore {
	return &PaymentAddressStore{db: db}
}

func (s *PaymentAddressStore) Create(ctx context.Context, addr X402PaymentAddress) error {
	return s.db.WithContext(ctx).Create(&addr).Error
}

func (s *PaymentAddressStore) GetByNonce(ctx context.Context, nonce string) ([]X402PaymentAddress, error) {
	var addrs []X402PaymentAddress
	err := s.db.WithContext(ctx).Where("nonce = ?", nonce).Find(&addrs).Error
	return addrs, err
}

func (s *PaymentAddressStore) GetByPaymentID(ctx context.Context, paymentID string) (*X402PaymentAddress, error) {
	var addr X402PaymentAddress
	err := s.db.WithContext(ctx).Where("payment_id = ?", paymentID).First(&addr).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &addr, err
}

func (s *PaymentAddressStore) DeleteByNonce(ctx context.Context, nonce string) error {
	return s.db.WithContext(ctx).Where("nonce = ?", nonce).Delete(&X402PaymentAddress{}).Error
}

var _ = decimal.Zero
