package dto

// CheckoutUIRequest represents a request for checkout UI fragments
type CheckoutUIRequest struct {
	PlanID      uint   `json:"plan_id" validate:"required"`
	GatewayType string `json:"gateway_type,omitempty"` // e.g., "stripe", "paypal"
}
