package service

import (
	"context"
	"fmt"
	"github.com/go-openapi/strfmt"
	"github.com/killbill/kbcli/v3/kbclient/account"
	"github.com/killbill/kbcli/v3/kbclient/payment_method"
	"github.com/killbill/kbcli/v3/kbclient/subscription"
	"github.com/killbill/kbcli/v3/kbmodel"
	"time"
)

const paymentMethodPluginName = "hyperswitch-plugin"

func (b *BillingServiceDefault) handleNewSubscription(ctx context.Context, accountID strfmt.UUID, planId string) error {
	// Create a new subscription
	resp, err := b.api.Subscription.CreateSubscription(ctx, &subscription.CreateSubscriptionParams{
		Body: &kbmodel.Subscription{
			AccountID: accountID,
			PlanName:  &planId,
			State:     kbmodel.SubscriptionStatePENDING,
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

	// Fetch the subscription details
	_, err = b.api.Subscription.GetSubscription(ctx, &subscription.GetSubscriptionParams{
		SubscriptionID: strfmt.UUID(subID),
	})
	if err != nil {
		return fmt.Errorf("failed to fetch subscription details: %w", err)
	}

	err = b.setAutoPay(ctx, accountID, false)
	if err != nil {
		return err
	}

	err = b.setUserPaymentMethod(ctx, accountID)
	if err != nil {
		return err
	}

	err = b.authorizePayment(ctx, accountID)
	if err != nil {
		return err
	}

	return nil
}

func (b *BillingServiceDefault) authorizePayment(ctx context.Context, accountID strfmt.UUID) error {
	invoiceResp, err := b.api.Account.GetInvoicesForAccount(ctx, &account.GetInvoicesForAccountParams{AccountID: accountID})
	if err != nil {
		return err
	}

	if len(invoiceResp.Payload) == 0 {
		return fmt.Errorf("no invoices found for account")
	}

	// Assuming the latest invoice is the one to be authorized
	latestInvoice := invoiceResp.Payload[0]

	_, err = b.api.Account.ProcessPayment(ctx, &account.ProcessPaymentParams{
		AccountID: accountID,
		Body: &kbmodel.PaymentTransaction{
			Amount:          latestInvoice.Balance,
			Currency:        kbmodel.PaymentTransactionCurrencyEnum(latestInvoice.Currency),
			EffectiveDate:   strfmt.DateTime(time.Now().UTC()),
			Status:          kbmodel.PaymentTransactionStatusPENDING,
			TransactionType: kbmodel.PaymentTransactionTransactionTypeAUTHORIZE,
			AuditLogs:       make([]*kbmodel.AuditLog, 0),
		},
	})
	if err != nil {
		return err
	}

	return nil
}

func (b *BillingServiceDefault) setUserPaymentMethod(ctx context.Context, acctID strfmt.UUID) error {
	def := true
	_, err := b.api.Account.CreatePaymentMethod(ctx, &account.CreatePaymentMethodParams{
		AccountID: acctID,
		Body: &kbmodel.PaymentMethod{
			PluginName: paymentMethodPluginName,
			PluginInfo: &kbmodel.PaymentMethodPluginDetail{
				IsDefaultPaymentMethod: true,
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
