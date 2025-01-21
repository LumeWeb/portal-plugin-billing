package service

import (
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"github.com/killbill/kbcli/v3/kbmodel"
	"github.com/samber/lo"
	"go.lumeweb.com/portal-plugin-billing/internal/api/messages"
	"slices"
	"strings"
	"time"
)

func isSubscriptionPending(sub *kbmodel.Subscription) bool {
	return lo.Contains([]kbmodel.SubscriptionStateEnum{kbmodel.SubscriptionStatePENDING, kbmodel.SubscriptionStateBLOCKED}, sub.State)
}
func isSubscriptionActive(sub *kbmodel.Subscription) bool {
	return lo.Contains([]kbmodel.SubscriptionStateEnum{kbmodel.SubscriptionStateACTIVE}, sub.State)
}

func findActiveOrPendingSubscription(bundles []*kbmodel.Bundle) *kbmodel.Subscription {
	for _, bundle := range bundles {
		for _, sub := range bundle.Subscriptions {
			if isSubscriptionPending(sub) || isSubscriptionActive(sub) {
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

func remoteBillingPeriodToLocal(period kbmodel.PlanDetailFinalPhaseBillingPeriodEnum) messages.PlanPeriod {
	switch period {
	case kbmodel.PlanDetailFinalPhaseBillingPeriodMONTHLY:
		return messages.PeriodMonthly
	case kbmodel.PlanDetailFinalPhaseBillingPeriodANNUAL:
		return messages.PeriodYearly
	default:
		return messages.PeriodMonthly
	}
}

func remoteSubscriptionStatusToLocal(status kbmodel.SubscriptionStateEnum) messages.PlanStatus {
	switch status {
	case kbmodel.SubscriptionStateACTIVE:
		return messages.StatusActive
	case kbmodel.SubscriptionStatePENDING, kbmodel.SubscriptionStateBLOCKED:
		return messages.StatusPending
	default:
		return messages.StatusPending
	}
}

func remoteSubscriptionPhaseToLocal(phase kbmodel.SubscriptionPhaseTypeEnum) messages.PlanPeriod {
	switch phase {
	case kbmodel.SubscriptionPhaseTypeEVERGREEN:
		return messages.PeriodMonthly
	case kbmodel.SubscriptionPhaseTypeTRIAL:
		return messages.PeriodMonthly
	default:
		return messages.PeriodMonthly
	}
}

func parseSubscriptionIDFromLocation(location string) (string, error) {
	parts := strings.Split(location, "/")
	if len(parts) == 0 {
		return "", fmt.Errorf("invalid Location header format")
	}
	return parts[len(parts)-1], nil
}

func sortInvoices(invoices []*kbmodel.Invoice, order SortOrder) []*kbmodel.Invoice {
	sorted := slices.Clone(invoices)
	slices.SortFunc(sorted, func(i, j *kbmodel.Invoice) int {
		iTime := time.Time(i.TargetDate)
		jTime := time.Time(j.TargetDate)

		if order == SortAscending {
			switch {
			case iTime.Before(jTime):
				return -1
			case iTime.After(jTime):
				return 1
			default:
				return 0
			}
		}
		switch {
		case iTime.After(jTime):
			return -1
		case iTime.Before(jTime):
			return 1
		default:
			return 0
		}
	})
	return sorted
}

func filterRecurringInvoices(invoices []*kbmodel.Invoice) []*kbmodel.Invoice {
	return slices.DeleteFunc(slices.Clone(invoices), func(inv *kbmodel.Invoice) bool {
		for _, item := range inv.Items {
			if item.ItemType == kbmodel.InvoiceItemItemTypeRECURRING {
				return false
			}
		}
		return true
	})
}
func filterUnpaidInvoices(invoices []*kbmodel.Invoice) []*kbmodel.Invoice {
	return slices.DeleteFunc(slices.Clone(invoices), func(inv *kbmodel.Invoice) bool {
		return inv.Balance <= 0
	})
}

func filterInvoicesNoCredit(invoices []*kbmodel.Invoice) []*kbmodel.Invoice {
	return slices.DeleteFunc(slices.Clone(invoices), func(inv *kbmodel.Invoice) bool {
		return len(lo.Filter(inv.Items, func(item *kbmodel.InvoiceItem, _ int) bool {
			return lo.Contains([]kbmodel.InvoiceItemItemTypeEnum{kbmodel.InvoiceItemItemTypeREPAIRADJ, kbmodel.InvoiceItemItemTypeCREDITADJ}, item.ItemType)
		})) > 0
	})
}

func verifyWebhookSignature(payload []byte, signature string, secretKey string) error {
	// Generate HMAC-SHA512 signature using the raw payload
	mac := hmac.New(sha512.New, []byte(secretKey))
	mac.Write(payload)
	expectedSignature := hex.EncodeToString(mac.Sum(nil))

	// Compare signatures using constant-time comparison
	if !hmac.Equal([]byte(signature), []byte(expectedSignature)) {
		return fmt.Errorf("signature mismatch")
	}
	return nil
}
