package service

import (
	"context"
	"fmt"
	"github.com/go-openapi/strfmt"
	"github.com/killbill/kbcli/v3/kbclient/account"
	"github.com/killbill/kbcli/v3/kbclient/invoice"
	"github.com/killbill/kbcli/v3/kbclient/payment"
	"github.com/killbill/kbcli/v3/kbclient/payment_method"
	"github.com/killbill/kbcli/v3/kbclient/subscription"
	"github.com/killbill/kbcli/v3/kbmodel"
	"github.com/samber/lo"
	"go.lumeweb.com/portal-plugin-billing/internal/api/messages"
	"time"
)

const paymentMethodPluginName = "hyperswitch-plugin"

var (
	errNoPaymentAuthorization = fmt.Errorf("no payment authorization found")
)

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

	err = b.disableAutoPay(ctx, accountID, true)
	if err != nil {
		return err
	}

	err = b.setUserPaymentMethod(ctx, accountID)
	if err != nil {
		return err
	}

	err = b.authorizePayment(ctx, accountID, strfmt.UUID(subID))
	if err != nil {
		return err
	}

	return nil
}

func (b *BillingServiceDefault) authorizePayment(ctx context.Context, accountID strfmt.UUID, subscription strfmt.UUID) error {
	invoices, err := b.getInvoicesForSubscription(ctx, accountID, subscription)
	invoices = filterUnpaidInvoices(invoices)
	invoices = filterRecurringInvoices(invoices)
	if err != nil {
		return err
	}

	if len(invoices) == 0 {
		return fmt.Errorf("no invoices found for subscription")
	}

	invoiceResp, err := b.api.Invoice.GetInvoice(ctx, &invoice.GetInvoiceParams{
		InvoiceID: invoices[0].InvoiceID,
	})

	if err != nil {
		return err
	}

	latestInvoice := invoiceResp.Payload

	resp, err := b.api.Account.ProcessPayment(ctx, &account.ProcessPaymentParams{
		AccountID: accountID,
		Body: &kbmodel.PaymentTransaction{
			Amount:          latestInvoice.Balance,
			Currency:        kbmodel.PaymentTransactionCurrencyEnum(latestInvoice.Currency),
			Status:          kbmodel.PaymentTransactionStatusPENDING,
			TransactionType: kbmodel.PaymentTransactionTransactionTypeAUTHORIZE,
		},
		ProcessLocationHeader: true,
	})
	if err != nil {
		return err
	}

	paymentResp, err := b.api.Payment.GetPayment(ctx, &payment.GetPaymentParams{
		PaymentID:      resp.Payload.PaymentID,
		WithPluginInfo: lo.ToPtr(true),
	})

	if err != nil {
		return err
	}

	_ = paymentResp

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

func (b *BillingServiceDefault) getLastSubscriptionAuthorizePaymentMethod(ctx context.Context, userID uint) (*messages.Payment, error) {
	acct, err := b.getAccount(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get account: %w", err)
	}

	withPluginInfo := true
	params := &account.GetPaymentsForAccountParams{
		AccountID:      acct.AccountID,
		WithPluginInfo: &withPluginInfo,
		Context:        ctx,
	}

	resp, err := b.api.Account.GetPaymentsForAccount(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to get payments: %w", err)
	}

	var mostRecentAuth *kbmodel.Payment
	var mostRecentDate time.Time

	// Iterate through all payments to find the most recent pending authorization
	for _, _payment := range resp.Payload {
		if !isMatchingAuthorization(_payment) {
			continue
		}

		_payment, err := b.api.Payment.GetPayment(ctx, &payment.GetPaymentParams{PaymentID: _payment.PaymentID, WithPluginInfo: lo.ToPtr(true)})
		if err != nil {
			return nil, fmt.Errorf("failed to get payment: %w", err)
		}

		// Get the effective date of the last transaction
		lastTx := _payment.Payload.Transactions[len(_payment.Payload.Transactions)-1]

		// Update if this is the most recent authorization we've seen
		if mostRecentAuth == nil || time.Time(lastTx.EffectiveDate).After(mostRecentDate) {
			mostRecentAuth = _payment.Payload
			mostRecentDate = time.Time(lastTx.EffectiveDate)
		}
	}

	if mostRecentAuth == nil || len(mostRecentAuth.Transactions) == 0 {
		return nil, errNoPaymentAuthorization
	}

	// Extract payment details from the most recent authorization
	return extractPaymentDetails(mostRecentAuth.Transactions[len(mostRecentAuth.Transactions)-1])
}

// extractPaymentDetails gets the Stripe payment details from transaction properties
func extractPaymentDetails(tx *kbmodel.PaymentTransaction) (*messages.Payment, error) {
	if tx == nil {
		return nil, fmt.Errorf("transaction is nil")
	}

	_payment := &messages.Payment{}
	var expiresAtStr string

	// Extract values from properties
	for _, prop := range tx.Properties {
		switch prop.Key {
		case "client_secret":
			_payment.ClientSecret = prop.Value
		case "expires_at":
			expiresAtStr = prop.Value
		}
	}

	// Parse expires_at if present
	if expiresAtStr != "" {
		expiresAt, err := time.Parse(time.RFC3339, expiresAtStr)
		if err != nil {
			return nil, fmt.Errorf("failed to parse expires_at: %w", err)
		}
		if !expiresAt.IsZero() {
			_payment.ExpiresAt = expiresAt
		}
	}

	// Validate required fields
	if _payment.ClientSecret == "" {
		return nil, fmt.Errorf("client_secret not found in transaction properties")
	}

	if _payment.ExpiresAt.IsZero() {
		return nil, fmt.Errorf("client_secret not found in transaction properties")
	}

	return _payment, nil
}

// isMatchingAuthorization checks if the payment has a pending authorization transaction
func isMatchingAuthorization(p *kbmodel.Payment) bool {
	if p == nil || len(p.Transactions) == 0 {
		return false
	}

	lastTx := p.Transactions[len(p.Transactions)-1]
	return lastTx.TransactionType == "AUTHORIZE" &&
		lastTx.Status == "PENDING"
}
