package service

import (
	"context"
	"errors"
	"fmt"
	"github.com/Boostport/address"
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
	if sub.State == kbmodel.SubscriptionStateBLOCKED {
		invoices, err := b.getInvoicesForSubscription(ctx, sub.AccountID, sub.SubscriptionID)
		if err != nil {
			return err
		}

		invoices = sortInvoices(invoices, SortDescending)
		invoices = filterRecurringInvoices(invoices)
		invoices = filterUnpaidInvoices(invoices)

		for _, _invoice := range invoices {
			if err, _ := b.api.Invoice.VoidInvoice(ctx, &invoice.VoidInvoiceParams{InvoiceID: _invoice.InvoiceID}); err != nil {
				return err
			}
		}
	}

	_, err := b.api.Subscription.ChangeSubscriptionPlan(ctx, &subscription.ChangeSubscriptionPlanParams{
		SubscriptionID: sub.SubscriptionID,
		Body: &kbmodel.Subscription{
			PlanName: &planID,
		},
	})

	if err != nil {
		return err
	}

	return nil
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
