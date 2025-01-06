package hyperswitch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.uber.org/zap"
)

// ClientConfig contains configuration for the Hyperswitch client
type ClientConfig struct {
	BaseURL       string
	APIKey        string
	MaxRetries    int
	RetryDelay    time.Duration
	RequestTimeout time.Duration
}

// Client handles communication with the Hyperswitch API
type Client struct {
	config     ClientConfig
	httpClient *http.Client
	logger     *zap.Logger
}

// NewClient creates a new Hyperswitch API client
func NewClient(config ClientConfig, logger *zap.Logger) *Client {
	// Set defaults if not specified
	if config.MaxRetries == 0 {
		config.MaxRetries = 3
	}
	if config.RetryDelay == 0 {
		config.RetryDelay = 1 * time.Second
	}
	if config.RequestTimeout == 0 {
		config.RequestTimeout = 30 * time.Second
	}

	return &Client{
		config: config,
		httpClient: &http.Client{
			Timeout: config.RequestTimeout,
		},
		logger: logger,
	}
}

// CreatePayment initiates a new payment
func (c *Client) CreatePayment(ctx context.Context, req *PaymentRequest) (*PaymentResponse, error) {
	c.logger.Info("creating payment",
		zap.Float64("amount", req.Amount),
		zap.String("currency", req.Currency),
		zap.String("customer_id", req.Customer.ID),
	)

	resp, err := c.makeRequest(ctx, http.MethodPost, "/payments", req)
	if err != nil {
		c.logger.Error("failed to create payment", zap.Error(err))
		return nil, fmt.Errorf("create payment failed: %w", err)
	}
	defer resp.Body.Close()

	var paymentResp PaymentResponse
	if err := json.NewDecoder(resp.Body).Decode(&paymentResp); err != nil {
		c.logger.Error("failed to decode payment response", zap.Error(err))
		return nil, fmt.Errorf("decode payment response failed: %w", err)
	}

	c.logger.Info("payment created successfully",
		zap.String("payment_id", paymentResp.PaymentID),
	)

	return &paymentResp, nil
}

// GetPayment retrieves payment details
func (c *Client) GetPayment(ctx context.Context, paymentID string) (*Payment, error) {
	c.logger.Info("fetching payment details", zap.String("payment_id", paymentID))

	resp, err := c.makeRequest(ctx, http.MethodGet, fmt.Sprintf("/payments/%s", paymentID), nil)
	if err != nil {
		c.logger.Error("failed to get payment", 
			zap.String("payment_id", paymentID),
			zap.Error(err),
		)
		return nil, fmt.Errorf("get payment failed: %w", err)
	}
	defer resp.Body.Close()

	var payment Payment
	if err := json.NewDecoder(resp.Body).Decode(&payment); err != nil {
		c.logger.Error("failed to decode payment response", zap.Error(err))
		return nil, fmt.Errorf("decode payment failed: %w", err)
	}

	c.logger.Debug("payment details retrieved",
		zap.String("payment_id", payment.ID),
		zap.String("status", payment.Status),
	)

	return &payment, nil
}

// UpdatePaymentMethod updates the payment method for a customer
func (c *Client) UpdatePaymentMethod(ctx context.Context, req *PaymentMethodRequest) (*PaymentMethodResponse, error) {
	url := fmt.Sprintf("%s/payment_methods", c.config.BaseURL)
	
	resp, err := c.makeRequest(ctx, http.MethodPost, url, req)
	if err != nil {
		return nil, fmt.Errorf("update payment method request failed: %w", err)
	}
	defer resp.Body.Close()

	var pmResp PaymentMethodResponse
	if err := json.NewDecoder(resp.Body).Decode(&pmResp); err != nil {
		return nil, fmt.Errorf("decode payment method response failed: %w", err)
	}

	return &pmResp, nil
}

// makeRequest performs the HTTP request with proper headers, logging and retries
func (c *Client) makeRequest(ctx context.Context, method, endpoint string, payload interface{}) (*http.Response, error) {
	url := fmt.Sprintf("%s%s", c.config.BaseURL, endpoint)
	var body io.Reader
	var payloadBytes []byte
	var err error

	if payload != nil {
		payloadBytes, err = json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("marshal payload failed: %w", err)
		}
		body = bytes.NewBuffer(payloadBytes)
	}

	c.logger.Debug("preparing hyperswitch request",
		zap.String("method", method),
		zap.String("url", url),
		zap.Any("payload", payload),
	)

	var resp *http.Response
	var lastErr error

	// Implement retry logic
	for attempt := 0; attempt <= c.config.MaxRetries; attempt++ {
		if attempt > 0 {
			// Log retry attempt
			c.logger.Info("retrying request",
				zap.String("url", url),
				zap.Int("attempt", attempt),
				zap.Error(lastErr),
			)

			// Reset request body for retry
			if payload != nil {
				body = bytes.NewBuffer(payloadBytes)
			}

			// Wait before retry with exponential backoff
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(c.config.RetryDelay * time.Duration(attempt)):
			}
		}

		req, err := http.NewRequestWithContext(ctx, method, url, body)
		if err != nil {
			lastErr = fmt.Errorf("create request failed: %w", err)
			continue
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		req.Header.Set("api-key", c.config.APIKey)

		resp, err = c.httpClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("request failed: %w", err)
			continue
		}

		// Don't retry on client errors (4xx)
		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			respBody, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return nil, &APIError{
				StatusCode: resp.StatusCode,
				Message:    string(respBody),
			}
		}

		// Retry on server errors (5xx)
		if resp.StatusCode >= 500 {
			respBody, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			lastErr = &APIError{
				StatusCode: resp.StatusCode,
				Message:    string(respBody),
			}
			continue
		}

		// Success
		return resp, nil
	}

	return nil, fmt.Errorf("max retries exceeded: %w", lastErr)
}

// APIError represents an error returned by the Hyperswitch API
type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("hyperswitch API error (status %d): %s", e.StatusCode, e.Message)
}
