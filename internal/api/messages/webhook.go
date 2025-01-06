package messages

// WebhookEvent represents the structure of incoming webhook events
type WebhookEvent struct {
	Type    string      `json:"type"`
	Data    WebhookData `json:"data"`
	Created int64       `json:"created"`
}

// WebhookData contains the payload data for webhook events
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
	PaymentAttempt PaymentAttempt    `json:"payment_attempt"`
}

// PaymentAttempt contains details about a payment attempt
type PaymentAttempt struct {
	Status       string `json:"status"`
	ErrorMessage string `json:"error_message,omitempty"`
}
