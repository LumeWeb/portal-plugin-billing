package service

import (
	"go.lumeweb.com/portal-plugin-billing/internal/service"
	"go.lumeweb.com/portal/core"
)

const BILLING_SERVICE = service.BILLING_SERVICE

type QuotaService interface {
	core.Service
	core.Configurable

	// RecordDownload records a download for a user
	RecordDownload(ctx core.Context, uploadID, userID uint, bytes uint64, ip string) error

	// CheckQuota checks if a user has enough quota for a requested number of bytes
	CheckQuota(ctx core.Context, userID uint, requestedBytes uint64) (bool, error)

	// Reconcile reconciles the quota usage for the previous day
	Reconcile(ctx core.Context) error
}
