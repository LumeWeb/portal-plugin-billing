package ledger

import (
	"fmt"
	"sync"

	"github.com/shopspring/decimal"
)

// CreditTypeRegistry defines the interface for credit type registration and retrieval.
//
// Implementations manage the registration and validation of credit types, enforcing
// business rules on amounts and direction for different credit categories.
type CreditTypeRegistry interface {
	// RegisterType registers a new credit type with the specified parameters.
	// Returns nil if already registered with identical parameters, or error otherwise.
	RegisterType(name string, direction Direction, min, max decimal.Decimal, description string) error

	// GetType retrieves a registered credit type by name.
	// Returns error if type is not registered.
	GetType(name string) (*CreditType, error)

	// ValidateAmount checks if the given amount is valid for the specified credit type.
	// Must verify type exists and amount is within min/max bounds.
	ValidateAmount(name string, amount decimal.Decimal) error
}

// InMemoryRegistry implements CreditTypeRegistry with thread-safe in-memory storage.
type InMemoryRegistry struct {
	mu    sync.RWMutex
	types map[string]*CreditType
}

// NewRegistry creates a new empty registry.
func NewRegistry() *InMemoryRegistry {
	return &InMemoryRegistry{
		types: make(map[string]*CreditType),
	}
}

// RegisterType registers a new credit type.
// Idempotent: returns nil if already registered with same parameters.
func (r *InMemoryRegistry) RegisterType(name string, direction Direction, min, max decimal.Decimal, description string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if ct, exists := r.types[name]; exists {
		// Validate existing matches request
		if ct.Direction != direction || !ct.MinAmount.Equal(min) || !ct.MaxAmount.Equal(max) {
			return fmt.Errorf("credit type %s already registered with different parameters", name)
		}
		return nil // Already registered
	}

	r.types[name] = &CreditType{
		Name:        name,
		Direction:   direction,
		MinAmount:   min,
		MaxAmount:   max,
		Description: description,
	}

	return nil
}

// Package-level registry instance
var globalRegistry = NewRegistry()

// getGlobalRegistry returns the global registry instance.
func getGlobalRegistry() *InMemoryRegistry {
	return globalRegistry
}

// GetGlobalRegistry returns the global registry instance.
func GetGlobalRegistry() *InMemoryRegistry {
	return globalRegistry
}

// Package init registers core credit types.
func init() {
	// Time-based (credits and debits)
	_ = globalRegistry.RegisterType("time", CreditDirection,
		decimal.NewFromInt(1), decimal.NewFromInt(100000),
		"Time-based credits for entitlements")

	_ = globalRegistry.RegisterType("time", DebitDirection,
		decimal.NewFromInt(1), decimal.NewFromInt(100000),
		"Time-based consumption deductions")

	// Usage-based (credits and debits)
	_ = globalRegistry.RegisterType("usage", CreditDirection,
		decimal.NewFromFloat(0.01), decimal.NewFromInt(100000),
		"Usage-based credits for refunds")

	_ = globalRegistry.RegisterType("usage", DebitDirection,
		decimal.NewFromFloat(0.01), decimal.NewFromInt(100000),
		"Deductions for usage consumption")

	// Charges and refunds
	_ = globalRegistry.RegisterType("charge", CreditDirection,
		decimal.NewFromInt(1), decimal.NewFromInt(1000000),
		"Charge reversals and refunds")

	_ = globalRegistry.RegisterType("charge", DebitDirection,
		decimal.NewFromInt(1), decimal.NewFromInt(1000000),
		"One-time fees or charges")

	_ = globalRegistry.RegisterType("refund", CreditDirection,
		decimal.NewFromInt(1), decimal.NewFromInt(1000000),
		"Refunds and reversals")

	// Manual adjustments (credits and debits)
	_ = globalRegistry.RegisterType("manual_adjustment", CreditDirection,
		decimal.NewFromInt(1), decimal.NewFromInt(1000000),
		"Admin-managed credit adjustments")

	_ = globalRegistry.RegisterType("manual_adjustment", DebitDirection,
		decimal.NewFromInt(1), decimal.NewFromInt(1000000),
		"Admin-managed debit adjustments")

	// Promotions
	_ = globalRegistry.RegisterType("promo", CreditDirection,
		decimal.NewFromInt(1), decimal.NewFromInt(100000),
		"Promotional or bonus credits")
}


// GetType retrieves a registered credit type.
func (r *InMemoryRegistry) GetType(name string) (*CreditType, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	ct, ok := r.types[name]
	if !ok {
		return nil, fmt.Errorf("credit type not registered: %s", name)
	}

	return ct, nil
}

// ValidateAmount checks if amount is within type's allowed range.
func (r *InMemoryRegistry) ValidateAmount(name string, amount decimal.Decimal) error {
	ct, err := r.GetType(name)
	if err != nil {
		return err
	}

	if amount.LessThan(ct.MinAmount) {
		return fmt.Errorf("amount %s below minimum %s for type %s", amount, ct.MinAmount, name)
	}

	if amount.GreaterThan(ct.MaxAmount) {
		return fmt.Errorf("amount %s exceeds maximum %s for type %s", amount, ct.MaxAmount, name)
	}

	return nil
}
