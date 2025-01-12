package service

import (
	"context"
	"go.lumeweb.com/portal-plugin-billing/internal/api/messages"
	"go.lumeweb.com/portal-plugin-billing/internal/service"
	"go.lumeweb.com/portal/core"
)

const BILLING_SERVICE = service.BILLING_SERVICE

type BillingService interface {
	core.Service
	core.Configurable

	// Core subscription operations
	GetSubscription(ctx context.Context, userID uint) (*messages.SubscriptionResponse, error)
	UpdateSubscription(ctx context.Context, userID uint, planID string) error
	//CancelSubscription(ctx context.Context, userID uint, req *messages.CancellationRequest) error

	// Plan management
	GetPlans(ctx context.Context) ([]*messages.SubscriptionPlan, error)

	// Customer management
	//CreateCustomer(ctx context.Context, user *models.User) error
	//UpdateBillingInfo(ctx context.Context, userID uint, info *messages.BillingInfo) error

	// Usage limits
	//GetUserMaxStorage(userID uint) (uint64, error)
	//GetUserMaxUpload(userID uint) (uint64, error)
	//GetUserMaxDownload(userID uint) (uint64, error)
}

var _ BillingService = (*service.BillingServiceDefault)(nil)
