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
	"go.lumeweb.com/portal/core"
	coreTesting "go.lumeweb.com/portal/core/testing"
	"go.lumeweb.com/queryutil"
)

// ============================================================
// Test Configuration and Setup Helpers
// ============================================================

func getCreditServiceTestOptions() coreTesting.TestContextBuilderOption {
	return coreTesting.CombineOptions(
		coreTesting.NewMockPluginBuilder(internal.PLUGIN_NAME).
			WithMigrations(core.DBMigration{core.DB_TYPE_SQLITE: migrations.GetSQLite()}).
			WithService(pluginCore.CREDIT_SERVICE, NewCreditService).
			BuilderOption(),
	)
}

func getCreditService(ctx coreTesting.TestContext) *CreditServiceDefault {
	svc := ctx.Service(pluginCore.CREDIT_SERVICE)
	return svc.(*CreditServiceDefault)
}

func getCreditRepo(ctx coreTesting.TestContext) *CreditRepository {
	db := ctx.DB()
	return NewCreditRepository(db)
}

func createTestCreditForList() *ledger.Credit {
	id := uuid.New()
	return &ledger.Credit{
		ID:            id,
		UserID:        123,
		Amount:        decimal.NewFromFloat(10.50),
		Type:          "payment",
		Direction:     ledger.GetDirection(ledger.CreditDirection),
		ReferenceID:   "ref_test_" + id.String(),
		ReferenceType: "stripe_payment",
		Description:   "Test credit for list",
		CreatedBy:     1,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
}

// ============================================================
// IssueCreditFromGateway Tests
// ============================================================

func TestCreditService_IssueCreditFromGateway_Success(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		svc := getCreditService(ctx)
		repo := getCreditRepo(ctx)

		userID := uint64(123)
		amount := decimal.NewFromFloat(50.00)

		err := svc.IssueCreditFromGateway(
			ctx,
			userID,
			pluginCore.TransactionTypeCharge,
			amount,
			pluginCore.ReferenceTypeStripeInvoice,
			"invoice_123",
			"Test charge",
			1,
		)

		require.NoError(tb, err)

		credits, err := repo.GetCreditsByReference(ctx, "invoice_123", pluginCore.ReferenceTypeStripeInvoice)
		require.NoError(tb, err)
		require.Len(tb, credits, 1)
		assert.Equal(tb, userID, credits[0].UserID)
		assert.True(tb, amount.Equal(credits[0].Amount), "amount should be equal")
		assert.Equal(tb, "credit", credits[0].Direction)
	}, getCreditServiceTestOptions())
}

func TestCreditService_IssueCreditFromGateway_RefundDirection(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		svc := getCreditService(ctx)
		repo := getCreditRepo(ctx)

		err := svc.IssueCreditFromGateway(
			ctx,
			123,
			pluginCore.TransactionTypeRefund,
			decimal.NewFromFloat(25.00),
			pluginCore.ReferenceTypeStripeInvoice,
			"refund_123",
			"Test refund",
			1,
		)

		require.NoError(tb, err)

		credits, err := repo.GetCreditsByReference(ctx, "refund_123", pluginCore.ReferenceTypeStripeInvoice)
		require.NoError(tb, err)
		require.Len(tb, credits, 1)
		assert.Equal(tb, "debit", credits[0].Direction)
	}, getCreditServiceTestOptions())
}

func TestCreditService_IssueCreditFromGateway_UsageDirection(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		svc := getCreditService(ctx)
		repo := getCreditRepo(ctx)

		err := svc.IssueCreditFromGateway(
			ctx,
			123,
			pluginCore.TransactionTypeUsage,
			decimal.NewFromFloat(5.00),
			pluginCore.ReferenceTypeAtlosPayment,
			"usage_123",
			"Test usage",
			1,
		)

		require.NoError(tb, err)

		credits, err := repo.GetCreditsByReference(ctx, "usage_123", pluginCore.ReferenceTypeAtlosPayment)
		require.NoError(tb, err)
		require.Len(tb, credits, 1)
		assert.Equal(tb, "debit", credits[0].Direction)
	}, getCreditServiceTestOptions())
}

func TestCreditService_IssueCreditFromGateway_InvalidCreditType(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		svc := getCreditService(ctx)

		err := svc.IssueCreditFromGateway(
			ctx,
			123,
			"invalid_type",
			decimal.NewFromFloat(50.00),
			pluginCore.ReferenceTypeStripeInvoice,
			"invoice_123",
			"Test",
			1,
		)

		assert.Error(tb, err)
		assert.Contains(tb, err.Error(), "invalid credit type")
	}, getCreditServiceTestOptions())
}

func TestCreditService_IssueCreditFromGateway_InvalidReferenceType(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		svc := getCreditService(ctx)

		err := svc.IssueCreditFromGateway(
			ctx,
			123,
			pluginCore.TransactionTypeCharge,
			decimal.NewFromFloat(50.00),
			"invalid_reference",
			"invoice_123",
			"Test",
			1,
		)

		assert.Error(tb, err)
		assert.Contains(tb, err.Error(), "invalid reference type")
	}, getCreditServiceTestOptions())
}

// ============================================================
// IssueCreditWithIdempotency Tests
// ============================================================

func TestCreditService_IssueCreditWithIdempotency_Success(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		svc := getCreditService(ctx)
		repo := getCreditRepo(ctx)

		userID := uint64(123)
		amount := decimal.NewFromFloat(50.00)

		err := svc.IssueCreditWithIdempotency(
			ctx,
			userID,
			pluginCore.TransactionTypeCharge,
			amount,
			pluginCore.ReferenceTypeStripeInvoice,
			"invoice_123",
			"Test charge",
			1,
		)

		require.NoError(tb, err)

		credits, err := repo.GetCreditsByReference(ctx, "invoice_123", pluginCore.ReferenceTypeStripeInvoice)
		require.NoError(tb, err)
		require.Len(tb, credits, 1)
		assert.Equal(tb, userID, credits[0].UserID)
		assert.True(tb, amount.Equal(credits[0].Amount), "amount should be equal")
	}, getCreditServiceTestOptions())
}

func TestCreditService_IssueCreditWithIdempotency_Duplicate(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		svc := getCreditService(ctx)
		repo := getCreditRepo(ctx)

		userID := uint64(123)
		amount := decimal.NewFromFloat(50.00)

		err := svc.IssueCreditWithIdempotency(
			ctx,
			userID,
			pluginCore.TransactionTypeCharge,
			amount,
			pluginCore.ReferenceTypeStripeInvoice,
			"invoice_123",
			"Test charge",
			1,
		)
		require.NoError(tb, err)

		credits, err := repo.GetCreditsByReference(ctx, "invoice_123", pluginCore.ReferenceTypeStripeInvoice)
		require.NoError(tb, err)
		initialCount := len(credits)

		err = svc.IssueCreditWithIdempotency(
			ctx,
			userID,
			pluginCore.TransactionTypeCharge,
			amount,
			pluginCore.ReferenceTypeStripeInvoice,
			"invoice_123",
			"Test charge",
			1,
		)

		assert.NoError(tb, err)

		credits, err = repo.GetCreditsByReference(ctx, "invoice_123", pluginCore.ReferenceTypeStripeInvoice)
		require.NoError(tb, err)
		assert.Equal(tb, initialCount, len(credits), "idempotency should prevent duplicate credits")
	}, getCreditServiceTestOptions())
}

func TestCreditService_IssueCreditWithIdempotency_InvalidCreditType(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		svc := getCreditService(ctx)

		err := svc.IssueCreditWithIdempotency(
			ctx,
			123,
			"invalid_type",
			decimal.NewFromFloat(50.00),
			pluginCore.ReferenceTypeStripeInvoice,
			"invoice_123",
			"Test",
			1,
		)

		assert.Error(tb, err)
		assert.Contains(tb, err.Error(), "invalid credit type")
	}, getCreditServiceTestOptions())
}

func TestCreditService_IssueCreditWithIdempotency_InvalidReferenceType(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		svc := getCreditService(ctx)

		err := svc.IssueCreditWithIdempotency(
			ctx,
			123,
			pluginCore.TransactionTypeCharge,
			decimal.NewFromFloat(50.00),
			"invalid_reference",
			"invoice_123",
			"Test",
			1,
		)

		assert.Error(tb, err)
		assert.Contains(tb, err.Error(), "invalid reference type")
	}, getCreditServiceTestOptions())
}

// ============================================================
// IssueUsageCredit Tests
// ============================================================

func TestCreditService_IssueUsageCredit_Success(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		svc := getCreditService(ctx)
		repo := getCreditRepo(ctx)

		userID := uint64(123)
		amount := decimal.NewFromFloat(10.00)

		err := svc.IssueUsageCredit(ctx, userID, pluginCore.TransactionTypeUsage, amount, "ref_123", "Test usage", 1)
		require.NoError(tb, err)

		credits, err := repo.GetCreditsByReference(ctx, "ref_123", "usage")
		require.NoError(tb, err)
		require.Len(tb, credits, 1)
		assert.Equal(tb, userID, credits[0].UserID)
		assert.True(tb, amount.Equal(credits[0].Amount), "amount should be equal")
		assert.Equal(tb, "debit", credits[0].Direction)
	}, getCreditServiceTestOptions())
}

func TestCreditService_IssueUsageCredit_TimeCredit(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		svc := getCreditService(ctx)
		repo := getCreditRepo(ctx)

		err := svc.IssueUsageCredit(ctx, 123, pluginCore.TransactionTypeTime, decimal.NewFromFloat(5.0), "time_ref", "Time credit", 1)
		require.NoError(tb, err)

		credits, err := repo.GetCreditsByReference(ctx, "time_ref", "usage")
		require.NoError(tb, err)
		require.Len(tb, credits, 1)
	}, getCreditServiceTestOptions())
}

func TestCreditService_IssueUsageCredit_InvalidType(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		svc := getCreditService(ctx)

		err := svc.IssueUsageCredit(ctx, 123, pluginCore.TransactionTypeCharge, decimal.NewFromFloat(50.00), "ref", "Test", 1)

		assert.Error(tb, err)
		assert.Contains(tb, err.Error(), "invalid usage credit type")
	}, getCreditServiceTestOptions())
}

// ============================================================
// GetUserBalance Tests
// ============================================================

func TestCreditService_GetUserBalance_Success(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		repo := getCreditRepo(ctx)
		svc := getCreditService(ctx)

		userID := uint64(123)

		credits := []*ledger.Credit{
			{ID: uuid.New(), UserID: userID, Amount: decimal.NewFromFloat(10.50), Type: "charge", Direction: ledger.GetDirection(ledger.CreditDirection), ReferenceID: "1", ReferenceType: "test"},
			{ID: uuid.New(), UserID: userID, Amount: decimal.NewFromFloat(5.25), Type: "charge", Direction: ledger.GetDirection(ledger.CreditDirection), ReferenceID: "2", ReferenceType: "test"},
			{ID: uuid.New(), UserID: userID, Amount: decimal.NewFromFloat(3.00), Type: "usage", Direction: ledger.GetDirection(ledger.DebitDirection), ReferenceID: "3", ReferenceType: "test"},
		}

		for _, credit := range credits {
			err := repo.CreateCredit(ctx, credit)
			require.NoError(tb, err)
		}

		balance, err := svc.GetUserBalance(ctx, userID)

		require.NoError(tb, err)
		expectedBalance := decimal.NewFromFloat(12.75)
		assert.Equal(tb, expectedBalance, balance)
	}, getCreditServiceTestOptions())
}

func TestCreditService_GetUserBalance_NoCredits(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		svc := getCreditService(ctx)

		balance, err := svc.GetUserBalance(ctx, 999)

		require.NoError(tb, err)
		assert.Equal(tb, decimal.Zero, balance)
	}, getCreditServiceTestOptions())
}

// ============================================================
// GetReferenceIdempotencyKey Tests
// ============================================================

func TestCreditService_GetReferenceIdempotencyKey_Success(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		svc := getCreditService(ctx)

		err := svc.IssueCreditWithIdempotency(ctx, 123, pluginCore.TransactionTypeCharge, decimal.NewFromFloat(50.00), pluginCore.ReferenceTypeStripeInvoice, "invoice_123", "Test", 1)
		require.NoError(tb, err)

		idempotencyKey, err := svc.GetReferenceIdempotencyKey(ctx, "invoice_123")

		require.NoError(tb, err)
		assert.NotEmpty(tb, idempotencyKey)
		assert.Contains(tb, idempotencyKey, "invoice_123")
	}, getCreditServiceTestOptions())
}

func TestCreditService_GetReferenceIdempotencyKey_NotFound(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		svc := getCreditService(ctx)

		idempotencyKey, err := svc.GetReferenceIdempotencyKey(ctx, "nonexistent")

		require.NoError(tb, err)
		assert.Equal(tb, "", idempotencyKey)
	}, getCreditServiceTestOptions())
}

// ============================================================
// SoftDeleteCredit Tests
// ============================================================

func TestCreditService_SoftDeleteCredit_Success(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		svc := getCreditService(ctx)
		repo := getCreditRepo(ctx)

		credit := &ledger.Credit{
			ID:            uuid.New(),
			UserID:        123,
			Amount:        decimal.NewFromFloat(10.50),
			Type:          "charge",
			Direction:     ledger.GetDirection(ledger.CreditDirection),
			ReferenceID:   "ref_123",
			ReferenceType: "stripe_payment",
			Description:   "Test",
			CreatedBy:     1,
		}

		err := repo.CreateCredit(ctx, credit)
		require.NoError(tb, err)

		err = svc.SoftDeleteCredit(ctx, credit.ID)
		require.NoError(tb, err)

		fetched, err := repo.GetCredit(ctx, credit.ID)
		assert.NoError(tb, err)
		assert.Nil(tb, fetched)

		deleted, err := svc.GetDeletedCredits(ctx, credit.UserID)
		require.NoError(tb, err)
		require.Len(tb, deleted, 1)
		assert.Equal(tb, credit.ID, deleted[0].ID)
	}, getCreditServiceTestOptions())
}

func TestCreditService_SoftDeleteCredit_NotFound(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		svc := getCreditService(ctx)

		err := svc.SoftDeleteCredit(ctx, uuid.New())

		assert.Error(tb, err)
	}, getCreditServiceTestOptions())
}

// ============================================================
// RestoreCredit Tests
// ============================================================

func TestCreditService_RestoreCredit_Success(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		svc := getCreditService(ctx)
		repo := getCreditRepo(ctx)

		credit := &ledger.Credit{
			ID:            uuid.New(),
			UserID:        123,
			Amount:        decimal.NewFromFloat(10.50),
			Type:          "charge",
			Direction:     ledger.GetDirection(ledger.CreditDirection),
			ReferenceID:   "ref_123",
			ReferenceType: "test",
			Description:   "Test",
			CreatedBy:     1,
		}

		err := repo.CreateCredit(ctx, credit)
		require.NoError(tb, err)

		err = svc.SoftDeleteCredit(ctx, credit.ID)
		require.NoError(tb, err)

		err = svc.RestoreCredit(ctx, credit.ID)
		require.NoError(tb, err)

		fetched, err := svc.GetCredit(ctx, credit.ID)
		require.NoError(tb, err)
		assert.NotNil(tb, fetched)
		assert.Equal(tb, credit.ID, fetched.ID)
	}, getCreditServiceTestOptions())
}

func TestCreditService_RestoreCredit_NotFound(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		svc := getCreditService(ctx)

		err := svc.RestoreCredit(ctx, uuid.New())

		assert.Error(tb, err)
	}, getCreditServiceTestOptions())
}

// ============================================================
// CreateCredit Tests
// ============================================================

func TestCreditService_CreateCredit_Success(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		svc := getCreditService(ctx)

		creditID := uuid.New()
		credit := &ledger.Credit{
			ID:            creditID,
			UserID:        123,
			Amount:        decimal.NewFromFloat(10.50),
			Type:          "charge",
			Direction:     ledger.GetDirection(ledger.CreditDirection),
			ReferenceID:   "ref_123",
			ReferenceType: "test",
			Description:   "Test",
			CreatedBy:     1,
		}

		err := svc.CreateCredit(ctx, credit)
		require.NoError(tb, err)

		fetched, err := svc.GetCredit(ctx, creditID)
		require.NoError(tb, err)
		assert.Equal(tb, creditID, fetched.ID)
		assert.Equal(tb, uint64(123), fetched.UserID)
	}, getCreditServiceTestOptions())
}

// ============================================================
// GetCredit Tests
// ============================================================

func TestCreditService_GetCredit_Success(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		svc := getCreditService(ctx)
		repo := getCreditRepo(ctx)

		credit := &ledger.Credit{
			ID:            uuid.New(),
			UserID:        123,
			Amount:        decimal.NewFromFloat(10.50),
			Type:          "charge",
			Direction:     ledger.GetDirection(ledger.CreditDirection),
			ReferenceID:   "ref_123",
			ReferenceType: "test",
			Description:   "Test",
			CreatedBy:     1,
		}

		err := repo.CreateCredit(ctx, credit)
		require.NoError(tb, err)

		fetched, err := svc.GetCredit(ctx, credit.ID)

		require.NoError(tb, err)
		assert.NotNil(tb, fetched)
		assert.Equal(tb, credit.ID, fetched.ID)
		assert.Equal(tb, uint64(123), fetched.UserID)
		assert.Equal(tb, decimal.NewFromFloat(10.50), fetched.Amount)
	}, getCreditServiceTestOptions())
}

func TestCreditService_GetCredit_NotFound(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		svc := getCreditService(ctx)

		credit, err := svc.GetCredit(ctx, uuid.New())

		assert.NoError(tb, err)
		assert.Nil(tb, credit)
	}, getCreditServiceTestOptions())
}

// ============================================================
// GetCreditsByReference Tests
// ============================================================

func TestCreditService_GetCreditsByReference_Success(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		svc := getCreditService(ctx)
		repo := getCreditRepo(ctx)

		refID := "stripe_payment_123"
		refType := "stripe_payment"

		credit1 := &ledger.Credit{
			ID:            uuid.New(),
			UserID:        123,
			Amount:        decimal.NewFromFloat(10.50),
			Type:          "charge",
			Direction:     ledger.GetDirection(ledger.CreditDirection),
			ReferenceID:   refID,
			ReferenceType: refType,
			CreatedBy:     1,
		}

		credit2 := &ledger.Credit{
			ID:            uuid.New(),
			UserID:        456,
			Amount:        decimal.NewFromFloat(15.75),
			Type:          "charge",
			Direction:     ledger.GetDirection(ledger.CreditDirection),
			ReferenceID:   refID,
			ReferenceType: refType,
			CreatedBy:     1,
		}

		err := repo.CreateCredit(ctx, credit1)
		require.NoError(tb, err)
		err = repo.CreateCredit(ctx, credit2)
		require.NoError(tb, err)

		credits, err := svc.GetCreditsByReference(ctx, refID, refType)

		require.NoError(tb, err)
		assert.Len(tb, credits, 2)
	}, getCreditServiceTestOptions())
}

func TestCreditService_GetCreditsByReference_NotFound(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		svc := getCreditService(ctx)

		credits, err := svc.GetCreditsByReference(ctx, "nonexistent", "test")

		require.NoError(tb, err)
		assert.Len(tb, credits, 0)
	}, getCreditServiceTestOptions())
}

// ============================================================
// ListCredits Tests
// ============================================================

func TestCreditService_ListCredits_Success(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		repo := getCreditRepo(ctx)
		svc := getCreditService(ctx)

		credit1 := createTestCreditForList()
		credit2 := createTestCreditForList()
		credit2.UserID = 456
		credit2.Amount = decimal.NewFromFloat(25.00)

		err := repo.CreateCredit(ctx, credit1)
		require.NoError(tb, err)
		err = repo.CreateCredit(ctx, credit2)
		require.NoError(tb, err)

		filters := []queryutil.CrudFilter{}
		sorts := []queryutil.Sort{}
		pagination, _ := queryutil.NewPagination(0, 10)

		credits, total, err := svc.ListCredits(ctx, filters, sorts, pagination)

		assert.NoError(tb, err)
		assert.Equal(tb, int64(2), total)
		assert.Equal(tb, 2, len(credits))
	}, getCreditServiceTestOptions())
}

func TestCreditService_ListCredits_EmptyResult(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		svc := getCreditService(ctx)

		filters := []queryutil.CrudFilter{}
		sorts := []queryutil.Sort{}
		pagination, _ := queryutil.NewPagination(0, 10)

		credits, total, err := svc.ListCredits(ctx, filters, sorts, pagination)

		assert.NoError(tb, err)
		assert.Equal(tb, int64(0), total)
		assert.Equal(tb, 0, len(credits))
	}, getCreditServiceTestOptions())
}

func TestCreditService_ListCredits_WithPagination(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		repo := getCreditRepo(ctx)
		svc := getCreditService(ctx)

		for i := 0; i < 5; i++ {
			credit := createTestCreditForList()
			credit.UserID = uint64(100 + i)
			err := repo.CreateCredit(ctx, credit)
			require.NoError(tb, err)
		}

		filters := []queryutil.CrudFilter{}
		sorts := []queryutil.Sort{}
		pagination, _ := queryutil.CreatePage(1, 2)

		credits, total, err := svc.ListCredits(ctx, filters, sorts, pagination)

		assert.NoError(tb, err)
		assert.Equal(tb, int64(5), total)
		assert.Equal(tb, 2, len(credits))

		pagination2, _ := queryutil.CreatePage(2, 2)

		credits, total, err = svc.ListCredits(ctx, filters, sorts, pagination2)

		assert.NoError(tb, err)
		assert.Equal(tb, int64(5), total)
		assert.Equal(tb, 2, len(credits))
	}, getCreditServiceTestOptions())
}

func TestCreditService_ListCredits_WithFilter(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		repo := getCreditRepo(ctx)
		svc := getCreditService(ctx)

		credit1 := createTestCreditForList()
		credit1.UserID = 123
		credit1.Amount = decimal.NewFromFloat(10.50)

		credit2 := createTestCreditForList()
		credit2.UserID = 456
		credit2.Amount = decimal.NewFromFloat(25.00)

		err := repo.CreateCredit(ctx, credit1)
		require.NoError(tb, err)
		err = repo.CreateCredit(ctx, credit2)
		require.NoError(tb, err)

		filters := []queryutil.CrudFilter{
			queryutil.Equal("user_id", "123"),
		}
		sorts := []queryutil.Sort{}
		pagination, _ := queryutil.NewPagination(0, 10)

		credits, total, err := svc.ListCredits(ctx, filters, sorts, pagination)

		assert.NoError(tb, err)
		assert.Equal(tb, int64(1), total)
		assert.Equal(tb, 1, len(credits))
		assert.Equal(tb, uint64(123), credits[0].UserID)
	}, getCreditServiceTestOptions())
}

// ============================================================
// GetDeletedCredits Tests
// ============================================================

func TestCreditService_GetDeletedCredits_Success(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		svc := getCreditService(ctx)
		repo := getCreditRepo(ctx)

		userID := uint64(123)

		credit1 := &ledger.Credit{
			ID:            uuid.New(),
			UserID:        userID,
			Amount:        decimal.NewFromFloat(10.50),
			Type:          "charge",
			Direction:     ledger.GetDirection(ledger.CreditDirection),
			ReferenceID:   "ref_1",
			ReferenceType: "test",
			CreatedBy:     1,
		}

		credit2 := &ledger.Credit{
			ID:            uuid.New(),
			UserID:        userID,
			Amount:        decimal.NewFromFloat(5.25),
			Type:          "charge",
			Direction:     ledger.GetDirection(ledger.CreditDirection),
			ReferenceID:   "ref_2",
			ReferenceType: "test",
			CreatedBy:     1,
		}

		err := repo.CreateCredit(ctx, credit1)
		require.NoError(tb, err)
		err = repo.CreateCredit(ctx, credit2)
		require.NoError(tb, err)

		err = svc.SoftDeleteCredit(ctx, credit1.ID)
		require.NoError(tb, err)
		err = svc.SoftDeleteCredit(ctx, credit2.ID)
		require.NoError(tb, err)

		deleted, err := svc.GetDeletedCredits(ctx, userID)

		require.NoError(tb, err)
		assert.Len(tb, deleted, 2)
	}, getCreditServiceTestOptions())
}

func TestCreditService_GetDeletedCredits_Empty(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		svc := getCreditService(ctx)

		deleted, err := svc.GetDeletedCredits(ctx, 123)

		require.NoError(tb, err)
		assert.Empty(tb, deleted)
	}, getCreditServiceTestOptions())
}

// ============================================================
// PurgeDeletedCredits Tests
// ============================================================

func TestCreditService_PurgeDeletedCredits_Success(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		svc := getCreditService(ctx)
		repo := getCreditRepo(ctx)

		userID := uint64(123)
		credit1 := &ledger.Credit{
			ID:            uuid.New(),
			UserID:        userID,
			Amount:        decimal.NewFromFloat(10.50),
			Type:          "charge",
			Direction:     ledger.GetDirection(ledger.CreditDirection),
			ReferenceID:   "ref_1",
			ReferenceType: "test",
			CreatedBy:     1,
		}
		credit2 := &ledger.Credit{
			ID:            uuid.New(),
			UserID:        userID,
			Amount:        decimal.NewFromFloat(5.25),
			Type:          "charge",
			Direction:     ledger.GetDirection(ledger.CreditDirection),
			ReferenceID:   "ref_2",
			ReferenceType: "test",
			CreatedBy:     1,
		}

		err := repo.CreateCredit(ctx, credit1)
		require.NoError(tb, err)
		err = repo.CreateCredit(ctx, credit2)
		require.NoError(tb, err)

		err = svc.SoftDeleteCredit(ctx, credit1.ID)
		require.NoError(tb, err)
		err = svc.SoftDeleteCredit(ctx, credit2.ID)
		require.NoError(tb, err)

		time.Sleep(time.Millisecond)
		count, err := svc.PurgeDeletedCredits(ctx, 0)

		require.NoError(tb, err)
		assert.Equal(tb, 2, count)

		deleted, err := svc.GetDeletedCredits(ctx, userID)

		require.NoError(tb, err)
		assert.Empty(tb, deleted)
	}, getCreditServiceTestOptions())
}

func TestCreditService_PurgeDeletedCredits_Partial(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		svc := getCreditService(ctx)
		repo := getCreditRepo(ctx)

		userID := uint64(123)
		credit1 := &ledger.Credit{
			ID:            uuid.New(),
			UserID:        userID,
			Amount:        decimal.NewFromFloat(10.50),
			Type:          "charge",
			Direction:     ledger.GetDirection(ledger.CreditDirection),
			ReferenceID:   "ref_1",
			ReferenceType: "test",
			CreatedBy:     1,
		}
		credit2 := &ledger.Credit{
			ID:            uuid.New(),
			UserID:        userID,
			Amount:        decimal.NewFromFloat(5.25),
			Type:          "charge",
			Direction:     ledger.GetDirection(ledger.CreditDirection),
			ReferenceID:   "ref_2",
			ReferenceType: "test",
			CreatedBy:     1,
		}

		err := repo.CreateCredit(ctx, credit1)
		require.NoError(tb, err)
		err = repo.CreateCredit(ctx, credit2)
		require.NoError(tb, err)

		err = svc.SoftDeleteCredit(ctx, credit1.ID)
		require.NoError(tb, err)
		err = svc.SoftDeleteCredit(ctx, credit2.ID)
		require.NoError(tb, err)

		time.Sleep(time.Millisecond)
		time.Sleep(time.Millisecond)

		count, err := svc.PurgeDeletedCredits(ctx, time.Hour)

		require.NoError(tb, err)
		assert.Equal(tb, 0, count, "no credits should be purged with 1 hour threshold")
	}, getCreditServiceTestOptions())
}
