package credit

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	ledger "go.lumeweb.com/portal-plugin-billing/pkg/ledger"
	"go.lumeweb.com/portal-plugin-billing/internal/db/models"
	"go.lumeweb.com/queryutil"
)

// CreditRepository implements CreditRepository using GORM ORM.
type CreditRepository struct {
	db *gorm.DB
}

// NewCreditRepository creates a new GORM repository.
func NewCreditRepository(db *gorm.DB) *CreditRepository {
	return &CreditRepository{db: db}
}

// CreateCredit inserts a new credit entry.
func (r *CreditRepository) CreateCredit(ctx context.Context, credit *ledger.Credit) error {
	if credit == nil {
		return errors.New("credit cannot be nil")
	}

	metadataJSON, err := json.Marshal(credit.Metadata)
	if err != nil {
		return &gormRepositoryError{op: "create", err: err}
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
		Metadata:     datatypes.JSON(metadataJSON),
	}

	if err := r.db.WithContext(ctx).Create(model).Error; err != nil {
		return &gormRepositoryError{op: "create", err: err}
	}

	return nil
}

// GetCredit retrieves a credit by ID.
func (r *CreditRepository) GetCredit(ctx context.Context, id uuid.UUID) (*ledger.Credit, error) {
	db := r.db
	var view models.CreditActiveView
	if err := db.WithContext(ctx).Where("id = ?", id).First(&view).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, &gormRepositoryError{op: "get", err: err}
	}

	return r.ViewToCredit(&view), nil
}

// GetUserBalance returns the current balance for a user.
func (r *CreditRepository) GetUserBalance(ctx context.Context, userID uint64) (decimal.Decimal, error) {
	var view models.CreditsBalanceView
	if err := r.db.WithContext(ctx).Where("user_id = ?", userID).First(&view).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return decimal.Zero, nil
		}
		return decimal.Zero, &gormRepositoryError{op: "balance", err: err}
	}

	return view.Balance, nil
}

// SoftDeleteCredit marks a credit as deleted with timestamp.
func (r *CreditRepository) SoftDeleteCredit(ctx context.Context, id uuid.UUID) error {
	now := time.Now()
	result := r.db.WithContext(ctx).
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
func (r *CreditRepository) RestoreCredit(ctx context.Context, id uuid.UUID) error {
	result := r.db.WithContext(ctx).
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
func (r *CreditRepository) GetDeletedCredits(ctx context.Context, userID uint64) ([]ledger.Credit, error) {
	var creditModels []models.CreditModel
	if err := r.db.WithContext(ctx).
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
		credits[i] = *r.ModelToCredit(&model)
	}

	return credits, nil
}

// PurgeDeletedCredits permanently removes credits older than duration.
func (r *CreditRepository) PurgeDeletedCredits(ctx context.Context, olderThan time.Duration) (int, error) {
	cutoff := time.Now().Add(-olderThan)

	result := r.db.WithContext(ctx).
		Where("deleted_at IS NOT NULL AND deleted_at < ?", cutoff).
		Delete(&models.CreditModel{})

	if result.Error != nil {
		return 0, &gormRepositoryError{op: "purge", err: result.Error}
	}

	return int(result.RowsAffected), nil
}

// GetCreditsByReference finds credits by reference ID and type.
func (r *CreditRepository) GetCreditsByReference(ctx context.Context, referenceID string, referenceType string) ([]ledger.Credit, error) {
	var views []models.CreditActiveView
	query := r.db.WithContext(ctx).Where("reference_id = ?", referenceID)
	
	// Only filter by reference_type if it's provided
	if referenceType != "" {
		query = query.Where("reference_type = ?", referenceType)
	}
	
	if err := query.Find(&views).Error; err != nil {
		return nil, &gormRepositoryError{op: "list_by_reference", err: err}
	}

	credits := make([]ledger.Credit, len(views))
	for i, view := range views {
		credits[i] = *r.ViewToCredit(&view)
	}

	return credits, nil
}

// ListCredits retrieves credits with filtering, sorting, and pagination.
func (r *CreditRepository) ListCredits(ctx context.Context, filters []queryutil.CrudFilter, sorts []queryutil.Sort, pagination queryutil.Pagination) ([]ledger.Credit, int64, error) {
	var views []models.CreditActiveView
	var total int64

	q := r.db.WithContext(ctx).Model(&models.CreditActiveView{})

	// Apply filters
	q = queryutil.ApplyFilters(q, filters, nil)

	// Apply sorting
	q = queryutil.ApplySort(q, sorts)

	// Count total
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, &gormRepositoryError{op: "count", err: err}
	}

	// Apply pagination
	q = queryutil.ApplyPagination(q, pagination)

	// Find credits
	if err := q.Find(&views).Error; err != nil {
		return nil, 0, &gormRepositoryError{op: "list", err: err}
	}

	// Convert views to credits
	credits := make([]ledger.Credit, len(views))
	for i, view := range views {
		credits[i] = *r.ViewToCredit(&view)
	}

	return credits, total, nil
}

// modelToCredit converts CreditModel to domain Credit.
func (r *CreditRepository) ModelToCredit(model *models.CreditModel) *ledger.Credit {
	deletedAt := time.Time{}
	if model.DeletedAt != nil {
		deletedAt = *model.DeletedAt
	}

	metadata := make(map[string]interface{})
	if len(model.Metadata) > 0 {
		if err := json.Unmarshal(model.Metadata, &metadata); err != nil {
			metadata = make(map[string]interface{})
		}
	}
	
	return &ledger.Credit{
		ID:           model.ID,
		UserID:       model.UserID,
		Amount:       model.Amount,
		Type:         model.Type,
		Direction:    model.Direction,
		ReferenceID:  model.ReferenceID,
		ReferenceType: model.ReferenceType,
		Description:  model.Description,
		CreatedBy:    model.CreatedBy,
		CreatedAt:    model.CreatedAt,
		UpdatedAt:    model.UpdatedAt,
		DeletedAt:    deletedAt,
		Metadata:     metadata,
	}
}

// viewToCredit converts CreditActiveView to domain Credit.
func (r *CreditRepository) ViewToCredit(view *models.CreditActiveView) *ledger.Credit {
	metadata := make(map[string]interface{})
	if len(view.Metadata) > 0 {
		if err := json.Unmarshal(view.Metadata, &metadata); err != nil {
			metadata = make(map[string]interface{})
		}
	}

	return &ledger.Credit{
		ID:           view.ID,
		UserID:       view.UserID,
		Amount:       view.Amount,
		Type:         view.Type,
		Direction:    view.Direction,
		ReferenceID:  view.ReferenceID,
		ReferenceType: view.ReferenceType,
		Description:  view.Description,
		CreatedBy:    view.CreatedBy,
		CreatedAt:    view.CreatedAt,
		UpdatedAt:    view.UpdatedAt,
		Metadata:     metadata,
	}
}

// gormRepositoryError wraps GORM errors with operation context.
type gormRepositoryError struct {
	op  string
	err error
}

func (e *gormRepositoryError) Error() string {
	return "repository " + e.op + " error: " + e.err.Error()
}

func (e *gormRepositoryError) Unwrap() error {
	return e.err
}
