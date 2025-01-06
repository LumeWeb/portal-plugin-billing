package service

import (
	"context"
	"go.lumeweb.com/portal-plugin-billing/internal/api/messages"
	"go.lumeweb.com/portal-plugin-billing/internal/service"
	"go.lumeweb.com/portal/core"
	"go.lumeweb.com/portal/db/models"
)

const BILLING_SERVICE = service.BILLING_SERVICE

type BillingService interface {
	core.Service
	core.Configurable

	// CreateCustomer creates a new customer
	CreateCustomer(ctx context.Context, user *models.User) error

	// CreateCustomerById creates a new customer by user id
	CreateCustomerById(ctx context.Context, userID uint) error

	// UpdateBillingInfo updates the billing info for a given user
	UpdateBillingInfo(ctx context.Context, userID uint, billingInfo *messages.BillingInfo) error

	// GetUserQuota returns the quota for a given user
	GetUserMaxStorage(userID uint) (uint64, error)

	// GetUserMaxUpload returns the max upload for a given user
	GetUserMaxUpload(userID uint) (uint64, error)

	// GetUserMaxDownload returns the max download for a given user
	GetUserMaxDownload(userID uint) (uint64, error)

	// GetPlans returns all available subscription plans
	GetPlans(ctx context.Context) ([]*messages.SubscriptionPlan, error)

	// GetSubscription returns the subscription for a given user
	GetSubscription(ctx context.Context, userID uint) (*messages.SubscriptionResponse, error)

	// ChangeSubscription changes the subscription for a given user
	ChangeSubscription(ctx context.Context, userID uint, planID string) error

	// ConnectSubscription connects a payment method to a user's subscription
	ConnectSubscription(ctx context.Context, userID uint, paymentMethodID string) error

	RequestPaymentMethodChange(ctx context.Context, userID uint) (*messages.RequestPaymentMethodChangeResponse, error)

	// CancelSubscription cancels a user's subscription
	CancelSubscription(ctx context.Context, userID uint, req *messages.CancellationRequest) (*messages.CancellationResponse, error)

	// GetSubscriptionManager returns the subscription manager instance
	GetSubscriptionManager() SubscriptionManager
}

type SubscriptionManager = service.SubscriptionManager

var _ BillingService = (*service.BillingServiceDefault)(nil)
