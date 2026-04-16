package dto

import (
	"time"

	"github.com/shopspring/decimal"
	"go.lumeweb.com/httputil"
	z "github.com/Oudwins/zog"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
)

var (
	_ httputil.DTOResponse[*pluginCore.ManagementCapabilities] = (*ManagementCapabilitiesResponse)(nil)
	_ httputil.DTOResponse[*pluginCore.ManagementResult] = (*ManagementResultResponse)(nil)
	_ httputil.DTOResponse[*pluginCore.PlanChangeResult] = (*PlanChangeResultResponse)(nil)
)

// ManagementCapabilitiesResponse represents the subscription management capabilities of a gateway
type ManagementCapabilitiesResponse struct {
	ManagementMode string            `json:"management_mode"`
	Operations     map[string]bool   `json:"operations"`
}

// FromModel converts ManagementCapabilities to response DTO
func (r *ManagementCapabilitiesResponse) FromModel(capabilities *pluginCore.ManagementCapabilities) error {
	if capabilities == nil {
		return nil
	}

	// Copy management mode
	r.ManagementMode = string(capabilities.ManagementMode)

	// Convert operations map
	r.Operations = make(map[string]bool)
	for op, supported := range capabilities.Operations {
		r.Operations[string(op)] = supported
	}

	return nil
}

// AdminChangePlanRequest represents an admin request to change a user's plan
type AdminChangePlanRequest struct {
	PeriodID uint `json:"period_id"`
}

// Schema returns the validation schema for AdminChangePlanRequest
func (r *AdminChangePlanRequest) Schema() *z.StructSchema {
	return z.Struct(z.Shape{
		"PeriodID": z.UintLike[uint]().Required(),
	})
}

// ToModel returns the request as-is (no transformation needed)
func (r *AdminChangePlanRequest) ToModel() (*AdminChangePlanRequest, error) {
	return r, nil
}

// CancellationMode represents the mode for subscription cancellation
type CancellationMode string

const (
	// CancellationModeGateway delegates cancellation to the payment gateway
	CancellationModeGateway CancellationMode = "gateway"
	// CancellationModeDatabase performs cancellation in the database only
	CancellationModeDatabase CancellationMode = "database"
)

// AdminCancelSubscriptionRequest represents an admin request to cancel a subscription
type AdminCancelSubscriptionRequest struct {
	Mode *CancellationMode `json:"mode,omitempty"`
}

// Schema returns the validation schema for AdminCancelSubscriptionRequest
func (r *AdminCancelSubscriptionRequest) Schema() *z.StructSchema {
	return z.Struct(z.Shape{
		"Mode": z.Ptr(z.StringLike[CancellationMode]().OneOf([]CancellationMode{CancellationModeGateway, CancellationModeDatabase})),
	})
}

// ToModel returns the request as-is (no transformation needed)
func (r *AdminCancelSubscriptionRequest) ToModel() (*AdminCancelSubscriptionRequest, error) {
	return r, nil
}




// ManagementResultResponse represents the result of a subscription management operation
type ManagementResultResponse struct {
	Action               pluginCore.ManagementAction `json:"action"`
	URL                  string                       `json:"url,omitempty"`
	APIEndpoint          *APIEndpointInfoResponse     `json:"api_endpoint,omitempty"`
	ErrorMessage         string                       `json:"error_message,omitempty"`
	RequiresConfirmation bool                         `json:"requires_confirmation"`
	ConfirmationMessage  string                       `json:"confirmation_message,omitempty"`
	EffectiveTime        *time.Time                   `json:"effective_time,omitempty"`
}

// APIEndpointInfoResponse represents an API endpoint for management operations
type APIEndpointInfoResponse struct {
	Method string `json:"method"`
	Path   string `json:"path"`
}

// FromModel converts ManagementResult to response DTO
func (r *ManagementResultResponse) FromModel(result *pluginCore.ManagementResult) error {
	if result == nil {
		return nil
	}

	r.Action = result.Action
	r.URL = result.URL
	r.ErrorMessage = result.ErrorMessage
	r.RequiresConfirmation = result.RequiresConfirmation
	r.ConfirmationMessage = result.ConfirmationMessage
	r.EffectiveTime = result.EffectiveTime

	// Populate APIEndpoint if present
	if result.APIEndpoint != nil {
		r.APIEndpoint = &APIEndpointInfoResponse{
			Method: result.APIEndpoint.Method,
			Path:   result.APIEndpoint.Path,
		}
	}

	return nil
}

// ManagementRequest represents a request for subscription management operation information
type ManagementRequest struct {
	Operation string `json:"operation"`
}

func (r *ManagementRequest) Schema() *z.StructSchema {
	return z.Struct(z.Shape{
		"Operation": z.String().Required().OneOf([]string{"cancel", "change_plan"}),
	})
}

func (r *ManagementRequest) ToModel() (*ManagementRequest, error) {
	return r, nil
}

// GetOperation returns the operation as a core.ManagementOperation
func (r *ManagementRequest) GetOperation() (*pluginCore.ManagementOperation, error) {
	if r.Operation == "" {
		return nil, nil
	}
	op := pluginCore.ManagementOperation(r.Operation)
	return &op, nil
}

// UIConfigResponse represents UI configuration for embedded management
type UIConfigResponse struct {
	UIType       string      `json:"ui_type"`
	Fields       []FormField `json:"fields,omitempty"`
	Instructions string      `json:"instructions,omitempty"`
	SubmitURL    string      `json:"submit_url,omitempty"`
	CancelURL    string      `json:"cancel_url,omitempty"`
}

// FormField represents a form field for management operations
type FormField struct {
	Name        string        `json:"name"`
	Type        string        `json:"type"`
	Label       string        `json:"label"`
	Required    bool          `json:"required"`
	Placeholder string        `json:"placeholder,omitempty"`
	Options     []FieldOption `json:"options,omitempty"`
}

// FieldOption represents a selectable option for form fields
type FieldOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// ChangePlanRequest represents a request to change the subscription plan
type ChangePlanRequest struct {
	PeriodID uint `json:"period_id"`
}

// Schema returns the validation schema for ChangePlanRequest
func (r *ChangePlanRequest) Schema() *z.StructSchema {
	return z.Struct(z.Shape{
		"PeriodID": z.UintLike[uint]().Required(),
	})
}

// ToModel returns the request as-is (no transformation needed)
func (r *ChangePlanRequest) ToModel() (*ChangePlanRequest, error) {
	return r, nil
}

// PlanChangeResultResponse represents the result of a plan change operation
type PlanChangeResultResponse struct {
	Action        string          `json:"action"`
	CheckoutLink  string          `json:"checkout_link,omitempty"`
	CreditApplied decimal.Decimal `json:"credit_applied"`
	ChargeDue     decimal.Decimal `json:"charge_due"`
	EffectiveDate *time.Time      `json:"effective_date,omitempty"`
}

// FromModel converts PlanChangeResult to response DTO
func (r *PlanChangeResultResponse) FromModel(result *pluginCore.PlanChangeResult) error {
	if result == nil {
		return nil
	}

	r.Action = string(result.Action)
	r.CheckoutLink = result.CheckoutLink
	r.CreditApplied = result.CreditApplied
	r.ChargeDue = result.ChargeDue
	r.EffectiveDate = result.EffectiveDate

	return nil
}
