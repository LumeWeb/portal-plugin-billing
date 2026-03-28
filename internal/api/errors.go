package api

import (
	"encoding/json"
	"net/http"
	"strings"

	router "go.lumeweb.com/portal-router"
	core "go.lumeweb.com/portal/core"
)

// Error keys
const (
	Namespace = "billing"

	// Service errors
	ErrKeyBillingServiceNotAvailable    core.ErrorType = "BILLING_SERVICE_NOT_AVAILABLE"
	ErrKeyPricingServiceNotAvailable    core.ErrorType = "PRICING_SERVICE_NOT_AVAILABLE"
	ErrKeyGatewayRegistryNotInitialized core.ErrorType = "GATEWAY_REGISTRY_NOT_INITIALIZED"

	// Authentication/Authorization errors
	ErrKeyUnauthorized     core.ErrorType = "UNAUTHORIZED"
	ErrKeyPermissionDenied core.ErrorType = "PERMISSION_DENIED"

	// Validation errors
	ErrKeyInvalidRequest     core.ErrorType = "INVALID_REQUEST"
	ErrKeyInvalidIdentifier  core.ErrorType = "INVALID_IDENTIFIER"
	ErrKeyInvalidPlanID      core.ErrorType = "INVALID_PLAN_ID"
	ErrKeyInvalidPriceLineID core.ErrorType = "INVALID_PRICE_LINE_ID"

	// Billing/Subscription errors
	ErrKeySubscriptionCheckFailed core.ErrorType = "SUBSCRIPTION_CHECK_FAILED"
	ErrKeyNoActiveSubscription    core.ErrorType = "NO_ACTIVE_SUBSCRIPTION"
	ErrKeyPaymentGatewayFailed    core.ErrorType = "PAYMENT_GATEWAY_FAILED"

	// Webhook errors
	ErrKeyPayloadTooLarge          core.ErrorType = "PAYLOAD_TOO_LARGE"
	ErrKeyWebhookPayloadReadFailed core.ErrorType = "WEBHOOK_PAYLOAD_READ_FAILED"
	ErrKeySignatureHeaderFailed    core.ErrorType = "SIGNATURE_HEADER_FAILED"
	ErrKeyMissingSignatureHeader   core.ErrorType = "MISSING_SIGNATURE_HEADER"
	ErrKeyWebhookProcessFailed     core.ErrorType = "WEBHOOK_PROCESS_FAILED"
	ErrKeyGatewayTypeRequired      core.ErrorType = "GATEWAY_TYPE_REQUIRED"

	// Pricing plan errors
	ErrKeyPricingPlanNotFound     core.ErrorType = "PRICING_PLAN_NOT_FOUND"
	ErrKeyPricingPlanCreateFailed core.ErrorType = "PRICING_PLAN_CREATE_FAILED"
	ErrKeyPricingPlanUpdateFailed core.ErrorType = "PRICING_PLAN_UPDATE_FAILED"
	ErrKeyPricingPlanDeleteFailed core.ErrorType = "PRICING_PLAN_DELETE_FAILED"

	// Price line errors
	ErrKeyPriceLineNotFound     core.ErrorType = "PRICE_LINE_NOT_FOUND"
	ErrKeyPriceLineCreateFailed core.ErrorType = "PRICE_LINE_CREATE_FAILED"
	ErrKeyPriceLineUpdateFailed core.ErrorType = "PRICE_LINE_UPDATE_FAILED"
	ErrKeyPriceLineDeleteFailed core.ErrorType = "PRICE_LINE_DELETE_FAILED"

	// Gateway errors
	ErrKeyGatewayNotFound           core.ErrorType = "GATEWAY_NOT_FOUND"
	ErrKeyGatewayLogoNotFound       core.ErrorType = "GATEWAY_LOGO_NOT_FOUND"

	// Checkout errors
	ErrKeyCheckoutSubscriptionActive    core.ErrorType = "CHECKOUT_SUBSCRIPTION_ACTIVE"
	ErrKeyCheckoutUIGenerationFailed   core.ErrorType = "CHECKOUT_UI_GENERATION_FAILED"

	// Management errors
	ErrKeyManagementCapabilitiesFailed  core.ErrorType = "MANAGEMENT_CAPABILITIES_FAILED"
	ErrKeyManagementOperationFailed    core.ErrorType = "MANAGEMENT_OPERATION_FAILED"
)

var _ router.ResponseError = (*BillingError)(nil)

// ErrorDetails represents the structured error response format
type ErrorDetails struct {
	Reason  string `json:"reason"`
	Details string `json:"details,omitempty"`
}

// ErrorWrapper wraps ErrorDetails for custom JSON marshaling
type ErrorWrapper struct {
	Error ErrorDetails `json:"error"`
}

// BillingError represents a Billing-specific error that can be marshaled to JSON
type BillingError struct {
	coreErr *core.Error
}

// MarshalJSON implements json.Marshaler interface
func (e *BillingError) MarshalJSON() ([]byte, error) {
	if e == nil || e.coreErr == nil {
		return json.Marshal(ErrorWrapper{Error: ErrorDetails{Reason: "Unknown"}})
	}
	reason := string(e.coreErr.Key)

	// First strip "ErrKey" prefix if present
	if strings.HasPrefix(reason, "ErrKey") {
		reason = reason[6:] // Strip "ErrKey" prefix
	}

	// Then strip "Err" prefix if present
	if strings.HasPrefix(reason, "Err") {
		reason = reason[3:] // Strip "Err" prefix
	}

	details := ErrorDetails{
		Reason:  reason,
		Details: e.coreErr.Message,
	}

	wrapper := ErrorWrapper{Error: details}
	return json.Marshal(wrapper)
}

func (e *BillingError) Error() string {
	return e.coreErr.Error()
}

func (e *BillingError) HttpStatus() int {
	return e.coreErr.HttpStatus()
}

// Unwrap exposes the underlying core.Error for errors.Is/As.
func (e *BillingError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.coreErr
}

func init() {
	core.MustRegisterNamespace(Namespace)
	core.MustRegisterDefaultErrorMessages(Namespace, map[core.ErrorType]core.ErrorDefinition{
		// Service errors
		ErrKeyBillingServiceNotAvailable:    {Key: ErrKeyBillingServiceNotAvailable, Message: "Billing service not available"},
		ErrKeyPricingServiceNotAvailable:    {Key: ErrKeyPricingServiceNotAvailable, Message: "Pricing service not available"},
		ErrKeyGatewayRegistryNotInitialized: {Key: ErrKeyGatewayRegistryNotInitialized, Message: "Gateway registry not initialized"},

		// Authentication/Authorization errors
		ErrKeyUnauthorized:     {Key: ErrKeyUnauthorized, Message: "Access denied. Please check your credentials and try again."},
		ErrKeyPermissionDenied: {Key: ErrKeyPermissionDenied, Message: "You don't have permission to perform this action"},

		// Validation errors
		ErrKeyInvalidRequest:     {Key: ErrKeyInvalidRequest, Message: "Invalid request parameter: %s"},
		ErrKeyInvalidIdentifier:  {Key: ErrKeyInvalidIdentifier, Message: "Invalid identifier format"},
		ErrKeyInvalidPlanID:      {Key: ErrKeyInvalidPlanID, Message: "Invalid plan ID format"},
		ErrKeyInvalidPriceLineID: {Key: ErrKeyInvalidPriceLineID, Message: "Invalid price line ID format"},

		// Billing/Subscription errors
		ErrKeySubscriptionCheckFailed:      {Key: ErrKeySubscriptionCheckFailed, Message: "Failed to check subscription status"},
		ErrKeyNoActiveSubscription:         {Key: ErrKeyNoActiveSubscription, Message: "No active subscription found"},
		ErrKeyCheckoutSubscriptionActive:   {Key: ErrKeyCheckoutSubscriptionActive, Message: "Checkout subscription already active"},
		ErrKeyCheckoutUIGenerationFailed:  {Key: ErrKeyCheckoutUIGenerationFailed, Message: "Failed to generate checkout UI"},
		ErrKeyPaymentGatewayFailed:         {Key: ErrKeyPaymentGatewayFailed, Message: "Failed to get payment gateway"},

		// Webhook errors
		ErrKeyPayloadTooLarge:          {Key: ErrKeyPayloadTooLarge, Message: "Payload too large"},
		ErrKeyWebhookPayloadReadFailed: {Key: ErrKeyWebhookPayloadReadFailed, Message: "Failed to read webhook payload"},
		ErrKeySignatureHeaderFailed:    {Key: ErrKeySignatureHeaderFailed, Message: "Failed to get signature header"},
		ErrKeyMissingSignatureHeader:   {Key: ErrKeyMissingSignatureHeader, Message: "Missing signature header"},
		ErrKeyWebhookProcessFailed:     {Key: ErrKeyWebhookProcessFailed, Message: "Failed to process webhook"},
		ErrKeyGatewayTypeRequired:      {Key: ErrKeyGatewayTypeRequired, Message: "Gateway type is required"},

		// Pricing plan errors
		ErrKeyPricingPlanNotFound:     {Key: ErrKeyPricingPlanNotFound, Message: "Pricing plan not found"},
		ErrKeyPricingPlanCreateFailed: {Key: ErrKeyPricingPlanCreateFailed, Message: "Failed to create pricing plan"},
		ErrKeyPricingPlanUpdateFailed: {Key: ErrKeyPricingPlanUpdateFailed, Message: "Failed to update pricing plan"},
		ErrKeyPricingPlanDeleteFailed: {Key: ErrKeyPricingPlanDeleteFailed, Message: "Failed to delete pricing plan"},

		// Price line errors
		ErrKeyPriceLineNotFound:     {Key: ErrKeyPriceLineNotFound, Message: "Price line not found"},
		ErrKeyPriceLineCreateFailed: {Key: ErrKeyPriceLineCreateFailed, Message: "Failed to create price line"},
		ErrKeyPriceLineUpdateFailed: {Key: ErrKeyPriceLineUpdateFailed, Message: "Failed to update price line"},
		ErrKeyPriceLineDeleteFailed: {Key: ErrKeyPriceLineDeleteFailed, Message: "Failed to delete price line"},

		// Gateway errors
		ErrKeyGatewayNotFound:     {Key: ErrKeyGatewayNotFound, Message: "Gateway not found"},
		ErrKeyGatewayLogoNotFound: {Key: ErrKeyGatewayLogoNotFound, Message: "Gateway logo not found"},

		// Management errors
		ErrKeyManagementCapabilitiesFailed: {Key: ErrKeyManagementCapabilitiesFailed, Message: "Failed to get management capabilities"},
		ErrKeyManagementOperationFailed:    {Key: ErrKeyManagementOperationFailed, Message: "Management operation failed"},
	})

	core.MustRegisterErrorCodes(Namespace, map[core.ErrorType]int{
		// Service errors
		ErrKeyBillingServiceNotAvailable:    http.StatusServiceUnavailable,
		ErrKeyPricingServiceNotAvailable:    http.StatusServiceUnavailable,
		ErrKeyGatewayRegistryNotInitialized: http.StatusInternalServerError,

		// Authentication/Authorization errors
		ErrKeyUnauthorized:     http.StatusUnauthorized,
		ErrKeyPermissionDenied: http.StatusForbidden,

		// Validation errors
		ErrKeyInvalidRequest:     http.StatusBadRequest,
		ErrKeyInvalidIdentifier:  http.StatusUnprocessableEntity,
		ErrKeyInvalidPlanID:      http.StatusBadRequest,
		ErrKeyInvalidPriceLineID: http.StatusBadRequest,

		// Billing/Subscription errors
		ErrKeySubscriptionCheckFailed:     http.StatusInternalServerError,
		ErrKeyNoActiveSubscription:        http.StatusNotFound,
		ErrKeyCheckoutSubscriptionActive:  http.StatusConflict,
		ErrKeyCheckoutUIGenerationFailed: http.StatusInternalServerError,
		ErrKeyPaymentGatewayFailed:        http.StatusInternalServerError,

		// Webhook errors
		ErrKeyPayloadTooLarge:          http.StatusRequestEntityTooLarge,
		ErrKeyWebhookPayloadReadFailed: http.StatusBadRequest,
		ErrKeySignatureHeaderFailed:    http.StatusBadRequest,
		ErrKeyMissingSignatureHeader:   http.StatusBadRequest,
		ErrKeyWebhookProcessFailed:     http.StatusBadRequest,
		ErrKeyGatewayTypeRequired:      http.StatusBadRequest,

		// Pricing plan errors
		ErrKeyPricingPlanNotFound:     http.StatusNotFound,
		ErrKeyPricingPlanCreateFailed: http.StatusInternalServerError,
		ErrKeyPricingPlanUpdateFailed: http.StatusInternalServerError,
		ErrKeyPricingPlanDeleteFailed: http.StatusInternalServerError,

		// Price line errors
		ErrKeyPriceLineNotFound:     http.StatusNotFound,
		ErrKeyPriceLineCreateFailed: http.StatusInternalServerError,
		ErrKeyPriceLineUpdateFailed: http.StatusInternalServerError,
		ErrKeyPriceLineDeleteFailed: http.StatusInternalServerError,

		// Gateway errors
		ErrKeyGatewayNotFound:     http.StatusNotFound,
		ErrKeyGatewayLogoNotFound: http.StatusNotFound,

		// Management errors
		ErrKeyManagementCapabilitiesFailed: http.StatusInternalServerError,
		ErrKeyManagementOperationFailed:    http.StatusInternalServerError,
	})
}

func NewError(key core.ErrorType, err error, args ...any) *BillingError {
	return &BillingError{core.NewError(Namespace, key, err, args...)}
}
