package service

import (
	"errors"
	"go.lumeweb.com/portal-plugin-billing/internal/config"
	pluginDb "go.lumeweb.com/portal-plugin-billing/internal/db"
	"go.lumeweb.com/portal/core"
	"go.lumeweb.com/portal/db"
	"go.lumeweb.com/portal/event"
	"gorm.io/gorm"
	"math"
	"time"
)

var _ core.Service = (*BillingService)(nil)
var _ core.Configurable = (*BillingService)(nil)

const QUOTA_SERVICE = "quota"

type QuotaService struct {
	ctx      core.Context
	db       *gorm.DB
	logger   *core.Logger
	pins     core.PinService
	metadata core.MetadataService
	billing  *BillingService
}

func NewQuotaService() (core.Service, []core.ContextBuilderOption, error) {
	_service := &QuotaService{}

	return _service, core.ContextOptions(
		core.ContextWithStartupFunc(func(ctx core.Context) error {
			_service.ctx = ctx
			_service.db = ctx.DB()
			_service.pins = core.GetService[core.PinService](ctx, core.PIN_SERVICE)
			_service.metadata = core.GetService[core.MetadataService](ctx, core.METADATA_SERVICE)
			_service.logger = ctx.ServiceLogger(_service)
			_service.billing = core.GetService[*BillingService](ctx, BILLING_SERVICE)
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
						err = _service.RecordDownload(ctx, evt.UploadID(), pin.UserID, shardedBytes, evt.IP())
						if err != nil {
							return err
						}
					}
					return nil
				})
			})
			return nil
		}),
	), nil
}

func (q *QuotaService) ID() string {
	return QUOTA_SERVICE
}

func (q *QuotaService) RecordDownload(ctx core.Context, uploadID, userID uint, bytes uint64, ip string) error {
	if !q.enabled() {
		return nil
	}
	return q.db.Transaction(func(tx *gorm.DB) error {
		// Record detailed download
		err := db.RetryOnLock(tx, func(tx *gorm.DB) *gorm.DB {
			return tx.WithContext(ctx).Create(&pluginDb.Download{
				UploadID: uploadID,
				UserID:   userID,
				Bytes:    bytes,
				IP:       ip,
			})
		})
		if err != nil {
			return err
		}

		// Update aggregated UserQuota
		today := time.Now().UTC().Truncate(24 * time.Hour)
		var userQuota pluginDb.UserQuota

		if err = db.RetryOnLock(tx, func(tx *gorm.DB) *gorm.DB {
			return tx.Model(&pluginDb.UserQuota{}).Where(&pluginDb.UserQuota{UserID: userID, Date: today}).FirstOrCreate(&userQuota)
		}); err != nil {
			return err
		}

		userQuota.BytesUsed += bytes

		if err = db.RetryOnLock(tx, func(tx *gorm.DB) *gorm.DB {
			return tx.Save(&userQuota)
		}); err != nil {
			return err
		}

		return nil
	})
}
func (q *QuotaService) CheckQuota(ctx core.Context, userID uint, requestedBytes uint64) (bool, error) {
	if !q.enabled() {
		return true, nil
	}

	var userQuota pluginDb.UserQuota
	today := time.Now().UTC().Truncate(24 * time.Hour)

	if err := q.db.Transaction(func(tx *gorm.DB) error {
		return db.RetryOnLock(tx, func(tx *gorm.DB) *gorm.DB {
			return tx.Model(&pluginDb.UserQuota{}).Where(&pluginDb.UserQuota{UserID: userID, Date: today}).First(&userQuota)
		})
	}); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return true, nil
		}
		return false, err
	}

	requestedBytes = (userQuota.BytesUsed + requestedBytes)

	maxQuota, err := q.billing.GetUserQuota(ctx, userID)

	if err != nil {
		return false, err
	}

	return requestedBytes <= maxQuota, nil
}

func (b *QuotaService) Config() (any, error) {
	return &config.QuotaConfig{}, nil
}

func (b *QuotaService) enabled() bool {
	svcConfig := b.ctx.Config().GetService(QUOTA_SERVICE).(*config.QuotaConfig)

	return svcConfig.Enabled
}
