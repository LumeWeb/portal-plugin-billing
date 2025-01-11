package old

import (
	"context"
	"errors"
	"github.com/killbill/kbcli/v3/kbmodel"
	hyperswitchTypes "go.lumeweb.com/portal-plugin-billing/internal/client/hyperswitch"
	"time"
)

var (
	ErrInvalidSignature     = errors.New("invalid webhook signature")
	ErrRetryable            = errors.New("retryable error")
	ErrPaymentMethodInvalid = errors.New("invalid payment method")
	ErrSubscriptionNotFound = errors.New("subscription not found")
	ErrMetadataMissing      = errors.New("required metadata missing")
)

type SubscriptionLifecycle interface {
	// Initial Subscription
	CreateSubscription(ctx context.Context, userID uint, planID string) (*SubscriptionResponse, error)
	GetSubscriptionStatus(ctx context.Context, userID uint) (*SubscriptionStatus, error)

	// Payment Method Management
	UpdatePaymentMethod(ctx context.Context, userID uint) (*PaymentMethodResponse, error)
	DeletePaymentMethod(ctx context.Context, userID uint, paymentMethodID string) error

	// Plan Management
	ChangePlan(ctx context.Context, userID uint, newPlanID string) (*PlanChangeResponse, error)
	CancelSubscription(ctx context.Context, userID uint) error

	// Webhook Handling
	HandleWebhook(ctx context.Context, event *hyperswitchTypes.WebhookEvent, signature string) error

	// Validation
	ValidatePaymentMethod(ctx context.Context, paymentMethodID string) error

	// State Management
	handleStateTransition(ctx context.Context, sub *kbmodel.Subscription, newState kbmodel.SubscriptionStateEnum) error
	determineCancellationPolicy(sub *kbmodel.Subscription) string
	determineChangePolicy(sub *kbmodel.Subscription, newPlanID string) string
}

type SubscriptionStatus struct {
	Status            string
	PaymentStatus     string
	CurrentPeriodEnd  time.Time
	CancelAtPeriodEnd bool
}

type SubscriptionResponse struct {
	Subscription  *kbmodel.Subscription
	PaymentIntent *PaymentIntent
}

type PaymentMethodResponse struct {
	ClientSecret string
}

type PlanChangeResponse struct {
	ClientSecret string
	PriceChange  float64
	Status       string
}

type PaymentIntent struct {
	ID           string
	ClientSecret string
	Amount       float64
	Status       string
}
