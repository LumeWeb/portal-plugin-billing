package service

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/go-openapi/runtime"
	httptransport "github.com/go-openapi/runtime/client"
	"github.com/go-openapi/strfmt"
	"github.com/killbill/kbcli/v3/kbclient"
	"github.com/killbill/kbcli/v3/kbclient/account"
	"github.com/killbill/kbcli/v3/kbclient/catalog"
	"github.com/killbill/kbcli/v3/kbclient/nodes_info"
	"github.com/killbill/kbcli/v3/kbcommon"
	"github.com/killbill/kbcli/v3/kbmodel"
	"go.lumeweb.com/portal-plugin-billing/internal/api/messages"
	"go.lumeweb.com/portal-plugin-billing/internal/config"
	pluginDb "go.lumeweb.com/portal-plugin-billing/internal/db"
	"go.lumeweb.com/portal/core"
	"go.lumeweb.com/portal/db"
	"go.lumeweb.com/portal/db/models"
	"go.lumeweb.com/portal/event"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"math"
	"sort"
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

			if !_service.enabled() {
				return nil
			}

			trp := httptransport.New(_service.cfg.KillBill.APIServer, "", nil)
			trp.Producers["text/xml"] = runtime.TextProducer()
			trp.Debug = false
			authWriter := runtime.ClientAuthInfoWriterFunc(func(r runtime.ClientRequest, _ strfmt.Registry) error {
				encoded := base64.StdEncoding.EncodeToString([]byte(_service.cfg.KillBill.Username + ":" + _service.cfg.KillBill.Password))
				if err := r.SetHeaderParam("Authorization", "Basic "+encoded); err != nil {
					return err
				}
				if err := r.SetHeaderParam("X-KillBill-ApiKey", _service.cfg.KillBill.APIKey); err != nil {
					return err
				}
				if err := r.SetHeaderParam("X-KillBill-ApiSecret", _service.cfg.KillBill.APISecret); err != nil {
					return err
				}
				return nil
			})
			_service.api = kbclient.New(trp, strfmt.Default, authWriter, kbclient.KillbillDefaults{})

			info, err := _service.api.NodesInfo.GetNodesInfo(ctx, &nodes_info.GetNodesInfoParams{})
			if err != nil || info.Payload == nil || len(info.Payload) == 0 {
				return fmt.Errorf("failed to connect to KillBill: %v", err)
			}

			event.Listen[*event.UserCreatedEvent](ctx, event.EVENT_USER_CREATED, func(evt *event.UserCreatedEvent) error {
				return _service.CreateCustomer(ctx, evt.User())
			})

			return nil
		}),
	), nil
}

func (b *BillingServiceDefault) ID() string {
	return BILLING_SERVICE
}

func (b *BillingServiceDefault) CreateCustomer(ctx context.Context, user *models.User) error {
	if !b.enabled() {
		return nil
	}

	externalKey := strconv.FormatUint(uint64(user.ID), 10)

	result, err := b.api.Account.GetAccountByKey(ctx, &account.GetAccountByKeyParams{
		ExternalKey: externalKey,
	})

	if err != nil || result == nil {
		if kbErr, ok := err.(*kbcommon.KillbillError); ok {
			if kbErr.HTTPCode != 404 {
				return nil
			}
		}
	}

	_, err = b.api.Account.CreateAccount(ctx, &account.CreateAccountParams{
		Body: &kbmodel.Account{
			ExternalKey: externalKey,
			Name:        fmt.Sprintf("%s %s", user.FirstName, user.LastName),
		},
	})

	if err != nil {
		return err
	}

	return nil
}

func (b *BillingServiceDefault) CreateCustomerById(ctx context.Context, userID uint) error {
	exists, user, err := b.user.AccountExists(userID)
	if err != nil {
		return err
	}

	if !exists {
		return errors.New("user does not exist")
	}

	return b.CreateCustomer(ctx, user)
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

	plan, err := b.getPlanByIdentifier(ctx, *subscription.PlanName)
	if err != nil {
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

func (b *BillingServiceDefault) getPlanByIdentifier(ctx context.Context, identifier string) (*pluginDb.Plan, error) {
	var plan pluginDb.Plan

	if err := db.RetryableTransaction(b.ctx, b.db, func(tx *gorm.DB) *gorm.DB {
		return tx.WithContext(ctx).Model(&pluginDb.Plan{}).Where(&pluginDb.Plan{Identifier: identifier}).First(&plan)
	}); err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			b.logger.Error("failed to get plan", zap.Error(err))
		}
		return nil, err
	}

	return &plan, nil
}

func (b *BillingServiceDefault) GetPlans(ctx context.Context) ([]*messages.SubscriptionPlan, error) {
	if !b.enabled() {
		return nil, nil
	}

	plans, err := b.api.Catalog.GetCatalogJSON(ctx, &catalog.GetCatalogJSONParams{})
	if err != nil {
		return nil, err
	}

	var result []*messages.SubscriptionPlan

	if len(plans.Payload) == 0 {
		return result, nil
	}

	sortedPlans, err := b.getSortedPlans(ctx, plans.Payload[0])
	if err != nil {
		return nil, err
	}

	for _, plan := range sortedPlans {
		basePlan, err := b.getBasePlanByID(ctx, plan.Name)
		if err != nil {
			return nil, err
		}

		if basePlan.FinalPhaseRecurringPrice == nil {
			continue
		}

		localPlan, err := b.getPlanByIdentifier(ctx, basePlan.Plan)
		if err != nil {
			return nil, err
		}

		var period messages.SubscriptionPlanPeriod

		switch basePlan.FinalPhaseBillingPeriod {
		case kbmodel.PlanDetailFinalPhaseBillingPeriodMONTHLY:
			period = messages.SubscriptionPlanPeriodMonth
		case kbmodel.PlanDetailFinalPhaseBillingPeriodANNUAL:
			period = messages.SubscriptionPlanPeriodYear
		default:
			continue
		}

		planName, err := b.getPlanNameById(ctx, basePlan.Plan)
		if err != nil {
			continue
		}

		result = append(result, &messages.SubscriptionPlan{
			Name:       planName,
			Identifier: basePlan.Plan,
			Price:      basePlan.FinalPhaseRecurringPrice[0].Value,
			Period:     period,
			Upload:     localPlan.Upload,
			Download:   localPlan.Download,
			Storage:    localPlan.Storage,
		})
	}

	return result, nil
}

func (b *BillingServiceDefault) getPlanNameById(ctx context.Context, id string) (string, error) {
	plans, err := b.api.Catalog.GetCatalogJSON(ctx, &catalog.GetCatalogJSONParams{})

	if err != nil {
		return "", err
	}

	for _, _catalog := range plans.Payload {
		for _, product := range _catalog.Products {
			for _, plan := range product.Plans {
				if plan.Name == id {
					return plan.PrettyName, nil
				}
			}
		}
	}

	return "", nil
}

func (b *BillingServiceDefault) getBasePlanByID(ctx context.Context, planId string) (*kbmodel.PlanDetail, error) {
	plans, err := b.api.Catalog.GetAvailableBasePlans(ctx, &catalog.GetAvailableBasePlansParams{})

	if err != nil {
		return nil, err
	}

	for _, plan := range plans.Payload {
		if plan.Plan == planId {
			return plan, nil
		}
	}

	return nil, nil
}

func (b *BillingServiceDefault) getSortedPlans(ctx context.Context, catalog *kbmodel.Catalog) ([]*kbmodel.Plan, error) {
	var plans []*kbmodel.Plan
	priceList := catalog.PriceLists[0]

	type planWithOrder struct {
		plan  *kbmodel.Plan
		order uint
	}
	var plansWithOrder []planWithOrder

	for _, planName := range priceList.Plans {
		for _, product := range catalog.Products {
			for _, plan := range product.Plans {
				if plan.Name == planName {
					localPlan, err := b.getPlanByIdentifier(ctx, plan.Name)
					if err != nil {
						// If we can't find the plan in our local database, we'll use a high order number
						plansWithOrder = append(plansWithOrder, planWithOrder{plan: plan, order: math.MaxUint32})
					} else {
						plansWithOrder = append(plansWithOrder, planWithOrder{plan: plan, order: localPlan.Order})
					}
					break
				}
			}
		}
	}

	// Sort the plans based on the Order field
	sort.Slice(plansWithOrder, func(i, j int) bool {
		return plansWithOrder[i].order < plansWithOrder[j].order
	})

	// Extract the sorted plans
	for _, p := range plansWithOrder {
		plans = append(plans, p.plan)
	}

	return plans, nil
}

/*
func findPrimaryBaseProduct(catalog []*kbmodel.Catalog) *kbmodel.Product {
	for _, _catalog := range catalog {
		for _, product := range _catalog.Products {
			if product.Type == "BASE" {
				return product
			}
		}
	}

	return nil
}*/

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
