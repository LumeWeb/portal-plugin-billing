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
		creditType string,
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
		creditType string,
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
		creditType string,
		amount decimal.Decimal,
		referenceID string,
		description string,
		createdBy uint64,
	) error

	// ListCredits retrieves credits with filtering, sorting, and pagination
	// Service-level method that wraps repository GetCredits with logging
	ListCredits(ctx context.Context, filters []queryutil.CrudFilter, sorts []queryutil.Sort, pagination queryutil.Pagination) ([]ledger.Credit, int64, error)


}

// DefaultSoftDeleteRetention is the default retention period for soft-deleted credits (30 days)
const DefaultSoftDeleteRetention = "720h"

// ReferenceType constants identify different event sources
// These are used for idempotency tracking and credit reference identification
const (
	ReferenceTypeStripeInvoice = "stripe.invoice"
	ReferenceTypeAtlosPayment  = "atlos.payment"
)

// CreditType constants identify different credit categories
const (
	CreditTypeCharge     = "charge"
	CreditTypeRefund     = "refund"
	CreditTypeUsage      = "usage"
	CreditTypeManual     = "manual_adjustment"
	CreditTypePromo      = "promo"
	CreditTypeTime       = "time"
	CreditTypeChargeBack = "charge_back"
	CreditTypeComp       = "comp"
)

// Direction constants
const (
	DirectionCredit = "credit"
	DirectionDebit  = "debit"
)
