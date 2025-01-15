package service

import (
	"fmt"
	"github.com/killbill/kbcli/v3/kbmodel"
	"go.lumeweb.com/portal-plugin-billing/internal/api/messages"
	"strings"
)

func findActiveOrPendingSubscription(bundles []*kbmodel.Bundle) *kbmodel.Subscription {
	for _, bundle := range bundles {
		for _, sub := range bundle.Subscriptions {
			if sub.State == kbmodel.SubscriptionStateACTIVE || sub.State == kbmodel.SubscriptionStatePENDING || sub.State == kbmodel.SubscriptionStateBLOCKED {
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
