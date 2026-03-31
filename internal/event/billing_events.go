package event

import (
	"context"
	"time"

	"go.lumeweb.com/portal/core"
	"github.com/shopspring/decimal"
)

const (
	// EVENT_PAYMENT_COMPLETED is fired when a payment is successfully processed
	EVENT_PAYMENT_COMPLETED = "billing.payment.completed"

	// EVENT_SUBSCRIPTION_CREATED is fired when a new subscription is created
	EVENT_SUBSCRIPTION_CREATED = "billing.subscription.created"

	// EVENT_SUBSCRIPTION_ACTIVE is fired when a subscription becomes active
	EVENT_SUBSCRIPTION_ACTIVE = "billing.subscription.active"

	// EVENT_SUBSCRIPTION_UPDATED is fired when a subscription is updated
	EVENT_SUBSCRIPTION_UPDATED = "billing.subscription.updated"

	// EVENT_SUBSCRIPTION_CANCELLED is fired when a subscription is cancelled
	EVENT_SUBSCRIPTION_CANCELLED = "billing.subscription.cancelled"

	// EVENT_PLAN_CHANGED is fired when a user's plan is changed
	EVENT_PLAN_CHANGED = "billing.plan.changed"

	// EVENT_PLAN_CHANGE_CREDIT_ONLY is fired when a plan change results in credit only (no payment)
	EVENT_PLAN_CHANGE_CREDIT_ONLY = "billing.plan_change.credit_only"

	// EVENT_PLAN_CHANGE_ZERO_AMOUNT is fired when a plan change has zero total due
	EVENT_PLAN_CHANGE_ZERO_AMOUNT = "billing.plan_change.zero_amount"

	// SSE event type constants for client-side events
	SSEEventTypePaymentCompleted      = "payment.completed"
	SSEEventTypeSubscriptionActive    = "subscription.active"
	SSEEventTypeSubscriptionCreated   = "subscription.created"
	SSEEventTypeSubscriptionUpdated   = "subscription.updated"
	SSEEventTypeSubscriptionCancelled = "subscription.cancelled"
	SSEEventTypePlanChanged           = "plan.changed"
	SSEEventTypeCreditOnly             = "plan.changed.credit_only"
	SSEEventTypeZeroAmount             = "plan.changed.zero_amount"
)

// PaymentCompletedEvent is fired when a payment is successfully processed
type PaymentCompletedEvent struct {
	Ctx        context.Context `json:"-"`
	UserID     uint            `json:"-"`
	Amount     decimal.Decimal `json:"amount,string"`
	Gateway    string          `json:"gateway"`
	InvoiceID  string          `json:"invoice_id"`
	ExternalID string          `json:"external_id"`
	PaidAt     time.Time       `json:"paid_at"`
}

// SubscriptionCreatedEvent is fired when a new subscription is created
type SubscriptionCreatedEvent struct {
	Ctx            context.Context `json:"-"`
	UserID         uint            `json:"-"`
	SubscriptionID string          `json:"subscription_id"`
	Gateway        string          `json:"gateway"`
	PlanID         uint            `json:"plan_id"`
	PeriodID       uint            `json:"period_id"`
	CreatedAt      time.Time       `json:"created_at"`
}

// SubscriptionActiveEvent is fired when a subscription becomes active
type SubscriptionActiveEvent struct {
	Ctx            context.Context `json:"-"`
	UserID         uint            `json:"-"`
	SubscriptionID string          `json:"subscription_id"`
	Gateway        string          `json:"gateway"`
	PlanID         uint            `json:"plan_id"`
	PeriodID       uint            `json:"period_id"`
	ActivatedAt    time.Time       `json:"activated_at"`
}

// SubscriptionUpdatedEvent is fired when a subscription is updated
type SubscriptionUpdatedEvent struct {
	Ctx            context.Context `json:"-"`
	UserID         uint            `json:"-"`
	SubscriptionID string          `json:"subscription_id"`
	Gateway        string          `json:"gateway"`
	OldPlanID      *uint           `json:"old_plan_id,omitempty"`
	NewPlanID      uint            `json:"new_plan_id"`
	OldPeriodID    *uint           `json:"old_period_id,omitempty"`
	NewPeriodID    uint            `json:"new_period_id"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

// SubscriptionCancelledEvent is fired when a subscription is cancelled
type SubscriptionCancelledEvent struct {
	Ctx            context.Context `json:"-"`
	UserID         uint            `json:"-"`
	SubscriptionID string          `json:"subscription_id"`
	Gateway        string          `json:"gateway"`
	PlanID         uint            `json:"plan_id"`
	CancelledAt    time.Time       `json:"cancelled_at"`
}

// PlanChangedEvent is fired when a user's plan is changed
type PlanChangedEvent struct {
	Ctx            context.Context `json:"-"`
	UserID         uint            `json:"-"`
	SubscriptionID string          `json:"subscription_id"`
	Gateway        string          `json:"gateway"`
	OldPlanID      uint            `json:"old_plan_id"`
	OldPeriodID    uint            `json:"old_period_id"`
	NewPlanID      uint            `json:"new_plan_id"`
	NewPeriodID    uint            `json:"new_period_id"`
	ProratedCredit decimal.Decimal `json:"prorated_credit,string"`
	ProratedCharge decimal.Decimal `json:"prorated_charge,string"`
	NetAmount      decimal.Decimal `json:"net_amount,string"`
	ChangedAt      time.Time       `json:"changed_at"`
}

// PlanChangeCreditOnlyEvent is fired when a plan change results in credit only
type PlanChangeCreditOnlyEvent struct {
	Ctx             context.Context `json:"-"`
	UserID          uint            `json:"-"`
	SubscriptionID  string          `json:"subscription_id"`
	Gateway         string          `json:"gateway"`
	OldPlanID       uint            `json:"old_plan_id"`
	OldPeriodID     uint            `json:"old_period_id"`
	NewPlanID       uint            `json:"new_plan_id"`
	NewPeriodID     uint            `json:"new_period_id"`
	CreditAmount    decimal.Decimal `json:"credit_amount,string"`
	EffectiveFrom   time.Time       `json:"effective_from"`
	BillingCycleEnd time.Time       `json:"billing_cycle_end"`
	CompletedAt     time.Time       `json:"completed_at"`
}

// PlanChangeZeroAmountEvent is fired when a plan change has zero total due
type PlanChangeZeroAmountEvent struct {
	Ctx             context.Context `json:"-"`
	UserID          uint            `json:"-"`
	SubscriptionID  string          `json:"subscription_id"`
	Gateway         string          `json:"gateway"`
	OldPlanID       uint            `json:"old_plan_id"`
	OldPeriodID     uint            `json:"old_period_id"`
	NewPlanID       uint            `json:"new_plan_id"`
	NewPeriodID     uint            `json:"new_period_id"`
	ProratedCredit  decimal.Decimal `json:"prorated_credit,string"`
	ProratedCharge  decimal.Decimal `json:"prorated_charge,string"`
	EffectiveFrom   time.Time       `json:"effective_from"`
	BillingCycleEnd time.Time       `json:"billing_cycle_end"`
	CompletedAt     time.Time       `json:"completed_at"`
}

// Factory functions

// NewPaymentCompletedEvent creates a PaymentCompletedEvent
func NewPaymentCompletedEvent(ctx context.Context, userID uint, amount decimal.Decimal, gateway, invoiceID, externalID string) *PaymentCompletedEvent {
	return &PaymentCompletedEvent{
		Ctx:        ctx,
		UserID:     userID,
		Amount:     amount,
		Gateway:    gateway,
		InvoiceID:  invoiceID,
		ExternalID: externalID,
		PaidAt:     time.Now(),
	}
}

// NewSubscriptionCreatedEvent creates a SubscriptionCreatedEvent
func NewSubscriptionCreatedEvent(ctx context.Context, userID uint, subscriptionID, gateway string, planID, periodID uint) *SubscriptionCreatedEvent {
	return &SubscriptionCreatedEvent{
		Ctx:            ctx,
		UserID:         userID,
		SubscriptionID: subscriptionID,
		Gateway:        gateway,
		PlanID:         planID,
		PeriodID:       periodID,
		CreatedAt:      time.Now(),
	}
}

// NewSubscriptionActiveEvent creates a SubscriptionActiveEvent
func NewSubscriptionActiveEvent(ctx context.Context, userID uint, subscriptionID, gateway string, planID, periodID uint) *SubscriptionActiveEvent {
	return &SubscriptionActiveEvent{
		Ctx:            ctx,
		UserID:         userID,
		SubscriptionID: subscriptionID,
		Gateway:        gateway,
		PlanID:         planID,
		PeriodID:       periodID,
		ActivatedAt:    time.Now(),
	}
}

// NewSubscriptionUpdatedEvent creates a SubscriptionUpdatedEvent
func NewSubscriptionUpdatedEvent(ctx context.Context, userID uint, subscriptionID, gateway string, oldPlanID, newPlanID, oldPeriodID, newPeriodID uint) *SubscriptionUpdatedEvent {
	var oldPlanIDPtr *uint
	if oldPlanID > 0 {
		oldPlanIDPtr = &oldPlanID
	}
	var oldPeriodIDPtr *uint
	if oldPeriodID > 0 {
		oldPeriodIDPtr = &oldPeriodID
	}
	return &SubscriptionUpdatedEvent{
		Ctx:            ctx,
		UserID:         userID,
		SubscriptionID: subscriptionID,
		Gateway:        gateway,
		OldPlanID:      oldPlanIDPtr,
		NewPlanID:      newPlanID,
		OldPeriodID:    oldPeriodIDPtr,
		NewPeriodID:    newPeriodID,
		UpdatedAt:      time.Now(),
	}
}

// NewSubscriptionCancelledEvent creates a SubscriptionCancelledEvent
func NewSubscriptionCancelledEvent(ctx context.Context, userID uint, subscriptionID, gateway string, planID uint) *SubscriptionCancelledEvent {
	return &SubscriptionCancelledEvent{
		Ctx:            ctx,
		UserID:         userID,
		SubscriptionID: subscriptionID,
		Gateway:        gateway,
		PlanID:         planID,
		CancelledAt:    time.Now(),
	}
}

// NewPlanChangedEvent creates a PlanChangedEvent
func NewPlanChangedEvent(ctx context.Context, userID uint, subscriptionID, gateway string, oldPlanID, oldPeriodID, newPlanID, newPeriodID uint, proratedCredit, proratedCharge, netAmount decimal.Decimal) *PlanChangedEvent {
	return &PlanChangedEvent{
		Ctx:            ctx,
		UserID:         userID,
		SubscriptionID: subscriptionID,
		Gateway:        gateway,
		OldPlanID:      oldPlanID,
		OldPeriodID:    oldPeriodID,
		NewPlanID:      newPlanID,
		NewPeriodID:    newPeriodID,
		ProratedCredit: proratedCredit,
		ProratedCharge: proratedCharge,
		NetAmount:      netAmount,
		ChangedAt:      time.Now(),
	}
}

// NewPlanChangeCreditOnlyEvent creates a PlanChangeCreditOnlyEvent
func NewPlanChangeCreditOnlyEvent(ctx context.Context, userID uint, subscriptionID, gateway string, oldPlanID, oldPeriodID, newPlanID, newPeriodID uint, creditAmount decimal.Decimal, effectiveFrom, billingCycleEnd time.Time) *PlanChangeCreditOnlyEvent {
	return &PlanChangeCreditOnlyEvent{
		Ctx:             ctx,
		UserID:          userID,
		SubscriptionID:  subscriptionID,
		Gateway:         gateway,
		OldPlanID:       oldPlanID,
		OldPeriodID:     oldPeriodID,
		NewPlanID:       newPlanID,
		NewPeriodID:     newPeriodID,
		CreditAmount:    creditAmount,
		EffectiveFrom:   effectiveFrom,
		BillingCycleEnd: billingCycleEnd,
		CompletedAt:     time.Now(),
	}
}

// NewPlanChangeZeroAmountEvent creates a PlanChangeZeroAmountEvent
func NewPlanChangeZeroAmountEvent(ctx context.Context, userID uint, subscriptionID, gateway string, oldPlanID, oldPeriodID, newPlanID, newPeriodID uint, proratedCredit, proratedCharge decimal.Decimal, effectiveFrom, billingCycleEnd time.Time) *PlanChangeZeroAmountEvent {
	return &PlanChangeZeroAmountEvent{
		Ctx:             ctx,
		UserID:          userID,
		SubscriptionID:  subscriptionID,
		Gateway:         gateway,
		OldPlanID:       oldPlanID,
		OldPeriodID:     oldPeriodID,
		NewPlanID:       newPlanID,
		NewPeriodID:     newPeriodID,
		ProratedCredit:  proratedCredit,
		ProratedCharge:  proratedCharge,
		EffectiveFrom:   effectiveFrom,
		BillingCycleEnd: billingCycleEnd,
		CompletedAt:     time.Now(),
	}
}

// Event listener helpers - following portal core event patterns

// OnPaymentCompleted registers a listener for payment completed events
func OnPaymentCompleted(ctx core.Context, handler func(context.Context, PaymentCompletedEvent) error, priority ...int) {
	core.Listen[PaymentCompletedEvent](ctx, EVENT_PAYMENT_COMPLETED, func(e *core.CoreEvent[PaymentCompletedEvent]) error {
		return handler(e.Data.Ctx, e.Data)
	}, priority...)
}

// OnSubscriptionCreated registers a listener for subscription created events
func OnSubscriptionCreated(ctx core.Context, handler func(context.Context, SubscriptionCreatedEvent) error, priority ...int) {
	core.Listen[SubscriptionCreatedEvent](ctx, EVENT_SUBSCRIPTION_CREATED, func(e *core.CoreEvent[SubscriptionCreatedEvent]) error {
		return handler(e.Data.Ctx, e.Data)
	}, priority...)
}

// OnSubscriptionActive registers a listener for subscription active events
func OnSubscriptionActive(ctx core.Context, handler func(context.Context, SubscriptionActiveEvent) error, priority ...int) {
	core.Listen[SubscriptionActiveEvent](ctx, EVENT_SUBSCRIPTION_ACTIVE, func(e *core.CoreEvent[SubscriptionActiveEvent]) error {
		return handler(e.Data.Ctx, e.Data)
	}, priority...)
}

// OnSubscriptionUpdated registers a listener for subscription updated events
func OnSubscriptionUpdated(ctx core.Context, handler func(context.Context, SubscriptionUpdatedEvent) error, priority ...int) {
	core.Listen[SubscriptionUpdatedEvent](ctx, EVENT_SUBSCRIPTION_UPDATED, func(e *core.CoreEvent[SubscriptionUpdatedEvent]) error {
		return handler(e.Data.Ctx, e.Data)
	}, priority...)
}

// OnSubscriptionCancelled registers a listener for subscription cancelled events
func OnSubscriptionCancelled(ctx core.Context, handler func(context.Context, SubscriptionCancelledEvent) error, priority ...int) {
	core.Listen[SubscriptionCancelledEvent](ctx, EVENT_SUBSCRIPTION_CANCELLED, func(e *core.CoreEvent[SubscriptionCancelledEvent]) error {
		return handler(e.Data.Ctx, e.Data)
	}, priority...)
}

// OnPlanChanged registers a listener for plan changed events
func OnPlanChanged(ctx core.Context, handler func(context.Context, PlanChangedEvent) error, priority ...int) {
	core.Listen[PlanChangedEvent](ctx, EVENT_PLAN_CHANGED, func(e *core.CoreEvent[PlanChangedEvent]) error {
		return handler(e.Data.Ctx, e.Data)
	}, priority...)
}

// OnPlanChangeCreditOnly registers a listener for credit-only plan change events
func OnPlanChangeCreditOnly(ctx core.Context, handler func(context.Context, PlanChangeCreditOnlyEvent) error, priority ...int) {
	core.Listen[PlanChangeCreditOnlyEvent](ctx, EVENT_PLAN_CHANGE_CREDIT_ONLY, func(e *core.CoreEvent[PlanChangeCreditOnlyEvent]) error {
		return handler(e.Data.Ctx, e.Data)
	}, priority...)
}

// OnPlanChangeZeroAmount registers a listener for zero-amount plan change events
func OnPlanChangeZeroAmount(ctx core.Context, handler func(context.Context, PlanChangeZeroAmountEvent) error, priority ...int) {
	core.Listen[PlanChangeZeroAmountEvent](ctx, EVENT_PLAN_CHANGE_ZERO_AMOUNT, func(e *core.CoreEvent[PlanChangeZeroAmountEvent]) error {
		return handler(e.Data.Ctx, e.Data)
	}, priority...)
}

// SSEEvent wraps a billing event with a type field for client-side consumption
type SSEEvent struct {
	Type string `json:"type"`
	// Data contains the actual event data
	Data any `json:"data"`
}

// NewSSEEvent creates a new SSE event wrapper
func NewSSEEvent(eventType string, data any) *SSEEvent {
	return &SSEEvent{
		Type: eventType,
		Data: data,
	}
}
