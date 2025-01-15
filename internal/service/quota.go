package service

import (
	"errors"
	"fmt"
	"go.lumeweb.com/portal-plugin-billing/internal/api/messages"
	"go.lumeweb.com/portal-plugin-billing/internal/config"
	pluginDb "go.lumeweb.com/portal-plugin-billing/internal/db"
	"go.lumeweb.com/portal/core"
	"go.lumeweb.com/portal/db"
	"go.lumeweb.com/portal/event"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"math"
	"time"
)

const QUOTA_SERVICE = "quota"

var noUsage = &messages.Usage{
	Current: messages.Resources{
		Storage:  0, // In bytes
		Upload:   0, // In bytes
		Download: 0, // In bytes
	},
}

type QuotaServiceDefault struct {
	ctx     core.Context
	db      *gorm.DB
	logger  *core.Logger
	pins    core.PinService
	upload  core.UploadService
	billing *BillingServiceDefault
}

func NewQuotaService() (core.Service, []core.ContextBuilderOption, error) {
	_service := &QuotaServiceDefault{}

	return _service, core.ContextOptions(
		core.ContextWithStartupFunc(func(ctx core.Context) error {
			_service.ctx = ctx
			_service.db = ctx.DB()
			_service.pins = core.GetService[core.PinService](ctx, core.PIN_SERVICE)
			_service.upload = core.GetService[core.UploadService](ctx, core.UPLOAD_SERVICE)
			_service.logger = ctx.ServiceLogger(_service)
			_service.billing = core.GetService[*BillingServiceDefault](ctx, BILLING_SERVICE)
			return nil
		}),

		core.ContextWithStartupFunc(func(ctx core.Context) error {
			if !_service.enabled() {
				return nil
			}

			event.Listen(ctx, event.EVENT_DOWNLOAD_COMPLETED, func(evt *event.DownloadCompletedEvent) error {
				pins, err := _service.pins.GetPinsByUploadID(ctx, evt.UploadID())
				if err != nil {
					return err
				}

				upload, err := _service.upload.GetUploadByID(ctx, evt.UploadID())
				if err != nil {
					return err
				}

				shardedBytes := uint64(math.Ceil(float64(upload.Size) / float64(uint64(len(pins)))))

				return _service.db.Transaction(func(tx *gorm.DB) error {
					for _, pin := range pins {
						err = _service.RecordDownload(evt.UploadID(), pin.UserID, shardedBytes, evt.IP())
						if err != nil {
							return err
						}
					}
					return nil
				})
			})

			event.Listen(ctx, event.EVENT_STORAGE_OBJECT_PINNED, func(evt *event.StorageObjectPinnedEvent) error {
				pin := evt.Pin()

				meta, err := _service.upload.GetUploadByID(ctx, pin.UploadID)
				if err != nil {
					return err
				}

				return db.RetryableTransaction(ctx, _service.db, func(tx *gorm.DB) *gorm.DB {
					err = _service.RecordUpload(pin.UploadID, pin.UserID, meta.Size, evt.IP())
					if err != nil {
						_ = tx.AddError(err)
					}

					err = _service.RecordStorage(pin.UploadID, pin.UserID, int64(meta.Size), evt.IP())
					if err != nil {
						_ = tx.AddError(err)
					}

					return tx
				})
			})
			return nil
		}),
	), nil
}

func (q *QuotaServiceDefault) ID() string {
	return QUOTA_SERVICE
}
func (q *QuotaServiceDefault) RecordUpload(uploadID, userID uint, bytes uint64, ip string) error {
	if !q.enabled() {
		return nil
	}

	return q.db.Transaction(func(tx *gorm.DB) error {
		// Record detailed upload
		upload := &pluginDb.UserUpload{
			UploadID: uploadID,
			UserID:   userID,
			Bytes:    bytes,
			IP:       ip,
		}
		if err := tx.Create(upload).Error; err != nil {
			return err
		}

		// Update aggregated UserBandwidthQuota
		today := time.Now().UTC().Truncate(24 * time.Hour)
		return tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "user_id"}, {Name: "date"}},
			DoUpdates: clause.Assignments(map[string]interface{}{
				"bytes_uploaded": gorm.Expr("bytes_uploaded + ?", bytes),
			}),
		}).Create(&pluginDb.UserBandwidthQuota{
			UserID:        userID,
			Date:          today,
			BytesUploaded: bytes,
		}).Error
	})
}

func (q *QuotaServiceDefault) RecordDownload(uploadID, userID uint, bytes uint64, ip string) error {
	if !q.enabled() {
		return nil
	}

	return q.db.Transaction(func(tx *gorm.DB) error {
		// Record detailed download
		download := &pluginDb.UserDownload{
			UploadID: uploadID,
			UserID:   userID,
			Bytes:    bytes,
			IP:       ip,
		}
		if err := tx.Create(download).Error; err != nil {
			return err
		}

		// Update aggregated UserBandwidthQuota
		today := time.Now().UTC().Truncate(24 * time.Hour)
		return tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "user_id"}, {Name: "date"}},
			DoUpdates: clause.Assignments(map[string]interface{}{
				"bytes_downloaded": gorm.Expr("bytes_downloaded + ?", bytes),
			}),
		}).Create(&pluginDb.UserBandwidthQuota{
			UserID:          userID,
			Date:            today,
			BytesDownloaded: bytes,
		}).Error
	})
}

func (q *QuotaServiceDefault) RecordStorage(uploadID, userID uint, bytes int64, ip string) error {
	if !q.enabled() {
		return nil
	}

	isAdd := bytes >= 0

	return q.db.Transaction(func(tx *gorm.DB) error {
		// Record detailed storage change
		storage := &pluginDb.UserStorage{
			UploadID: uploadID,
			UserID:   userID,
			Bytes:    uint64(math.Abs(float64(bytes))),
			IsAdd:    isAdd,
			IP:       ip,
		}
		if err := tx.Create(storage).Error; err != nil {
			return err
		}

		// Update aggregated UserBandwidthQuota
		return tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "user_id"}, {Name: "date"}},
			DoUpdates: clause.Assignments(map[string]interface{}{
				"bytes_stored": gorm.Expr("GREATEST(0, bytes_stored + ?)", bytes),
			}),
		}).Create(&pluginDb.UserStorageQuota{
			UserID:      userID,
			BytesStored: uint64(max(0, bytes)), // For new records
		}).Error
	})
}

func (q *QuotaServiceDefault) CheckDownloadQuota(userID uint, requestedBytes uint64) (bool, error) {
	return q.checkQuota(userID, requestedBytes, "download")
}

func (q *QuotaServiceDefault) CheckUploadQuota(userID uint, requestedBytes uint64) (bool, error) {
	return q.checkQuota(userID, requestedBytes, "upload")
}

func (q *QuotaServiceDefault) CheckStorageQuota(userID uint, requestedBytes uint64) (bool, error) {
	return q.checkQuota(userID, requestedBytes, "storage")
}

func (q *QuotaServiceDefault) getUserQuotaRecord(userID uint, date time.Time) (*pluginDb.UserBandwidthQuota, error) {
	var userQuota pluginDb.UserBandwidthQuota

	if err := db.RetryableTransaction(q.ctx, q.db, func(tx *gorm.DB) *gorm.DB {
		return tx.Model(&pluginDb.UserBandwidthQuota{}).Where(&pluginDb.UserBandwidthQuota{UserID: userID, Date: date}).First(&userQuota)
	}); err != nil {
		return nil, err
	}

	return &userQuota, nil
}

func (q *QuotaServiceDefault) checkQuota(userID uint, requestedBytes uint64, quotaType string) (bool, error) {
	if !q.enabled() {
		return true, nil
	}

	// Get the maximum quota for this quota type
	var maxQuota uint64
	var err error
	switch quotaType {
	case "download":
		maxQuota, err = q.billing.GetUserMaxDownload(userID)
	case "upload":
		maxQuota, err = q.billing.GetUserMaxUpload(userID)
	case "storage":
		maxQuota, err = q.billing.GetUserMaxStorage(userID)
	default:
		return false, fmt.Errorf("invalid quota type: %s", quotaType)
	}
	if err != nil {
		return false, err
	}

	// Get current usage
	usage, err := q.GetCurrentUsage(userID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// No quota record exists for the user
			return requestedBytes <= maxQuota, nil
		}
		return false, err
	}

	// Check against appropriate quota type
	var totalBytes uint64
	switch quotaType {
	case "download":
		totalBytes = usage.Current.Download + requestedBytes
	case "upload":
		totalBytes = usage.Current.Upload + requestedBytes
	case "storage":
		totalBytes = usage.Current.Storage + requestedBytes
	}

	return totalBytes <= maxQuota, nil
}

func (q *QuotaServiceDefault) Reconcile() error {
	yesterday := time.Now().UTC().Add(-24 * time.Hour).Truncate(24 * time.Hour)

	return db.RetryableTransaction(q.ctx, q.db, func(tx *gorm.DB) *gorm.DB {
		if err := q.reconcileBandwidth(tx, yesterday); err != nil {
			_ = tx.AddError(err)
			return nil
		}
		if err := q.reconcileStorage(tx, yesterday); err != nil {
			_ = tx.AddError(err)
			return nil
		}
		return nil
	})
}

func (q *QuotaServiceDefault) reconcileBandwidth(tx *gorm.DB, date time.Time) error {
	var query string
	var args []interface{}

	if q.isSQLite() {
		query = `
            INSERT INTO user_bandwidth_quotas (user_id, date, bytes_uploaded, bytes_downloaded)
            SELECT 
                user_id,
                ? as date,
                SUM(CASE WHEN type = 'upload' THEN bytes ELSE 0 END) as bytes_uploaded,
                SUM(CASE WHEN type = 'download' THEN bytes ELSE 0 END) as bytes_downloaded
            FROM (
                SELECT user_id, 'upload' as type, bytes
                FROM uploads
                WHERE created_at >= ? AND created_at < ?
                UNION ALL
                SELECT user_id, 'download' as type, bytes
                FROM downloads
                WHERE created_at >= ? AND created_at < ?
            ) combined
            GROUP BY user_id
            ON CONFLICT(user_id, date) DO UPDATE SET
                bytes_uploaded = user_quotas.bytes_uploaded + excluded.bytes_uploaded,
                bytes_downloaded = user_quotas.bytes_downloaded + excluded.bytes_downloaded
        `
	} else {
		// Assuming MySQL
		query = `
            INSERT INTO user_bandwidth_quotas (user_id, date, bytes_uploaded, bytes_downloaded)
            SELECT 
                user_id,
                ? as date,
                SUM(CASE WHEN type = 'upload' THEN bytes ELSE 0 END) as bytes_uploaded,
                SUM(CASE WHEN type = 'download' THEN bytes ELSE 0 END) as bytes_downloaded
            FROM (
                SELECT user_id, 'upload' as type, bytes
                FROM uploads
                WHERE created_at >= ? AND created_at < ?
                UNION ALL
                SELECT user_id, 'download' as type, bytes
                FROM downloads
                WHERE created_at >= ? AND created_at < ?
            ) combined
            GROUP BY user_id
            ON DUPLICATE KEY UPDATE
                bytes_uploaded = user_quotas.bytes_uploaded + VALUES(bytes_uploaded),
                bytes_downloaded = user_quotas.bytes_downloaded + VALUES(bytes_downloaded)
        `
	}

	args = []interface{}{date, date, date.Add(24 * time.Hour), date, date.Add(24 * time.Hour)}
	return tx.Exec(query, args...).Error
}

func (q *QuotaServiceDefault) reconcileStorage(tx *gorm.DB, date time.Time) error {
	var query string
	var args []interface{}

	if q.isSQLite() {
		query = `
            INSERT INTO user_storage_quotas (user_id, date, bytes_stored)
            SELECT 
                user_id,
                ? as date,
                MAX(0, SUM(CASE WHEN is_add THEN bytes ELSE -bytes END)) as bytes_stored
            FROM storage
            WHERE created_at < ?
            GROUP BY user_id
            ON CONFLICT(user_id, date) DO UPDATE SET
                bytes_stored = MAX(0, user_quotas.bytes_stored + excluded.bytes_stored)
        `
	} else {
		// Assuming MySQL
		query = `
            INSERT INTO user_bandwidth_quotas (user_id, date, bytes_stored)
            SELECT 
                user_id,
                ? as date,
                GREATEST(0, SUM(CASE WHEN is_add THEN bytes ELSE -bytes END)) as bytes_stored
            FROM storage
            WHERE created_at < ?
            GROUP BY user_id
            ON DUPLICATE KEY UPDATE
                bytes_stored = GREATEST(0, user_quotas.bytes_stored + VALUES(bytes_stored))
        `
	}

	args = []interface{}{date, date.Add(24 * time.Hour)}
	return tx.Exec(query, args...).Error
}

func (q *QuotaServiceDefault) isSQLite() bool {
	return q.db.Dialector.Name() == "sqlite"
}

func (q *QuotaServiceDefault) GetUploadUsageHistory(userID uint, period int) ([]*messages.UsagePoint, error) {
	return q.getUsageHistory(userID, period, "upload")
}

func (q *QuotaServiceDefault) GetDownloadUsageHistory(userID uint, period int) ([]*messages.UsagePoint, error) {
	return q.getUsageHistory(userID, period, "download")
}

func (q *QuotaServiceDefault) GetStorageUsageHistory(userID uint, period int) ([]*messages.UsagePoint, error) {
	return q.getUsageHistory(userID, period, "storage")
}

func (q *QuotaServiceDefault) GetCurrentUsage(userID uint) (*messages.Usage, error) {
	if !q.enabled() {
		return noUsage, nil
	}

	sub, err := q.billing.GetSubscription(q.ctx, userID)
	if err != nil {
		return nil, err
	}

	var resources messages.Resources
	now := time.Now().UTC()

	var innerErr error
	err = db.RetryableTransaction(q.ctx, q.db, func(tx *gorm.DB) *gorm.DB {
		var startDate, endDate time.Time

		if sub == nil || sub.Plan == nil {
			// If no subscription, get lifetime usage
			if err := tx.Model(&pluginDb.UserBandwidthQuota{}).
				Select("COALESCE(SUM(bytes_uploaded), 0) as upload, COALESCE(SUM(bytes_downloaded), 0) as download").
				Where("user_id = ?", userID).
				Scan(&resources).Error; err != nil {
				_ = tx.AddError(err)
				return tx
			}
		} else {
			// If subscription exists, calculate usage for the current period
			if sub.Plan.IsFree {
				startDate = now.Truncate(24 * time.Hour)
			} else {
				startDate = sub.CurrentPeriod.Start
			}

			switch sub.Plan.Period {
			case messages.PeriodMonthly:
				endDate = startDate.AddDate(0, 1, 0)
			case messages.PeriodYearly:
				endDate = startDate.AddDate(1, 0, 0)
			default:
				innerErr = fmt.Errorf("invalid subscription period: %s", sub.Plan.Period)
				return tx
			}

			// Find the current period
			for endDate.Before(now) {
				startDate = endDate
				endDate = endDate.Add(endDate.Sub(startDate))
			}

			// Get bandwidth usage for the current period
			if err := tx.Model(&pluginDb.UserBandwidthQuota{}).
				Select("COALESCE(SUM(bytes_uploaded), 0) as upload, COALESCE(SUM(bytes_downloaded), 0) as download").
				Where("user_id = ? AND date >= ? AND date < ?", userID, startDate, endDate).
				Scan(&resources).Error; err != nil {
				_ = tx.AddError(err)
				return tx
			}
		}

		// Calculate storage usage
		var storageUsage struct {
			Added   uint64
			Removed uint64
		}
		if err := tx.Model(&pluginDb.UserStorage{}).
			Select("COALESCE(SUM(CASE WHEN is_add THEN bytes ELSE 0 END), 0) as added, COALESCE(SUM(CASE WHEN NOT is_add THEN bytes ELSE 0 END), 0) as removed").
			Where("user_id = ?", userID).
			Scan(&storageUsage).Error; err != nil {
			_ = tx.AddError(err)
			return tx
		}
		resources.Storage = storageUsage.Added - storageUsage.Removed

		return tx
	})

	if err != nil {
		return nil, err
	}

	if innerErr != nil {
		return nil, innerErr
	}

	return &messages.Usage{
		Current: resources,
	}, nil
}

func (q *QuotaServiceDefault) getUsageHistory(userID uint, period int, usageType string) ([]*messages.UsagePoint, error) {
	if !q.enabled() {
		return nil, nil
	}

	endDate := time.Now().UTC().Truncate(24 * time.Hour).Add(24 * time.Hour)
	startDate := endDate.AddDate(0, 0, -period)

	var usagePoints []*messages.UsagePoint
	var err error

	if usageType == "storage" {
		err = q.getStorageUsageHistory(userID, startDate, endDate, &usagePoints)
	} else {
		err = q.getBandwidthUsageHistory(userID, startDate, endDate, usageType, &usagePoints)
	}

	if err != nil {
		return nil, err
	}

	return usagePoints, nil
}

func (q *QuotaServiceDefault) getBandwidthUsageHistory(userID uint, startDate, endDate time.Time, usageType string, usagePoints *[]*messages.UsagePoint) error {
	column := "bytes_uploaded"
	if usageType == "download" {
		column = "bytes_downloaded"
	}

	return db.RetryableTransaction(q.ctx, q.db, func(tx *gorm.DB) *gorm.DB {
		return tx.Model(&pluginDb.UserBandwidthQuota{}).
			Select(fmt.Sprintf("date as timestamp, %s as value", column)).
			Where("user_id = ? AND date >= ? AND date <= ?", userID, startDate, endDate).
			Order("date ASC").
			Scan(usagePoints)
	})
}

func (q *QuotaServiceDefault) getStorageUsageHistory(userID uint, startDate, endDate time.Time, usagePoints *[]*messages.UsagePoint) error {
	return db.RetryableTransaction(q.ctx, q.db, func(tx *gorm.DB) *gorm.DB {
		// First, get daily changes
		var dailyChanges []struct {
			Timestamp   time.Time
			DailyChange int64
		}

		query := `
           SELECT 
               DATE(created_at) as timestamp,
               SUM(CASE WHEN is_add THEN bytes ELSE -bytes END) as daily_change
           FROM user_storage
           WHERE user_id = ? AND created_at >= ? AND created_at <= ?
           GROUP BY DATE(created_at)
           ORDER BY timestamp ASC
       `

		if err := tx.Raw(query, userID, startDate, endDate).Scan(&dailyChanges).Error; err != nil {
			_ = tx.AddError(err)
			return tx
		}

		// Calculate cumulative sums
		var cumulativeSum int64
		*usagePoints = make([]*messages.UsagePoint, len(dailyChanges))
		for i, change := range dailyChanges {
			cumulativeSum += change.DailyChange
			(*usagePoints)[i] = &messages.UsagePoint{
				Timestamp: change.Timestamp,
				Value:     uint64(max(0, cumulativeSum)),
			}
		}

		return tx
	})
}

func (b *QuotaServiceDefault) Config() (any, error) {
	return &config.QuotaConfig{}, nil
}

func (b *QuotaServiceDefault) enabled() bool {
	svcConfig := b.ctx.Config().GetService(QUOTA_SERVICE).(*config.QuotaConfig)

	return svcConfig.Enabled
}
