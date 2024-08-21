package service

import (
	pluginDb "go.lumeweb.com/portal-plugin-billing/internal/db"
	"go.lumeweb.com/portal/core"
	"go.lumeweb.com/portal/db"
	"go.lumeweb.com/portal/event"
	"gorm.io/gorm"
)

var _ core.Service = (*BillingService)(nil)

const QUOTA_SERVICE = "quota"

type QuotaService struct {
	ctx      core.Context
	db       *gorm.DB
	logger   *core.Logger
	pins     core.PinService
	metadata core.MetadataService
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

			return nil
		}),

		core.ContextWithStartupFunc(func(ctx core.Context) error {
			event.Listen(ctx, event.EVENT_DOWNLOAD_COMPLETED, func(evt *event.DownloadCompletedEvent) error {
				pins, err := _service.pins.GetPinsByUploadID(ctx, evt.UploadID())
				if err != nil {
					return err
				}

				upload, err := _service.metadata.GetUploadByID(ctx, evt.UploadID())
				if err != nil {
					return err
				}

				shardedBytes := upload.Size / uint64(len(pins))

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

func (q *QuotaService) RecordDownload(ctx core.Context, uploadID, userId uint, bytes uint64, ip string) error {
	return q.db.Transaction(func(tx *gorm.DB) error {
		return db.RetryOnLock(tx, func(tx *gorm.DB) *gorm.DB {
			return tx.WithContext(ctx).Create(&pluginDb.Download{
				UploadID: uploadID,
				UserID:   userId,
				Bytes:    bytes,
				IP:       ip,
			})
		})
	})
}
