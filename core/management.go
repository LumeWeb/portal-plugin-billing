package core

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
)

// ManagementOperation represents different subscription management operations
type ManagementOperation string

const (
	// OperationCancel initiates subscription cancellation
	OperationCancel ManagementOperation = "cancel"

	// OperationChangePlan allows plan upgrades or downgrades
	OperationChangePlan ManagementOperation = "change_plan"
)

// Predefined management endpoint paths
const (
	CancelEndpointPath     = "/api/account/billing/cancel"
	AbortCancelEndpointPath = "/api/account/billing/cancel/abort"
	ChangePlanEndpointPath = "/api/account/billing/change-plan"
)

// ManagementMode defines how a gateway handles subscription management
type ManagementMode string

const (
	// ModePortal indicates the gateway uses a customer portal for all operations
	// UI should redirect users to the portal URL
	ModePortal ManagementMode = "portal"

	// ModeAPI indicates the gateway requires API calls for operations
	// UI must implement its own management interface
	ModeAPI ManagementMode = "api"
)

// ManagementAction defines how the UI should handle a management operation result
type ManagementAction string

const (
	// ActionRedirect indicates the user should be redirected to a URL
	ActionRedirect ManagementAction = "redirect"

	// ActionShowUI indicates the gateway provides UI data to display
	ActionShowUI ManagementAction = "show_ui"

	// ActionAPIRequired indicates the UI must call a specific API endpoint
	ActionAPIRequired ManagementAction = "api_required"

	// ActionUnsupported indicates the operation is not supported
	ActionUnsupported ManagementAction = "unsupported"

	// ActionError indicates an error occurred processing the request
	ActionError ManagementAction = "error"
)

// CancellationStatus represents the state of a cancellation operation
type CancellationStatus string

const (
	// CancellationStatusScheduled indicates cancellation is scheduled for end of billing period
	CancellationStatusScheduled CancellationStatus = "scheduled"
	// CancellationStatusImmediate indicates cancellation was processed immediately
	CancellationStatusImmediate CancellationStatus = "immediate"
	// CancellationStatusPortal indicates user was redirected to portal for cancellation
	CancellationStatusPortal CancellationStatus = "portal"
	// CancellationStatusCompleted indicates cancellation has been fully processed
	CancellationStatusCompleted CancellationStatus = "completed"
	// CancellationStatusAborted indicates a scheduled cancellation was aborted
	CancellationStatusAborted CancellationStatus = "aborted"
)

// CancellationResult contains the result of a cancellation operation
type CancellationResult struct {
	// Status indicates the type of cancellation
	Status CancellationStatus
	// EffectiveAt is when the cancellation takes effect (nil for portal redirects)
	EffectiveAt *time.Time
	// CanAbort indicates whether this cancellation can be reversed
	CanAbort bool
}

// ManagementResult contains the result of a management operation
type ManagementResult struct {
	// Action tells the UI how to handle this result
	Action ManagementAction

	// URL is the redirect target for ActionRedirect
	URL string

	// UIConfig contains configuration for embedded UI (for ActionShowUI)
	UIConfig *ManagementUIConfig

	// APIEndpoint is the endpoint to call for ActionAPIRequired
	APIEndpoint *APIEndpointInfo

	// ErrorMessage contains any error details
	ErrorMessage string

	// RequiresConfirmation indicates user must confirm before proceeding
	RequiresConfirmation bool

	// ConfirmationMessage is the text to show in confirmation dialog
	ConfirmationMessage string

	// EffectiveTime is when the operation takes effect (for cancellations)
	EffectiveTime *time.Time

	// Status indicates the current status of the operation
	Status string

	// CanAbort indicates whether this operation can be reversed
	CanAbort bool
}

// ManagementUIConfig contains configuration for embedded management UI
type ManagementUIConfig struct {
	// Type of UI to display
	UIType string

	// Form fields required for this operation
	Fields []FormField

	// Instructions to display to the user
	Instructions string

	// SubmitURL where the completed form should be submitted
	SubmitURL string

	// CancelURL where to redirect if user cancels
	CancelURL string
}

// FormField represents a form field for management operations
type FormField struct {
	Name        string
	Type        string
	Label       string
	Required    bool
	Placeholder string
	Options     []FieldOption
}

// FieldOption represents a selectable option for form fields
type FieldOption struct {
	Value string
	Label string
}

// APIEndpointInfo describes an API endpoint for management operations
type APIEndpointInfo struct {
	// HTTP method (GET, POST, DELETE, etc.)
	Method string

	// Endpoint URL path
	Path string
}

// predefinedManagementEndpoints defines the API endpoints for each management operation
var predefinedManagementEndpoints = map[ManagementOperation]*APIEndpointInfo{
	OperationCancel: {
		Method: "POST",
		Path:   CancelEndpointPath,
	},
	OperationChangePlan: {
		Method: "POST",
		Path:   ChangePlanEndpointPath,
	},
}

// GetManagementAPIEndpoint returns the predefined API endpoint for a management operation
// Returns nil if the operation does not have a predefined endpoint
func GetManagementAPIEndpoint(operation ManagementOperation) *APIEndpointInfo {
	if endpoint, exists := predefinedManagementEndpoints[operation]; exists {
		return endpoint
	}
	return nil
}

// SubscriptionManager defines the interface for subscription management operations
type SubscriptionManager interface {
	// GetManagementInfo returns management capabilities for operations
	GetManagementInfo(ctx context.Context, userID uint) (*ManagementCapabilities, error)

	// GetManagementURL returns the appropriate action for a management operation
	GetManagementURL(ctx context.Context, userID uint, operation ManagementOperation) (*ManagementResult, error)
}

// PlanChangeAction defines how the UI should handle a plan change result
type PlanChangeAction string

const (
	// PlanChangeActionCheckoutRequired indicates the user must complete a checkout to apply the plan change
	PlanChangeActionCheckoutRequired PlanChangeAction = "checkout_required"
	// PlanChangeActionComplete indicates the plan change was completed immediately
	PlanChangeActionComplete PlanChangeAction = "complete"
	// PlanChangeActionPending indicates the plan change is pending (e.g., awaiting webhook confirmation)
	PlanChangeActionPending PlanChangeAction = "pending"
)

// PlanChangeResult contains the result of a plan change operation
type PlanChangeResult struct {
	// Action tells the UI how to handle this result
	Action PlanChangeAction

	// CheckoutLink contains the checkout session ID for ActionCheckoutRequired
	CheckoutLink string

	// CreditApplied is the amount of credit issued for unused time in the previous plan
	CreditApplied decimal.Decimal

	// ChargeDue is the amount the customer needs to pay (may be zero or negative)
	ChargeDue decimal.Decimal

	// EffectiveDate is when the plan change takes effect
	EffectiveDate *time.Time
}

// SubscriptionExecutor defines the interface for executing subscription operations
// This is used by gateways that require backend API calls for management operations
type SubscriptionExecutor interface {
	// ExecuteCancel cancels the subscription through the gateway's API
	// and updates the local subscriber state.
	// The immediate parameter determines whether to cancel immediately or schedule
	// cancellation at the end of the billing period.
	// Returns a CancellationResult indicating whether cancellation is scheduled
	// (at end of billing period) or immediate, and whether it can be aborted.
	ExecuteCancel(ctx context.Context, userID uint, immediate bool) (*CancellationResult, error)

	// ExecutePlanChange executes a plan change operation
	// For gateways that don't support direct plan updates (like ATLOS), this involves:
	// - Calculating proration between old and new plans
	// - Issuing credit for unused time in the old plan
	// - Canceling the old subscription
	// - Returning checkout UI for the new plan
	ExecutePlanChange(ctx context.Context, userID uint, newPeriodID uint) (*PlanChangeResult, error)

	// ReconcileCancellation handles pending subscription cancellations that were scheduled
	// for a future date. This method is called by the cancellation reconciliation cron job
	// to process subscriptions where WillCancelAt has been reached. Gateways should implement
	// this to:
	// - Verify the cancellation status with the gateway
	// - Deactivate the subscriber locally
	// - Issue any applicable credits
	// - Update the subscriber's CancelledAt field
	ReconcileCancellation(ctx context.Context, userID uint) error

	// AbortCancellation cancels a scheduled subscription cancellation, restoring
	// the subscription to active status. Returns an error if no scheduled
	// cancellation exists or if the gateway doesn't support abort.
	AbortCancellation(ctx context.Context, userID uint) error
}

// ManagementCapabilities describes what management operations a gateway supports
type ManagementCapabilities struct {
	// ManagementMode indicates how the gateway handles management operations for users.
	// ModePortal: users are redirected to gateway portal
	// ModeAPI: users call our API which calls gateway backend
	ManagementMode ManagementMode

	// Operations maps operation types to support status for USER context.
	// These are discovered via GetManagementURL and executed by users.
	Operations map[ManagementOperation]bool

	// AdminOperations maps operation types to support status for ADMIN context.
	// These are backend API calls that admins can execute directly.
	// Portal-mode gateways may still support admin backend operations.
	AdminOperations map[ManagementOperation]bool
}
