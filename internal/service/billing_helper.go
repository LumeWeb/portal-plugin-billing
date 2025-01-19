package service

import (
	"context"
	"errors"
	"fmt"
	"github.com/Boostport/address"
	"github.com/avast/retry-go"
	"github.com/go-openapi/strfmt"
	"github.com/killbill/kbcli/v3/kbclient/account"
	"github.com/killbill/kbcli/v3/kbclient/catalog"
	"github.com/killbill/kbcli/v3/kbclient/invoice"
	"github.com/killbill/kbcli/v3/kbclient/subscription"
	"github.com/killbill/kbcli/v3/kbmodel"
	"github.com/samber/lo"
	"go.lumeweb.com/portal-plugin-billing/internal/api/messages"
	pluginDb "go.lumeweb.com/portal-plugin-billing/internal/db"
	"go.lumeweb.com/portal/db"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"math"
	"sort"
	"strconv"
	"time"
)

type SortOrder int

const (
	SortAscending SortOrder = iota
	SortDescending
)

func (b *BillingServiceDefault) enabled() bool {
	return b.cfg.Enabled
}

func (b *BillingServiceDefault) paidEnabled() bool {
	return b.cfg.PaidPlansEnabled
}

func (b *BillingServiceDefault) freeEnabled() bool {
	return b.cfg.FreePlanEnabled
}

func (b *BillingServiceDefault) normalizeBillingInfo(billingInfo *messages.Billing) error {
	if billingInfo == nil {
		return fmt.Errorf("billing info is required")
	}

	addr, err := address.NewValid(
		address.WithCountry(billingInfo.Address.Country),
		address.WithName(billingInfo.Name),
		address.WithStreetAddress([]string{billingInfo.Address.Line1, billingInfo.Address.Line2}),
		address.WithLocality(billingInfo.Address.City),
		address.WithDependentLocality(billingInfo.Address.DependentLocality),
		address.WithAdministrativeArea(billingInfo.Address.State),
		address.WithPostCode(billingInfo.Address.PostalCode),
		address.WithSortingCode(billingInfo.Address.SortingCode),
		address.WithOrganization(billingInfo.Organization),
	)

	if err != nil {
		var unsupportedErr *address.ErrUnsupportedFields
		if errors.As(err, &unsupportedErr) {
			// Unset only the unsupported fields
			for _, field := range unsupportedErr.Fields {
				switch field {
				case address.Name:
					billingInfo.Name = ""
				case address.Organization:
					billingInfo.Organization = ""
				case address.StreetAddress:
					billingInfo.Address.Line1 = ""
					billingInfo.Address.Line2 = ""
				case address.Locality:
					billingInfo.Address.City = ""
				case address.AdministrativeArea:
					billingInfo.Address.State = ""
				case address.PostCode:
					billingInfo.Address.PostalCode = ""
				case address.Country:
					billingInfo.Address.Country = ""
				}
			}
		} else {
			return err
		}
	}

	// Update billingInfo with normalized values, only for supported fields
	if addr.Name != "" {
		billingInfo.Name = addr.Name
	}

	if addr.Organization != "" {
		billingInfo.Organization = addr.Organization
	}

	if len(addr.StreetAddress) > 0 {
		billingInfo.Address.Line1 = addr.StreetAddress[0]
		if len(addr.StreetAddress) > 1 {
			billingInfo.Address.Line2 = addr.StreetAddress[1]
		}
	}

	if addr.Locality != "" {
		billingInfo.Address.City = addr.Locality
	}

	if addr.AdministrativeArea != "" {
		billingInfo.Address.State = addr.AdministrativeArea
	}

	if addr.PostCode != "" {
		billingInfo.Address.PostalCode = addr.PostCode
	}

	if addr.Country != "" {
		billingInfo.Address.Country = addr.Country
	}

	if addr.DependentLocality != "" {
		billingInfo.Address.DependentLocality = addr.DependentLocality
	}

	if addr.SortingCode != "" {
		billingInfo.Address.SortingCode = addr.SortingCode
	}

	return nil
}

func (b *BillingServiceDefault) getFreeUsage(usageType UsageType) uint64 {
	switch usageType {
	case StorageUsage:
		return b.cfg.FreeStorage
	case UploadUsage:
		return b.cfg.FreeUpload
	case DownloadUsage:
		return b.cfg.FreeDownload
	default:
		return 0
	}
}

func (b *BillingServiceDefault) getUsageByUserID(ctx context.Context, userID uint, usageType UsageType) (uint64, error) {
	if !b.enabled() {
		return math.MaxUint64, nil
	}

	if !b.paidEnabled() {
		return b.getFreeUsage(usageType), nil
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

	if sub == nil || len(*sub.PlanName) == 0 {
		return b.getFreeUsage(usageType), nil
	}

	plan, err := b.getPlanByIdentifier(ctx, *sub.PlanName)
	if err != nil {
		return 0, err
	}

	return b.getPlanUsage(plan, usageType), nil
}

func (b *BillingServiceDefault) getPlanUsage(plan *pluginDb.Plan, usageType UsageType) uint64 {
	switch usageType {
	case StorageUsage:
		return plan.Storage
	case UploadUsage:
		return plan.Upload
	case DownloadUsage:
		return plan.Download
	default:
		return 0
	}
}
func (b *BillingServiceDefault) getFreePlan() *messages.Plan {
	if !b.enabled() || !b.freeEnabled() {
		return nil
	}

	return &messages.Plan{
		ID:     b.cfg.FreePlanID,
		Name:   b.cfg.FreePlanName,
		Period: messages.PeriodMonthly,
		Price:  0,
		Resources: messages.Resources{
			Storage:  b.cfg.FreeStorage,
			Upload:   b.cfg.FreeUpload,
			Download: b.cfg.FreeDownload,
		},
		IsFree: true,
	}
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

func (b *BillingServiceDefault) submitSubscriptionPlanChange(ctx context.Context, sub *kbmodel.Subscription, planID string) error {
	if sub.State != kbmodel.SubscriptionStateBLOCKED {
		return b.changeSubscriptionPlan(ctx, sub.SubscriptionID, planID)
	}

	// Log initial state
	if err := b.logAccountTagState(ctx, sub.AccountID, "Starting subscription plan change"); err != nil {
		return err
	}

	// Set overdue enforcement off
	if err := b.ensureDisableOverdueEnforcement(ctx, sub.AccountID, true); err != nil {
		b.cleanupTags(ctx, sub.AccountID)
		return err
	}

	// Log after overdue enforcement
	if err := b.logAccountTagState(ctx, sub.AccountID, "After setting overdue enforcement off"); err != nil {
		return err
	}

	// Set auto invoicing off
	if err := b.ensureDisableAutoInvoicing(ctx, sub.AccountID, true); err != nil {
		b.cleanupTags(ctx, sub.AccountID)
		return err
	}

	// Log after auto invoicing
	if err := b.logAccountTagState(ctx, sub.AccountID, "After setting auto invoicing off"); err != nil {
		return err
	}

	// Cancel subscription
	if _, err := b.api.Subscription.CancelSubscriptionPlan(ctx, &subscription.CancelSubscriptionPlanParams{
		SubscriptionID: sub.SubscriptionID,
	}); err != nil {
		b.cleanupTags(ctx, sub.AccountID)
		return err
	}

	// Log after cancellation
	if err := b.logAccountTagState(ctx, sub.AccountID, "After subscription cancellation"); err != nil {
		return err
	}

	// Get invoices first to determine if we need to disable anything
	invoices, err := b.getInvoicesForSubscription(ctx, sub.AccountID, sub.SubscriptionID)
	if err != nil {
		b.cleanupTags(ctx, sub.AccountID)
		return err
	}

	invoices = sortInvoices(invoices, SortDescending)
	invoices = filterRecurringInvoices(invoices)
	invoices = filterUnpaidInvoices(invoices)

	if len(invoices) > 0 {
		for _, _invoice := range invoices {
			if err := b.voidInvoice(ctx, _invoice.InvoiceID); err != nil {
				// Cleanup both tags if voiding fails
				b.cleanupTags(ctx, sub.AccountID)
				return err
			}
		}
	}

	// Log after invoice handling
	if err := b.logAccountTagState(ctx, sub.AccountID, "After voiding invoices"); err != nil {
		return err
	}

	// Create new subscription
	_, err = b.api.Subscription.CreateSubscription(ctx, &subscription.CreateSubscriptionParams{
		Body: &kbmodel.Subscription{
			AccountID: sub.AccountID,
			PlanName:  &planID,
			State:     kbmodel.SubscriptionStatePENDING,
		},
	})
	if err != nil {
		b.cleanupTags(ctx, sub.AccountID)
		return err
	}

	// Log after new subscription creation
	if err := b.logAccountTagState(ctx, sub.AccountID, "After creating new subscription"); err != nil {
		return err
	}

	// Remove controls in reverse order
	if err := b.ensureDisableAutoInvoicing(ctx, sub.AccountID, false); err != nil {
		return err
	}

	// Log after removing auto invoicing
	if err := b.logAccountTagState(ctx, sub.AccountID, "After removing auto invoicing"); err != nil {
		return err
	}

	if err := b.ensureDisableOverdueEnforcement(ctx, sub.AccountID, false); err != nil {
		return err
	}

	// Log final state
	if err := b.logAccountTagState(ctx, sub.AccountID, "Completed subscription plan change"); err != nil {
		return err
	}

	return nil
}

func (b *BillingServiceDefault) logAccountTagState(ctx context.Context, accountID strfmt.UUID, msg string) error {
	tags, err := b.api.Account.GetAccountTags(ctx, &account.GetAccountTagsParams{
		AccountID: accountID,
	})
	if err != nil {
		return fmt.Errorf("failed to get account tags: %w", err)
	}

	// Check for specific control tags
	var hasAutoInvoicingOff, hasOverdueEnforcementOff bool
	for _, tag := range tags.Payload {
		switch tag.TagDefinitionName {
		case TagAutoInvoicingOff:
			hasAutoInvoicingOff = true
		case TagOverdueEnforcementOff:
			hasOverdueEnforcementOff = true
		}
	}

	b.logger.Info(msg,
		zap.String("account", string(accountID)),
		zap.Bool("autoInvoicingOff", hasAutoInvoicingOff),
		zap.Bool("overdueEnforcementOff", hasOverdueEnforcementOff),
		zap.Int("totalTags", len(tags.Payload)))

	return nil
}

/*func (b *BillingServiceDefault) submitSubscriptionPlanChange(ctx context.Context, sub *kbmodel.Subscription, planID string) error {
	if sub.State != kbmodel.SubscriptionStateBLOCKED {
		return b.changeSubscriptionPlan(ctx, sub.SubscriptionID, planID)
	}

	// Get invoices first to determine if we need to disable anything
	invoices, err := b.getInvoicesForSubscription(ctx, sub.AccountID, sub.SubscriptionID)
	if err != nil {
		return err
	}

	invoices = sortInvoices(invoices, SortDescending)
	invoices = filterRecurringInvoices(invoices)
	invoices = filterUnpaidInvoices(invoices)
	invoices = filterInvoicesNoCredit(invoices)

	if len(invoices) > 0 {

		// Disable overdue enforcement
		if err = b.disableOverdueEnforcement(ctx, sub.AccountID, true); err != nil {
			b.cleanupTags(ctx, sub.AccountID)
			return err
		}

		// Disable auto-invoicing
		if err = b.disableAutoInvoicing(ctx, sub.AccountID, true); err != nil {
			return err
		}

		// Void invoices
		for _, _invoice := range invoices {
			if err = b.voidInvoice(ctx, _invoice.InvoiceID); err != nil {
				// Cleanup both tags if voiding fails
				b.cleanupTags(ctx, sub.AccountID)
				return err
			}
		}
	}

	// Change the plan
	if err = b.changeSubscriptionPlan(ctx, sub.SubscriptionID, planID); err != nil {
		if len(invoices) > 0 {
			b.cleanupTags(ctx, sub.AccountID)
		}
		return err
	}

	// Cleanup tags if they were set
	if len(invoices) > 0 {
		b.cleanupTags(ctx, sub.AccountID)
	}

	return nil
}*/

func (b *BillingServiceDefault) cleanupTags(ctx context.Context, accountID strfmt.UUID) {
	if err := b.ensureDisableAutoInvoicing(ctx, accountID, false); err != nil {
		b.logger.Error("failed to cleanup auto invoicing", zap.Error(err))
	}
	if err := b.ensureDisableAutoInvoicing(ctx, accountID, false); err != nil {
		b.logger.Error("failed to cleanup overdue enforcement", zap.Error(err))
	}
}

func (b *BillingServiceDefault) getInvoicesForSubscription(ctx context.Context, accountID strfmt.UUID, subId strfmt.UUID) ([]*kbmodel.Invoice, error) {
	var allInvoices []*kbmodel.Invoice
	var offset int64 = 0
	const batchSize int64 = 100

	for {
		result, err := b.api.Account.GetInvoicesForAccountPaginated(ctx, &account.GetInvoicesForAccountPaginatedParams{
			AccountID: accountID,
			Limit:     lo.ToPtr(batchSize),
			Offset:    &offset,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to get invoices batch: %w", err)
		}

		for _, _invoice := range result.Payload {
			invRresult, err := b.api.Invoice.GetInvoice(ctx, &invoice.GetInvoiceParams{
				InvoiceID: _invoice.InvoiceID,
			})
			if err != nil {
				return nil, fmt.Errorf("failed to get invoice: %w", err)
			}
			_invoice = invRresult.Payload
			for _, item := range _invoice.Items {
				if item.SubscriptionID == subId {
					allInvoices = append(allInvoices, _invoice)
					break
				}
			}
		}

		if int64(len(result.Payload)) < batchSize {
			break
		}

		offset += batchSize
	}

	return allInvoices, nil
}

func (b *BillingServiceDefault) changeSubscriptionPlan(ctx context.Context, subscriptionID strfmt.UUID, planID string) error {
	_, err := b.api.Subscription.ChangeSubscriptionPlan(ctx, &subscription.ChangeSubscriptionPlanParams{
		SubscriptionID: subscriptionID,
		Body: &kbmodel.Subscription{
			PlanName: &planID,
		},
	})
	return err
}

func (b *BillingServiceDefault) voidInvoice(ctx context.Context, invoiceID strfmt.UUID) error {
	return retry.Do(
		func() error {
			_, err := b.api.Invoice.VoidInvoice(ctx, &invoice.VoidInvoiceParams{
				InvoiceID: invoiceID,
			})
			return err
		},
		retry.Attempts(3),
		retry.Delay(1*time.Second),
		retry.OnRetry(func(n uint, err error) {
			b.logger.Warn("retrying void invoice",
				zap.Stringer("invoiceId", invoiceID),
				zap.Error(err))
		}),
	)
}
