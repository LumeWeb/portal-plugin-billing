package service

import (
	"context"
	"fmt"
	"github.com/go-openapi/strfmt"
	"github.com/killbill/kbcli/v3/kbclient/subscription"
	"github.com/killbill/kbcli/v3/kbmodel"
)

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

	// Fetch the subscription details
	sub, err := b.api.Subscription.GetSubscription(ctx, &subscription.GetSubscriptionParams{
		SubscriptionID: strfmt.UUID(subID),
	})
	if err != nil {
		return fmt.Errorf("failed to fetch subscription details: %w", err)
	}

	_ = sub

	// Create new payment
	/*	_, err = b.createNewPayment(ctx, accountID, sub.Payload, false)
		if err != nil {
			return fmt.Errorf("failed to create new payment: %w", err)
		}
	*/
	return nil
}

/*func (b *BillingServiceDefault) createNewPayment(ctx context.Context, accountID strfmt.UUID, sub *kbmodel.Subscription, zeroAuth bool) (string, error) {
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

	payload := message.PaymentCancelRequest{
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
}*/
