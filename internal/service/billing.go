package service

import (
	"github.com/go-openapi/runtime"
	httptransport "github.com/go-openapi/runtime/client"
	"github.com/go-openapi/strfmt"
	"github.com/killbill/kbcli/v3/kbclient"
	"github.com/killbill/kbcli/v3/kbclient/account"
	"github.com/killbill/kbcli/v3/kbclient/usage"
	"github.com/killbill/kbcli/v3/kbmodel"
	"go.lumeweb.com/portal-plugin-billing/internal/config"
	"go.lumeweb.com/portal-plugin-billing/service"
	"go.lumeweb.com/portal/core"
	"gorm.io/gorm"
	"math"
	"strconv"
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
}

func NewBillingService() (core.Service, []core.ContextBuilderOption, error) {
	_service := &BillingServiceDefault{}

	return _service, core.ContextOptions(
		core.ContextWithStartupFunc(func(ctx core.Context) error {
			_service.ctx = ctx
			_service.db = ctx.DB()
			_service.logger = ctx.ServiceLogger(_service)
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

func (b *BillingServiceDefault) GetUserStorageQuota(ctx core.Context, userID uint) (uint64, error) {
	return b.getUsageByUserID(ctx, userID, b.cfg.KillBill.StorageUsageUnitName)
}

func (b *BillingServiceDefault) GetUserUploadQuota(ctx core.Context, userID uint) (uint64, error) {
	return b.getUsageByUserID(ctx, userID, b.cfg.KillBill.UploadUsageUnitName)
}

func (b *BillingServiceDefault) GetUserDownloadQuota(ctx core.Context, userID uint) (uint64, error) {
	return b.getUsageByUserID(ctx, userID, b.cfg.KillBill.DownloadUsageUnitName)
}

func (b *BillingServiceDefault) enabled() bool {
	svcConfig := b.ctx.Config().GetService(BILLING_SERVICE).(*config.BillingConfig)

	return svcConfig.Enabled
}

func (b *BillingServiceDefault) Config() (any, error) {
	return &config.BillingConfig{}, nil
}

func (b *BillingServiceDefault) getUsageByUserID(ctx core.Context, userID uint, unitName string) (uint64, error) {
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

	getUsage, err := b.api.Usage.GetUsage(ctx, &usage.GetUsageParams{
		UnitType:       unitName,
		SubscriptionID: subscription.SubscriptionID,
	})
	if err != nil {
		return 0, err
	}

	for _, unit := range getUsage.Payload.RolledUpUnits {
		if unit.UnitType == b.cfg.KillBill.DownloadUsageUnitName {
			return uint64(math.Ceil(unit.Amount)), nil
		}
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
