package ledger

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// Direction represents whether a credit is being added or subtracted from a balance.
//
// Direction is used internally for type safety, while Direction string values are used
// for serialization and API responses.
type Direction int

const (
	// CreditDirection adds to a user's balance (increases entitlement).
	CreditDirection Direction = iota

	// DebitDirection subtracts from a user's balance (decreases entitlement).
	DebitDirection
)

// GetDirection returns the string representation of a Direction enum value.
//
// This is needed for serialization to APIs where string values are preferred
// over integers for clarity and type safety in client applications.
func GetDirection(direction Direction) string {
	switch direction {
	case CreditDirection:
		return "credit"
	case DebitDirection:
		return "debit"
	default:
		return "unknown"
	}
}

// Credit represents a single credit or debit entry in the ledger.
//
// Credits are immutable records of value movement. The Direction field determines
// whether the amount is added (credit) or subtracted (debit) from a balance.
// Amounts are always positive; the direction indicates the flow.
//
// Thread Safety: Credit structs are intended to be used safely across goroutines
// as immutable value objects. After creation, the ID should never change.
type Credit struct {
	// ID uniquely identifies this credit entry.
	ID uuid.UUID

	// UserID identifies the user this entry applies to.
	UserID uint64

	// Amount is the absolute value. Direction determines whether this adds or subtracts.
	// Must be positive; negative values will cause runtime panics in repository implementations.
	Amount decimal.Decimal

	// Type specifies the category of credit (e.g., "time", "usage", "charge", "refund").
	// Type values are defined and validated by CreditType registrations.
	Type string

	// Direction indicates value flow direction ("credit" or "debit").
	// This field stores the serialized direction string for storage and API responses.
	Direction string

	// ReferenceID is an optional external identifier for correlated transactions.
	// Allows linking ledger entries to external systems (e.g., payment IDs, invoice numbers).
	ReferenceID string

	// ReferenceType indicates what ReferenceID points to (e.g., "payment", "invoice", "subscription").
	ReferenceType string

	// Description provides human-readable context for this credit entry.
	Description string

	// Metadata contains additional structured data about the entry.
	// Keys are limited to first-level primitives; nested structures are not supported.
	// Common keys include: "original_amount", "tax_amount", "discount_amount".
	Metadata map[string]interface{}

	// CreatedAt is the timestamp when this entry was first recorded.
	CreatedAt time.Time

	// UpdatedAt tracks the last modification time.
	UpdatedAt time.Time

	// DeletedAt is set to a non-zero time when the entry is soft-deleted.
	// Soft-deletion preserves audit trails while excluding entries from active queries.
	DeletedAt time.Time

	// CreatedBy identifies the system or user that created this entry.
	CreatedBy uint64
}

// CreditMetadata provides structured access to credit metadata with type-safe fields.
//
// This struct extracts common metadata fields from raw map structures to provide
// compile-time safety and IDE autocomplete for frequently accessed fields.
type CreditMetadata struct {
	// Description provides a human-readable summary of the credit entry.
	Description string

	// CreatedBy identifies the user or system that created the associated credit.
	CreatedBy uint64

	// Raw contains the full metadata map for accessing custom or additional fields.
	// Use Raw for dynamic field access when named fields in this struct are insufficient.
	Raw map[string]interface{}
}

// CreditType defines the constraints and properties for a specific credit type.
//
// CreditTypes are registered at application startup to enforce validation rules
// and document the semantics of different credit categories (e.g., time credits,
// usage charges, refunds). Registration prevents invalid operations by ensuring
// amounts and directions match type definitions.
type CreditType struct {
	// Name uniquely identifies this type. Use consistent naming conventions.
	// Example values: "time", "usage", "charge", "refund".
	Name string

	// Direction specifies whether this type represents adding or subtracting value.
	// Must be either CreditDirection or DebitDirection.
	Direction Direction

	// MinAmount is the minimum positive amount allowed for this type.
	// Set to zero to enforce no minimum (positive amounts only, not negative).
	MinAmount decimal.Decimal

	// MaxAmount is the maximum positive amount allowed for this type.
	// Set to zero to enforce no maximum limit.
	MaxAmount decimal.Decimal

	// Description documents the purpose and usage of this credit type.
	Description string
}