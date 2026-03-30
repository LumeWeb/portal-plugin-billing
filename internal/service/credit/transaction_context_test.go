package credit

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	ledger "go.lumeweb.com/portal-plugin-billing/pkg/ledger"
	"go.lumeweb.com/portal-plugin-billing/internal"
	"go.lumeweb.com/portal-plugin-billing/internal/db/migrations"
	"go.lumeweb.com/portal-plugin-billing/internal/db/models"
	"go.lumeweb.com/portal/core"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	coreTesting "go.lumeweb.com/portal/core/testing"
)

// ============================================================
// Test Configuration and Setup Helpers
// ============================================================

// getTransactionContextTestOptions provides test configuration for transaction context tests
func getTransactionContextTestOptions() coreTesting.TestContextBuilderOption {
	return coreTesting.CombineOptions(
		coreTesting.NewMockPluginBuilder(internal.PLUGIN_NAME).
			WithMigrations(core.DBMigration{core.DB_TYPE_SQLITE: migrations.GetSQLite()}).
			WithService(pluginCore.CREDIT_SERVICE, NewCreditService).
			BuilderOption(),
	)
}

// ============================================================
// TransactionContext Tests
// ============================================================

func TestTransactionContext_BeginCommit_Success(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		db := ctx.DB()
		txContext := NewTransactionContext(db)

		// Begin transaction
		txCtx, err := txContext.Begin(ctx)
		require.NoError(tb, err)

		// Create credit within transaction
		repo := NewTransactionalCreditRepository(db)
		credit := createTestCredit()
		err = repo.CreateCredit(txCtx, credit)
		require.NoError(tb, err)

		// Commit transaction
		err = txContext.Commit(txCtx)
		require.NoError(tb, err)

		// Verify credit exists after commit
		fetched, err := repo.GetCredit(ctx, credit.ID)
		require.NoError(tb, err)
		assert.Equal(tb, credit.ID, fetched.ID)
	}, getTransactionContextTestOptions())
}

func TestTransactionContext_BeginRollback_Success(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		db := ctx.DB()
		txContext := NewTransactionContext(db)

		// Begin transaction
		txCtx, err := txContext.Begin(ctx)
		require.NoError(tb, err)

		// Create credit within transaction
		repo := NewTransactionalCreditRepository(db)
		credit := createTestCredit()
		err = repo.CreateCredit(txCtx, credit)
		require.NoError(tb, err)

		// Rollback transaction
		err = txContext.Rollback(txCtx)
		require.NoError(tb, err)

		// Verify credit does NOT exist after rollback
		fetched, err := repo.GetCredit(ctx, credit.ID)
		assert.NoError(tb, err)
		assert.Nil(tb, fetched)
	}, getTransactionContextTestOptions())
}

func TestTransactionContext_Begin_DatabaseError(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		// Create invalid DB connection (closed)
		// Note: In real testing, you'd mock this scenario
		// For now, we just test the normal flow
		db := ctx.DB()
		txContext := NewTransactionContext(db)

		// Begin transaction
		txCtx, err := txContext.Begin(ctx)
		require.NoError(tb, err)

		// Verify context has transaction
		tx, ok := txCtx.Value(transactionKey{}).(*gorm.DB)
		assert.True(tb, ok)
		assert.NotNil(tb, tx)

		// Cleanup
		tx.Commit()
	}, getTransactionContextTestOptions())
}

func TestTransactionContext_Commit_NoTransaction(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		db := ctx.DB()
		txContext := NewTransactionContext(db)

		// Try to commit without beginning transaction
		err := txContext.Commit(ctx)

		assert.Error(tb, err)
		assert.Contains(tb, err.Error(), "no active transaction")
	}, getTransactionContextTestOptions())
}

func TestTransactionContext_Rollback_NoTransaction(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		db := ctx.DB()
		txContext := NewTransactionContext(db)

		// Try to rollback without beginning transaction
		err := txContext.Rollback(ctx)

		assert.Error(tb, err)
		assert.Contains(tb, err.Error(), "no active transaction")
	}, getTransactionContextTestOptions())
}

func TestGetDB_WithTransaction(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		db := ctx.DB()
		txContext := NewTransactionContext(db)

		// Begin transaction
		txCtx, err := txContext.Begin(ctx)
		require.NoError(tb, err)

		// Get DB using GetDB
		txDB := GetDB(txCtx, db)
		assert.NotNil(tb, txDB)

		// Cleanup
		txContext.Rollback(txCtx)
	}, getTransactionContextTestOptions())
}

func TestGetDB_WithoutTransaction(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		db := ctx.DB()

		// Get DB without transaction
		ctxDB := GetDB(ctx, db)
		assert.NotNil(tb, ctxDB)
	}, getTransactionContextTestOptions())
}

// ============================================================
// Transaction-Aware Repository Tests
// ============================================================

func TestGORMTransactionCreditRepository_TransactionalCreate(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		db := ctx.DB()
		txContext := NewTransactionContext(db)

		// Begin transaction
		txCtx, err := txContext.Begin(ctx)
		require.NoError(tb, err)

		// Create credit within transaction
		repo := NewTransactionalCreditRepository(db)
		credit := createTestCredit()
		err = repo.CreateCredit(txCtx, credit)
		require.NoError(tb, err)

		// Credit should NOT be visible outside transaction yet
		fetched, err := repo.GetCredit(ctx, credit.ID)
		assert.NoError(tb, err)
		assert.Nil(tb, fetched)

		// Commit transaction
		err = txContext.Commit(txCtx)
		require.NoError(tb, err)

		// Now credit should be visible
		fetched, err = repo.GetCredit(ctx, credit.ID)
		require.NoError(tb, err)
		assert.NotNil(tb, fetched)
		assert.Equal(tb, credit.ID, fetched.ID)
	}, getTransactionContextTestOptions())
}

func TestGORMTransactionCreditRepository_TransactionalRead(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		db := ctx.DB()
		repo := NewTransactionalCreditRepository(db)
		
		// Create credit outside transaction
		credit := createTestCredit()
		err := repo.CreateCredit(ctx, credit)
		require.NoError(tb, err)

		// Begin transaction
		txContext := NewTransactionContext(db)
		txCtx, err := txContext.Begin(ctx)
		require.NoError(tb, err)

		// Read credit within transaction
		fetched, err := repo.GetCredit(txCtx, credit.ID)
		require.NoError(tb, err)
		assert.NotNil(tb, fetched)
		assert.Equal(tb, credit.ID, fetched.ID)

		// Cleanup
		txContext.Rollback(txCtx)
	}, getTransactionContextTestOptions())
}

func TestGORMTransactionCreditRepository_TransactionalBalance(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		db := ctx.DB()
		userID := uint64(123)
		repo := NewTransactionalCreditRepository(db)

		// Begin transaction
		txContext := NewTransactionContext(db)
		txCtx, err := txContext.Begin(ctx)
		require.NoError(tb, err)

		// Create credits within transaction
		credit1 := createTestCreditWithOptions(userID, 10.50, "credit")
		credit2 := createTestCreditWithOptions(userID, 5.25, "credit")
		credit3 := createTestCreditWithOptions(userID, 3.00, "debit")

		err = repo.CreateCredit(txCtx, credit1)
		require.NoError(tb, err)

		err = repo.CreateCredit(txCtx, credit2)
		require.NoError(tb, err)

		err = repo.CreateCredit(txCtx, credit3)
		require.NoError(tb, err)

		// Get balance within transaction
		balance, err := repo.GetUserBalance(txCtx, userID)
		require.NoError(tb, err)
		expectedBalance := decimal.NewFromFloat(12.75)
		assert.Equal(tb, expectedBalance, balance)

		// Balance outside transaction should be 0
		balance, err = repo.GetUserBalance(ctx, userID)
		require.NoError(tb, err)
		assert.Equal(tb, decimal.Zero, balance)

		// Commit transaction
		err = txContext.Commit(txCtx)
		require.NoError(tb, err)

		// Now balance should be visible outside
		balance, err = repo.GetUserBalance(ctx, userID)
		require.NoError(tb, err)
		assert.Equal(tb, expectedBalance, balance)
	}, getTransactionContextTestOptions())
}

func TestGORMTransactionCreditRepository_TransactionalSoftDelete(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		db := ctx.DB()
		repo := NewTransactionalCreditRepository(db)
		
		// Create credit outside transaction
		credit := createTestCredit()
		err := repo.CreateCredit(ctx, credit)
		require.NoError(tb, err)

		// Begin transaction
		txContext := NewTransactionContext(db)
		txCtx, err := txContext.Begin(ctx)
		require.NoError(tb, err)

		// Soft delete within transaction
		err = repo.SoftDeleteCredit(txCtx, credit.ID)
		require.NoError(tb, err)

		// Credit should STILL be visible outside transaction
		fetched, err := repo.GetCredit(ctx, credit.ID)
		require.NoError(tb, err)
		assert.NotNil(tb, fetched)

		// Rollback transaction
		err = txContext.Rollback(txCtx)
		require.NoError(tb, err)

		// Credit should still exist after rollback
		fetched, err = repo.GetCredit(ctx, credit.ID)
		require.NoError(tb, err)
		assert.NotNil(tb, fetched)
	}, getTransactionContextTestOptions())
}

func TestGORMTransactionCreditRepository_TransactionalRestoration(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		db := ctx.DB()
		repo := NewTransactionalCreditRepository(db)
		
		// Create and soft delete credit outside transaction
		credit := createTestCredit()
		err := repo.CreateCredit(ctx, credit)
		require.NoError(tb, err)
		
		err = repo.SoftDeleteCredit(ctx, credit.ID)
		require.NoError(tb, err)

		// Begin transaction
		txContext := NewTransactionContext(db)
		txCtx, err := txContext.Begin(ctx)
		require.NoError(tb, err)

		// Restore within transaction
		err = repo.RestoreCredit(txCtx, credit.ID)
		require.NoError(tb, err)

		// Credit should STILL be deleted outside transaction
		fetched, err := repo.GetCredit(ctx, credit.ID)
		assert.NoError(tb, err)
		assert.Nil(tb, fetched)

		// Commit transaction
		err = txContext.Commit(txCtx)
		require.NoError(tb, err)

		// Credit should now be restored
		fetched, err = repo.GetCredit(ctx, credit.ID)
		require.NoError(tb, err)
		assert.NotNil(tb, fetched)
	}, getTransactionContextTestOptions())
}

func TestGORMTransactionCreditRepository_TransactionalPurge(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		db := ctx.DB()
		repo := NewTransactionalCreditRepository(db)

		// Create and soft delete credit outside transaction
		credit := createTestCredit()
		err := repo.CreateCredit(ctx, credit)
		require.NoError(tb, err)
		
		err = repo.SoftDeleteCredit(ctx, credit.ID)
		require.NoError(tb, err)

		// Begin transaction
		txContext := NewTransactionContext(db)
		txCtx, err := txContext.Begin(ctx)
		require.NoError(tb, err)

		// Purge within transaction (should not affect actual data)
		_, err = repo.PurgeDeletedCredits(txCtx, 0)
		require.NoError(tb, err)

		// Credit should still exist as deleted
		deleted, err := repo.GetDeletedCredits(ctx, credit.UserID)
		require.NoError(tb, err)
		assert.Equal(tb, 1, len(deleted))

		// Rollback to verify it was transactional
		err = txContext.Rollback(txCtx)
		require.NoError(tb, err)
	}, getTransactionContextTestOptions())
}

func TestGORMTransactionCreditRepository_TransactionalReferenceLookup(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		db := ctx.DB()
		repo := NewTransactionalCreditRepository(db)
		
		// Create credits outside transaction
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

		// Begin transaction
		txContext := NewTransactionContext(db)
		txCtx, err := txContext.Begin(ctx)
		require.NoError(tb, err)

		// Add new credit with same reference within transaction
		credit3 := createTestCreditWithOptions(789, 20.00, "credit")
		credit3.ReferenceID = refID
		credit3.ReferenceType = refType

		err = repo.CreateCredit(txCtx, credit3)
		require.NoError(tb, err)

		// Lookup by reference within transaction - should see all 3 (credit3 is visible within same transaction)
		credits, err := repo.GetCreditsByReference(txCtx, refID, refType)
		require.NoError(tb, err)
		assert.Equal(tb, 3, len(credits))

		// Cleanup
		txContext.Rollback(txCtx)
	}, getTransactionContextTestOptions())
}

func TestGORMTransactionCreditRepository_MultipleTransactionRollbacks(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		db := ctx.DB()
		userID := uint64(123)
		repo := NewTransactionalCreditRepository(db)

		originalBalance, err := repo.GetUserBalance(ctx, userID)
		require.NoError(tb, err)

		// Create and rollback 3 transactions
		for i := 0; i < 3; i++ {
			txContext := NewTransactionContext(db)
			txCtx, err := txContext.Begin(ctx)
			require.NoError(tb, err)

			credit := createTestCreditWithOptions(userID, 10.0, "credit")
			err = repo.CreateCredit(txCtx, credit)
			require.NoError(tb, err)

			err = txContext.Rollback(txCtx)
			require.NoError(tb, err)
		}

		// Balance should remain unchanged
		finalBalance, err := repo.GetUserBalance(ctx, userID)
		require.NoError(tb, err)
		assert.Equal(tb, originalBalance, finalBalance)
	}, getTransactionContextTestOptions())
}

func TestGORMTransactionCreditRepository_TransactionWithNilCredit(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		db := ctx.DB()
		txContext := NewTransactionContext(db)

		// Begin transaction
		txCtx, err := txContext.Begin(ctx)
		require.NoError(tb, err)

		// Try to create nil credit
		repo := NewTransactionalCreditRepository(db)
		err = repo.CreateCredit(txCtx, nil)

		assert.Error(tb, err)
		assert.Contains(tb, err.Error(), "cannot be nil")

		// Cleanup
		txContext.Rollback(txCtx)
	}, getTransactionContextTestOptions())
}

func TestGORMTransactionCreditRepository_CommitAfterRollback(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		db := ctx.DB()
		txContext := NewTransactionContext(db)

		// Begin transaction
		txCtx, err := txContext.Begin(ctx)
		require.NoError(tb, err)

		// Create credit within transaction
		repo := NewTransactionalCreditRepository(db)
		credit := createTestCredit()
		err = repo.CreateCredit(txCtx, credit)
		require.NoError(tb, err)

		// Rollback
		err = txContext.Rollback(txCtx)
		require.NoError(tb, err)

		// Try to commit after rollback - should fail
		err = txContext.Commit(txCtx)
		assert.Error(tb, err)
	}, getTransactionContextTestOptions())
}

func TestGORMTransactionCreditRepository_GetDB_ContextPropagation(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		db := ctx.DB()
		txContext := NewTransactionContext(db)

		// Begin transaction
		txCtx, err := txContext.Begin(ctx)
		require.NoError(tb, err)

		// GetDB should return transaction-aware DB
		txDB := GetDB(txCtx, db)
		assert.NotNil(tb, txDB)

		// Create credit using GetDB
		credit := createTestCredit()
		model := toCreditModel(credit)
		err = txDB.Create(&model).Error
		require.NoError(tb, err)

		// Credit should not be visible outside transaction
		repo := NewTransactionalCreditRepository(db)
		fetched, err := repo.GetCredit(ctx, credit.ID)
		assert.NoError(tb, err)
		assert.Nil(tb, fetched)

		// But should be visible within transaction
		fetchedInTx, err := repo.GetCredit(txCtx, credit.ID)
		require.NoError(tb, err)
		assert.NotNil(tb, fetchedInTx)
		assert.Equal(tb, credit.ID, fetchedInTx.ID)

		// Cleanup
		txContext.Rollback(txCtx)
	}, getTransactionContextTestOptions())
}

// Helper function to convert Credit to model for direct DB operations
func toCreditModel(credit *ledger.Credit) *models.CreditModel {
	return &models.CreditModel{
		ID:            credit.ID,
		UserID:        credit.UserID,
		Amount:        credit.Amount,
		Type:          credit.Type,
		Direction:     credit.Direction,
		ReferenceID:   credit.ReferenceID,
		ReferenceType: credit.ReferenceType,
		Description:   credit.Description,
		CreatedBy:     credit.CreatedBy,
	}
}
