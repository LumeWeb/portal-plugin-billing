package service

import (
	"go.lumeweb.com/portal-plugin-billing/internal/service"
	"go.lumeweb.com/portal/core"
)

const QUOTA_SERVICE = service.QUOTA_SERVICE

type QuotaService interface {
	core.Service
	core.Configurable

	// RecordDownload records a download for a user
	RecordDownload(uploadID, userID uint, bytes uint64, ip string) error

	// CheckStorageQuota checks if a user has enough storage quota for a requested number of bytes
	CheckStorageQuota(userID uint, requestedBytes uint64) (bool, error)

	// CheckUploadQuota checks if a user has enough upload quota for a requested number of bytes
	CheckUploadQuota(userID uint, requestedBytes uint64) (bool, error)

	// CheckDownloadQuota checks if a user has enough download quota for a requested number of bytes
	CheckDownloadQuota(userID uint, requestedBytes uint64) (bool, error)

	// Reconcile reconciles the quota usage for the previous day
	Reconcile() error
}

var _ QuotaService = (*service.QuotaServiceDefault)(nil)
