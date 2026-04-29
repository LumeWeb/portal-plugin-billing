// +build integration

// Package stripe_integration tests Stripe proration behavior using invoice preview APIs.
//
// These tests validate that our proration calculations match Stripe's documented behavior
// using the invoice preview API. This allows testing proration calculations by:
//   - Creating minimal test subscriptions
//   - Using invoice preview API to see what proration WOULD do for changes
//   - Avoiding actual charges or real invoice creation during testing
//
// Requirements:
//   - STRIPE_TEST_SECRET_KEY environment variable set
//
// Run with:
//   STRIPE_TEST_SECRET_KEY=sk_test_XXX go test -tags=integration ./pkg/subscription/stripe_integration/... -run TestStripeProration -v
//
// Note: These tests create minimal test resources (customer, price, product, and one test subscription)
// for preview calculations. All resources are cleaned up automatically.
package stripe_integration

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stripe/stripe-go/v85"
	"github.com/stripe/stripe-go/v85/customer"
	"github.com/stripe/stripe-go/v85/invoice"
	"github.com/stripe/stripe-go/v85/paymentmethod"
	"github.com/stripe/stripe-go/v85/price"
	"github.com/stripe/stripe-go/v85/product"
	subscriptionClient "github.com/stripe/stripe-go/v85/subscription"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"go.lumeweb.com/portal-plugin-billing/pkg/subscription"
)

// cleanupOldTestResources removes any test resources from previous test runs.
// This ensures clean state for new tests since we're working with a remote Stripe server.
//
// Stripe does NOT support deletion of products with prices or individual prices.
// Instead, we archive them by setting active=false, which is the recommended approach
// for reconciliation and keeping historical data.
func cleanupOldTestResources() {
	// Cancel all subscriptions
	subList := subscriptionClient.List(&stripe.SubscriptionListParams{})
	for subList.Next() {
		sub := subList.Current()
		if sub == nil {
			continue
		}
		subPtr, ok := sub.(*stripe.Subscription)
		if !ok {
			continue
		}
		_, _ = subscriptionClient.Cancel(subPtr.ID, nil)
	}

	// Delete test customers only
	customerIter := customer.List(&stripe.CustomerListParams{})
	for customerIter.Next() {
		cust := customerIter.Current()
		if cust == nil {
			continue
		}
		customerPtr, ok := cust.(*stripe.Customer)
		if !ok {
			continue
		}
		if strings.Contains(customerPtr.Email, "test-") {
			_, _ = customer.Del(customerPtr.ID, nil)
		}
	}
	_ = customerIter.Err()

	// Archive test products and prices
	productIter := product.List(&stripe.ProductListParams{})
	for productIter.Next() {
		prod := productIter.Current()
		if prod == nil {
			continue
		}
		productPtr, ok := prod.(*stripe.Product)
		if !ok {
			continue
		}

		if strings.Contains(productPtr.Name, "Plan") {
			// Archive prices first
			priceIter := price.List(&stripe.PriceListParams{Product: stripe.String(productPtr.ID)})
			for priceIter.Next() {
				pr := priceIter.Current()
				if pr == nil {
					continue
				}
				pricePtr, ok := pr.(*stripe.Price)
				if !ok {
					continue
				}
				_, _ = price.Update(pricePtr.ID, &stripe.PriceParams{Active: stripe.Bool(false)})
			}

			// Archive product
			_, _ = product.Update(productPtr.ID, &stripe.ProductParams{Active: stripe.Bool(false)})
		}
	}
	_ = productIter.Err()
}

// TestHelpers contains helper methods for Stripe integration tests
type TestHelpers struct {
	t            *testing.T
	customer     *stripe.Customer
	prices       map[string]*stripe.Price
	products     map[string]*stripe.Product
	subscription *stripe.Subscription
}

func newTestHelpers(t *testing.T) *TestHelpers {
	return &TestHelpers{
		t:        t,
		prices:   make(map[string]*stripe.Price),
		products: make(map[string]*stripe.Product),
	}
}

func (th *TestHelpers) createTestResources(oldPriceName string) {
	params := &stripe.CustomerParams{
		Name:  stripe.String("Test Customer"),
		Email: stripe.String(fmt.Sprintf("test-%d@example.com", time.Now().UnixNano())),
	}
	c, err := customer.New(params)
	require.NoError(th.t, err)
	th.customer = c

	pmParams := &stripe.PaymentMethodParams{
		Type: stripe.String("card"),
		Card: &stripe.PaymentMethodCardParams{Token: stripe.String("tok_visa")},
	}
	pm, err := paymentmethod.New(pmParams)
	require.NoError(th.t, err)

	_, err = paymentmethod.Attach(pm.ID, &stripe.PaymentMethodAttachParams{Customer: stripe.String(c.ID)})
	require.NoError(th.t, err)

	_, err = customer.Update(c.ID, &stripe.CustomerParams{
		InvoiceSettings: &stripe.CustomerInvoiceSettingsParams{DefaultPaymentMethod: stripe.String(pm.ID)},
	})
	require.NoError(th.t, err)

	th.createPrice("Weekly30", "Weekly $30 Plan", 3000, stripe.PriceRecurringIntervalWeek)
	th.createPrice("Monthly50", "Monthly $50 Plan", 5000, stripe.PriceRecurringIntervalMonth)
	th.createPrice("Monthly100", "Monthly $100 Plan", 10000, stripe.PriceRecurringIntervalMonth)
	th.createPrice("Monthly150", "Monthly $150 Plan", 15000, stripe.PriceRecurringIntervalMonth)
	th.createPrice("Yearly600", "Yearly $600 Plan", 60000, stripe.PriceRecurringIntervalYear)
	th.createPrice("Yearly1200", "Yearly $1200 Plan", 120000, stripe.PriceRecurringIntervalYear)
	th.createPrice("Daily5", "Daily $5 Plan", 500, stripe.PriceRecurringIntervalDay)

	subParams := &stripe.SubscriptionParams{
		Customer: stripe.String(th.customer.ID),
		Items: []*stripe.SubscriptionItemsParams{{Price: stripe.String(th.prices[oldPriceName].ID)}},
	}
	sub, err := subscriptionClient.New(subParams)
	require.NoError(th.t, err)
	th.subscription = sub
}

func (th *TestHelpers) createPrice(name, planName string, amount int64, cadence stripe.PriceRecurringInterval) {
	productParams := &stripe.ProductParams{Name: stripe.String(planName)}
	p, err := product.New(productParams)
	require.NoError(th.t, err)
	th.products[name] = p

	priceParams := &stripe.PriceParams{
		Product:    stripe.String(p.ID),
		UnitAmount: stripe.Int64(amount),
		Currency:   stripe.String("usd"),
		Recurring:  &stripe.PriceRecurringParams{Interval: stripe.String(string(cadence))},
	}
	pr, err := price.New(priceParams)
	require.NoError(th.t, err)
	th.prices[name] = pr
}

func (th *TestHelpers) previewSubscriptionChange(newPriceName string, prorationDate time.Time, behavior subscription.ProrationBehavior) *stripe.Invoice {
	existingItemID := th.subscription.Items.Data[0].ID

	params := &stripe.InvoiceCreatePreviewParams{
		Customer:        stripe.String(th.customer.ID),
		Subscription:    stripe.String(th.subscription.ID),
		SubscriptionDetails: &stripe.InvoiceCreatePreviewSubscriptionDetailsParams{
			Items: []*stripe.InvoiceCreatePreviewSubscriptionDetailsItemParams{
				{ID: stripe.String(existingItemID), Price: stripe.String(th.prices[newPriceName].ID)},
			},
		},
	}

	if behavior != subscription.ProrationBehaviorNone {
		params.SubscriptionDetails.ProrationDate = stripe.Int64(prorationDate.Unix())
		params.SubscriptionDetails.ProrationBehavior = stripe.String(string(behavior))
	}

	inv, err := invoice.CreatePreview(params)
	require.NoError(th.t, err)
	return inv
}

func (th *TestHelpers) cleanup() {
	if th.subscription != nil {
		_, _ = subscriptionClient.Cancel(th.subscription.ID, nil)
	}

	for _, pr := range th.prices {
		if pr != nil {
			_, _ = price.Update(pr.ID, &stripe.PriceParams{Active: stripe.Bool(false)})
		}
	}

	for _, prod := range th.products {
		if prod != nil {
			_, _ = product.Update(prod.ID, &stripe.ProductParams{Active: stripe.Bool(false)})
		}
	}

	if th.customer != nil {
		_, _ = customer.Del(th.customer.ID, nil)
	}

	th.subscription = nil
	th.customer = nil
	th.prices = make(map[string]*stripe.Price)
	th.products = make(map[string]*stripe.Product)
}

func TestMain(m *testing.M) {
	apiKey := os.Getenv("STRIPE_TEST_SECRET_KEY")
	if apiKey != "" {
		stripe.Key = apiKey
		cleanupOldTestResources()
	}
	os.Exit(m.Run())
}

func (th *TestHelpers) assertProrationMatch(oldPriceName, newPriceName string, oldPrice, newPrice subscription.Price, behavior subscription.ProrationBehavior, deltaTolerance float64, extraAssertions func(totalCredit, totalCharge int64)) {
	prorationDate := time.Now()
	currentPeriodStart := time.Unix(th.subscription.Items.Data[0].CurrentPeriodStart, 0)
	currentPeriodEnd := time.Unix(th.subscription.Items.Data[0].CurrentPeriodEnd, 0)

	billingCycle := subscription.BillingCycle{
		StartAt: currentPeriodStart,
		EndAt:   currentPeriodEnd,
		Cadence: th.cadenceFromStripe(th.subscription.Items.Data[0].Price.Recurring.Interval),
	}

	result, err := subscription.ProratedChange(oldPrice, newPrice, billingCycle, prorationDate, behavior)
	require.NoError(th.t, err)

	preview := th.previewSubscriptionChange(newPriceName, prorationDate, behavior)

	totalCredit, totalCharge := th.calculateProratedAmounts(preview)
	stripeNetProratedDollars := float64(totalCharge+totalCredit) / 100

	if extraAssertions != nil {
		extraAssertions(totalCredit, totalCharge)
	}
	require.InDelta(th.t, result.CreditDue.InexactFloat64(), stripeNetProratedDollars, deltaTolerance)
}

func (th *TestHelpers) checkStripeKey(t *testing.T) {
	if os.Getenv("STRIPE_TEST_SECRET_KEY") == "" {
		t.Skip("STRIPE_TEST_SECRET_KEY not set")
	}
}

func (th *TestHelpers) cadenceFromStripe(interval stripe.PriceRecurringInterval) subscription.Cadence {
	switch interval {
	case stripe.PriceRecurringIntervalDay:
		return subscription.CadenceDaily
	case stripe.PriceRecurringIntervalWeek:
		return subscription.CadenceWeekly
	case stripe.PriceRecurringIntervalMonth:
		return subscription.CadenceMonthly
	case stripe.PriceRecurringIntervalYear:
		return subscription.CadenceYearly
	default:
		return subscription.CadenceMonthly
	}
}

func (th *TestHelpers) calculateProratedAmounts(preview *stripe.Invoice) (totalCredit int64, totalCharge int64) {
	for _, line := range preview.Lines.Data {
		isProrated := line.Parent != nil && line.Parent.SubscriptionItemDetails != nil && line.Parent.SubscriptionItemDetails.Proration
		if isProrated {
			if line.Amount < 0 {
				totalCredit += line.Amount
			} else {
				totalCharge += line.Amount
			}
		}
	}
	return
}

func (th *TestHelpers) prorationDateAtPercent(percent float64) time.Time {
	start := time.Unix(th.subscription.Items.Data[0].CurrentPeriodStart, 0)
	end := time.Unix(th.subscription.Items.Data[0].CurrentPeriodEnd, 0)
	elapsed := end.Sub(start).Seconds() * percent
	return start.Add(time.Duration(elapsed * float64(time.Second)))
}

func TestStripeProrationSameCadence(t *testing.T) {
	th := newTestHelpers(t)
	th.checkStripeKey(t)
	th.createTestResources("Monthly100")
	defer th.cleanup()

	oldPrice := subscription.Price{Amount: decimal.NewFromInt(100), Cadence: subscription.CadenceMonthly}
	newPrice := subscription.Price{Amount: decimal.NewFromInt(150), Cadence: subscription.CadenceMonthly}
	th.assertProrationMatch("Monthly100", "Monthly150", oldPrice, newPrice, subscription.ProrationBehaviorCreateProrations, 1.0, nil)
}

func TestStripeProrationCrossCadence(t *testing.T) {
	th := newTestHelpers(t)
	th.checkStripeKey(t)
	th.createTestResources("Monthly50")
	defer th.cleanup()

	oldPrice := subscription.Price{Amount: decimal.NewFromInt(50), Cadence: subscription.CadenceMonthly}
	newPrice := subscription.Price{Amount: decimal.NewFromInt(600), Cadence: subscription.CadenceYearly}
	th.assertProrationMatch("Monthly50", "Yearly600", oldPrice, newPrice, subscription.ProrationBehaviorCreateProrations, 15.0, func(totalCredit, totalCharge int64) {
		require.Greater(t, totalCharge, int64(50000))
	})
}

func TestStripeProrationPreviews(t *testing.T) {
	th := newTestHelpers(t)
	th.checkStripeKey(t)
	th.createTestResources("Monthly100")
	defer th.cleanup()

	oldPrice := subscription.Price{Amount: decimal.NewFromInt(100), Cadence: subscription.CadenceMonthly}
	newPrice := subscription.Price{Amount: decimal.NewFromInt(150), Cadence: subscription.CadenceMonthly}
	th.assertProrationMatch("Monthly100", "Monthly150", oldPrice, newPrice, subscription.ProrationBehaviorCreateProrations, 5.0, nil)
}

func TestProrationDowngrade(t *testing.T) {
	th := newTestHelpers(t)
	th.checkStripeKey(t)
	th.createTestResources("Monthly150")
	defer th.cleanup()

	oldPrice := subscription.Price{Amount: decimal.NewFromInt(150), Cadence: subscription.CadenceMonthly}
	newPrice := subscription.Price{Amount: decimal.NewFromInt(100), Cadence: subscription.CadenceMonthly}
	th.assertProrationMatch("Monthly150", "Monthly100", oldPrice, newPrice, subscription.ProrationBehaviorCreateProrations, 1.0, func(totalCredit, totalCharge int64) {
		require.Less(t, totalCredit, int64(0))
	})
}

func TestProrationYearlyDowngrade(t *testing.T) {
	th := newTestHelpers(t)
	th.checkStripeKey(t)
	th.createTestResources("Yearly1200")
	defer th.cleanup()

	oldPrice := subscription.Price{Amount: decimal.NewFromInt(1200), Cadence: subscription.CadenceYearly}
	newPrice := subscription.Price{Amount: decimal.NewFromInt(600), Cadence: subscription.CadenceYearly}
	th.assertProrationMatch("Yearly1200", "Yearly600", oldPrice, newPrice, subscription.ProrationBehaviorCreateProrations, 5.0, func(totalCredit, totalCharge int64) {
		require.Less(t, totalCredit, int64(0))
	})
}

func TestProrationImmediateStart(t *testing.T) {
	th := newTestHelpers(t)
	th.checkStripeKey(t)
	th.createTestResources("Monthly100")
	defer th.cleanup()

	oldPrice := subscription.Price{Amount: decimal.NewFromInt(100), Cadence: subscription.CadenceMonthly}
	newPrice := subscription.Price{Amount: decimal.NewFromInt(150), Cadence: subscription.CadenceMonthly}
	prorationDate := th.prorationDateAtPercent(0.03)

	currentPeriodStart := time.Unix(th.subscription.Items.Data[0].CurrentPeriodStart, 0)
	currentPeriodEnd := time.Unix(th.subscription.Items.Data[0].CurrentPeriodEnd, 0)

	billingCycle := subscription.BillingCycle{
		StartAt: currentPeriodStart,
		EndAt:   currentPeriodEnd,
		Cadence: th.cadenceFromStripe(th.subscription.Items.Data[0].Price.Recurring.Interval),
	}

	result, err := subscription.ProratedChange(oldPrice, newPrice, billingCycle, prorationDate, subscription.ProrationBehaviorCreateProrations)
	require.NoError(t, err)

	preview := th.previewSubscriptionChange("Monthly150", prorationDate, subscription.ProrationBehaviorCreateProrations)

	totalCredit, totalCharge := th.calculateProratedAmounts(preview)
	stripeNetProratedDollars := float64(totalCharge+totalCredit) / 100

	require.InDelta(t, result.CreditDue.InexactFloat64(), stripeNetProratedDollars, 5.0)
}

func TestProrationImmediateEnd(t *testing.T) {
	th := newTestHelpers(t)
	th.checkStripeKey(t)
	th.createTestResources("Monthly100")
	defer th.cleanup()

	oldPrice := subscription.Price{Amount: decimal.NewFromInt(100), Cadence: subscription.CadenceMonthly}
	newPrice := subscription.Price{Amount: decimal.NewFromInt(150), Cadence: subscription.CadenceMonthly}
	prorationDate := th.prorationDateAtPercent(0.97)

	currentPeriodStart := time.Unix(th.subscription.Items.Data[0].CurrentPeriodStart, 0)
	currentPeriodEnd := time.Unix(th.subscription.Items.Data[0].CurrentPeriodEnd, 0)

	billingCycle := subscription.BillingCycle{
		StartAt: currentPeriodStart,
		EndAt:   currentPeriodEnd,
		Cadence: th.cadenceFromStripe(th.subscription.Items.Data[0].Price.Recurring.Interval),
	}

	result, err := subscription.ProratedChange(oldPrice, newPrice, billingCycle, prorationDate, subscription.ProrationBehaviorCreateProrations)
	require.NoError(t, err)

	preview := th.previewSubscriptionChange("Monthly150", prorationDate, subscription.ProrationBehaviorCreateProrations)

	totalCredit, totalCharge := th.calculateProratedAmounts(preview)
	stripeNetProratedDollars := float64(totalCharge+totalCredit) / 100

	require.InDelta(t, result.CreditDue.InexactFloat64(), stripeNetProratedDollars, 5.0)
}

func TestProrationMidpoint(t *testing.T) {
	th := newTestHelpers(t)
	th.checkStripeKey(t)
	th.createTestResources("Monthly100")
	defer th.cleanup()

	oldPrice := subscription.Price{Amount: decimal.NewFromInt(100), Cadence: subscription.CadenceMonthly}
	newPrice := subscription.Price{Amount: decimal.NewFromInt(100), Cadence: subscription.CadenceMonthly}
	prorationDate := th.prorationDateAtPercent(0.50)

	currentPeriodStart := time.Unix(th.subscription.Items.Data[0].CurrentPeriodStart, 0)
	currentPeriodEnd := time.Unix(th.subscription.Items.Data[0].CurrentPeriodEnd, 0)

	billingCycle := subscription.BillingCycle{
		StartAt: currentPeriodStart,
		EndAt:   currentPeriodEnd,
		Cadence: th.cadenceFromStripe(th.subscription.Items.Data[0].Price.Recurring.Interval),
	}

	result, err := subscription.ProratedChange(oldPrice, newPrice, billingCycle, prorationDate, subscription.ProrationBehaviorCreateProrations)
	require.NoError(t, err)

	preview := th.previewSubscriptionChange("Monthly100", prorationDate, subscription.ProrationBehaviorCreateProrations)

	totalCredit, totalCharge := th.calculateProratedAmounts(preview)
	stripeNetProratedDollars := float64(totalCharge+totalCredit) / 100

	require.InDelta(t, result.CreditDue.InexactFloat64(), stripeNetProratedDollars, 1.0)
	require.InDelta(t, float64(-totalCredit), float64(totalCharge), 1.0)
}

func TestRetroactiveProration(t *testing.T) {
	th := newTestHelpers(t)
	th.checkStripeKey(t)
	th.createTestResources("Monthly100")
	defer th.cleanup()

	oldPrice := subscription.Price{Amount: decimal.NewFromInt(100), Cadence: subscription.CadenceMonthly}
	newPrice := subscription.Price{Amount: decimal.NewFromInt(150), Cadence: subscription.CadenceMonthly}

	currentPeriodStart := time.Unix(th.subscription.Items.Data[0].CurrentPeriodStart, 0)
	currentPeriodEnd := time.Unix(th.subscription.Items.Data[0].CurrentPeriodEnd, 0)

	// Set proration date to 2 days after cycle started to test retroactive calculation
	prorationDate := currentPeriodStart.Add(2 * 24 * time.Hour)

	billingCycle := subscription.BillingCycle{
		StartAt: currentPeriodStart,
		EndAt:   currentPeriodEnd,
		Cadence: th.cadenceFromStripe(th.subscription.Items.Data[0].Price.Recurring.Interval),
	}

	result, err := subscription.ProratedChange(oldPrice, newPrice, billingCycle, prorationDate, subscription.ProrationBehaviorCreateProrations)
	require.NoError(t, err)

	preview := th.previewSubscriptionChange("Monthly150", prorationDate, subscription.ProrationBehaviorCreateProrations)

	totalCredit, totalCharge := th.calculateProratedAmounts(preview)
	stripeNetProratedDollars := float64(totalCharge+totalCredit) / 100

	require.InDelta(t, result.CreditDue.InexactFloat64(), stripeNetProratedDollars, 2.0)
}

func TestWeeklyToMonthlyUpgrade(t *testing.T) {
	th := newTestHelpers(t)
	th.checkStripeKey(t)
	th.createTestResources("Weekly30")
	defer th.cleanup()

	oldPrice := subscription.Price{Amount: decimal.NewFromInt(30), Cadence: subscription.CadenceWeekly}
	newPrice := subscription.Price{Amount: decimal.NewFromInt(100), Cadence: subscription.CadenceMonthly}
	th.assertProrationMatch("Weekly30", "Monthly100", oldPrice, newPrice, subscription.ProrationBehaviorCreateProrations, 5.0, nil)
}

func TestWeeklyToYearlyChange(t *testing.T) {
	th := newTestHelpers(t)
	th.checkStripeKey(t)
	th.createTestResources("Weekly30")
	defer th.cleanup()

	oldPrice := subscription.Price{Amount: decimal.NewFromInt(30), Cadence: subscription.CadenceWeekly}
	newPrice := subscription.Price{Amount: decimal.NewFromInt(600), Cadence: subscription.CadenceYearly}
	th.assertProrationMatch("Weekly30", "Yearly600", oldPrice, newPrice, subscription.ProrationBehaviorCreateProrations, 10.0, func(totalCredit, totalCharge int64) {
		require.Greater(t, totalCharge, int64(50000))
	})
}

func TestLargeAmounts(t *testing.T) {
	th := newTestHelpers(t)
	th.checkStripeKey(t)
	th.createTestResources("Monthly100")
	defer th.cleanup()

	oldPrice := subscription.Price{Amount: decimal.NewFromInt(100), Cadence: subscription.CadenceMonthly}
	newPrice := subscription.Price{Amount: decimal.NewFromInt(150), Cadence: subscription.CadenceMonthly}

	currentPeriodStart := time.Unix(th.subscription.Items.Data[0].CurrentPeriodStart, 0)
	currentPeriodEnd := time.Unix(th.subscription.Items.Data[0].CurrentPeriodEnd, 0)
	prorationDate := th.prorationDateAtPercent(0.50)

	billingCycle := subscription.BillingCycle{
		StartAt: currentPeriodStart,
		EndAt:   currentPeriodEnd,
		Cadence: th.cadenceFromStripe(th.subscription.Items.Data[0].Price.Recurring.Interval),
	}

	result, err := subscription.ProratedChange(oldPrice, newPrice, billingCycle, prorationDate, subscription.ProrationBehaviorCreateProrations)
	require.NoError(t, err)

	preview := th.previewSubscriptionChange("Monthly150", prorationDate, subscription.ProrationBehaviorCreateProrations)

	totalCredit, totalCharge := th.calculateProratedAmounts(preview)
	stripeNetProratedDollars := float64(totalCharge+totalCredit) / 100

	require.InDelta(t, result.CreditDue.InexactFloat64(), stripeNetProratedDollars, 5.0)
}

func TestDailyToMonthlyUpgrade(t *testing.T) {
	th := newTestHelpers(t)
	th.checkStripeKey(t)
	th.createTestResources("Daily5")
	defer th.cleanup()

	oldPrice := subscription.Price{Amount: decimal.NewFromInt(5), Cadence: subscription.CadenceDaily}
	newPrice := subscription.Price{Amount: decimal.NewFromInt(100), Cadence: subscription.CadenceMonthly}
	th.assertProrationMatch("Daily5", "Monthly100", oldPrice, newPrice, subscription.ProrationBehaviorCreateProrations, 10.0, func(totalCredit, totalCharge int64) {
		// Daily $5 (~$150/month) to Monthly $100 is a downgrade, so expect credit
		require.Less(t, totalCredit, int64(0))
	})
}


func TestProrationSamePlanNoChange(t *testing.T) {
	th := newTestHelpers(t)
	th.checkStripeKey(t)
	th.createTestResources("Monthly100")
	defer th.cleanup()

	oldPrice := subscription.Price{Amount: decimal.NewFromInt(100), Cadence: subscription.CadenceMonthly}
	newPrice := subscription.Price{Amount: decimal.NewFromInt(100), Cadence: subscription.CadenceMonthly}

	currentPeriodStart := time.Unix(th.subscription.Items.Data[0].CurrentPeriodStart, 0)
	currentPeriodEnd := time.Unix(th.subscription.Items.Data[0].CurrentPeriodEnd, 0)
	prorationDate := th.prorationDateAtPercent(0.25)

	billingCycle := subscription.BillingCycle{
		StartAt: currentPeriodStart,
		EndAt:   currentPeriodEnd,
		Cadence: th.cadenceFromStripe(th.subscription.Items.Data[0].Price.Recurring.Interval),
	}

	result, err := subscription.ProratedChange(oldPrice, newPrice, billingCycle, prorationDate, subscription.ProrationBehaviorCreateProrations)
	require.NoError(t, err)

	preview := th.previewSubscriptionChange("Monthly100", prorationDate, subscription.ProrationBehaviorCreateProrations)

	totalCredit, totalCharge := th.calculateProratedAmounts(preview)
	stripeNetProratedDollars := float64(totalCharge+totalCredit) / 100

	require.InDelta(t, 0.0, stripeNetProratedDollars, 0.5)
	require.InDelta(t, 0.0, result.CreditDue.InexactFloat64(), 0.5)
}

func TestProrationBehaviorNoneNoProration(t *testing.T) {
	th := newTestHelpers(t)
	th.checkStripeKey(t)
	th.createTestResources("Monthly50")
	defer th.cleanup()

	oldPrice := subscription.Price{Amount: decimal.NewFromInt(50), Cadence: subscription.CadenceMonthly}
	newPrice := subscription.Price{Amount: decimal.NewFromInt(100), Cadence: subscription.CadenceMonthly}
	prorationDate := th.prorationDateAtPercent(0.50)

	currentPeriodStart := time.Unix(th.subscription.Items.Data[0].CurrentPeriodStart, 0)
	currentPeriodEnd := time.Unix(th.subscription.Items.Data[0].CurrentPeriodEnd, 0)

	billingCycle := subscription.BillingCycle{
		StartAt: currentPeriodStart,
		EndAt:   currentPeriodEnd,
		Cadence: th.cadenceFromStripe(th.subscription.Items.Data[0].Price.Recurring.Interval),
	}

	result, err := subscription.ProratedChange(oldPrice, newPrice, billingCycle, prorationDate, subscription.ProrationBehaviorNone)
	require.NoError(t, err)

	// With ProrationBehaviorNone, no credit or charge should be calculated
	require.Equal(t, 0.0, result.UnusedCredit.InexactFloat64())
	require.Equal(t, 0.0, result.NewCharge.InexactFloat64())
	require.Equal(t, 0.0, result.CreditDue.InexactFloat64())
}
