package credit

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	ledger "go.lumeweb.com/portal-plugin-billing/pkg/ledger"
	"go.lumeweb.com/portal-plugin-billing/internal/db/models"
	"go.lumeweb.com/queryutil"
	"gorm.io/gorm"
)

// TransactionContext implements TransactionContext using GORM transactions.
type TransactionContext struct {
	db *gorm.DB
}

// transactionKey is the context key for storing GORM transactions.
type transactionKey struct{}

// NewTransactionContext creates a new transaction context.
func NewTransactionContext(db *gorm.DB) *TransactionContext {
	return &TransactionContext{db: db}
}

// Begin begins a new transaction and returns a context with transaction.
func (t *TransactionContext) Begin(ctx context.Context) (context.Context, error) {
	tx := t.db.Begin()
	if tx.Error != nil {
		return ctx, &transactionError{op: "begin", err: tx.Error}
	}
	return context.WithValue(ctx, transactionKey{}, tx), nil
}

// Commit commits the current transaction.
func (t *TransactionContext) Commit(ctx context.Context) error {
	tx, ok := ctx.Value(transactionKey{}).(*gorm.DB)
	if !ok {
		return errors.New("no active transaction in context")
	}
	if err := tx.Commit().Error; err != nil {
		return &transactionError{op: "commit", err: err}
	}
	return nil
}

// Rollback aborts the current transaction.
func (t *TransactionContext) Rollback(ctx context.Context) error {
	tx, ok := ctx.Value(transactionKey{}).(*gorm.DB)
	if !ok {
		return errors.New("no active transaction in context")
	}
	if err := tx.Rollback().Error; err != nil {
		return &transactionError{op: "rollback", err: err}
	}
	return nil
}

// GetDB returns the GORM DB instance from context (with transaction if active).
func GetDB(ctx context.Context, defaultDB *gorm.DB) *gorm.DB {
	tx, ok := ctx.Value(transactionKey{}).(*gorm.DB)
	if !ok {
		return defaultDB.WithContext(ctx)
	}
	return tx.WithContext(ctx)
}

// transactionalCreditRepository wraps repository with transaction-aware DB.
type transactionalCreditRepository struct {
	repo *CreditRepository
}

// NewTransactionalCreditRepository creates repository with transaction support.
func NewTransactionalCreditRepository(db *gorm.DB) ledger.CreditRepository {
	return &transactionalCreditRepository{
		repo: &CreditRepository{db: db},
	}
}

// getDB returns the appropriate DB instance (transactional or default) based on context.
func (r *transactionalCreditRepository) getDB(ctx context.Context) *gorm.DB {
	return GetDB(ctx, r.repo.db)
}

// CreateCredit inserts a new credit entry with transaction support.
func (r *transactionalCreditRepository) CreateCredit(ctx context.Context, credit *ledger.Credit) error {
	if credit == nil {
		return errors.New("credit cannot be nil")
	}

	db := r.getDB(ctx)
	if credit == nil {
		return errors.New("credit cannot be nil")
	}

	model := &models.CreditModel{
		ID:           credit.ID,
		UserID:       credit.UserID,
		Amount:       credit.Amount,
		Type:         credit.Type,
		Direction:    credit.Direction,
		ReferenceID:  credit.ReferenceID,
		ReferenceType: credit.ReferenceType,
		Description:  credit.Description,
		CreatedBy:    credit.CreatedBy,
	}

	if err := db.WithContext(ctx).Create(model).Error; err != nil {
		return &gormRepositoryError{op: "create", err: err}
	}

	return nil
}

// GetCredit retrieves a credit by ID.
func (r *transactionalCreditRepository) GetCredit(ctx context.Context, id uuid.UUID) (*ledger.Credit, error) {
	db := r.getDB(ctx)
	var view models.CreditActiveView
	if err := db.WithContext(ctx).Where("id = ?", id).First(&view).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, &gormRepositoryError{op: "get", err: err}
	}

	return r.repo.ViewToCredit(&view), nil
}

// GetUserBalance returns the current balance for a user.
func (r *transactionalCreditRepository) GetUserBalance(ctx context.Context, userID uint64) (decimal.Decimal, error) {
	db := r.getDB(ctx)
	var view models.CreditsBalanceView
	if err := db.WithContext(ctx).Where("user_id = ?", userID).First(&view).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return decimal.Zero, nil
		}
		return decimal.Zero, &gormRepositoryError{op: "balance", err: err}
	}

	return view.Balance, nil
}

// SoftDeleteCredit marks a credit as deleted with timestamp.
func (r *transactionalCreditRepository) SoftDeleteCredit(ctx context.Context, id uuid.UUID) error {
	db := r.getDB(ctx)
	now := time.Now()
	result := db.WithContext(ctx).
		Unscoped().
		Model(&models.CreditModel{}).
		Where("id = ?", id).
		Update("deleted_at", &now)

	if result.Error != nil {
		return &gormRepositoryError{op: "soft_delete", err: result.Error}
	}

	if result.RowsAffected == 0 {
		return errors.New("credit not found")
	}

	return nil
}

// RestoreCredit restores a soft-deleted credit.
func (r *transactionalCreditRepository) RestoreCredit(ctx context.Context, id uuid.UUID) error {
	db := r.getDB(ctx)
	result := db.WithContext(ctx).
		Unscoped().
		Model(&models.CreditModel{}).
		Where("id = ?", id).
		Update("deleted_at", nil)

	if result.Error != nil {
		return &gormRepositoryError{op: "restore", err: result.Error}
	}

	if result.RowsAffected == 0 {
		return errors.New("credit not found")
	}

	return nil
}

// GetDeletedCredits retrieves deleted credits by user ID.
func (r *transactionalCreditRepository) GetDeletedCredits(ctx context.Context, userID uint64) ([]ledger.Credit, error) {
	db := r.getDB(ctx)
	var creditModels []models.CreditModel
	if err := db.WithContext(ctx).
		Unscoped().
		Where("user_id = ?", userID).
		Find(&creditModels).Error; err != nil {
		return nil, &gormRepositoryError{op: "list_deleted", err: err}
	}

	// Filter to only deleted credits (deleted_at is not nil)
	var deletedModels []models.CreditModel
	for _, model := range creditModels {
		if model.DeletedAt != nil {
			deletedModels = append(deletedModels, model)
		}
	}

	credits := make([]ledger.Credit, len(deletedModels))
	for i, model := range deletedModels {
		credits[i] = *r.repo.ModelToCredit(&model)
	}

	return credits, nil
}

// PurgeDeletedCredits permanently removes credits older than duration.
func (r *transactionalCreditRepository) PurgeDeletedCredits(ctx context.Context, olderThan time.Duration) (int, error) {
	db := r.getDB(ctx)
	cutoff := time.Now().Add(-olderThan)

	result := db.WithContext(ctx).
		Where("deleted_at IS NOT NULL AND deleted_at < ?", cutoff).
		Delete(&models.CreditModel{})

	if result.Error != nil {
		return 0, &gormRepositoryError{op: "purge", err: result.Error}
	}

	return int(result.RowsAffected), nil
}

// GetCreditsByReference finds credits by reference ID and type.
func (r *transactionalCreditRepository) GetCreditsByReference(ctx context.Context, referenceID string, referenceType string) ([]ledger.Credit, error) {
	db := r.getDB(ctx)
	var views []models.CreditActiveView
	if err := db.WithContext(ctx).
		Where("reference_id = ? AND reference_type = ?", referenceID, referenceType).
		Find(&views).Error; err != nil {
		return nil, &gormRepositoryError{op: "list_by_reference", err: err}
	}

	credits := make([]ledger.Credit, len(views))
	for i, view := range views {
		credits[i] = *r.repo.ViewToCredit(&view)
	}

	return credits, nil
}

// ListCredits retrieves credits with filtering, sorting, and pagination.
func (r *transactionalCreditRepository) ListCredits(ctx context.Context, filters []queryutil.CrudFilter, sorts []queryutil.Sort, pagination queryutil.Pagination) ([]ledger.Credit, int64, error) {
	return r.repo.ListCredits(ctx, filters, sorts, pagination)
}

// transactionError wraps transaction errors with operation context.
type transactionError struct {
	op  string
	err error
}

func (e *transactionError) Error() string {
	return "transaction " + e.op + " error: " + e.err.Error()
}

func (e *transactionError) Unwrap() error {
	return e.err
}

// anyToDuration converts time.Duration or time.Time to Duration.
func anyToDuration(v any) any {
	return v
}
