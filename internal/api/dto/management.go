package dto

import (
	"time"

	"go.lumeweb.com/httputil"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
)

var (
	_ httputil.DTOResponse[*pluginCore.ManagementCapabilities] = (*ManagementCapabilitiesResponse)(nil)
	_ httputil.DTOResponse[*pluginCore.ManagementResult] = (*ManagementResultResponse)(nil)
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

func (r *ManagementRequest) Schema() interface{} {
	return map[string]any{
		"operation": map[string]any{
			"type":        "string",
			"enum":        []string{"cancel", "change_plan"},
			"description": "The management operation to perform",
		},
	}
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
