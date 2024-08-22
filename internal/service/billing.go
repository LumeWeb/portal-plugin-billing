package service

import (
	"context"
	"errors"
	"fmt"
	"github.com/go-openapi/runtime"
	httptransport "github.com/go-openapi/runtime/client"
	"github.com/go-openapi/strfmt"
	"github.com/killbill/kbcli/v3/kbclient"
	"github.com/killbill/kbcli/v3/kbclient/account"
	"github.com/killbill/kbcli/v3/kbmodel"
	"go.lumeweb.com/portal-plugin-billing/internal/config"
	pluginDb "go.lumeweb.com/portal-plugin-billing/internal/db"
	"go.lumeweb.com/portal-plugin-billing/service"
	"go.lumeweb.com/portal/core"
	"go.lumeweb.com/portal/db"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"math"
	"strconv"
)

type UsageType string

const (
	StorageUsage  UsageType = "storage"
	UploadUsage   UsageType = "upload"
	DownloadUsage UsageType = "download"
)

var _ core.Service = (*BillingServiceDefault)(nil)
var _ core.Configurable = (*BillingServiceDefault)(nil)
var _ service.BillingService = (*BillingServiceDefault)(nil)

const BILLING_SERVICE = "billing"

type BillingServiceDefault struct {
	ctx    core.Context
	db     *gorm.DB
	logger *core.Logger
	cfg    *config.BillingConfig
	api    *kbclient.KillBill
	user   core.UserService
}

func NewBillingService() (core.Service, []core.ContextBuilderOption, error) {
	_service := &BillingServiceDefault{}

	return _service, core.ContextOptions(
		core.ContextWithStartupFunc(func(ctx core.Context) error {
			_service.ctx = ctx
			_service.db = ctx.DB()
			_service.logger = ctx.ServiceLogger(_service)
			_service.user = core.GetService[core.UserService](ctx, core.USER_SERVICE)
			_service.cfg = ctx.Config().GetService(BILLING_SERVICE).(*config.BillingConfig)

			if _service.enabled() {
				trp := httptransport.New(_service.cfg.KillBill.APIServer, "", nil)
				trp.Producers["text/xml"] = runtime.TextProducer()
				trp.Debug = false
				authWriter := runtime.ClientAuthInfoWriterFunc(func(r runtime.ClientRequest, _ strfmt.Registry) error {
					if err := r.SetHeaderParam("X-KillBill-ApiKey", _service.cfg.KillBill.APIKey); err != nil {
						return err
					}
					if err := r.SetHeaderParam("X-KillBill-ApiSecret", _service.cfg.KillBill.APISecret); err != nil {
						return err
					}
					return nil
				})
				_service.api = kbclient.New(trp, strfmt.Default, authWriter, kbclient.KillbillDefaults{})
			}

			return nil
		}),
	), nil
}

func (b *BillingServiceDefault) ID() string {
	return BILLING_SERVICE
}

func (b *BillingServiceDefault) CreateCustomer(ctx context.Context, userID uint) error {
	if !b.enabled() {
		return nil
	}

	exists, user, err := b.user.AccountExists(userID)
	if err != nil {
		return err
	}

	if !exists {
		return errors.New("user does not exist")
	}

	externalKey := strconv.FormatUint(uint64(userID), 10)

	result, err := b.api.Account.GetAccountByKey(ctx, &account.GetAccountByKeyParams{
		ExternalKey: externalKey,
	})

	if err != nil {
		return err
	}

	if result.Payload == nil {
		_, err = b.api.Account.CreateAccount(ctx, &account.CreateAccountParams{
			Body: &kbmodel.Account{
				ExternalKey: externalKey,
				Name:        fmt.Sprintf("%s %s", user.FirstName, user.LastName),
			},
		})

		if err != nil {
			return err
		}
	}

	return nil
}

func (b *BillingServiceDefault) GetUserMaxStorage(ctx context.Context, userID uint) (uint64, error) {
	return b.getUsageByUserID(ctx, userID, StorageUsage)
}

func (b *BillingServiceDefault) GetUserMaxUpload(ctx context.Context, userID uint) (uint64, error) {
	return b.getUsageByUserID(ctx, userID, UploadUsage)
}

func (b *BillingServiceDefault) GetUserMaxDownload(ctx context.Context, userID uint) (uint64, error) {
	return b.getUsageByUserID(ctx, userID, DownloadUsage)
}

func (b *BillingServiceDefault) enabled() bool {
	svcConfig := b.ctx.Config().GetService(BILLING_SERVICE).(*config.BillingConfig)

	return svcConfig.Enabled
}

func (b *BillingServiceDefault) Config() (any, error) {
	return &config.BillingConfig{}, nil
}

func (b *BillingServiceDefault) getUsageByUserID(ctx context.Context, userID uint, usageType UsageType) (uint64, error) {
	if !b.enabled() {
		return math.MaxUint64, nil
	}

	acct, err := b.api.Account.GetAccountByKey(ctx, &account.GetAccountByKeyParams{
		ExternalKey: strconv.FormatUint(uint64(userID), 10),
	})

	if err != nil {
		return 0, err
	}

	bundles, err := b.api.Account.GetAccountBundles(ctx, &account.GetAccountBundlesParams{
		AccountID: acct.Payload.AccountID,
	})
	if err != nil {
		return 0, err
	}

	subscription := findActiveSubscription(bundles.Payload)

	if subscription == nil {
		return 0, nil
	}

	if len(*subscription.PlanName) == 0 {
		return 0, nil
	}

	var plan pluginDb.Plan

	if err = db.RetryableTransaction(b.ctx, b.db, func(tx *gorm.DB) *gorm.DB {
		return tx.Model(&pluginDb.Plan{}).Where(&pluginDb.Plan{Identifier: *subscription.PlanName}).First(&plan)
	}); err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			b.logger.Error("failed to get plan", zap.Error(err))
		}
		return 0, err
	}

	switch usageType {
	case StorageUsage:
		return plan.Storage, nil
	case UploadUsage:
		return plan.Upload, nil
	case DownloadUsage:
		return plan.Download, nil
	}

	return 0, nil
}

func findActiveSubscription(bundles []*kbmodel.Bundle) *kbmodel.Subscription {
	for _, bundle := range bundles {
		for _, subscription := range bundle.Subscriptions {
			if subscription.State == kbmodel.SubscriptionStateACTIVE {
				return subscription
			}
		}
	}

	return nil
}
