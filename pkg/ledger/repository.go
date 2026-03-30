package ledger

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// CreditRepository defines the interface for credit data operations.
// Implementations can use GORM, pure SQL, or any data store.
type CreditRepository interface {
	// CreateCredit inserts a new credit entry.
	// Must return error on duplicate ID.
	CreateCredit(ctx context.Context, credit *Credit) error

	// GetCredit retrieves a credit by ID.
	GetCredit(ctx context.Context, id uuid.UUID) (*Credit, error)

	// GetUserBalance returns the current balance for a user.
	// Credits add to balance, debits subtract.
	GetUserBalance(ctx context.Context, userID uint64) (decimal.Decimal, error)

	// SoftDeleteCredit marks a credit as deleted with timestamp.
	SoftDeleteCredit(ctx context.Context, id uuid.UUID) error

	// RestoreCredit restores a soft-deleted credit.
	RestoreCredit(ctx context.Context, id uuid.UUID) error

	// GetDeletedCredits retrieves deleted credits by user ID.
	GetDeletedCredits(ctx context.Context, userID uint64) ([]Credit, error)

	// PurgeDeletedCredits permanently removes credits older than duration.
	PurgeDeletedCredits(ctx context.Context, olderThan time.Duration) (int, error)

	// GetCreditsByReference finds credits by reference ID and type.
	GetCreditsByReference(ctx context.Context, referenceID string, referenceType string) ([]Credit, error)
}
