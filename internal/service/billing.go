package service

import (
	"go.lumeweb.com/portal/core"
	"gorm.io/gorm"
)

var _ core.Service = (*BillingService)(nil)

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

func (b BillingService) ID() string {
	return BILLING_SERVICE
}
