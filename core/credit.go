package core

import (
	"context"

	"github.com/shopspring/decimal"
	"go.lumeweb.com/portal-plugin-billing/pkg/ledger"
	"go.lumeweb.com/portal/core"
	"go.lumeweb.com/queryutil"
)

// CREDIT_SERVICE is the service ID for CreditService
const CREDIT_SERVICE = "billing.credit"

// CreditService manages credit operations bridging gateway events and the ledger.
// Embodies ledger.CreditRepository for data operations and adds gateway event integration.
type CreditService interface {
	ledger.CreditRepository
	core.Service

	// IssueCreditFromGateway creates a credit entry from a gateway event
	// Maps gateway-specific referenceTypes and metadata to ledger entries
	IssueCreditFromGateway(
		ctx context.Context,
		userID uint64,
		transactionType string,
		amount decimal.Decimal,
		referenceType string,
		referenceID string,
		description string,
		createdBy uint64,
	) error

	// IssueCreditWithIdempotency creates a credit entry with idempotency protection
	// Prevents duplicate credits from re-delivered webhook events
	IssueCreditWithIdempotency(
		ctx context.Context,
		userID uint64,
		transactionType string,
		amount decimal.Decimal,
		referenceType string,
		referenceID string,
		description string,
		createdBy uint64,
	) error

	// IssueUsageCredit creates a usage-based debit for resource consumption
	IssueUsageCredit(
		ctx context.Context,
		userID uint64,
		transactionType string,
		amount decimal.Decimal,
		referenceID string,
		description string,
		createdBy uint64,
	) error

	// ListCredits retrieves credits with filtering, sorting, and pagination
	// Service-level method that wraps repository GetCredits with logging
	ListCredits(ctx context.Context, filters []queryutil.CrudFilter, sorts []queryutil.Sort, pagination queryutil.Pagination) ([]ledger.Credit, int64, error)

	// ValidateSubscriptionChange validates that a subscription change is acceptable
	// based on the user's current ledger balance and credit history
	ValidateSubscriptionChange(
		ctx context.Context,
		userID uint64,
		changeType SubscriptionChangeType,
		expectedAmount decimal.Decimal,
	) error

}

// DefaultSoftDeleteRetention is the default retention period for soft-deleted credits (30 days)
const DefaultSoftDeleteRetention = "720h"

// ReferenceType constants identify different event sources
// These are used for idempotency tracking and credit reference identification
const (
	// Gateway-specific event sources
	ReferenceTypeStripeInvoice = "stripe.invoice"
	ReferenceTypeAtlosPayment  = "atlos.payment"

	// System-generated reference types
	ReferenceTypeManual = "manual" // For admin-created manual adjustments
	ReferenceTypeUsage  = "usage"  // For usage-based consumption entries
)

// TransactionType constants identify different transaction categories
const (
	// TransactionTypeCharge represents a credit entry for payments received from payment gateways
	// Indicates funds added to the user's ledger via successful invoice payments
	TransactionTypeCharge = "charge"

	// TransactionTypeRefund represents a refund that returns credits to the user
	// Typically issued as a debit to reduce the ledger balance when payments are reversed
	TransactionTypeRefund = "refund"

	// TransactionTypeUsage represents resource consumption that debits from the user's balance
	// Applied when users consume billable resources beyond their allocated amounts
	TransactionTypeUsage = "usage"

	// TransactionTypeManual represents manual adjustments made by administrators
	// Used for corrections, credit additions, or balance modifications outside automated systems
	TransactionTypeManual = "manual_adjustment"

	// TransactionTypePromo represents promotional credits applied to user accounts
	// Issued as a credit for marketing campaigns, signup bonuses, or promotional offers
	TransactionTypePromo = "promo"

	// TransactionTypeTime represents subscription time or billing period costs
	// Debited from the user's ledger when a subscription billing period is consumed
	TransactionTypeTime = "time"

	// TransactionTypeChargeBack represents a disputed payment reversal
	// Issued as a debit when a payment is successfully disputed by the user through their bank
	TransactionTypeChargeBack = "charge_back"

	// TransactionTypeComp represents complimentary or compensation credits
	// Issued for customer service gestures, goodwill credits, or subscription cancellation prorations
	TransactionTypeComp = "comp"
)

// Direction constants
const (
	DirectionCredit = "credit"
	DirectionDebit  = "debit"
)
