package service

import (
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

type QuotaServiceDefault struct {
	ctx      core.Context
	db       *gorm.DB
	logger   *core.Logger
	pins     core.PinService
	metadata core.MetadataService
	billing  *BillingServiceDefault
}

type userByte struct {
	UserID    uint
	BytesUsed uint64
}

func NewQuotaService() (core.Service, []core.ContextBuilderOption, error) {
	_service := &QuotaServiceDefault{}

	return _service, core.ContextOptions(
		core.ContextWithStartupFunc(func(ctx core.Context) error {
			_service.ctx = ctx
			_service.db = ctx.DB()
			_service.pins = core.GetService[core.PinService](ctx, core.PIN_SERVICE)
			_service.metadata = core.GetService[core.MetadataService](ctx, core.METADATA_SERVICE)
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

				upload, err := _service.metadata.GetUploadByID(ctx, evt.UploadID())
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

			event.Listen(ctx, event.EVENT_STORAGE_OBJECT_UPLOADED, func(evt *event.StorageObjectUploadedEvent) error {
				pin := evt.Pin()

				meta, err := _service.metadata.GetUploadByID(ctx, pin.UploadID)
				if err != nil {
					return err
				}

				return db.RetryableTransaction(ctx, _service.db, func(tx *gorm.DB) *gorm.DB {
					err = _service.RecordUpload(pin.UploadID, pin.UserID, meta.Size, evt.IP())
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

func (q *QuotaServiceDefault) RecordDownload(uploadID, userID uint, bytes uint64, ip string) error {
	if !q.enabled() {
		return nil
	}
	return q.db.Transaction(func(tx *gorm.DB) error {
		// Record detailed download
		if err := db.RetryableTransaction(q.ctx, q.db, func(tx *gorm.DB) *gorm.DB {
			return tx.Create(&pluginDb.Download{
				UploadID: uploadID,
				UserID:   userID,
				Bytes:    bytes,
				IP:       ip,
			})
		}); err != nil {
			return err
		}

		// Update aggregated UserQuota
		today := time.Now().UTC().Truncate(24 * time.Hour)
		var userQuota pluginDb.UserQuota

		if err := db.RetryableTransaction(q.ctx, q.db, func(tx *gorm.DB) *gorm.DB {
			return tx.Model(&pluginDb.UserQuota{}).Where(&pluginDb.UserQuota{UserID: userID, Date: today}).FirstOrCreate(&userQuota)
		}); err != nil {
			return err
		}

		userQuota.BytesDownloaded += bytes

		if err := db.RetryableTransaction(q.ctx, q.db, func(tx *gorm.DB) *gorm.DB {
			return tx.Model(&pluginDb.UserQuota{}).Where(&pluginDb.UserQuota{UserID: userID, Date: today}).First(&userQuota)
		}); err != nil {
			return err
		}

		return nil
	})
}

func (q *QuotaServiceDefault) RecordUpload(uploadID, userID uint, bytes uint64, ip string) error {
	if !q.enabled() {
		return nil
	}
	return q.db.Transaction(func(tx *gorm.DB) error {
		// Record detailed upload
		if err := db.RetryableTransaction(q.ctx, q.db, func(tx *gorm.DB) *gorm.DB {
			return tx.Create(&pluginDb.Upload{
				UploadID: uploadID,
				UserID:   userID,
				Bytes:    bytes,
				IP:       ip,
			})
		}); err != nil {
			return err
		}

		// Update aggregated UserQuota
		today := time.Now().UTC().Truncate(24 * time.Hour)
		var userQuota pluginDb.UserQuota

		if err := db.RetryableTransaction(q.ctx, q.db, func(tx *gorm.DB) *gorm.DB {
			return tx.Model(&pluginDb.UserQuota{}).Where(&pluginDb.UserQuota{UserID: userID, Date: today}).FirstOrCreate(&userQuota)
		}); err != nil {
			return err
		}

		userQuota.BytesUploaded += bytes

		if err := db.RetryableTransaction(q.ctx, q.db, func(tx *gorm.DB) *gorm.DB {
			return tx.Model(&pluginDb.UserQuota{}).Where(&pluginDb.UserQuota{UserID: userID, Date: today}).First(&userQuota)
		}); err != nil {
			return err
		}

		return nil
	})
}

func (q *QuotaServiceDefault) CheckDownloadQuota(userID uint, requestedBytes uint64) (bool, error) {
	if !q.enabled() {
		return true, nil
	}

	today := time.Now().UTC().Truncate(24 * time.Hour)

	record, err := q.getUserQuotaRecord(userID, today)
	if err != nil {
		return false, err
	}

	requestedBytes = record.BytesDownloaded + requestedBytes

	maxQuota, err := q.billing.GetUserMaxStorage(userID)
	if err != nil {
		return false, err
	}

	return requestedBytes <= maxQuota, nil
}

func (q *QuotaServiceDefault) CheckStorageQuota(userID uint, requestedBytes uint64) (bool, error) {
	if !q.enabled() {
		return true, nil
	}
	today := time.Now().UTC().Truncate(24 * time.Hour)

	record, err := q.getUserQuotaRecord(userID, today)
	if err != nil {
		return false, err
	}

	requestedBytes = record.BytesDownloaded + requestedBytes

	maxQuota, err := q.billing.GetUserMaxStorage(userID)

	if err != nil {
		return false, err
	}

	return requestedBytes <= maxQuota, nil
}

func (q *QuotaServiceDefault) CheckUploadQuota(userID uint, requestedBytes uint64) (bool, error) {
	if !q.enabled() {
		return true, nil
	}

	today := time.Now().UTC().Truncate(24 * time.Hour)

	record, err := q.getUserQuotaRecord(userID, today)
	if err != nil {
		return false, err
	}

	requestedBytes = record.BytesDownloaded + requestedBytes

	maxQuota, err := q.billing.GetUserMaxStorage(userID)

	if err != nil {
		return false, err
	}

	return requestedBytes <= maxQuota, nil
}

func (q *QuotaServiceDefault) getUserQuotaRecord(userID uint, date time.Time) (*pluginDb.UserQuota, error) {
	var userQuota pluginDb.UserQuota

	if err := db.RetryableTransaction(q.ctx, q.db, func(tx *gorm.DB) *gorm.DB {
		return tx.Model(&pluginDb.UserQuota{}).Where(&pluginDb.UserQuota{UserID: userID, Date: date}).First(&userQuota)
	}); err != nil {
		return nil, err
	}

	return &userQuota, nil
}

func (q *QuotaServiceDefault) Reconcile() error {
	yesterday := time.Now().UTC().Add(-24 * time.Hour).Truncate(24 * time.Hour)

	return db.RetryableTransaction(q.ctx, q.db, func(tx *gorm.DB) *gorm.DB {
		if err := q.reconcileDownloads(tx, yesterday); err != nil {
			_ = tx.AddError(err)
			return tx
		}
		if err := q.reconcileUploads(tx, yesterday); err != nil {
			_ = tx.AddError(err)
			return tx
		}
		if err := q.reconcileStorage(tx, yesterday); err != nil {
			_ = tx.AddError(err)
			return tx
		}

		return tx
	})
}

func (q *QuotaServiceDefault) reconcileDownloads(tx *gorm.DB, date time.Time) error {
	var userBytes []userByte

	err := db.RetryableTransaction(q.ctx, tx, func(tx *gorm.DB) *gorm.DB {
		return tx.Table("downloads").
			Select("user_id, COALESCE(SUM(bytes), 0) as bytes_used").
			Where("created_at >= ? AND created_at < ?", date, date.Add(24*time.Hour)).
			Group("user_id").
			Scan(&userBytes)
	})
	if err != nil {
		return err
	}

	return q.updateQuotas(tx, userBytes, date, "bytes_downloaded")
}

func (q *QuotaServiceDefault) reconcileUploads(tx *gorm.DB, date time.Time) error {
	var userBytes []userByte

	err := db.RetryableTransaction(q.ctx, tx, func(tx *gorm.DB) *gorm.DB {
		return tx.Table("uploads").
			Select("user_id, COALESCE(SUM(bytes), 0) as bytes_used").
			Where("created_at >= ? AND created_at < ?", date, date.Add(24*time.Hour)).
			Group("user_id").
			Scan(&userBytes)
	})
	if err != nil {
		return err
	}

	return q.updateQuotas(tx, userBytes, date, "bytes_uploaded")
}

func (q *QuotaServiceDefault) reconcileStorage(tx *gorm.DB, date time.Time) error {
	var userBytes []userByte

	err := db.RetryableTransaction(q.ctx, tx, func(tx *gorm.DB) *gorm.DB {
		return tx.Table("files").
			Select("user_id, COALESCE(SUM(size), 0) as bytes_used").
			Where("created_at < ?", date.Add(24*time.Hour)).
			Group("user_id").
			Scan(&userBytes)
	})
	if err != nil {
		return err
	}

	return q.updateQuotas(tx, userBytes, date, "bytes_stored")
}

func (q *QuotaServiceDefault) updateQuotas(tx *gorm.DB, userBytes []userByte, date time.Time, updateColumn string) error {
	for _, ub := range userBytes {
		quota := &pluginDb.UserQuota{
			UserID: ub.UserID,
			Date:   date,
		}

		switch updateColumn {
		case "bytes_downloaded":
			quota.BytesDownloaded = ub.BytesUsed
		case "bytes_uploaded":
			quota.BytesUploaded = ub.BytesUsed
		case "bytes_stored":
			quota.BytesStored = ub.BytesUsed
		}

		if err := db.RetryableTransaction(q.ctx, tx, func(tx *gorm.DB) *gorm.DB {
			return tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "user_id"}, {Name: "date"}},
				DoUpdates: clause.AssignmentColumns([]string{updateColumn, "updated_at"}),
			}).Create(quota)
		}); err != nil {
			return err
		}
	}

	return nil
}

func (b *QuotaServiceDefault) Config() (any, error) {
	return &config.QuotaConfig{}, nil
}

func (b *QuotaServiceDefault) enabled() bool {
	svcConfig := b.ctx.Config().GetService(QUOTA_SERVICE).(*config.QuotaConfig)

	return svcConfig.Enabled
}
