package ledger_test

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/portal-plugin-billing/pkg/ledger"
)

func TestRegisterType_Success(t *testing.T) {
	registry := ledger.NewRegistry()

	err := registry.RegisterType("new_type", ledger.CreditDirection,
		decimal.NewFromInt(1), decimal.NewFromInt(1000), "New credit type")

	assert.NoError(t, err)

	creditType, err := registry.GetType("new_type")
	require.NoError(t, err)
	assert.Equal(t, "new_type", creditType.Name)
	assert.Equal(t, ledger.CreditDirection, creditType.Direction)
	assert.True(t, creditType.MinAmount.Equal(decimal.NewFromInt(1)))
	assert.True(t, creditType.MaxAmount.Equal(decimal.NewFromInt(1000)))
	assert.Equal(t, "New credit type", creditType.Description)
}

func TestRegisterType_Duplicate(t *testing.T) {
	registry := ledger.NewRegistry()

	name := "duplicate_type"
	params := []interface{}{name, ledger.CreditDirection,
		decimal.NewFromInt(1), decimal.NewFromInt(1000), "Duplicate type"}

	// First registration should succeed
	err := registry.RegisterType(params[0].(string), params[1].(ledger.Direction),
		params[2].(decimal.Decimal), params[3].(decimal.Decimal), params[4].(string))
	require.NoError(t, err)

	// Second registration with identical parameters should be idempotent
	err = registry.RegisterType(params[0].(string), params[1].(ledger.Direction),
		params[2].(decimal.Decimal), params[3].(decimal.Decimal), params[4].(string))
	assert.NoError(t, err)

	// Registration with different parameters should fail
	err = registry.RegisterType(name, ledger.DebitDirection,
		decimal.NewFromInt(10), decimal.NewFromInt(100), "Different type")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already registered with different parameters")
}

func TestGetType_NotFound(t *testing.T) {
	registry := ledger.NewRegistry()

	_, err := registry.GetType("nonexistent_type")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "credit type not registered")
}

func TestValidateAmount_Success(t *testing.T) {
	registry := ledger.NewRegistry()

	name := "valid_type"
	err := registry.RegisterType(name, ledger.CreditDirection,
		decimal.NewFromInt(10), decimal.NewFromInt(1000), "Valid type")
	require.NoError(t, err)

	testAmounts := []struct {
		amount decimal.Decimal
	}{
		{amount: decimal.NewFromInt(10)},
		{amount: decimal.NewFromInt(100)},
		{amount: decimal.NewFromInt(1000)},
		{amount: decimal.NewFromFloat(50.5)},
	}

	for _, tt := range testAmounts {
		t.Run(tt.amount.String(), func(t *testing.T) {
			err := registry.ValidateAmount(name, tt.amount)
			assert.NoError(t, err)
		})
	}
}

func TestValidateAmount_TooLow(t *testing.T) {
	registry := ledger.NewRegistry()

	name := "bounded_type"
	minAmount := decimal.NewFromInt(10)
	err := registry.RegisterType(name, ledger.CreditDirection,
		minAmount, decimal.NewFromInt(1000), "Bounded type")
	require.NoError(t, err)

	testAmounts := []struct {
		amount decimal.Decimal
	}{
		{amount: decimal.Zero},
		{amount: decimal.NewFromInt(-1)},
		{amount: decimal.NewFromInt(9)},
		{amount: decimal.NewFromFloat(9.99)},
	}

	for _, tt := range testAmounts {
		t.Run(tt.amount.String(), func(t *testing.T) {
			err := registry.ValidateAmount(name, tt.amount)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "below minimum")
		})
	}
}

func TestValidateAmount_TooHigh(t *testing.T) {
	registry := ledger.NewRegistry()

	name := "bounded_type"
	maxAmount := decimal.NewFromInt(1000)
	err := registry.RegisterType(name, ledger.CreditDirection,
		decimal.NewFromInt(10), maxAmount, "Bounded type")
	require.NoError(t, err)

	testAmounts := []struct {
		amount decimal.Decimal
	}{
		{amount: decimal.NewFromInt(1001)},
		{amount: decimal.NewFromInt(2000)},
		{amount: decimal.NewFromFloat(1000.01)},
	}

	for _, tt := range testAmounts {
		t.Run(tt.amount.String(), func(t *testing.T) {
			err := registry.ValidateAmount(name, tt.amount)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "exceeds maximum")
		})
	}
}

func TestValidateAmount_UnregisteredType(t *testing.T) {
	registry := ledger.NewRegistry()

	err := registry.ValidateAmount("unregistered_type", decimal.NewFromInt(100))

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "credit type not registered")
}

func TestGetDirection_Constants(t *testing.T) {
	tests := []struct {
		direction ledger.Direction
		expected  string
	}{
		{direction: ledger.CreditDirection, expected: "credit"},
		{direction: ledger.DebitDirection, expected: "debit"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := ledger.GetDirection(tt.direction)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetDirection_Unknown(t *testing.T) {
	result := ledger.GetDirection(ledger.Direction(99))
	assert.Equal(t, "unknown", result)
}
