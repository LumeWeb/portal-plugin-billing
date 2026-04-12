package credit

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	"go.lumeweb.com/portal-plugin-billing/pkg/ledger"
	"go.lumeweb.com/portal/core"
	"go.lumeweb.com/queryutil"
	"go.uber.org/zap"
)

type CreditServiceDefault struct {
	*core.BaseComponent
	ledger    *ledger.Ledger
	repo      CreditRepositoryWithQuery
}

func (s *CreditServiceDefault) ID() string {
	return pluginCore.CREDIT_SERVICE
}

var _ pluginCore.CreditService = (*CreditServiceDefault)(nil)

// NewCreditService creates a new credit service
func NewCreditService() (core.Service, []core.ContextBuilderOption, error) {
	service := &CreditServiceDefault{}

	return service, core.ContextOptions(
		core.ContextWithStartupFunc(func(ctx core.Context) error {
			// Initialize ledger with repository
			db := ctx.DB()
			if db == nil {
				return fmt.Errorf("database service is required but not available")
			}

			creditRepo := NewCreditRepository(db)
			service.repo = creditRepo
			service.ledger = ledger.NewLedger(creditRepo, ledger.GetGlobalRegistry())
			
			return nil
		}),
	), nil
}

// IssueCreditFromGateway creates a credit entry in the ledger from a gateway event.
// It maps gateway-specific referenceTypes and metadata to ledger entries.
//
// ctx: Context for the operation
// userID: User ID to issue credit to
// transactionType: Type of transaction (charge, refund, etc.)
// amount: Credit amount (always positive credit, negative debit)
// referenceType: Gateway event source type (e.g., pluginCore.ReferenceTypeStripeInvoice, pluginCore.ReferenceTypeAtlosPayment)
// referenceID: External transaction ID (e.g., Stripe invoice ID, Atlos transaction ID)
// description: Human-readable description of the credit
// createdBy: User ID of the admin/system creating this credit (0 for system)
//
// Returns error if credit issuance fails
func (s *CreditServiceDefault) IssueCreditFromGateway(
	ctx context.Context,
	userID uint64,
	transactionType string,
	amount decimal.Decimal,
	referenceType string,
	referenceID string,
	description string,
	createdBy uint64,
) error {
	s.Logger().Debug("IssueCreditFromGateway",
		zap.Uint64("user_id", userID),
		zap.String("transaction_type", transactionType),
		zap.String("amount", amount.String()),
		zap.String("reference_type", referenceType),
		zap.String("reference_id", referenceID),
	)

	// Validate credit type
	if !s.isValidTransactionType(transactionType) {
		s.Logger().Error("Invalid credit type",
			zap.String("transaction_type", transactionType),
			zap.Uint64("user_id", userID))
		return fmt.Errorf("invalid credit type: %s", transactionType)
	}

	// Validate reference type
	if !s.isValidReferenceType(referenceType) {
		s.Logger().Error("Invalid reference type",
			zap.String("reference_type", referenceType),
			zap.Uint64("user_id", userID))
		return fmt.Errorf("invalid reference type: %s", referenceType)
	}

	// Determine direction: most gateway events are credits (positive)
	// Debits are typically usage-based or refunds
	direction := ledger.CreditDirection
	if transactionType == pluginCore.TransactionTypeRefund || transactionType == pluginCore.TransactionTypeUsage || transactionType == pluginCore.TransactionTypeChargeBack {
		direction = ledger.DebitDirection
	}

	// Build metadata
	rawMetadata := map[string]interface{}{
		"source": referenceType,
	}

	metadata := ledger.CreditMetadata{
		Description: description,
		CreatedBy:  createdBy,
		Raw:        rawMetadata,
	}

	// Issue the credit
	err := s.ledger.IssueCredit(
		ctx,
		userID,
		amount,
		transactionType,
		direction,
		referenceID,
		referenceType,
		metadata,
	)

	if err != nil {
		s.Logger().Error("Failed to issue credit",
			zap.Error(err),
			zap.Uint64("user_id", userID))
		return fmt.Errorf("failed to issue credit: %w", err)
	}

	s.Logger().Info("Credit issued successfully",
		zap.Uint64("user_id", userID),
		zap.String("transaction_type", transactionType),
		zap.String("amount", amount.String()))

	return nil
}

// IssueCreditWithIdempotency creates a credit entry with idempotency protection.
// Uses the referenceType and referenceID to construct a unique idempotency key.
// This prevents duplicate credits from re-delivered webhook events.
//
// Returns error if credit issuance fails
func (s *CreditServiceDefault) IssueCreditWithIdempotency(
	ctx context.Context,
	userID uint64,
	transactionType string,
	amount decimal.Decimal,
	referenceType string,
	referenceID string,
	description string,
	createdBy uint64,
) error {
	s.Logger().Debug("IssueCreditWithIdempotency",
		zap.Uint64("user_id", userID),
		zap.String("transaction_type", transactionType),
		zap.String("reference_type", referenceType),
		zap.String("reference_id", referenceID),
	)

	// Validate credit type
	if !s.isValidTransactionType(transactionType) {
		s.Logger().Error("Invalid credit type for idempotent credit",
			zap.String("transaction_type", transactionType),
			zap.Uint64("user_id", userID))
		return fmt.Errorf("invalid credit type: %s", transactionType)
	}

	// Validate reference type
	if !s.isValidReferenceType(referenceType) {
		s.Logger().Error("Invalid reference type for idempotent credit",
			zap.String("reference_type", referenceType),
			zap.Uint64("user_id", userID))
		return fmt.Errorf("invalid reference type: %s", referenceType)
	}

	// Build idempotency key from reference info
	idempotencyKey := s.buildIdempotencyKey(referenceType, referenceID)

	// Build metadata
	rawMetadata := map[string]interface{}{
		"source": referenceType,
	}

	metadata := ledger.CreditMetadata{
		Description: description,
		CreatedBy:  createdBy,
		Raw:        rawMetadata,
	}

	// Issue idempotent credit
	err := s.ledger.IssueIdempotentCredit(
		ctx,
		userID,
		amount,
		transactionType,
		referenceID,
		referenceType,
		idempotencyKey,
		metadata,
	)

	if err != nil {
		s.Logger().Error("Failed to issue idempotent credit",
			zap.Error(err),
			zap.Uint64("user_id", userID))
		return fmt.Errorf("failed to issue idempotent credit: %w", err)
	}

	s.Logger().Info("Idempotent credit issued successfully",
		zap.Uint64("user_id", userID),
		zap.String("reference_id", referenceID))

	return nil
}

// IssueUsageCredit creates a usage-based debit for consumption of resources.
// For example: deducting time credits when service is used.
//
// Returns error if credit issuance fails
func (s *CreditServiceDefault) IssueUsageCredit(
	ctx context.Context,
	userID uint64,
	transactionType string,
	amount decimal.Decimal,
	referenceID string,
	description string,
	createdBy uint64,
) error {
	s.Logger().Debug("IssueUsageCredit",
		zap.Uint64("user_id", userID),
		zap.String("transaction_type", transactionType),
		zap.String("amount", amount.String()),
	)

	// Validate credit type for usage
	validUsageTypes := map[string]bool{
		pluginCore.TransactionTypeUsage: true,
		pluginCore.TransactionTypeTime:  true,
		pluginCore.TransactionTypePromo: true,
		pluginCore.TransactionTypeComp:  true,
	}

	if !validUsageTypes[transactionType] {
		s.Logger().Error("Invalid usage credit type",
			zap.String("transaction_type", transactionType),
			zap.Uint64("user_id", userID))
		return fmt.Errorf("invalid usage credit type: %s", transactionType)
	}

	rawMetadata := map[string]interface{}{
		"source": "usage",
	}

	metadata := ledger.CreditMetadata{
		Description: description,
		CreatedBy:  createdBy,
		Raw:        rawMetadata,
	}

	err := s.ledger.DebitCredit(
		ctx,
		userID,
		amount,
		transactionType,
		referenceID,
		"usage",
		metadata,
	)

	if err != nil {
		s.Logger().Error("Failed to issue usage credit",
			zap.Error(err),
			zap.Uint64("user_id", userID))
		return fmt.Errorf("failed to issue usage credit: %w", err)
	}

	s.Logger().Info("Usage credit issued successfully",
		zap.Uint64("user_id", userID),
		zap.String("amount", amount.String()))

	return nil
}

// GetUserBalance retrieves the current balance for a user.
// Positive balance = user has credits
// Negative balance = user owes money (e.g., refunds exceed charges)
func (s *CreditServiceDefault) GetUserBalance(ctx context.Context, userID uint64) (decimal.Decimal, error) {
	s.Logger().Debug("GetUserBalance", zap.Uint64("user_id", userID))

	balance, err := s.ledger.GetUserBalance(ctx, userID)
	if err != nil {
		s.Logger().Error("Failed to get user balance",
			zap.Error(err),
			zap.Uint64("user_id", userID))
		return decimal.Zero, err
	}

	s.Logger().Debug("User balance retrieved",
		zap.Uint64("user_id", userID),
		zap.String("balance", balance.String()))
	return balance, nil
}

// ValidateSubscriptionChange validates that a subscription change is acceptable
// based on the user's current ledger balance and credit history.
//
// Parameters:
//   - ctx: Context for the operation
//   - userID: User ID to validate
//   - changeType: Type of subscription change (NewSubscription, Upgrade, Downgrade, Cancel, Renewal)
//   - expectedAmount: Expected credit/debit amount
//
// Returns:
//   - error if validation fails, nil if validation passes
func (s *CreditServiceDefault) ValidateSubscriptionChange(
	ctx context.Context,
	userID uint64,
	changeType pluginCore.SubscriptionChangeType,
	expectedAmount decimal.Decimal,
) error {
	s.Logger().Debug("ValidateSubscriptionChange",
		zap.Uint64("user_id", userID),
		zap.String("change_type", string(changeType)),
		zap.String("expected_amount", expectedAmount.String()),
	)

	// Get current balance
	balance, err := s.GetUserBalance(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to get balance: %w", err)
	}

	// Validation rules based on change type
	switch changeType {
	case pluginCore.ChangeTypeNewSubscription:
		// For new subscriptions, allow even if balance is negative
		// (they're paying now)
		return nil

	case pluginCore.ChangeTypeUpgrade:
		// For upgrades, ensure user has sufficient balance after change
		projectedBalance := balance.Add(expectedAmount)
		if projectedBalance.LessThan(decimal.Zero) {
			return fmt.Errorf("insufficient balance for upgrade: current=%s, change=%s, projected=%s",
				balance.String(), expectedAmount.String(), projectedBalance.String())
		}
		return nil

	case pluginCore.ChangeTypeDowngrade:
		// For downgrades, credit is issued so balance increases
		// No validation needed
		return nil

	case pluginCore.ChangeTypeCancel:
		// For cancellations, credit is issued so balance increases
		// No validation needed
		return nil

	case pluginCore.ChangeTypeRenewal:
		// For renewals, allow even if balance is negative
		// (they're paying again)
		return nil

	default:
		return fmt.Errorf("unsupported change type: %s", changeType)
	}
}

// GetReferenceIdempotencyKey retrieves the idempotency key for a reference ID.
// Useful for checking if credit was previously issued.
func (s *CreditServiceDefault) GetReferenceIdempotencyKey(
	ctx context.Context,
	referenceID string,
) (string, error) {
	s.Logger().Debug("GetReferenceIdempotencyKey",
		zap.String("reference_id", referenceID))

	idempotencyKey, err := s.ledger.GetIdempotencyKey(ctx, referenceID)
	if err != nil {
		s.Logger().Error("Failed to get reference idempotency key",
			zap.Error(err),
			zap.String("reference_id", referenceID))
		return "", err
	}

	s.Logger().Debug("Reference idempotency key retrieved",
		zap.String("reference_id", referenceID),
		zap.Bool("found", idempotencyKey != ""))
	return idempotencyKey, nil
}

// SoftDeleteCredit marks a credit as deleted (soft delete).
// Useful for correcting errors or reversing transactions.
func (s *CreditServiceDefault) SoftDeleteCredit(ctx context.Context, creditID uuid.UUID) error {
	s.Logger().Debug("SoftDeleteCredit", zap.String("credit_id", creditID.String()))

	err := s.ledger.SoftDeleteCredit(ctx, creditID)
	if err != nil {
		s.Logger().Error("Failed to soft delete credit",
			zap.Error(err),
			zap.String("credit_id", creditID.String()))
		return err
	}

	s.Logger().Info("Credit soft deleted successfully",
		zap.String("credit_id", creditID.String()))
	return nil
}

// RestoreCredit restores a previously soft-deleted credit.
func (s *CreditServiceDefault) RestoreCredit(ctx context.Context, creditID uuid.UUID) error {
	s.Logger().Debug("RestoreCredit", zap.String("credit_id", creditID.String()))

	err := s.ledger.RestoreCredit(ctx, creditID)
	if err != nil {
		s.Logger().Error("Failed to restore credit",
			zap.Error(err),
			zap.String("credit_id", creditID.String()))
		return err
	}

	s.Logger().Info("Credit restored successfully",
		zap.String("credit_id", creditID.String()))
	return nil
}

// CreateCredit creates a new credit delegate method
func (s *CreditServiceDefault) CreateCredit(ctx context.Context, credit *ledger.Credit) error {
	return s.repo.CreateCredit(ctx, credit)
}

// GetCredit retrieves a credit by ID
func (s *CreditServiceDefault) GetCredit(ctx context.Context, id uuid.UUID) (*ledger.Credit, error) {
	return s.repo.GetCredit(ctx, id)
}

// GetCreditsByReference finds credits by reference ID and type
func (s *CreditServiceDefault) GetCreditsByReference(ctx context.Context, referenceID string, referenceType string) ([]ledger.Credit, error) {
	return s.repo.GetCreditsByReference(ctx, referenceID, referenceType)
}

// ListCredits retrieves credits with filtering, sorting, and pagination
func (s *CreditServiceDefault) ListCredits(ctx context.Context, filters []queryutil.CrudFilter, sorts []queryutil.Sort, pagination queryutil.Pagination) ([]ledger.Credit, int64, error) {
	s.Logger().Debug("ListCredits",
		zap.Int("filter_count", len(filters)),
		zap.Int("sort_count", len(sorts)),
	)

	credits, total, err := s.repo.ListCredits(ctx, filters, sorts, pagination)
	if err != nil {
		s.Logger().Error("Failed to list credits",
			zap.Error(err))
		return nil, 0, err
	}

	s.Logger().Debug("Credits retrieved successfully",
		zap.Int("count", len(credits)),
		zap.Int64("total", total))

	return credits, total, nil
}

// GetDeletedCredits retrieves soft-deleted credits for a user.
// Useful for audit and recovery operations.
func (s *CreditServiceDefault) GetDeletedCredits(ctx context.Context, userID uint64) ([]ledger.Credit, error) {
	s.Logger().Debug("GetDeletedCredits", zap.Uint64("user_id", userID))

	credits, err := s.ledger.GetDeletedCredits(ctx, userID)
	if err != nil {
		s.Logger().Error("Failed to get deleted credits",
			zap.Error(err),
			zap.Uint64("user_id", userID))
		return nil, err
	}

	s.Logger().Debug("Deleted credits retrieved",
		zap.Uint64("user_id", userID),
		zap.Int("count", len(credits)))
	return credits, nil
}

// PurgeDeletedCredits permanently removes soft-deleted credits older than a duration.
// Useful for cleanup and retention policies.
func (s *CreditServiceDefault) PurgeDeletedCredits(ctx context.Context, olderThan time.Duration) (int, error) {
	s.Logger().Debug("PurgeDeletedCredits", zap.Duration("older_than", olderThan))

	count, err := s.ledger.PurgeDeletedCredits(ctx, olderThan)
	if err != nil {
		s.Logger().Error("Failed to purge deleted credits",
			zap.Error(err))
		return 0, err
	}

	s.Logger().Info("Deleted credits purged successfully",
		zap.Int("count", count),
		zap.Duration("duration", olderThan))
	return count, nil
}

// isValidTransactionType checks if a transaction type is valid
func (s *CreditServiceDefault) isValidTransactionType(transactionType string) bool {
	validTypes := map[string]bool{
		pluginCore.TransactionTypeCharge:     true,
		pluginCore.TransactionTypeRefund:     true,
		pluginCore.TransactionTypeUsage:      true,
		pluginCore.TransactionTypeManual:     true,
		pluginCore.TransactionTypePromo:      true,
		pluginCore.TransactionTypeTime:       true,
		pluginCore.TransactionTypeChargeBack: true,
		pluginCore.TransactionTypeComp:       true,
	}
	return validTypes[transactionType]
}

// isValidReferenceType checks if a reference type is valid
func (s *CreditServiceDefault) isValidReferenceType(referenceType string) bool {
	validTypes := map[string]bool{
		// Gateway-specific reference types
		pluginCore.ReferenceTypeStripeInvoice: true,
		pluginCore.ReferenceTypeAtlosPayment:  true,
		// System-generated reference types
		pluginCore.ReferenceTypeManual: true,
		pluginCore.ReferenceTypeUsage:  true,
	}
	return validTypes[referenceType]
}

// buildIdempotencyKey creates a unique key from reference info
func (s *CreditServiceDefault) buildIdempotencyKey(referenceType string, referenceID string) string {
	return fmt.Sprintf("%s:%s", referenceType, referenceID)
}

// parseDuration parses a duration string (e.g., "24h", "7d", "30d")
func parseDuration(durationStr string) (time.Duration, error) {
	// Parse duration string and convert to time.Duration
	// Supported formats:
	// - "Xh" (hours): "24h", "48h"
	// - "Xd" (days): "7d", "30d"
	// - "Xw" (weeks): "2w", "4w"
	// - "Xm" (months): "3m", "6m"

	if durationStr == "" {
		durationStr = pluginCore.DefaultSoftDeleteRetention
	}

	return time.ParseDuration(durationStr)
}
