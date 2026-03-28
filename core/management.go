package core

import (
	"context"
	"time"
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

// SubscriptionExecutor defines the interface for executing subscription operations
// This is used by gateways that require backend API calls for management operations
type SubscriptionExecutor interface {
	// ExecuteCancel cancels the subscription through the gateway's API
	// and updates the local subscriber state
	ExecuteCancel(ctx context.Context, userID uint) error
}

// ManagementCapabilities describes what management operations a gateway supports
type ManagementCapabilities struct {
	// ManagementMode indicates how the gateway handles management operations
	ManagementMode ManagementMode

	// Operations maps operation types to support status
	Operations map[ManagementOperation]bool
}
