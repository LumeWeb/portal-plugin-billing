package service

type PaymentCancelRequest struct {
	CancellationReason string `json:"cancellation_reason"`
}

type PaymentMetadata struct {
	SubscriptionID string `json:"subscription_id"`
	PlanID         string `json:"plan_id"`
}

// PaymentResponse represents the response from the payment creation API
type PaymentResponse struct {
	PaymentID    string `json:"payment_id"`
	ClientSecret string `json:"client_secret"`
}

// PaymentRequest represents the payment request payload
type PaymentRequest struct {
	Amount           float64         `json:"amount"`
	Currency         string          `json:"currency"`
	Confirm          bool            `json:"confirm"`
	Customer         Customer        `json:"customer"`
	Billing          CustomerBilling `json:"billing,omitempty"`
	Description      string          `json:"description"`
	Metadata         PaymentMetadata `json:"metadata"`
	SetupFutureUsage string          `json:"setup_future_usage"`
	PaymentType      string          `json:"payment_type"`
}

// Customer represents the customer information in the payment request
type Customer struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

type CustomerBilling struct {
	Address CustomerBillingAddress `json:"address"`
	Email   string                 `json:"email"`
}

type CustomerBillingAddress struct {
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	City      string `json:"city"`
	Country   string `json:"country"`
	Line1     string `json:"line1"`
	Zip       string `json:"zip"`
	State     string `json:"state"`
}
