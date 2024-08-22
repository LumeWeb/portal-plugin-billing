package service

import (
	"context"
	"go.lumeweb.com/portal-plugin-billing/internal/service"
	"go.lumeweb.com/portal/core"
)

const BILLING_SERVICE = service.BILLING_SERVICE

type BillingService interface {
	core.Service
	core.Configurable

	// CreateCustomer creates a customer in the billing system
	CreateCustomer(ctx context.Context, userID uint) error

	// GetUserQuota returns the quota for a given user
	GetUserMaxStorage(ctx context.Context, userID uint) (uint64, error)

	// GetUserMaxUpload returns the max upload for a given user
	GetUserMaxUpload(ctx context.Context, userID uint) (uint64, error)

	// GetUserMaxDownload returns the max download for a given user
	GetUserMaxDownload(ctx context.Context, userID uint) (uint64, error)
}
