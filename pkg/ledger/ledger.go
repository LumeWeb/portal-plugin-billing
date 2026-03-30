package ledger

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// Ledger provides credit/debit operations with repository and registry integration.
type Ledger struct {
	repo    CreditRepository
	registry CreditTypeRegistry
}

// NewLedger creates a new Ledger with the given repository and registry.
func NewLedger(repo CreditRepository, registry CreditTypeRegistry) *Ledger {
	return &Ledger{
		repo:    repo,
		registry: registry,
	}
}

// IssueCredit creates a credit entry with type validation.
func (l *Ledger) IssueCredit(
	ctx context.Context,
	userID uint64,
	amount decimal.Decimal,
	creditType string,
	direction Direction,
	referenceID string,
	referenceType string,
	metadata CreditMetadata,
) error {
	if _, err := l.registry.GetType(creditType); err != nil {
		return fmt.Errorf("unknown credit type: %s", creditType)
	}

	if err := l.registry.ValidateAmount(creditType, amount); err != nil {
		return fmt.Errorf("invalid amount: %w", err)
	}

	credit := &Credit{
		ID:            uuid.New(),
		UserID:        userID,
		Amount:        amount,
		Type:          creditType,
		Direction:     GetDirection(direction),
		ReferenceID:   referenceID,
		ReferenceType: referenceType,
		Description:   metadata.Description,
		Metadata:      metadata.Raw,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
		CreatedBy:     metadata.CreatedBy,
	}

	return l.repo.CreateCredit(ctx, credit)
}

// DebitCredit creates a debit entry (calls IssueCredit with DebitDirection).
func (l *Ledger) DebitCredit(
	ctx context.Context,
	userID uint64,
	amount decimal.Decimal,
	debitType string,
	referenceID string,
	referenceType string,
	metadata CreditMetadata,
) error {
	return l.IssueCredit(ctx, userID, amount, debitType, DebitDirection,
		referenceID, referenceType, metadata)
}

// GetUserBalance returns the current balance for a user.
func (l *Ledger) GetUserBalance(ctx context.Context, userID uint64) (decimal.Decimal, error) {
	return l.repo.GetUserBalance(ctx, userID)
}

// SoftDeleteCredit marks a credit as deleted.
func (l *Ledger) SoftDeleteCredit(ctx context.Context, id uuid.UUID) error {
	return l.repo.SoftDeleteCredit(ctx, id)
}

// RestoreCredit restores a soft-deleted credit.
func (l *Ledger) RestoreCredit(ctx context.Context, id uuid.UUID) error {
	return l.repo.RestoreCredit(ctx, id)
}

// GetDeletedCredits retrieves deleted credits by user ID.
func (l *Ledger) GetDeletedCredits(ctx context.Context, userID uint64) ([]Credit, error) {
	return l.repo.GetDeletedCredits(ctx, userID)
}

// PurgeDeletedCredits permanently removes aged deleted credits.
func (l *Ledger) PurgeDeletedCredits(ctx context.Context, olderThan time.Duration) (int, error) {
	return l.repo.PurgeDeletedCredits(ctx, olderThan)
}

// GetIdempotencyKey returns the idempotency key from metadata if present.
func (l *Ledger) GetIdempotencyKey(ctx context.Context, referenceID string) (string, error) {
	credits, err := l.repo.GetCreditsByReference(ctx, referenceID, "")
	if err != nil {
		return "", err
	}

	for _, credit := range credits {
		if key, ok := credit.Metadata["idempotency_key"].(string); ok {
			return key, nil
		}
	}

	return "", nil
}

// IssueIdempotentCredit safely issues credit, ignoring duplicates.
func (l *Ledger) IssueIdempotentCredit(
	ctx context.Context,
	userID uint64,
	amount decimal.Decimal,
	creditType string,
	referenceID string,
	referenceType string,
	idempotencyKey string,
	metadata CreditMetadata,
) error {
	existing, err := l.GetIdempotencyKey(ctx, referenceID)
	if err != nil {
		return err
	}

	if existing == idempotencyKey {
		return nil
	}

	if metadata.Raw == nil {
		metadata.Raw = make(map[string]interface{})
	}
	metadata.Raw["idempotency_key"] = idempotencyKey

	return l.IssueCredit(ctx, userID, amount, creditType, CreditDirection,
		referenceID, referenceType, metadata)
}
