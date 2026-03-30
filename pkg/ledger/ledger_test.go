package ledger_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/portal-plugin-billing/pkg/ledger"
	mocks "go.lumeweb.com/portal-plugin-billing/pkg/ledger/mocks"
)

func TestIssueCredit_Success(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(mocks.MockCreditRepository)
	registry := ledger.NewRegistry()

	// Register a valid credit type
	err := registry.RegisterType("test_type", ledger.CreditDirection,
		decimal.NewFromInt(1), decimal.NewFromInt(1000), "Test type")
	require.NoError(t, err)

	ledgerService := ledger.NewLedger(mockRepo, registry)

	userID := uint64(12345)
	amount := decimal.NewFromInt(100)
	metadata := ledger.CreditMetadata{
		Description: "Test credit",
		CreatedBy:   67890,
	}

	// Expect CreateCredit to be called
	mockRepo.On("CreateCredit", ctx, mock.MatchedBy(func(c *ledger.Credit) bool {
		return c.UserID == userID && c.Amount.Equal(amount)
	})).Return(nil)

	err = ledgerService.IssueCredit(ctx, userID, amount, "test_type", ledger.CreditDirection,
		uuid.New().String(), "", metadata)

	assert.NoError(t, err)
	mockRepo.AssertExpectations(t)
}

func TestIssueCredit_InvalidType(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(mocks.MockCreditRepository)
	registry := ledger.NewRegistry()

	ledgerService := ledger.NewLedger(mockRepo, registry)

	userID := uint64(12345)
	amount := decimal.NewFromInt(100)
	metadata := ledger.CreditMetadata{
		Description: "Test credit",
		CreatedBy:   67890,
	}

	err := ledgerService.IssueCredit(ctx, userID, amount, "unknown_type", ledger.CreditDirection,
		uuid.New().String(), "", metadata)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown credit type")
}

func TestIssueCredit_InvalidAmount(t *testing.T) {
	tests := []struct {
		name   string
		amount decimal.Decimal
	}{
		{amount: decimal.Zero, name: "zero amount"},
		{amount: decimal.NewFromInt(-100), name: "negative amount"},
		{amount: decimal.NewFromInt(10000), name: "above maximum"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			mockRepo := new(mocks.MockCreditRepository)
			registry := ledger.NewRegistry()

			// Register a credit type with limited range
			err := registry.RegisterType("limited_type", ledger.CreditDirection,
				decimal.NewFromInt(1), decimal.NewFromInt(1000), "Limited type")
			require.NoError(t, err)

			ledgerService := ledger.NewLedger(mockRepo, registry)

			userID := uint64(12345)
			metadata := ledger.CreditMetadata{
				Description: "Test credit",
				CreatedBy:   67890,
			}

			err = ledgerService.IssueCredit(ctx, userID, tt.amount, "limited_type", ledger.CreditDirection,
				uuid.New().String(), "", metadata)

			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid amount")
		})
	}
}

func TestDebitCredit_Success(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(mocks.MockCreditRepository)
	registry := ledger.NewRegistry()

	// Register a valid debit type
	err := registry.RegisterType("debit_type", ledger.DebitDirection,
		decimal.NewFromInt(1), decimal.NewFromInt(1000), "Debit type")
	require.NoError(t, err)

	ledgerService := ledger.NewLedger(mockRepo, registry)

	userID := uint64(12345)
	amount := decimal.NewFromInt(50)
	metadata := ledger.CreditMetadata{
		Description: "Test debit",
		CreatedBy:   67890,
	}

	mockRepo.On("CreateCredit", ctx, mock.MatchedBy(func(c *ledger.Credit) bool {
		return c.UserID == userID && c.Direction == "debit"
	})).Return(nil)

	err = ledgerService.DebitCredit(ctx, userID, amount, "debit_type", uuid.New().String(), "", metadata)

	assert.NoError(t, err)
	mockRepo.AssertExpectations(t)
}

func TestGetUserBalance_Success(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(mocks.MockCreditRepository)
	registry := ledger.NewRegistry()

	ledgerService := ledger.NewLedger(mockRepo, registry)

	userID := uint64(12345)
	expectedBalance := decimal.NewFromInt(500)

	mockRepo.On("GetUserBalance", ctx, userID).Return(expectedBalance, nil)

	balance, err := ledgerService.GetUserBalance(ctx, userID)

	assert.NoError(t, err)
	assert.True(t, balance.Equal(expectedBalance))
	mockRepo.AssertExpectations(t)
}

func TestIssueIdempotentCredit_NewCredit(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(mocks.MockCreditRepository)
	registry := ledger.NewRegistry()

	err := registry.RegisterType("test_type", ledger.CreditDirection,
		decimal.NewFromInt(1), decimal.NewFromInt(1000), "Test type")
	require.NoError(t, err)

	ledgerService := ledger.NewLedger(mockRepo, registry)

	userID := uint64(12345)
	amount := decimal.NewFromInt(100)
	referenceID := uuid.New().String()
	idempotencyKey := "unique-key-123"
	metadata := ledger.CreditMetadata{
		Description: "Test credit",
		CreatedBy:   67890,
	}

	// No existing credits found
	mockRepo.On("GetCreditsByReference", ctx, referenceID, "").Return([]ledger.Credit{}, nil)

	// CreateCredit should be called
	mockRepo.On("CreateCredit", ctx, mock.MatchedBy(func(c *ledger.Credit) bool {
		key, ok := c.Metadata["idempotency_key"].(string)
		return ok && key == idempotencyKey
	})).Return(nil)

	err = ledgerService.IssueIdempotentCredit(ctx, userID, amount, "test_type", referenceID,
		"", idempotencyKey, metadata)

	assert.NoError(t, err)
	mockRepo.AssertExpectations(t)
}

func TestIssueIdempotentCredit_DuplicateSkipped(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(mocks.MockCreditRepository)
	registry := ledger.NewRegistry()

	err := registry.RegisterType("test_type", ledger.CreditDirection,
		decimal.NewFromInt(1), decimal.NewFromInt(1000), "Test type")
	require.NoError(t, err)

	ledgerService := ledger.NewLedger(mockRepo, registry)

	userID := uint64(12345)
	amount := decimal.NewFromInt(100)
	referenceID := uuid.New().String()
	idempotencyKey := "unique-key-123"
	metadata := ledger.CreditMetadata{
		Description: "Test credit",
		CreatedBy:   67890,
	}

	existingCredit := ledger.Credit{
		ID: uuid.New(),
		Metadata: map[string]interface{}{
			"idempotency_key": idempotencyKey,
		},
	}

	// Credit with same idempotency key already exists
	mockRepo.On("GetCreditsByReference", ctx, referenceID, "").
		Return([]ledger.Credit{existingCredit}, nil)

	err = ledgerService.IssueIdempotentCredit(ctx, userID, amount, "test_type", referenceID,
		"", idempotencyKey, metadata)

	assert.NoError(t, err)
	mockRepo.AssertExpectations(t)

	// Verify CreateCredit was NOT called
	mockRepo.AssertNotCalled(t, "CreateCredit", mock.Anything, mock.Anything)
}

func TestSoftDeleteCredit_Success(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(mocks.MockCreditRepository)
	registry := ledger.NewRegistry()

	ledgerService := ledger.NewLedger(mockRepo, registry)

	creditID := uuid.New()
	mockRepo.On("SoftDeleteCredit", ctx, creditID).Return(nil)

	err := ledgerService.SoftDeleteCredit(ctx, creditID)

	assert.NoError(t, err)
	mockRepo.AssertExpectations(t)
}

func TestRestoreCredit_Success(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(mocks.MockCreditRepository)
	registry := ledger.NewRegistry()

	ledgerService := ledger.NewLedger(mockRepo, registry)

	creditID := uuid.New()
	mockRepo.On("RestoreCredit", ctx, creditID).Return(nil)

	err := ledgerService.RestoreCredit(ctx, creditID)

	assert.NoError(t, err)
	mockRepo.AssertExpectations(t)
}

func TestPurgeDeletedCredits_Success(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(mocks.MockCreditRepository)
	registry := ledger.NewRegistry()

	ledgerService := ledger.NewLedger(mockRepo, registry)

	olderThan := 30 * 24 * time.Hour
	expectedCount := 5

	mockRepo.On("PurgeDeletedCredits", ctx, olderThan).Return(expectedCount, nil)

	count, err := ledgerService.PurgeDeletedCredits(ctx, olderThan)

	assert.NoError(t, err)
	assert.Equal(t, expectedCount, count)
	mockRepo.AssertExpectations(t)
}

func TestIssueIdempotentCredit_GetCreditsError(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(mocks.MockCreditRepository)
	registry := ledger.NewRegistry()

	err := registry.RegisterType("test_type", ledger.CreditDirection,
		decimal.NewFromInt(1), decimal.NewFromInt(1000), "Test type")
	require.NoError(t, err)

	ledgerService := ledger.NewLedger(mockRepo, registry)

	userID := uint64(12345)
	amount := decimal.NewFromInt(100)
	referenceID := uuid.New().String()
	idempotencyKey := "unique-key-123"
	metadata := ledger.CreditMetadata{
		Description: "Test credit",
		CreatedBy:   67890,
	}

	mockRepo.On("GetCreditsByReference", ctx, referenceID, "").
		Return(nil, errors.New("database error"))

	err = ledgerService.IssueIdempotentCredit(ctx, userID, amount, "test_type", referenceID,
		"", idempotencyKey, metadata)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database error")
}
