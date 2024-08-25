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
	"github.com/killbill/kbcli/v3/kbclient/nodes_info"
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
)

type UsageType string

const (
	StorageUsage  UsageType = "storage"
	UploadUsage   UsageType = "upload"
	DownloadUsage UsageType = "download"
)

const paymentIdCustomField = "payment_id"

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

			if sub.State == kbmodel.SubscriptionStatePENDING {
				// Get the client secret
				_paymentID, err := b.getCustomField(ctx, sub.SubscriptionID, paymentIdCustomField)
				if err != nil {
					return nil, err
				}

				if _paymentID != nil {
					_clientSecret, err := b.fetchClientSecret(ctx, *_paymentID.Value)
					if err != nil {
						return nil, err
					}

					paymentID = *_paymentID.Value
					clientSecret = _clientSecret
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
			PaymentID:    paymentID,
			ClientSecret: clientSecret,
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

	/*	if sub.State == kbmodel.SubscriptionStatePENDING {
			return b.handlePendingSubscription(ctx, acct.Payload.AccountID, sub.SubscriptionID, planID)
		} else if sub.State == kbmodel.SubscriptionStateACTIVE {
			return b.submitSubscriptionPlanChange(ctx, sub.SubscriptionID, planID)
		}*/

	return fmt.Errorf("unexpected subscription state: %s", sub.State)
}

func (b *BillingServiceDefault) handleNewSubscription(ctx context.Context, accountID strfmt.UUID, planId string) error {
	// Create a new subscription
	sub, err := b.api.Subscription.CreateSubscription(ctx, &subscription.CreateSubscriptionParams{
		Body: &kbmodel.Subscription{
			AccountID: accountID,
			PlanName:  &planId,
		},
	})

	if err != nil {
		return err
	}

	err = b.createNewPayment(ctx, accountID, sub.Payload)
	if err != nil {
		return err
	}

	return nil
}

/*
func (b *BillingServiceDefault) handlePendingSubscription(ctx context.Context, subscription *kbmodel.Subscription) error {
	paymentID, err := b.getCustomField(ctx, accountID, subscriptionID, customFieldName)
	if err != nil {
		return err
	}

	if paymentID == "" {
		return b.createNewPayment(ctx, accountID, subscriptionID, planID)
	}

	// TODO: Implement logic to check if the client secret has expired
	// For now, we'll assume it has expired and recreate the payment
	if err := b.cancelPayment(ctx, paymentID); err != nil {
		return err
	}

	return b.createNewPayment(ctx, accountID, subscriptionID, planID)
}*/

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

func (b *BillingServiceDefault) createNewPayment(ctx context.Context, accountID strfmt.UUID, sub *kbmodel.Subscription) error {
	url := fmt.Sprintf("%s/payments", b.cfg.Hyperswitch.APIServer)

	planName, err := b.getPlanNameById(ctx, *sub.PlanName)
	if err != nil {
		return err
	}

	// Create the payment request payload
	payload := map[string]interface{}{
		"amount":   6540, // This should be dynamically set based on the plan
		"currency": "USD",
		"confirm":  false,
		"customer": map[string]string{
			"id": accountID.String(),
		},
		"description": fmt.Sprintf("Subscription change to plan: %s", planName),
		"metadata": map[string]string{
			"subscription_id": sub.SubscriptionID.String(),
			"plan_id":         *sub.PlanName,
		},
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("error marshaling payment payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return fmt.Errorf("error creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Add("Authorization", fmt.Sprintf("api-key %s", b.cfg.Hyperswitch.APIKey))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("error making request: %w", err)
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			b.logger.Error("error closing response body", zap.Error(err))
		}
	}(resp.Body)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("error creating payment, status: %d, body: %s", resp.StatusCode, string(body))
	}

	var paymentResponse struct {
		PaymentID string `json:"payment_id"`
	}

	if err = json.NewDecoder(resp.Body).Decode(&paymentResponse); err != nil {
		return fmt.Errorf("error decoding response: %w", err)
	}

	// Store the payment ID in a custom field
	if err = b.setCustomField(ctx, sub.SubscriptionID, paymentIdCustomField, paymentResponse.PaymentID); err != nil {
		return fmt.Errorf("error setting custom field: %w", err)
	}

	return nil
}

/*func (b *BillingServiceDefault) cancelPayment(ctx context.Context, paymentID string) error {
	url := fmt.Sprintf("%s/payments/%s/cancel", paymentAPIBaseURL, paymentID)

	req, err := http.NewRequestWithContext(ctx, "POST", url, nil)
	if err != nil {
		return fmt.Errorf("error creating request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	// TODO: Add API key authentication header

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("error making request: %w", err)
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
}*/

func (b *BillingServiceDefault) submitSubscriptionPlanChange(ctx context.Context, subscriptionID, planID string) error {
	// TODO: Implement API call to change the subscription plan
	return nil
}

func (b *BillingServiceDefault) fetchClientSecret(ctx context.Context, paymentID string) (string, error) {
	url := fmt.Sprintf("%s/payments/%s?force_sync=true&expand_captures=true&expand_attempts=true", b.cfg.Hyperswitch.APIServer, paymentID)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", err
	}

	req.Header.Add("Accept", "application/json")
	req.Header.Add("Authorization", fmt.Sprintf("api-key %s", b.cfg.Hyperswitch.APIKey))

	// Use http.DefaultClient instead of b.httpClient
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			b.logger.Error("error closing response body", zap.Error(err))
		}
	}(resp.Body)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var paymentResponse struct {
		ClientSecret string `json:"client_secret"`
	}

	if err := json.Unmarshal(body, &paymentResponse); err != nil {
		return "", err
	}

	return paymentResponse.ClientSecret, nil
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
