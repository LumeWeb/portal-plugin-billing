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
// creditType: Type of credit (charge, refund, etc.)
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
	creditType string,
	amount decimal.Decimal,
	referenceType string,
	referenceID string,
	description string,
	createdBy uint64,
) error {
	s.Logger().Debug("IssueCreditFromGateway",
		zap.Uint64("user_id", userID),
		zap.String("credit_type", creditType),
		zap.String("amount", amount.String()),
		zap.String("reference_type", referenceType),
		zap.String("reference_id", referenceID),
	)

	// Validate credit type
	if !s.isValidCreditType(creditType) {
		s.Logger().Error("Invalid credit type",
			zap.String("credit_type", creditType),
			zap.Uint64("user_id", userID))
		return fmt.Errorf("invalid credit type: %s", creditType)
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
	if creditType == pluginCore.CreditTypeRefund || creditType == pluginCore.CreditTypeUsage || creditType == pluginCore.CreditTypeChargeBack {
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
		creditType,
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
		zap.String("credit_type", creditType),
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
	creditType string,
	amount decimal.Decimal,
	referenceType string,
	referenceID string,
	description string,
	createdBy uint64,
) error {
	s.Logger().Debug("IssueCreditWithIdempotency",
		zap.Uint64("user_id", userID),
		zap.String("credit_type", creditType),
		zap.String("reference_type", referenceType),
		zap.String("reference_id", referenceID),
	)

	// Validate credit type
	if !s.isValidCreditType(creditType) {
		s.Logger().Error("Invalid credit type for idempotent credit",
			zap.String("credit_type", creditType),
			zap.Uint64("user_id", userID))
		return fmt.Errorf("invalid credit type: %s", creditType)
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
		creditType,
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
	creditType string,
	amount decimal.Decimal,
	referenceID string,
	description string,
	createdBy uint64,
) error {
	s.Logger().Debug("IssueUsageCredit",
		zap.Uint64("user_id", userID),
		zap.String("credit_type", creditType),
		zap.String("amount", amount.String()),
	)

	// Validate credit type for usage
	validUsageTypes := map[string]bool{
		pluginCore.CreditTypeUsage: true,
		pluginCore.CreditTypeTime:  true,
		pluginCore.CreditTypePromo: true,
		pluginCore.CreditTypeComp:  true,
	}

	if !validUsageTypes[creditType] {
		s.Logger().Error("Invalid usage credit type",
			zap.String("credit_type", creditType),
			zap.Uint64("user_id", userID))
		return fmt.Errorf("invalid usage credit type: %s", creditType)
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
		creditType,
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

// isValidCreditType checks if a credit type is valid
func (s *CreditServiceDefault) isValidCreditType(creditType string) bool {
	validTypes := map[string]bool{
		pluginCore.CreditTypeCharge:     true,
		pluginCore.CreditTypeRefund:     true,
		pluginCore.CreditTypeUsage:      true,
		pluginCore.CreditTypeManual:     true,
		pluginCore.CreditTypePromo:      true,
		pluginCore.CreditTypeTime:       true,
		pluginCore.CreditTypeChargeBack: true,
		pluginCore.CreditTypeComp:       true,
	}
	return validTypes[creditType]
}

// isValidReferenceType checks if a reference type is valid
func (s *CreditServiceDefault) isValidReferenceType(referenceType string) bool {
	validTypes := map[string]bool{
		pluginCore.ReferenceTypeStripeInvoice: true,
		pluginCore.ReferenceTypeAtlosPayment:  true,
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
