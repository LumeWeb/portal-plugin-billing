package credit

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	ledger "go.lumeweb.com/portal-plugin-billing/pkg/ledger"
	"go.lumeweb.com/portal-plugin-billing/internal"
	"go.lumeweb.com/portal-plugin-billing/internal/db/migrations"
	"go.lumeweb.com/portal-plugin-billing/internal/db/models"
	"go.lumeweb.com/portal/core"
	coreTesting "go.lumeweb.com/portal/core/testing"
)

// ============================================================
// Test Configuration and Setup Helpers
// ============================================================

// getCreditTestOptions provides the standard test configuration for credit tests
func getCreditTestOptions() coreTesting.TestContextBuilderOption {
	return coreTesting.CombineOptions(
		coreTesting.NewMockPluginBuilder(internal.PLUGIN_NAME).
			WithMigrations(core.DBMigration{core.DB_TYPE_SQLITE: migrations.GetSQLite()}).
			WithService(pluginCore.CREDIT_SERVICE, NewCreditService).
			BuilderOption(),
	)
}

// getCreditRepository returns a CreditRepository instance from the test context
func getCreditRepository(ctx coreTesting.TestContext) *CreditRepository {
	db := ctx.DB()
	return NewCreditRepository(db)
}

// ============================================================
// Test Entity Creation Helpers
// ============================================================

// createTestCredit creates a test credit with reasonable defaults
func createTestCredit() *ledger.Credit {
	id := uuid.New()
	return &ledger.Credit{
		ID:           id,
		UserID:       123,
		Amount:       decimal.NewFromFloat(10.50),
		Type:         "payment",
		Direction:    "credit",
		ReferenceID:  "ref_test_123",
		ReferenceType: "stripe_payment",
		Description:  "Test credit",
		CreatedBy:    1,
	}
}

// createTestCreditWithOptions creates a test credit with custom options
func createTestCreditWithOptions(userID uint64, amount float64, direction string) *ledger.Credit {
	id := uuid.New()
	return &ledger.Credit{
		ID:           id,
		UserID:       userID,
		Amount:       decimal.NewFromFloat(amount),
		Type:         "payment",
		Direction:    direction,
		ReferenceID:  uuid.New().String(),
		ReferenceType: "test",
		Description:  "Test credit",
		CreatedBy:    1,
	}
}

// ============================================================
// CreditRepository Tests
// ============================================================

func TestCreditRepository_CreateCredit_Success(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		db := ctx.DB()
		repo := NewCreditRepository(db)
		credit := createTestCredit()

		err := repo.CreateCredit(ctx, credit)

		require.NoError(tb, err)

		// Verify credit was created
		fetched, err := repo.GetCredit(ctx, credit.ID)
		require.NoError(tb, err)
		require.NotNil(tb, fetched, "fetched credit should not be nil")
		assert.Equal(tb, credit.ID, fetched.ID)
		assert.Equal(tb, credit.UserID, fetched.UserID)
		assert.Equal(tb, credit.Amount, fetched.Amount)
		assert.Equal(tb, credit.Direction, fetched.Direction)
	}, getCreditTestOptions())
}

func TestCreditRepository_CreateCredit_NilCredit(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		db := ctx.DB()
		repo := NewCreditRepository(db)

		err := repo.CreateCredit(ctx, nil)

		assert.Error(tb, err)
		assert.Contains(tb, err.Error(), "cannot be nil")
	}, getCreditTestOptions())
}

func TestCreditRepository_CreateCredit_DuplicateID(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		db := ctx.DB()
		repo := NewCreditRepository(db)
		credit := createTestCredit()

		// Create credit once
		err := repo.CreateCredit(ctx, credit)
		require.NoError(tb, err)

		// Try to create with same ID
		err = repo.CreateCredit(ctx, credit)

		assert.Error(tb, err)
	}, getCreditTestOptions())
}

func TestCreditRepository_GetCredit_Success(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		db := ctx.DB()
		repo := NewCreditRepository(db)
		credit := createTestCredit()

		err := repo.CreateCredit(ctx, credit)
		require.NoError(tb, err)

		// Get the credit
		fetched, err := repo.GetCredit(ctx, credit.ID)

		require.NoError(tb, err)
		assert.NotNil(tb, fetched)
		assert.Equal(tb, credit.ID, fetched.ID)
		assert.Equal(tb, credit.UserID, fetched.UserID)
		assert.Equal(tb, credit.Amount, fetched.Amount)
		assert.Equal(tb, credit.Direction, fetched.Direction)
		assert.Equal(tb, credit.Type, fetched.Type)
		assert.Equal(tb, credit.ReferenceID, fetched.ReferenceID)
		assert.Equal(tb, credit.ReferenceType, fetched.ReferenceType)
	}, getCreditTestOptions())
}

func TestCreditRepository_GetCredit_NotFound(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		db := ctx.DB()
		repo := NewCreditRepository(db)
		nonExistentID := uuid.New()

		credit, err := repo.GetCredit(ctx, nonExistentID)

		assert.NoError(tb, err)
		assert.Nil(tb, credit)
	}, getCreditTestOptions())
}

func TestCreditRepository_GetUserBalance_Success(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		db := ctx.DB()
		repo := NewCreditRepository(db)
		userID := uint64(123)

		// Create multiple credits for the user
		credit1 := createTestCreditWithOptions(userID, 10.50, "credit")
		credit2 := createTestCreditWithOptions(userID, 5.25, "credit")
		credit3 := createTestCreditWithOptions(userID, 3.00, "debit")

		err := repo.CreateCredit(ctx, credit1)
		require.NoError(tb, err)

		err = repo.CreateCredit(ctx, credit2)
		require.NoError(tb, err)

		err = repo.CreateCredit(ctx, credit3)
		require.NoError(tb, err)

		// Get balance: 10.50 + 5.25 - 3.00 = 12.75
		balance, err := repo.GetUserBalance(ctx, userID)

		require.NoError(tb, err)
		expectedBalance := decimal.NewFromFloat(12.75)
		assert.Equal(tb, expectedBalance, balance)
	}, getCreditTestOptions())
}

func TestCreditRepository_GetUserBalance_NoCredits(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		db := ctx.DB()
		repo := NewCreditRepository(db)
		userID := uint64(999)

		balance, err := repo.GetUserBalance(ctx, userID)

		require.NoError(tb, err)
		assert.Equal(tb, decimal.Zero, balance)
	}, getCreditTestOptions())
}

func TestCreditRepository_SoftDeleteCredit_Success(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		db := ctx.DB()
		repo := NewCreditRepository(db)
		credit := createTestCredit()

		err := repo.CreateCredit(ctx, credit)
		require.NoError(tb, err)

		// Soft delete the credit
		err = repo.SoftDeleteCredit(ctx, credit.ID)
		require.NoError(tb, err)

		// Verify credit is not returned by normal get
		fetched, err := repo.GetCredit(ctx, credit.ID)
		assert.NoError(tb, err)
		assert.Nil(tb, fetched)

		// Verify credit is in deleted list
		deleted, err := repo.GetDeletedCredits(ctx, credit.UserID)
		require.NoError(tb, err)
		assert.Equal(tb, 1, len(deleted))
		assert.Equal(tb, credit.ID, deleted[0].ID)
	}, getCreditTestOptions())
}

func TestCreditRepository_SoftDeleteCredit_NotFound(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		db := ctx.DB()
		repo := NewCreditRepository(db)
		nonExistentID := uuid.New()

		err := repo.SoftDeleteCredit(ctx, nonExistentID)

		assert.Error(tb, err)
		assert.Contains(tb, err.Error(), "not found")
	}, getCreditTestOptions())
}

func TestCreditRepository_RestoreCredit_Success(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		db := ctx.DB()
		repo := NewCreditRepository(db)
		credit := createTestCredit()

		err := repo.CreateCredit(ctx, credit)
		require.NoError(tb, err)

		// Soft delete the credit
		err = repo.SoftDeleteCredit(ctx, credit.ID)
		require.NoError(tb, err)

		// Restore the credit
		err = repo.RestoreCredit(ctx, credit.ID)
		require.NoError(tb, err)

		// Verify credit is returned by normal get
		fetched, err := repo.GetCredit(ctx, credit.ID)
		require.NoError(tb, err)
		assert.NotNil(tb, fetched)
		assert.Equal(tb, credit.ID, fetched.ID)
	}, getCreditTestOptions())
}

func TestCreditRepository_RestoreCredit_NotFound(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		db := ctx.DB()
		repo := NewCreditRepository(db)
		nonExistentID := uuid.New()

		err := repo.RestoreCredit(ctx, nonExistentID)

		assert.Error(tb, err)
		assert.Contains(tb, err.Error(), "not found")
	}, getCreditTestOptions())
}

func TestCreditRepository_GetDeletedCredits_Success(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		db := ctx.DB()
		repo := NewCreditRepository(db)
		userID := uint64(123)

		// Create credits
		credit1 := createTestCreditWithOptions(userID, 10.50, "credit")
		credit2 := createTestCreditWithOptions(userID, 5.25, "credit")
		credit3 := createTestCreditWithOptions(userID, 3.00, "debit")

		err := repo.CreateCredit(ctx, credit1)
		require.NoError(tb, err)

		err = repo.CreateCredit(ctx, credit2)
		require.NoError(tb, err)

		err = repo.CreateCredit(ctx, credit3)
		require.NoError(tb, err)

		// Soft delete two credits
		err = repo.SoftDeleteCredit(ctx, credit1.ID)
		require.NoError(tb, err)

		err = repo.SoftDeleteCredit(ctx, credit2.ID)
		require.NoError(tb, err)

		// Get deleted credits
		deleted, err := repo.GetDeletedCredits(ctx, userID)

		require.NoError(tb, err)
		assert.Equal(tb, 2, len(deleted))

		// Verify IDs match (order may vary)
		deletedIDs := make(map[uuid.UUID]bool)
		for _, d := range deleted {
			deletedIDs[d.ID] = true
		}
		assert.True(tb, deletedIDs[credit1.ID])
		assert.True(tb, deletedIDs[credit2.ID])
		assert.False(tb, deletedIDs[credit3.ID])
	}, getCreditTestOptions())
}

func TestCreditRepository_GetDeletedCredits_Empty(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		db := ctx.DB()
		repo := NewCreditRepository(db)
		userID := uint64(123)

		deleted, err := repo.GetDeletedCredits(ctx, userID)

		require.NoError(tb, err)
		assert.Equal(tb, 0, len(deleted))
	}, getCreditTestOptions())
}

func TestCreditRepository_PurgeDeletedCredits_Success(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		db := ctx.DB()
		repo := NewCreditRepository(db)
		userID := uint64(123)

		// Create credits
		credit1 := createTestCreditWithOptions(userID, 10.50, "credit")
		credit2 := createTestCreditWithOptions(userID, 5.25, "credit")

		err := repo.CreateCredit(ctx, credit1)
		require.NoError(tb, err)

		err = repo.CreateCredit(ctx, credit2)
		require.NoError(tb, err)

		// Soft delete credits
		err = repo.SoftDeleteCredit(ctx, credit1.ID)
		require.NoError(tb, err)

		err = repo.SoftDeleteCredit(ctx, credit2.ID)
		require.NoError(tb, err)

		// Manually update deleted_at to make them old enough
		oldTime := time.Now().Add(-2 * time.Hour)
		db.Model(&models.CreditModel{}).Where("id = ?", credit1.ID).Update("deleted_at", oldTime)
		db.Model(&models.CreditModel{}).Where("id = ?", credit2.ID).Update("deleted_at", oldTime)

		// Purge credits older than 1 hour
		count, err := repo.PurgeDeletedCredits(ctx, time.Hour)

		require.NoError(tb, err)
		assert.Equal(tb, 2, count)

		// Verify they're truly gone from deleted list
		deleted, err := repo.GetDeletedCredits(ctx, userID)
		require.NoError(tb, err)
		assert.Equal(tb, 0, len(deleted))
	}, getCreditTestOptions())
}

func TestCreditRepository_PurgeDeletedCredits_Partial(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		db := ctx.DB()
		repo := NewCreditRepository(db)
		userID := uint64(123)

		// Create credits
		credit1 := createTestCreditWithOptions(userID, 10.50, "credit")
		credit2 := createTestCreditWithOptions(userID, 5.25, "credit")

		err := repo.CreateCredit(ctx, credit1)
		require.NoError(tb, err)

		err = repo.CreateCredit(ctx, credit2)
		require.NoError(tb, err)

		// Soft delete credits
		err = repo.SoftDeleteCredit(ctx, credit1.ID)
		require.NoError(tb, err)

		err = repo.SoftDeleteCredit(ctx, credit2.ID)
		require.NoError(tb, err)

		// Make only credit1 old enough
		oldTime := time.Now().Add(-2 * time.Hour)
		db.Model(&models.CreditModel{}).Where("id = ?", credit1.ID).Update("deleted_at", oldTime)

		// Purge credits older than 1 hour
		count, err := repo.PurgeDeletedCredits(ctx, time.Hour)

		require.NoError(tb, err)
		assert.Equal(tb, 1, count)

		// Verify credit2 still in deleted list
		deleted, err := repo.GetDeletedCredits(ctx, userID)
		require.NoError(tb, err)
		assert.Equal(tb, 1, len(deleted))
		assert.Equal(tb, credit2.ID, deleted[0].ID)
	}, getCreditTestOptions())
}

func TestCreditRepository_GetCreditsByReference_Success(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		db := ctx.DB()
		repo := NewCreditRepository(db)
		
		// Create credits with same reference
		refID := "stripe_payment_123"
		refType := "stripe_payment"
		
		credit1 := createTestCredit()
		credit1.ReferenceID = refID
		credit1.ReferenceType = refType
		
		credit2 := createTestCreditWithOptions(456, 15.75, "credit")
		credit2.ReferenceID = refID
		credit2.ReferenceType = refType
		
		// Create credit with different reference
		credit3 := createTestCreditWithOptions(789, 20.00, "credit")
		credit3.ReferenceID = "payment_456"
		credit3.ReferenceType = refType

		err := repo.CreateCredit(ctx, credit1)
		require.NoError(tb, err)
		
		err = repo.CreateCredit(ctx, credit2)
		require.NoError(tb, err)
		
		err = repo.CreateCredit(ctx, credit3)
		require.NoError(tb, err)

		// Get credits by reference
		credits, err := repo.GetCreditsByReference(ctx, refID, refType)

		require.NoError(tb, err)
		assert.Equal(tb, 2, len(credits))
		
		// Verify IDs match
		creditIDs := make(map[uuid.UUID]bool)
		for _, c := range credits {
			creditIDs[c.ID] = true
		}
		assert.True(tb, creditIDs[credit1.ID])
		assert.True(tb, creditIDs[credit2.ID])
		assert.False(tb, creditIDs[credit3.ID])
	}, getCreditTestOptions())
}

func TestCreditRepository_GetCreditsByReference_NotFound(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		db := ctx.DB()
		repo := NewCreditRepository(db)

		credits, err := repo.GetCreditsByReference(ctx, "nonexistent", "test")

		require.NoError(tb, err)
		assert.Equal(tb, 0, len(credits))
	}, getCreditTestOptions())
}

func TestCreditRepository_GetCreditsByReference_ExcludesDeleted(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		db := ctx.DB()
		repo := NewCreditRepository(db)
		
		refID := "stripe_payment_123"
		refType := "stripe_payment"
		
		credit1 := createTestCredit()
		credit1.ReferenceID = refID
		credit1.ReferenceType = refType
		
		credit2 := createTestCreditWithOptions(456, 15.75, "credit")
		credit2.ReferenceID = refID
		credit2.ReferenceType = refType

		err := repo.CreateCredit(ctx, credit1)
		require.NoError(tb, err)
		
		err = repo.CreateCredit(ctx, credit2)
		require.NoError(tb, err)
		
		// Soft delete one credit
		err = repo.SoftDeleteCredit(ctx, credit1.ID)
		require.NoError(tb, err)

		// Get credits by reference should exclude deleted
		credits, err := repo.GetCreditsByReference(ctx, refID, refType)

		require.NoError(tb, err)
		assert.Equal(tb, 1, len(credits))
		assert.Equal(tb, credit2.ID, credits[0].ID)
	}, getCreditTestOptions())
}
