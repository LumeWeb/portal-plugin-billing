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
func remoteSubscriptionStatusToLocal(status kbmodel.SubscriptionStateEnum) messages.SubscriptionPlanStatus {
	switch status {
	case kbmodel.SubscriptionStateACTIVE:
		return messages.SubscriptionPlanStatusActive
	case kbmodel.SubscriptionStatePENDING, kbmodel.SubscriptionStateBLOCKED:
		return messages.SubscriptionPlanStatusPending
	default:
		return messages.SubscriptionPlanStatusPending
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

func parseSubscriptionIDFromLocation(location string) (string, error) {
	parts := strings.Split(location, "/")
	if len(parts) == 0 {
		return "", fmt.Errorf("invalid Location header format")
	}
	return parts[len(parts)-1], nil
}
