package hyperswitch

import "time"

// PaymentRequest represents a request to create a payment
type PaymentRequest struct {
	Amount           float64         `json:"amount"`
	Currency         string          `json:"currency"`
	Confirm          bool            `json:"confirm"`
	Customer         Customer        `json:"customer"`
	Billing          CustomerBilling `json:"billing,omitempty"`
	Description      string          `json:"description"`
	Metadata         PaymentMetadata `json:"metadata"`
	SetupFutureUsage string          `json:"setup_future_usage,omitempty"`
	PaymentType      string          `json:"payment_type,omitempty"`
}

// PaymentResponse represents the response from payment creation
type PaymentResponse struct {
	PaymentID    string `json:"payment_id"`
	ClientSecret string `json:"client_secret"`
	Status       string `json:"status"`
}

// Payment represents a payment record
type Payment struct {
	ID           string    `json:"payment_id"`
	Amount       float64   `json:"amount"`
	Currency     string    `json:"currency"`
	Status       string    `json:"status"`
	Created      time.Time `json:"created"`
	CustomerID   string    `json:"customer_id"`
	Description  string    `json:"description"`
	Metadata     map[string]string `json:"metadata"`
}

// Customer represents customer information
type Customer struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// CustomerBilling represents billing information
type CustomerBilling struct {
	Address CustomerBillingAddress `json:"address"`
	Email   string                `json:"email"`
}

// CustomerBillingAddress represents a billing address
type CustomerBillingAddress struct {
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	City      string `json:"city"`
	Country   string `json:"country"`
	Line1     string `json:"line1"`
	Zip       string `json:"zip"`
	State     string `json:"state"`
}

// PaymentMetadata represents metadata for a payment
type PaymentMetadata struct {
	SubscriptionID string `json:"subscription_id"`
	PlanID         string `json:"plan_id"`
}

// PaymentMethodRequest represents a request to update payment method
type PaymentMethodRequest struct {
	CustomerID      string `json:"customer_id"`
	PaymentMethodID string `json:"payment_method_id"`
	IsDefault       bool   `json:"is_default"`
}

// PaymentMethodResponse represents response from payment method operations
type PaymentMethodResponse struct {
	CustomerID      string `json:"customer_id"`
	PaymentMethodID string `json:"payment_method_id"`
	Status          string `json:"status"`
}

// WebhookEvent represents an incoming webhook event from Hyperswitch
type WebhookEvent struct {
	Type    string      `json:"type"`
	Data    WebhookData `json:"data"`
	Created int64       `json:"created"`
	EventID string      `json:"event_id"`
}

// WebhookData represents the data payload in a webhook event
type WebhookData struct {
	PaymentID      string            `json:"payment_id"`
	Status         string            `json:"status"`
	Amount         float64           `json:"amount"`
	Currency       string            `json:"currency"`
	CustomerID     string            `json:"customer_id"`
	PaymentMethod  string            `json:"payment_method"`
	ErrorMessage   string            `json:"error_message,omitempty"`
	Metadata       map[string]string `json:"metadata"`
	Created        int64             `json:"created"`
	Updated        int64             `json:"updated"`
}
