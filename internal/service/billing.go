package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/go-openapi/runtime"
	httptransport "github.com/go-openapi/runtime/client"
	"github.com/go-openapi/strfmt"
	"github.com/killbill/kbcli/v3/kbclient"
	"github.com/killbill/kbcli/v3/kbclient/account"
	"github.com/killbill/kbcli/v3/kbclient/catalog"
	"github.com/killbill/kbcli/v3/kbclient/invoice"
	"github.com/killbill/kbcli/v3/kbclient/nodes_info"
	"github.com/killbill/kbcli/v3/kbclient/payment_method"
	"github.com/killbill/kbcli/v3/kbclient/subscription"
	"github.com/killbill/kbcli/v3/kbcommon"
	"github.com/killbill/kbcli/v3/kbmodel"
	"github.com/samber/lo"
	"go.lumeweb.com/portal-plugin-billing/internal/api/messages"
	"go.lumeweb.com/portal-plugin-billing/internal/config"
	pluginDb "go.lumeweb.com/portal-plugin-billing/internal/db"
	"go.lumeweb.com/portal/core"
	"go.lumeweb.com/portal/db"
	"go.lumeweb.com/portal/db/models"
	"go.lumeweb.com/portal/event"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"io"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

type UsageType string

const (
	StorageUsage  UsageType = "storage"
	UploadUsage   UsageType = "upload"
	DownloadUsage UsageType = "download"
)

const paymentIdCustomField = "payment_id"
const pendingCustomField = "pending"
const paymentMethodPluginName = "hyperswitch-plugin"
const subscriptionSetupCustomField = "setup"

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
			Email:       user.Email,
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
	if !b.enabled() {
		return nil
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

	sub := findActiveSubscription(bundles.Payload)

	if sub == nil {
		return 0, nil
	}

	if len(*sub.PlanName) == 0 {
		return 0, nil
	}

	plan, err := b.getPlanByIdentifier(ctx, *sub.PlanName)
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

func (b *BillingServiceDefault) getPlanPriceById(ctx context.Context, id string, kind string, currency string) (float64, error) {
	plans, err := b.api.Catalog.GetCatalogJSON(ctx, &catalog.GetCatalogJSONParams{})

	if err != nil {
		return 0, err
	}

	for _, _catalog := range plans.Payload {
		for _, product := range _catalog.Products {
			for _, plan := range product.Plans {
				if plan.Name == id {
					for _, price := range plan.Phases {
						if price.Type == kind {
							for _, priceValue := range price.Prices {
								if priceValue.Currency == kbmodel.PriceCurrencyEnum(currency) {
									return priceValue.Value, nil
								}
							}
						}
					}
				}
			}
		}
	}
	return 0, nil
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

func (b *BillingServiceDefault) GetSubscription(ctx context.Context, userID uint) (*messages.SubscriptionResponse, error) {
	if !b.enabled() {
		return nil, nil
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
	publishableKey := b.cfg.Hyperswitch.PublishableKey

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
			}

			cfState, err := b.getCustomField(ctx, sub.SubscriptionID, pendingCustomField)
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
			}
		}
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
			PublishableKey: publishableKey,
		},
	}, nil
}

func (b *BillingServiceDefault) ChangeSubscription(ctx context.Context, userID uint, planID string) error {
	if !b.enabled() {
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
		return b.handleNewSubscription(ctx, acct.Payload.AccountID, planID)
	}

	cfPending, err := b.getCustomField(ctx, sub.SubscriptionID, pendingCustomField)
	if err != nil {
		return err
	}

	if sub.State == kbmodel.SubscriptionStatePENDING || (cfPending != nil && *cfPending.Value == "1") {
		return b.handlePendingSubscription(ctx, sub)
	} else if sub.State == kbmodel.SubscriptionStateACTIVE {
		return b.submitSubscriptionPlanChange(ctx, sub.SubscriptionID, planID)
	}

	return fmt.Errorf("unexpected subscription state: %s", sub.State)
}

func (b *BillingServiceDefault) ConnectSubscription(ctx context.Context, userID uint, paymentMethodID string) error {
	if !b.enabled() {
		return nil
	}

	// Verify the payment method ID
	if err := b.verifyPaymentMethod(ctx, paymentMethodID); err != nil {
		return fmt.Errorf("invalid payment method: %w", err)
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
		return fmt.Errorf("no active or pending subscription found")
	}

	cfPending, err := b.getCustomField(ctx, sub.SubscriptionID, pendingCustomField)
	if err != nil {
		return err
	}

	if sub.State == kbmodel.SubscriptionStatePENDING || (cfPending != nil && *cfPending.Value == "1") {
		err = b.setUserPaymentMethod(ctx, acct.Payload.AccountID, paymentMethodID)
		if err != nil {
			return err
		}

		err = b.deleteCustomField(ctx, sub.SubscriptionID, pendingCustomField)
		if err != nil {
			return err
		}

		subSetup, err := b.getCustomField(ctx, sub.SubscriptionID, subscriptionSetupCustomField)
		if err != nil {
			return err
		}

		if subSetup != nil {
			return nil
		}

		unpaid := true

		invoices, err := b.api.Account.GetInvoicesForAccount(ctx, &account.GetInvoicesForAccountParams{
			AccountID:          acct.Payload.AccountID,
			UnpaidInvoicesOnly: &unpaid,
		})
		if err != nil {
			return err
		}

		for _, inv := range invoices.Payload {
			_, err = b.api.Invoice.AdjustInvoiceItem(ctx, &invoice.AdjustInvoiceItemParams{
				InvoiceID: inv.InvoiceID,
				Body: &kbmodel.InvoiceItem{
					AccountID:     &acct.Payload.AccountID,
					InvoiceItemID: inv.Items[0].InvoiceItemID,
					Amount:        inv.Amount,
					Currency:      kbmodel.InvoiceItemCurrencyEnum(inv.Currency),
					Description:   "Initial outside subscription payment",
				},
			})

			if err != nil {
				return err
			}
		}

		err = b.setCustomField(ctx, sub.SubscriptionID, subscriptionSetupCustomField, "1")
		if err != nil {
			return err
		}

		return nil
	} else if sub.State == kbmodel.SubscriptionStateACTIVE {
		return b.setUserPaymentMethod(ctx, acct.Payload.AccountID, paymentMethodID)
	}

	return fmt.Errorf("unexpected subscription state: %s", sub.State)
}

func (b *BillingServiceDefault) prunePaymentMethods(ctx context.Context, acctID strfmt.UUID) error {
	paymentMethods, err := b.api.Account.GetPaymentMethodsForAccount(ctx, &account.GetPaymentMethodsForAccountParams{
		AccountID: acctID,
	})
	if err != nil {
		return err
	}

	for _, method := range paymentMethods.Payload {
		if method.PluginName == paymentMethodPluginName {
			if method.IsDefault {
				continue
			}
			_, err = b.api.PaymentMethod.DeletePaymentMethod(ctx, &payment_method.DeletePaymentMethodParams{
				PaymentMethodID: method.PaymentMethodID,
			})
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (b *BillingServiceDefault) setUserPaymentMethod(ctx context.Context, acctID strfmt.UUID, paymentMethodID string) error {
	def := true
	_, err := b.api.Account.CreatePaymentMethod(ctx, &account.CreatePaymentMethodParams{
		AccountID: acctID,
		Body: &kbmodel.PaymentMethod{
			PluginName: paymentMethodPluginName,
			PluginInfo: &kbmodel.PaymentMethodPluginDetail{
				IsDefaultPaymentMethod: true,
				Properties: []*kbmodel.PluginProperty{
					{
						Key:         "mandateId",
						Value:       paymentMethodID,
						IsUpdatable: false,
					},
				},
			},
			IsDefault: true,
		},
		IsDefault: &def,
	})
	if err != nil {
		return err
	}

	err = b.prunePaymentMethods(ctx, acctID)
	if err != nil {
		return err
	}

	return nil
}

func (b *BillingServiceDefault) GenerateEphemeralKey(ctx context.Context, userID uint) (*messages.EphemeralKeyResponse, error) {
	acct, err := b.api.Account.GetAccountByKey(ctx, &account.GetAccountByKeyParams{
		ExternalKey: strconv.FormatUint(uint64(userID), 10),
	})
	if err != nil {
		return nil, err
	}

	data := &EphemeralKeyRequest{
		CustomerID: acct.Payload.AccountID.String(),
	}
	url := fmt.Sprintf("%s/ephemeral_keys", b.cfg.Hyperswitch.APIServer)

	resp, err := b.makeRequest(ctx, "POST", url, data)
	if err != nil {
		return nil, err
	}

	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			b.logger.Error("error closing response body", zap.Error(err))
		}
	}(resp.Body)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var key EphemeralKeyResponse
	if err = json.Unmarshal(body, &key); err != nil {
		return nil, err
	}

	return &messages.EphemeralKeyResponse{
		Key: key.Secret,
	}, nil
}

func (b *BillingServiceDefault) RequestPaymentMethodChange(ctx context.Context, userID uint) (*messages.RequestPaymentMethodChangeResponse, error) {
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

	sub := findActiveSubscription(bundles.Payload)
	if sub == nil {
		return nil, fmt.Errorf("no active subscription found")
	}

	clientSecret, err := b.createNewPayment(ctx, acct.Payload.AccountID, sub, true)
	if err != nil {
		return nil, err
	}

	return &messages.RequestPaymentMethodChangeResponse{
		ClientSecret: clientSecret,
	}, nil
}

func (b *BillingServiceDefault) CancelSubscription(ctx context.Context, userID uint) error {
	return b.ChangeSubscription(ctx, userID, b.cfg.FreePlan)
}

func (b *BillingServiceDefault) verifyPaymentMethod(ctx context.Context, paymentMethodID string) error {
	url := fmt.Sprintf("%s/payment_methods/%s", b.cfg.Hyperswitch.APIServer, paymentMethodID)

	resp, err := b.makeRequest(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			b.logger.Error("error closing response body", zap.Error(err))
		}
	}(resp.Body)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to verify payment method, status: %d, body: %s", resp.StatusCode, string(body))
	}

	return nil
}

func (b *BillingServiceDefault) makeRequest(ctx context.Context, method, url string, payload interface{}) (*http.Response, error) {
	var body io.Reader
	if payload != nil {
		payloadBytes, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("error marshaling payload: %w", err)
		}
		body = bytes.NewBuffer(payloadBytes)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("api-key", b.cfg.Hyperswitch.APIKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error making request: %w", err)
	}

	return resp, nil
}

func (b *BillingServiceDefault) handleNewSubscription(ctx context.Context, accountID strfmt.UUID, planId string) error {
	// Create a new subscription
	resp, err := b.api.Subscription.CreateSubscription(ctx, &subscription.CreateSubscriptionParams{
		Body: &kbmodel.Subscription{
			AccountID: accountID,
			PlanName:  &planId,
		},
	})

	if err != nil {
		return err
	}

	// Parse subscription ID from the Location header
	locationHeader := resp.HttpResponse.GetHeader("Location")
	subID, err := parseSubscriptionIDFromLocation(locationHeader)
	if err != nil {
		return fmt.Errorf("failed to parse subscription ID: %w", err)
	}

	err = b.setCustomField(ctx, strfmt.UUID(subID), pendingCustomField, "1")
	if err != nil {
		return err
	}

	// Fetch the subscription details
	sub, err := b.api.Subscription.GetSubscription(ctx, &subscription.GetSubscriptionParams{
		SubscriptionID: strfmt.UUID(subID),
	})
	if err != nil {
		return fmt.Errorf("failed to fetch subscription details: %w", err)
	}

	// Create new payment
	_, err = b.createNewPayment(ctx, accountID, sub.Payload, false)
	if err != nil {
		return fmt.Errorf("failed to create new payment: %w", err)
	}

	return nil
}

func (b *BillingServiceDefault) handlePendingSubscription(ctx context.Context, sub *kbmodel.Subscription) error {
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
}

func (b *BillingServiceDefault) getCustomField(ctx context.Context, subscriptionID strfmt.UUID, fieldName string) (*kbmodel.CustomField, error) {
	fields, err := b.api.Subscription.GetSubscriptionCustomFields(ctx, &subscription.GetSubscriptionCustomFieldsParams{
		SubscriptionID: subscriptionID,
	})
	if err != nil {
		return nil, err
	}

	for _, field := range fields.Payload {
		if *field.Name == fieldName {
			return field, nil
		}
	}

	return nil, nil
}

func (b *BillingServiceDefault) setCustomField(ctx context.Context, subscriptionID strfmt.UUID, fieldName, value string) error {
	existingField, err := b.getCustomField(ctx, subscriptionID, fieldName)
	if err != nil {
		return err
	}

	if existingField == nil {
		_, err = b.api.Subscription.CreateSubscriptionCustomFields(ctx, &subscription.CreateSubscriptionCustomFieldsParams{
			SubscriptionID: subscriptionID,
			Body: []*kbmodel.CustomField{
				{
					Name:  &fieldName,
					Value: &value,
				},
			},
		})
		return err
	}

	existingField.Value = &value

	_, err = b.api.Subscription.ModifySubscriptionCustomFields(ctx, &subscription.ModifySubscriptionCustomFieldsParams{
		SubscriptionID: subscriptionID,
		Body: []*kbmodel.CustomField{
			existingField,
		},
	})

	return err
}

func (b *BillingServiceDefault) deleteCustomField(ctx context.Context, subscriptionID strfmt.UUID, fieldName string) error {
	field, err := b.getCustomField(ctx, subscriptionID, fieldName)
	if err != nil {
		return err
	}

	if field == nil {
		return nil
	}

	_, err = b.api.Subscription.DeleteSubscriptionCustomFields(ctx, &subscription.DeleteSubscriptionCustomFieldsParams{
		SubscriptionID: subscriptionID,
		CustomField:    []strfmt.UUID{field.CustomFieldID},
	})

	return err
}

func (b *BillingServiceDefault) createNewPayment(ctx context.Context, accountID strfmt.UUID, sub *kbmodel.Subscription, zeroAuth bool) (string, error) {
	url := fmt.Sprintf("%s/payments", b.cfg.Hyperswitch.APIServer)

	planName, err := b.getPlanNameById(ctx, *sub.PlanName)
	if err != nil {
		return "", err
	}

	acct, err := b.api.Account.GetAccount(ctx, &account.GetAccountParams{
		AccountID: accountID,
	})
	if err != nil {
		return "", err
	}

	planPrice, err := b.getPlanPriceById(ctx, *sub.PlanName, string(sub.PhaseType), string(acct.Payload.Currency))
	if err != nil {
		return "", err
	}

	exists, userAcct, err := b.user.EmailExists(acct.Payload.Email)
	if err != nil || !exists {
		if !exists {
			return "", fmt.Errorf("user does not exist")
		}

		return "", err
	}

	amount := planPrice * 100
	description := fmt.Sprintf("Subscription change to plan: %s", planName)
	paymentType := "new_mandate"

	if zeroAuth {
		amount = 0
		description = "Authorization for new payment method"
		paymentType = "setup_mandate"
	}

	payload := PaymentRequest{
		Amount:           amount,
		PaymentType:      paymentType,
		SetupFutureUsage: "off_session",
		Currency:         string(acct.Payload.Currency),
		Confirm:          false,
		Customer: Customer{
			ID:    accountID.String(),
			Name:  acct.Payload.Name,
			Email: acct.Payload.Email,
		},
		Billing: CustomerBilling{
			Address: CustomerBillingAddress{
				FirstName: userAcct.FirstName,
				LastName:  userAcct.LastName,
				City:      acct.Payload.City,
				Country:   acct.Payload.Country,
				Line1:     acct.Payload.Address1,
				Zip:       acct.Payload.PostalCode,
				State:     acct.Payload.State,
			},
			Email: acct.Payload.Email,
		},
		Description: description,
		Metadata: PaymentMetadata{
			SubscriptionID: sub.SubscriptionID.String(),
			PlanID:         *sub.PlanName,
		},
	}

	resp, err := b.makeRequest(ctx, "POST", url, payload)
	if err != nil {
		return "", fmt.Errorf("error creating payment: %w", err)
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			b.logger.Error("error closing response body", zap.Error(err))
		}
	}(resp.Body)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("error creating payment, status: %d, body: %s", resp.StatusCode, string(body))
	}

	var paymentResponse PaymentResponse
	if err = json.NewDecoder(resp.Body).Decode(&paymentResponse); err != nil {
		return "", fmt.Errorf("error decoding response: %w", err)
	}

	if !zeroAuth {
		// Store the payment ID in a custom field
		if err = b.setCustomField(ctx, sub.SubscriptionID, paymentIdCustomField, paymentResponse.PaymentID); err != nil {
			return "", fmt.Errorf("error setting custom field: %w", err)
		}
	}

	return paymentResponse.ClientSecret, nil
}

func (b *BillingServiceDefault) cancelPayment(ctx context.Context, paymentID string) error {
	url := fmt.Sprintf("%s/payments/%s/cancel", b.cfg.Hyperswitch.APIServer, paymentID)

	payload := PaymentCancelRequest{
		CancellationReason: "payment_expired",
	}

	resp, err := b.makeRequest(ctx, "POST", url, payload)
	if err != nil {
		return fmt.Errorf("error cancelling payment: %w", err)
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			b.logger.Error("error closing response body", zap.Error(err))
		}
	}(resp.Body)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("error cancelling payment, status: %d, body: %s", resp.StatusCode, string(body))
	}

	return nil
}

func (b *BillingServiceDefault) submitSubscriptionPlanChange(ctx context.Context, subscriptionID strfmt.UUID, planID string) error {
	_, err := b.api.Subscription.ChangeSubscriptionPlan(ctx, &subscription.ChangeSubscriptionPlanParams{
		SubscriptionID: subscriptionID,
		Body: &kbmodel.Subscription{
			PlanName: &planID,
		},
	})

	if err != nil {
		return err
	}

	return nil
}

func (b *BillingServiceDefault) fetchClientSecret(ctx context.Context, paymentID string) (string, time.Time, error) {
	url := fmt.Sprintf("%s/payments/%s?force_sync=true&expand_captures=true&expand_attempts=true", b.cfg.Hyperswitch.APIServer, paymentID)

	resp, err := b.makeRequest(ctx, "GET", url, nil)
	if err != nil {
		return "", time.Time{}, err
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			b.logger.Error("error closing response body", zap.Error(err))
		}
	}(resp.Body)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", time.Time{}, err
	}

	var paymentResponse struct {
		ClientSecret string    `json:"client_secret"`
		Created      time.Time `json:"created"`
	}

	if err := json.Unmarshal(body, &paymentResponse); err != nil {
		return "", time.Time{}, err
	}

	return paymentResponse.ClientSecret, paymentResponse.Created, nil
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

func findActiveOrPendingSubscription(bundles []*kbmodel.Bundle) *kbmodel.Subscription {
	for _, bundle := range bundles {
		for _, sub := range bundle.Subscriptions {
			if sub.State == kbmodel.SubscriptionStateACTIVE || sub.State == kbmodel.SubscriptionStatePENDING {
				return sub
			}
		}
	}

	return nil
}

func findActiveSubscription(bundles []*kbmodel.Bundle) *kbmodel.Subscription {
	for _, bundle := range bundles {
		for _, sub := range bundle.Subscriptions {
			if sub.State == kbmodel.SubscriptionStateACTIVE {
				return sub
			}
		}
	}

	return nil
}

func localBillingPeriodToRemote(period messages.SubscriptionPlanPeriod) kbmodel.PlanDetailFinalPhaseBillingPeriodEnum {
	switch period {
	case messages.SubscriptionPlanPeriodMonth:
		return kbmodel.PlanDetailFinalPhaseBillingPeriodMONTHLY
	case messages.SubscriptionPlanPeriodYear:
		return kbmodel.PlanDetailFinalPhaseBillingPeriodANNUAL
	default:
		return kbmodel.PlanDetailFinalPhaseBillingPeriodMONTHLY
	}
}

func remoteBillingPeriodToLocal(period kbmodel.PlanDetailFinalPhaseBillingPeriodEnum) messages.SubscriptionPlanPeriod {
	switch period {
	case kbmodel.PlanDetailFinalPhaseBillingPeriodMONTHLY:
		return messages.SubscriptionPlanPeriodMonth
	case kbmodel.PlanDetailFinalPhaseBillingPeriodANNUAL:
		return messages.SubscriptionPlanPeriodYear
	default:
		return messages.SubscriptionPlanPeriodMonth
	}
}

func remoteSubscriptionPhaseToLocal(phase kbmodel.SubscriptionPhaseTypeEnum) messages.SubscriptionPlanPeriod {
	switch phase {
	case kbmodel.SubscriptionPhaseTypeEVERGREEN:
		return messages.SubscriptionPlanPeriodMonth
	case kbmodel.SubscriptionPhaseTypeTRIAL:
		return messages.SubscriptionPlanPeriodMonth
	default:
		return messages.SubscriptionPlanPeriodMonth
	}
}

func remoteSubscriptionStatusToLocal(status kbmodel.SubscriptionStateEnum) messages.SubscriptionPlanStatus {
	switch status {
	case kbmodel.SubscriptionStateACTIVE:
		return messages.SubscriptionPlanStatusActive
	case kbmodel.SubscriptionStatePENDING:
		return messages.SubscriptionPlanStatusPending
	default:
		return messages.SubscriptionPlanStatusPending
	}
}
func parseSubscriptionIDFromLocation(location string) (string, error) {
	parts := strings.Split(location, "/")
	if len(parts) == 0 {
		return "", fmt.Errorf("invalid Location header format")
	}
	return parts[len(parts)-1], nil
}

type Subscription struct {
	SubscriptionID strfmt.UUID
	PlanName       *string
	// Add other fields as needed
}

// PaymentRequest represents the payment request payload
type PaymentRequest struct {
	Amount           float64         `json:"amount"`
	Currency         string          `json:"currency"`
	Confirm          bool            `json:"confirm"`
	Customer         Customer        `json:"customer"`
	Billing          CustomerBilling `json:"billing,omitempty"`
	Description      string          `json:"description"`
	Metadata         PaymentMetadata `json:"metadata"`
	SetupFutureUsage string          `json:"setup_future_usage"`
	PaymentType      string          `json:"payment_type"`
}

// Customer represents the customer information in the payment request
type Customer struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

type CustomerBilling struct {
	Address CustomerBillingAddress `json:"address"`
	Email   string                 `json:"email"`
}

type CustomerBillingAddress struct {
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	City      string `json:"city"`
	Country   string `json:"country"`
	Line1     string `json:"line1"`
	Zip       string `json:"zip"`
	State     string `json:"state"`
}

// PaymentMetadata represents the metadata for a payment
type PaymentMetadata struct {
	SubscriptionID string `json:"subscription_id"`
	PlanID         string `json:"plan_id"`
}

// PaymentResponse represents the response from the payment creation API
type PaymentResponse struct {
	PaymentID    string `json:"payment_id"`
	ClientSecret string `json:"client_secret"`
}

type PaymentCancelRequest struct {
	CancellationReason string `json:"cancellation_reason"`
}

type EphemeralKeyRequest struct {
	CustomerID string `json:"customer_id"`
}

type EphemeralKeyResponse struct {
	Secret string `json:"secret"`
}
