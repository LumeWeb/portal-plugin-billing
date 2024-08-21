package service

import (
	"go.lumeweb.com/portal-plugin-billing/internal/config"
	"go.lumeweb.com/portal/core"
	"gorm.io/gorm"
	"math"
)

var _ core.Service = (*BillingService)(nil)
var _ core.Configurable = (*BillingService)(nil)

const BILLING_SERVICE = "billing"

type BillingService struct {
	ctx    core.Context
	db     *gorm.DB
	logger *core.Logger
}

func NewBillingService() (core.Service, []core.ContextBuilderOption, error) {
	_service := &BillingService{}

	return _service, core.ContextOptions(
		core.ContextWithStartupFunc(func(ctx core.Context) error {
			_service.ctx = ctx
			_service.db = ctx.DB()
			_service.logger = ctx.ServiceLogger(_service)

			return nil
		}),
	), nil
}

func (b *BillingService) ID() string {
	return BILLING_SERVICE
}

func (b *BillingService) GetUserQuota(ctx core.Context, userID uint) (uint64, error) {
	if !b.enabled() {
		return math.MaxUint64, nil
	}

	return 0, nil
}

func (b *BillingService) enabled() bool {
	svcConfig := b.ctx.Config().GetService(BILLING_SERVICE).(*config.BillingConfig)

	return svcConfig.Enabled
}

func (b *BillingService) Config() (any, error) {
	return &config.BillingConfig{}, nil
}
