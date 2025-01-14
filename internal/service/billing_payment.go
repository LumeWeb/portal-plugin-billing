package service

import (
	"context"
	"fmt"
	"github.com/cenkalti/backoff/v4"
	"github.com/go-openapi/strfmt"
	"github.com/killbill/kbcli/v3/kbclient/account"
	"github.com/killbill/kbcli/v3/kbclient/payment_method"
	"github.com/killbill/kbcli/v3/kbclient/subscription"
	"github.com/killbill/kbcli/v3/kbmodel"
	"github.com/samber/lo"
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
	// Configure exponential backoff
	_backoff := backoff.NewExponentialBackOff()
	_backoff.InitialInterval = 2 * time.Second
	_backoff.MaxInterval = 30 * time.Second
	_backoff.MaxElapsedTime = 5 * time.Minute

	var latestInvoice *kbmodel.Invoice
	operation := func() error {
		invoiceResp, err := b.api.Account.GetInvoicesForAccount(ctx, &account.GetInvoicesForAccountParams{AccountID: accountID})
		if err != nil {
			return backoff.Permanent(err) // Don't retry on API errors
		}

		if len(invoiceResp.Payload) == 0 {
			return backoff.Permanent(fmt.Errorf("no invoices found for account"))
		}

		latestInvoice = invoiceResp.Payload[0]
		if latestInvoice.Balance == 0 {
			return fmt.Errorf("invoice amount is 0, waiting for KillBill processing")
		}

		return nil
	}

	// Execute the retry logic
	err := backoff.Retry(operation, backoff.WithContext(_backoff, ctx))
	if err != nil {
		return fmt.Errorf("failed to get valid invoice after retries: %w", err)
	}

	// Process the payment once we have a valid invoice
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
		return fmt.Errorf("failed to process payment: %w", err)
	}

	return nil
}

func (b *BillingServiceDefault) setUserPaymentMethod(ctx context.Context, acctID strfmt.UUID) error {
	_, err := b.api.Account.CreatePaymentMethod(ctx, &account.CreatePaymentMethodParams{
		AccountID: acctID,
		Body: &kbmodel.PaymentMethod{
			PluginName: paymentMethodPluginName,
			PluginInfo: &kbmodel.PaymentMethodPluginDetail{
				IsDefaultPaymentMethod: true,
			},
			IsDefault: true,
		},
		IsDefault: lo.ToPtr(true),
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
