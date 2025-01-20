package service

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/avast/retry-go"
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

func (b *BillingServiceDefault) UpdateBillingInfo(ctx context.Context, userID uint, billingInfo *messages.Billing) error {
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

	if acct.Payload.Address1 != billingInfo.Address.Line1 && len(billingInfo.Address.Line1) > 0 {
		acctChanges.Address1 = billingInfo.Address.Line1
	}

	if acct.Payload.Address2 != billingInfo.Address.Line2 && len(billingInfo.Address.Line2) > 0 {
		acctChanges.Address2 = billingInfo.Address.Line2
	}

	if acct.Payload.City != billingInfo.Address.City && len(billingInfo.Address.City) > 0 {
		acctChanges.City = billingInfo.Address.City
	}

	if acct.Payload.State != billingInfo.Address.State && len(billingInfo.Address.State) > 0 {
		acctChanges.State = billingInfo.Address.State
	}

	if acct.Payload.PostalCode != billingInfo.Address.PostalCode && len(billingInfo.Address.PostalCode) > 0 {
		acctChanges.PostalCode = billingInfo.Address.PostalCode
	}

	if acct.Payload.Country != billingInfo.Address.Country && len(billingInfo.Address.Country) > 0 {
		acctChanges.Country = billingInfo.Address.Country
	}

	// Check if any changes were made
	if len(acctChanges.Name) == 0 && len(acctChanges.Company) == 0 &&
		len(acctChanges.Address1) == 0 && len(acctChanges.Address2) == 0 &&
		len(acctChanges.City) == 0 && len(acctChanges.State) == 0 &&
		len(acctChanges.PostalCode) == 0 && len(acctChanges.Country) == 0 {
		return nil
	}

	_, err = b.api.Account.UpdateAccount(ctx, &account.UpdateAccountParams{
		AccountID: acct.Payload.AccountID,
		Body: &kbmodel.Account{
			Name:       billingInfo.Name,
			Company:    billingInfo.Organization,
			Address1:   billingInfo.Address.Line1,
			Address2:   billingInfo.Address.Line2,
			City:       billingInfo.Address.City,
			State:      billingInfo.Address.State,
			PostalCode: billingInfo.Address.PostalCode,
			Country:    billingInfo.Address.Country,
		},
	})

	if err != nil {
		return err
	}

	return nil
}

func (b *BillingServiceDefault) GetPlans(ctx context.Context) ([]*messages.Plan, error) {
	if !b.enabled() {
		return nil, nil
	}

	if !b.paidEnabled() {
		return []*messages.Plan{b.getFreePlan()}, nil
	}

	plans, err := b.api.Catalog.GetCatalogJSON(ctx, &catalog.GetCatalogJSONParams{})
	if err != nil {
		return nil, err
	}

	var result []*messages.Plan

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

		result = append(result, &messages.Plan{
			ID:     basePlan.Plan,
			Name:   planName,
			Price:  basePlan.FinalPhaseRecurringPrice[0].Value,
			Period: period,
			Resources: messages.Resources{
				Storage:  localPlan.Upload,
				Upload:   localPlan.Download,
				Download: localPlan.Storage,
			},
		})
	}

	freePlan := b.getFreePlan()

	if freePlan != nil {
		result = slices.Insert(result, 0, freePlan)
	}

	return result, nil
}

func (b *BillingServiceDefault) GetSubscription(ctx context.Context, userID uint) (*messages.Subscription, error) {
	if !b.enabled() {
		return nil, nil
	}

	if !b.paidEnabled() {
		freePlan := b.getFreePlan()
		return &messages.Subscription{
			Plan:   freePlan,
			Status: messages.StatusActive,
			CurrentPeriod: messages.Period{
				Start: time.Unix(0, 0),
				End:   time.Now().AddDate(1, 0, 0),
			},
		}, nil
	}

	acct, err := b.getAccount(ctx, userID)
	if err != nil {
		return nil, err
	}

	sub, err := b.getSubscription(ctx, userID)
	if err != nil {
		return nil, err
	}

	var subPlan *messages.Plan

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
			subPlan = &messages.Plan{
				ID:     plan.Identifier,
				Name:   planName,
				Price:  prices[0].RecurringPrice,
				Period: remoteSubscriptionPhaseToLocal(kbmodel.SubscriptionPhaseTypeEnum(*sub.BillingPeriod)),
				Resources: messages.Resources{
					Storage:  plan.Storage,
					Upload:   plan.Upload,
					Download: plan.Download,
				},
			}
		}
	}

	if subPlan == nil {
		subPlan = b.getFreePlan()
	}

	status := messages.StatusPending
	var startDate time.Time

	if sub != nil && subPlan != nil {
		status = remoteSubscriptionStatusToLocal(sub.State)
	}
	if sub != nil {
		startDate = time.Time(sub.StartDate)
	}

	paymentAuth, err := b.getLastSubscriptionAuthorizePaymentMethod(ctx, userID)
	if err != nil {
		if !errors.Is(err, errNoPaymentAuthorization) {
			return nil, err
		}
	}

	_payment := &messages.Payment{}
	if paymentAuth != nil {
		_payment = paymentAuth
		_payment.PublishableKey = b.cfg.Hyperswitch.PublishableKey
	}

	return &messages.Subscription{
		Plan:   subPlan,
		Status: status,
		CurrentPeriod: messages.Period{
			Start: startDate,
			End:   time.Time{},
		},
		Billing: &messages.Billing{
			Name:         acct.Name,
			Organization: acct.Company,
			Address: messages.Address{
				Line1:             acct.Address1,
				Line2:             acct.Address2,
				City:              acct.City,
				State:             acct.State,
				PostalCode:        acct.PostalCode,
				Country:           acct.Country,
				DependentLocality: "", // KillBill doesn't have these fields
				SortingCode:       "", // KillBill doesn't have these fields
			},
		},
		Payment: _payment,
	}, nil
}

func (b *BillingServiceDefault) CreateSubscription(ctx context.Context, userID uint, planID string) error {
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

	if isSubscriptionPending(sub) {
		fullSub, err := b.GetSubscription(ctx, userID)
		if err != nil {
			return err
		}

		if !fullSub.Payment.ExpiresAt.IsZero() {
			if fullSub.Payment.ExpiresAt.Before(time.Now()) {
				err = b.authorizePayment(ctx, acct.Payload.AccountID, sub.SubscriptionID)
				if err != nil {
					return err
				}
			}
		}
	}

	return fmt.Errorf("subscription already exists")
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

func (b *BillingServiceDefault) UpdateSubscription(ctx context.Context, userID uint, planID string) error {
	if planID == b.cfg.FreePlanID {
		return b.CancelSubscription(ctx, userID)
	}

	if !b.enabled() || !b.paidEnabled() {
		return nil
	}

	acct, err := b.getAccount(ctx, userID)
	if err != nil {
		return err
	}

	sub, err := b.getSubscription(ctx, userID)
	if err != nil {
		return err
	}

	if sub == nil {
		return b.handleNewSubscription(ctx, acct.AccountID, planID)
	}

	if sub.State == kbmodel.SubscriptionStateACTIVE || sub.State == kbmodel.SubscriptionStateBLOCKED {
		return b.submitSubscriptionPlanChange(ctx, sub, planID)
	}

	return fmt.Errorf("unexpected subscription state: %s", sub.State)
}

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

	invoices, err := b.getInvoicesForSubscription(ctx, acct.Payload.AccountID, sub.SubscriptionID)
	if err != nil {
		return err
	}

	invoices = filterRecurringInvoices(invoices)
	invoices = filterUnpaidInvoices(invoices)

	for _, _invoice := range invoices {
		err = b.setInvoiceControlTag(ctx, _invoice.InvoiceID, TagInvoiceWrittenOff, true)
		if err != nil {
			return err
		}
	}

	if err = retry.Do(
		func() error {
			acct, err := b.api.Account.GetOverdueAccount(ctx, &account.GetOverdueAccountParams{AccountID: acct.Payload.AccountID})
			if err != nil {
				return err
			}

			if acct.Payload.IsClearState {
				return nil
			}

			return fmt.Errorf("account is overdue")
		},
		retry.Attempts(10),
	); err != nil {
		return err
	}

	return nil
}
