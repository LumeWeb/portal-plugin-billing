package service

import (
	"go.lumeweb.com/portal-plugin-billing/internal/api/messages"
	"go.lumeweb.com/portal-plugin-billing/internal/service"
	"go.lumeweb.com/portal/core"
)

const QUOTA_SERVICE = service.QUOTA_SERVICE

type QuotaService interface {
	core.Service
	core.Configurable

	// RecordDownload records a download for a user
	RecordDownload(uploadID, userID uint, bytes uint64, ip string) error

	// RecordUpload records an upload for a user
	RecordUpload(uploadID, userID uint, bytes uint64, ip string) error

	// CheckStorageQuota checks if a user has enough storage quota for a requested number of bytes
	CheckStorageQuota(userID uint, requestedBytes uint64) (bool, error)

	// CheckUploadQuota checks if a user has enough upload quota for a requested number of bytes
	CheckUploadQuota(userID uint, requestedBytes uint64) (bool, error)

	// CheckDownloadQuota checks if a user has enough download quota for a requested number of bytes
	CheckDownloadQuota(userID uint, requestedBytes uint64) (bool, error)

	// Reconcile reconciles the quota usage for the previous day
	Reconcile() error

	// GetCurrentUsage retrieves the current usage for a user
	GetCurrentUsage(userID uint) (*messages.CurrentUsageResponse, error)

	// GetUploadUsageHistory retrieves the upload usage history for a user for the specified number of days
	GetUploadUsageHistory(userID uint, days int) ([]*messages.UsageData, error)

	// GetDownloadUsageHistory retrieves the download usage history for a user for the specified number of days
	GetDownloadUsageHistory(userID uint, days int) ([]*messages.UsageData, error)

	// GetStorageUsageHistory retrieves the storage usage history for a user for the specified number of days
	GetStorageUsageHistory(userID uint, days int) ([]*messages.UsageData, error)
}

var _ QuotaService = (*service.QuotaServiceDefault)(nil)
