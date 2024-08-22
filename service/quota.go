package service

import (
	"go.lumeweb.com/portal-plugin-billing/internal/service"
	"go.lumeweb.com/portal/core"
)

const QUOTA_SERVICE = service.QUOTA_SERVICE

type BillingService interface {
	core.Service
	core.Configurable

	// GetUserQuota returns the quota for a given user
	GetUserStorageQuota(ctx core.Context, userID uint) (uint64, error)
	GetUserUploadQuota(ctx core.Context, userID uint) (uint64, error)
	GetUserDownloadQuota(ctx core.Context, userID uint) (uint64, error)
}
