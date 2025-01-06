package repository

import (
	"context"
	"fmt"
	"github.com/go-openapi/strfmt"
	"github.com/killbill/kbcli/v3/kbclient"
	"github.com/killbill/kbcli/v3/kbclient/account"
	"github.com/killbill/kbcli/v3/kbclient/catalog"
	"github.com/killbill/kbcli/v3/kbclient/nodes_info"
	"github.com/killbill/kbcli/v3/kbclient/payment_method"
	"github.com/killbill/kbcli/v3/kbclient/subscription"
	"github.com/killbill/kbcli/v3/kbmodel"
	"github.com/samber/lo"
	"go.lumeweb.com/portal-plugin-billing/internal/client/hyperswitch"
	"strconv"
)

const paymentMethodPluginName = "hyperswitch-plugin"

type KillBillRepository struct {
	api *kbclient.KillBill
}

func NewKillBillRepository(api *kbclient.KillBill) *KillBillRepository {
	return &KillBillRepository{api: api}
}

func (r *KillBillRepository) GetAccountByUserId(ctx context.Context, userID uint) (*account.GetAccountByKeyOK, error) {
	return r.api.Account.GetAccountByKey(ctx, &account.GetAccountByKeyParams{
		ExternalKey: strconv.FormatUint(uint64(userID), 10),
	})
}

func (r *KillBillRepository) GetAccount(ctx context.Context, accountID strfmt.UUID) (*account.GetAccountOK, error) {
	return r.api.Account.GetAccount(ctx, &account.GetAccountParams{
		AccountID: accountID,
	})
}

func (r *KillBillRepository) CreateAccount(ctx context.Context, acc *kbmodel.Account) (*account.CreateAccountCreated, error) {
	return r.api.Account.CreateAccount(ctx, &account.CreateAccountParams{
		Body: acc,
	})
}

func (r *KillBillRepository) UpdateAccount(ctx context.Context, accountID strfmt.UUID, acc *kbmodel.Account) (*account.UpdateAccountNoContent, error) {
	return r.api.Account.UpdateAccount(ctx, &account.UpdateAccountParams{
		AccountID: accountID,
		Body:      acc,
	})
}

func (r *KillBillRepository) GetBundlesByAccountId(ctx context.Context, accountID strfmt.UUID) (*account.GetAccountBundlesOK, error) {
	return r.api.Account.GetAccountBundles(ctx, &account.GetAccountBundlesParams{
		AccountID: accountID,
	})
}

func (r *KillBillRepository) GetSubscription(ctx context.Context, subscriptionID strfmt.UUID) (*subscription.GetSubscriptionOK, error) {
	return r.api.Subscription.GetSubscription(ctx, &subscription.GetSubscriptionParams{
		SubscriptionID: subscriptionID,
	})
}

func (r *KillBillRepository) CreateSubscription(ctx context.Context, sub *kbmodel.Subscription) (*subscription.CreateSubscriptionCreated, error) {
	return r.api.Subscription.CreateSubscription(ctx, &subscription.CreateSubscriptionParams{
		Body: sub,
	})
}

func (r *KillBillRepository) UpdateSubscription(ctx context.Context, subscriptionID strfmt.UUID, sub *kbmodel.Subscription) error {
	params := &subscription.ChangeSubscriptionPlanParams{
		SubscriptionID: subscriptionID,
		Body:           sub,
	}

	if sub.BillingPeriod != nil {
		params.BillingPolicy = lo.ToPtr(string(*sub.BillingPeriod))
	}

	_, err := r.api.Subscription.ChangeSubscriptionPlan(ctx, params)
	return err
}

func (r *KillBillRepository) GetCustomFields(ctx context.Context, subscriptionID strfmt.UUID) (*subscription.GetSubscriptionCustomFieldsOK, error) {
	return r.api.Subscription.GetSubscriptionCustomFields(ctx, &subscription.GetSubscriptionCustomFieldsParams{
		SubscriptionID: subscriptionID,
	})
}

func (r *KillBillRepository) SetCustomField(ctx context.Context, subscriptionID strfmt.UUID, name string, value string) error {
	_, err := r.api.Subscription.CreateSubscriptionCustomFields(ctx, &subscription.CreateSubscriptionCustomFieldsParams{
		SubscriptionID: subscriptionID,
		Body: []*kbmodel.CustomField{
			{
				Name:  &name,
				Value: &value,
			},
		},
	})
	return err
}

func (r *KillBillRepository) DeleteCustomField(ctx context.Context, subscriptionID strfmt.UUID, fieldID strfmt.UUID) error {
	_, err := r.api.Subscription.DeleteSubscriptionCustomFields(ctx, &subscription.DeleteSubscriptionCustomFieldsParams{
		SubscriptionID: subscriptionID,
		CustomField:    []strfmt.UUID{fieldID},
	})
	return err
}

func (r *KillBillRepository) UpdateSubscriptionPlan(ctx context.Context, subscriptionID strfmt.UUID, planID string) error {
	_, err := r.api.Subscription.ChangeSubscriptionPlan(ctx, &subscription.ChangeSubscriptionPlanParams{
		SubscriptionID: subscriptionID,
		Body: &kbmodel.Subscription{
			PlanName: &planID,
		},
	})
	return err
}

func (r *KillBillRepository) CancelSubscription(ctx context.Context, subscriptionID strfmt.UUID, policy *string) error {
	params := &subscription.CancelSubscriptionPlanParams{
		SubscriptionID: subscriptionID,
	}

	if policy != nil {
		switch *policy {
		case "IMMEDIATE":
			params.BillingPolicy = policy
			params.EntitlementPolicy = policy
		case "END_OF_TERM":
			params.BillingPolicy = policy
			params.EntitlementPolicy = policy
		default:
			return fmt.Errorf("invalid cancellation policy: %s", *policy)
		}
	}

	_, err := r.api.Subscription.CancelSubscriptionPlan(ctx, params)
	return err
}

func (r *KillBillRepository) GetCatalog(ctx context.Context) (*catalog.GetCatalogJSONOK, error) {
	return r.api.Catalog.GetCatalogJSON(ctx, &catalog.GetCatalogJSONParams{})
}

func (r *KillBillRepository) GetAvailablePlans(ctx context.Context) (*catalog.GetAvailableBasePlansOK, error) {
	return r.api.Catalog.GetAvailableBasePlans(ctx, &catalog.GetAvailableBasePlansParams{})
}

func (r *KillBillRepository) GetPaymentMethods(ctx context.Context, accountID strfmt.UUID) (*account.GetPaymentMethodsForAccountOK, error) {
	return r.api.Account.GetPaymentMethodsForAccount(ctx, &account.GetPaymentMethodsForAccountParams{
		AccountID: accountID,
	})
}

func (r *KillBillRepository) CreatePaymentMethod(ctx context.Context, accountID strfmt.UUID, pm *kbmodel.PaymentMethod, isDefault bool) (*account.CreatePaymentMethodCreated, error) {
	return r.api.Account.CreatePaymentMethod(ctx, &account.CreatePaymentMethodParams{
		AccountID: accountID,
		Body:      pm,
		IsDefault: &isDefault,
	})
}

func (r *KillBillRepository) DeletePaymentMethod(ctx context.Context, _ strfmt.UUID, paymentMethodID strfmt.UUID, forceDefault bool) error {
	_, err := r.api.PaymentMethod.DeletePaymentMethod(ctx, &payment_method.DeletePaymentMethodParams{
		PaymentMethodID:        paymentMethodID,
		ForceDefaultPmDeletion: &forceDefault,
	})
	return err
}

func (r *KillBillRepository) ValidatePaymentMethod(ctx context.Context, paymentMethodID strfmt.UUID) error {
	_, err := r.api.PaymentMethod.GetPaymentMethod(ctx, &payment_method.GetPaymentMethodParams{
		PaymentMethodID: paymentMethodID,
	})
	return err
}

func (r *KillBillRepository) RefreshPaymentMethods(ctx context.Context, accountID strfmt.UUID) error {
	// First get all payment methods for the account
	withPluginInfo := true
	methods, err := r.api.Account.GetPaymentMethodsForAccount(ctx, &account.GetPaymentMethodsForAccountParams{
		AccountID:      accountID,
		WithPluginInfo: &withPluginInfo,
	})
	if err != nil {
		return fmt.Errorf("failed to get payment methods: %w", err)
	}

	// Refresh each payment method
	for _, method := range methods.Payload {
		if method.PluginName == paymentMethodPluginName {
			_, err := r.api.Account.RefreshPaymentMethods(ctx, &account.RefreshPaymentMethodsParams{
				AccountID: accountID,
			})
			if err != nil {
				return fmt.Errorf("failed to refresh payment method %s: %w", method.PaymentMethodID, err)
			}
		}
	}
	return nil
}
func (r *KillBillRepository) GetNodesInfo(ctx context.Context) ([]*kbmodel.NodeInfo, error) {
	resp, err := r.api.NodesInfo.GetNodesInfo(ctx, &nodes_info.GetNodesInfoParams{})
	if err != nil {
		return nil, err
	}
	return resp.Payload, nil
}

// ProcessWebhook is a mock implementation for handling Hyperswitch webhooks
func (r *KillBillRepository) ProcessWebhook(ctx context.Context, event *hyperswitch.WebhookEvent) error {
	// Mock implementation - in reality this would forward the webhook to Kill Bill
	// and handle the response appropriately
	return nil
}
