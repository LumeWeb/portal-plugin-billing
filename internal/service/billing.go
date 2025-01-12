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
	"github.com/killbill/kbcli/v3/kbclient/subscription"
	"github.com/killbill/kbcli/v3/kbcommon"
	"github.com/killbill/kbcli/v3/kbmodel"
	"github.com/samber/lo"
	"go.lumeweb.com/portal-plugin-billing/internal/api/messages"
	"go.lumeweb.com/portal-plugin-billing/internal/config"
	"go.lumeweb.com/portal/core"
	"go.lumeweb.com/portal/db/models"
	"gorm.io/gorm"
	"slices"
	"strconv"
	"time"
)

type UsageType string

const BILLING_SERVICE = "billing"

const (
	StorageUsage  UsageType = "storage"
	UploadUsage   UsageType = "upload"
	DownloadUsage UsageType = "download"
)

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
			_service.logger = ctx.ServiceLogger(_service)
			_service.ctx = ctx
			_service.db = ctx.DB()
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

			return nil
		}),
	), nil
}

func (b *BillingServiceDefault) ID() string {
	return BILLING_SERVICE
}

func (b *BillingServiceDefault) Config() (any, error) {
	return &config.BillingConfig{}, nil
}

func (b *BillingServiceDefault) CreateCustomer(ctx context.Context, user *models.User) error {
	if !b.enabled() || !b.paidEnabled() {
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

	if result != nil {
		return nil
	}

	_, err = b.api.Account.CreateAccount(ctx, &account.CreateAccountParams{
		Body: &kbmodel.Account{
			ExternalKey: externalKey,
			Name:        fmt.Sprintf("%s %s", user.FirstName, user.LastName),
			Email:       user.Email,
			Currency:    kbmodel.AccountCurrencyUSD,
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

func (b *BillingServiceDefault) UpdateBillingInfo(ctx context.Context, userID uint, billingInfo *messages.BillingInfo) error {
	if !b.enabled() || !b.paidEnabled() {
		return nil
	}

	err := b.normalizeBillingInfo(billingInfo)
	if err != nil {
		return err
	}

	acct, err := b.api.Account.GetAccountByKey(ctx, &account.GetAccountByKeyParams{
		ExternalKey: strconv.FormatUint(uint64(userID), 10),
	})

	if err != nil {
		return err
	}

	acctChanges := &kbmodel.Account{}

	if acct.Payload.Name != billingInfo.Name && len(billingInfo.Name) > 0 {
		acctChanges.Name = billingInfo.Name
	}

	if acct.Payload.Company != billingInfo.Organization && len(billingInfo.Organization) > 0 {
		acctChanges.Company = billingInfo.Organization
	}

	if acct.Payload.Address1 != billingInfo.Address && len(billingInfo.Address) > 0 {
		acctChanges.Address1 = billingInfo.Address
	}

	if acct.Payload.City != billingInfo.City && len(billingInfo.City) > 0 {
		acctChanges.City = billingInfo.City
	}

	if acct.Payload.State != billingInfo.State && len(billingInfo.State) > 0 {
		acctChanges.State = billingInfo.State
	}

	if acct.Payload.PostalCode != billingInfo.Zip && len(billingInfo.Zip) > 0 {
		acctChanges.PostalCode = billingInfo.Zip
	}

	if acct.Payload.Country != billingInfo.Country && len(billingInfo.Country) > 0 {
		acctChanges.Country = billingInfo.Country
	}

	if len(acctChanges.Name) == 0 && len(acctChanges.Address1) == 0 && len(acctChanges.City) == 0 && len(acctChanges.State) == 0 && len(acctChanges.PostalCode) == 0 && len(acctChanges.Country) == 0 {
		return nil
	}

	_, err = b.api.Account.UpdateAccount(ctx, &account.UpdateAccountParams{
		AccountID: acct.Payload.AccountID,
		Body: &kbmodel.Account{
			Address1:   billingInfo.Address,
			City:       billingInfo.City,
			State:      billingInfo.State,
			PostalCode: billingInfo.Zip,
			Country:    billingInfo.Country,
		},
	})

	if err != nil {
		return err
	}

	return nil
}

func (b *BillingServiceDefault) GetPlans(ctx context.Context) ([]*messages.SubscriptionPlan, error) {
	if !b.enabled() {
		return nil, nil
	}

	if !b.paidEnabled() {
		return []*messages.SubscriptionPlan{b.getFreePlan()}, nil
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

		period := remoteBillingPeriodToLocal(basePlan.FinalPhaseBillingPeriod)

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

	freePlan := b.getFreePlan()

	if freePlan != nil {
		result = slices.Insert(result, 0, freePlan)
	}

	return result, nil
}

func (b *BillingServiceDefault) GetSubscription(ctx context.Context, userID uint) (*messages.SubscriptionResponse, error) {
	if !b.enabled() {
		return nil, nil
	}

	if !b.paidEnabled() {
		freePlan := b.getFreePlan()
		return &messages.SubscriptionResponse{
			Plan:        freePlan,
			BillingInfo: messages.BillingInfo{},
			PaymentInfo: messages.PaymentInfo{},
		}, nil
	}

	err := b.CreateCustomerById(ctx, userID)
	if err != nil {
		return nil, err
	}

	acct, err := b.api.Account.GetAccountByKey(ctx, &account.GetAccountByKeyParams{
		ExternalKey: strconv.FormatUint(uint64(userID), 10),
	})

	if err != nil {
		return nil, err
	}

	bundles, err := b.api.Account.GetAccountBundles(ctx, &account.GetAccountBundlesParams{
		AccountID: acct.Payload.AccountID,
	})
	if err != nil {
		return nil, err
	}

	var subPlan *messages.SubscriptionPlan
	var paymentID string
	var clientSecret string
	var paymentExpires time.Time

	sub := findActiveOrPendingSubscription(bundles.Payload)

	if sub != nil {
		plan, err := b.getPlanByIdentifier(ctx, *sub.PlanName)
		if err != nil {
			return nil, err
		}

		planName, err := b.getPlanNameById(ctx, *sub.PlanName)
		if err != nil {
			return nil, err
		}

		prices := lo.Filter(sub.Prices, func(price *kbmodel.PhasePrice, _ int) bool {
			return kbmodel.SubscriptionPhaseTypeEnum(price.PhaseType) == sub.PhaseType
		})

		if len(prices) > 0 {
			subPlan = &messages.SubscriptionPlan{
				Name:       planName,
				Price:      prices[0].RecurringPrice,
				Status:     remoteSubscriptionStatusToLocal(sub.State),
				Identifier: plan.Identifier,
				Period:     remoteSubscriptionPhaseToLocal(kbmodel.SubscriptionPhaseTypeEnum(*sub.BillingPeriod)),
				Storage:    plan.Storage,
				Upload:     plan.Upload,
				Download:   plan.Download,
				StartDate:  &sub.StartDate,
			}

			/*			cfState, err := b.getCustomField(ctx, sub.SubscriptionID, pendingCustomField)
						if err != nil {
							return nil, err
						}

						if sub.State == kbmodel.SubscriptionStatePENDING || (cfState != nil && *cfState.Value == "1") {
							// Get the client secret
							_paymentID, err := b.getCustomField(ctx, sub.SubscriptionID, paymentIdCustomField)
							if err != nil {
								return nil, err
							}

							if _paymentID != nil {
								_clientSecret, created, err := b.fetchClientSecret(ctx, *_paymentID.Value)
								if err != nil {
									return nil, err
								}

								paymentID = *_paymentID.Value
								paymentExpires = created.Add(15 * time.Minute)
								clientSecret = _clientSecret
								subPlan.Status = messages.SubscriptionPlanStatusPending
							}
						}*/
		}
	}

	if subPlan == nil {
		subPlan = b.getFreePlan()
	}

	return &messages.SubscriptionResponse{
		Plan: subPlan,
		BillingInfo: messages.BillingInfo{
			Name:    acct.Payload.Name,
			Address: acct.Payload.Address1,
			City:    acct.Payload.City,
			State:   acct.Payload.State,
			Zip:     acct.Payload.PostalCode,
			Country: acct.Payload.Country,
		},
		PaymentInfo: messages.PaymentInfo{
			PaymentID:      paymentID,
			PaymentExpires: paymentExpires,
			ClientSecret:   clientSecret,
			//	PublishableKey: publishableKey,
		},
	}, nil
}

func (b *BillingServiceDefault) GetUserMaxStorage(userID uint) (uint64, error) {
	return b.getUsageByUserID(b.ctx, userID, StorageUsage)
}

func (b *BillingServiceDefault) GetUserMaxUpload(userID uint) (uint64, error) {
	return b.getUsageByUserID(b.ctx, userID, UploadUsage)
}

func (b *BillingServiceDefault) GetUserMaxDownload(userID uint) (uint64, error) {
	return b.getUsageByUserID(b.ctx, userID, DownloadUsage)
}

func (b *BillingServiceDefault) ChangeSubscription(ctx context.Context, userID uint, planID string) error {
	if planID == b.cfg.FreePlanID {
		return b.CancelSubscription(ctx, userID)
	}

	if !b.enabled() || !b.paidEnabled() {
		return nil
	}

	err := b.CreateCustomerById(ctx, userID)
	if err != nil {
		return err
	}

	acct, err := b.api.Account.GetAccountByKey(ctx, &account.GetAccountByKeyParams{
		ExternalKey: strconv.FormatUint(uint64(userID), 10),
	})
	if err != nil {
		return err
	}

	bundles, err := b.api.Account.GetAccountBundles(ctx, &account.GetAccountBundlesParams{
		AccountID: acct.Payload.AccountID,
	})
	if err != nil {
		return err
	}

	sub := findActiveOrPendingSubscription(bundles.Payload)
	if sub == nil {
		return b.handleNewSubscription(ctx, acct.Payload.AccountID, planID)
	}

	/*	if sub.State == kbmodel.SubscriptionStatePENDING {
		return b.handlePendingSubscription(ctx, sub)
	} else */if sub.State == kbmodel.SubscriptionStateACTIVE {
		return b.submitSubscriptionPlanChange(ctx, sub.SubscriptionID, planID)
	}

	return fmt.Errorf("unexpected subscription state: %s", sub.State)
}

/*func (b *BillingServiceDefault) handlePendingSubscription(ctx context.Context, sub *kbmodel.Subscription) error {
	paymentID, err := b.getCustomField(ctx, sub.SubscriptionID, paymentIdCustomField)
	if err != nil {
		return err
	}

	if paymentID == nil {
		_, err = b.createNewPayment(ctx, sub.AccountID, sub, false)
		return err
	}

	if err = b.cancelPayment(ctx, *paymentID.Value); err != nil {
		return err
	}

	_, err = b.createNewPayment(ctx, sub.AccountID, sub, false)
	return err
}*/

func (b *BillingServiceDefault) CancelSubscription(ctx context.Context, userID uint) error {
	if !b.enabled() || !b.paidEnabled() {
		return nil
	}

	acct, err := b.api.Account.GetAccountByKey(ctx, &account.GetAccountByKeyParams{
		ExternalKey: strconv.FormatUint(uint64(userID), 10),
	})
	if err != nil {
		return err
	}

	bundles, err := b.api.Account.GetAccountBundles(ctx, &account.GetAccountBundlesParams{
		AccountID: acct.Payload.AccountID,
	})
	if err != nil {
		return err
	}

	sub := findActiveOrPendingSubscription(bundles.Payload)
	if sub == nil {
		return nil
	}

	_, err = b.api.Subscription.CancelSubscriptionPlan(ctx, &subscription.CancelSubscriptionPlanParams{SubscriptionID: sub.SubscriptionID})
	if err != nil {
		return err
	}

	return nil
}
