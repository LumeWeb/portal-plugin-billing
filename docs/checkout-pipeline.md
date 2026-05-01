# Stripe Checkout Pipeline

End-to-end documentation of the checkout flow in the billing plugin, from API request through Stripe Embedded Checkout to webhook-driven subscription activation.

## Architecture Overview

The checkout pipeline is a **three-phase, two-step activation** flow:

1. **Frontend Initiation** — API call creates a Stripe Checkout Session and returns embedded UI fragments
2. **Embedded Checkout** — Stripe.js renders the payment form in-page; `paymentCompleted` event fires on completion
3. **Webhook Activation** — Two sequential Stripe webhooks transition the subscriber from pending → active

```
┌──────────┐     GET /checkout/ui/:planId     ┌───────────────┐
│  Frontend │ ────────────────────────────────► │  API Handler  │
│           │ ◄── CheckoutUIResponse (3 frags) │               │
└─────┬─────┘                                  └───────┬───────┘
      │                                                │
      │ Renders embedded Stripe form                   │ BillingService.GetCheckoutUI()
      │                                                │ → StripeGateway.GetCheckoutUI()
      │                                                │ → Stripe CheckoutSession API
      ▼                                                ▼
┌──────────────┐                            ┌──────────────────────┐
│  Stripe.js   │  checkout.session.completed │  Webhook Handler    │
│  Embedded    │ ──────────────────────────► │  → Create PENDING  │
│  Checkout    │                             │    subscriber       │
│              │  invoice.paid               │                    │
│              │ ──────────────────────────► │  → Activate        │
└──────────────┘                             │    subscriber       │
                                             └──────────────────────┘
```

---

## Phase 1: API Layer — Checkout UI Request

### Endpoint

```
GET /api/account/billing/checkout/ui/:planId?period_id=<periodID>&gateway=stripe
```

| Parameter | Location | Required | Description |
|-----------|----------|----------|-------------|
| `planId`  | Path     | Yes      | UUID of the pricing plan |
| `period_id` | Query  | Yes      | UUID of the pricing plan period (billing cadence) |
| `gateway` | Query    | No       | Gateway type, defaults to `"stripe"` |

**Auth required.** User ID extracted from the authenticated session.

### API Handler (`api_extension.go`)

1. Extract `userID` from auth context
2. Parse `planID` (path), `periodID` (query), `gateway` (query, default `"stripe"`)
3. Call `billingService.GetCheckoutUI(ctx, userID, planID, gatewayType, periodID)`
4. Return `CheckoutUIResponse` as JSON
5. On error: HTTP 409 if already subscribed, 500 otherwise

### BillingService Layer (`service/billing/checkout.go`)

Business validation before delegating to gateway:

1. **Validate `periodID != 0`** — reject empty period
2. **Check for active subscription** — `GetActiveSubscription(userID)` → reject with `ErrUserAlreadySubscribed` if found (HTTP 409)
3. **Validate pricing plan** — `GetPricingPlan(planID)` → must be active and public
4. **Resolve gateway** — default to `"stripe"`, lookup via `GetGateway(gatewayType)`
5. **Delegate** — `gateway.AsCheckoutProvider().GetCheckoutUI(ctx, userID, planID, periodID)`

---

## Phase 2: Stripe Gateway — Session Creation

### `StripeGateway.GetCheckoutUI()` (`stripe.go:1941-2064`)

Orchestrates Stripe Checkout Session creation and returns UI fragments:

```
Step 1: ValidateServices(users, quota)         — ensure dependencies available
Step 2: GetPricingPlan(planID)                  — validate plan is active
Step 3: GetPricingPlanPeriods(planID)           — find matching periodID
Step 4: GetGatewayProductMapping(periodID, "stripe") — get RemotePriceID
Step 5: getUser(userID)                         — validate user exists
Step 6: getOrCreateStripeCustomer(userID)       — ensure Stripe customer exists
Step 7: Create Stripe CheckoutSession            — embedded mode, subscription
Step 8: buildEmbeddedCheckoutFragment(secret)    — return 3 UI fragments
```

#### Step 6: `getOrCreateStripeCustomer`

- Query `billing_subscribers` table for existing Stripe customer ID for this user
- If found, reuse the existing `customerID`
- If not found, create a new Stripe Customer via API with `metadata[user_id]` set
- Return the `customerID`

#### Step 7: Stripe CheckoutSession Parameters

| Parameter | Value |
|-----------|-------|
| `UIMode` | `embedded` |
| `Mode` | `subscription` |
| `LineItem` | `{Price: RemotePriceID}` |
| `Customer` | `customerID` |
| `ClientReferenceID` | `userID` |
| `RedirectOnCompletion` | `if_required` |
| `ReturnURL` | Built via `BuildAbsoluteURL` with dashboard subdomain |
| `AutomaticTax` | Enabled |
| `AllowPromotionCodes` | Enabled |

**Return URL format:**
```
https://<dashboard>.<domain>/account/subscription?checkout_return=1&session_id={CHECKOUT_SESSION_ID}
```

#### Step 8: `buildEmbeddedCheckoutFragment(clientSecret)`

Returns a `CheckoutUIResponse` with **3 fragments**:

| # | FragmentType | Content |
|---|-------------|----------|
| 1 | `script_url` | `https://js.stripe.com/dahlia/stripe.js` |
| 2 | `html` | `<div id="stripe-checkout-container"><div id="stripe-checkout"></div></div>` |
| 3 | `script` | Rendered `embedded_checkout.tpl` with `PublishableKey` + `ClientSecret` |

### Core Types

```go
// core/gateway.go

type CheckoutUIResponse struct {
    Fragments []CheckoutUIFragment
    SessionID string
    ExpiresAt *time.Time
    Metadata  map[string]any
}

type CheckoutUIFragment struct {
    Type    FragmentType       // link, html, script, script_url, iframe, modal, button, form
    HTML    string
    Script  string
    Link    string
    CSS     string
    Metadata map[string]any
}

type SessionStatus struct {
    Status        SessionStatusEnum // open, complete, expired
    CustomerEmail string
    SessionID     string
    UserID        string
}
```

### Gateway Interfaces

```go
type CheckoutProvider interface {
    GetCheckoutUI(ctx context.Context, userID string, planID string, periodID string) (*CheckoutUIResponse, error)
}

type SessionStatusProvider interface {
    GetSessionStatus(ctx context.Context, sessionID string) (*SessionStatus, error)
}
```

---

## Phase 2 (Client): Embedded Checkout Rendering

### `embedded_checkout.tpl`

The template renders an IIFE that:

1. Initializes Stripe: `globalThis.Stripe(publishableKey)`
2. Creates embedded checkout page: `stripe.createEmbeddedCheckoutPage({clientSecret, onComplete})`
3. Mounts to `#stripe-checkout` DOM element
4. On checkout completion, dispatches `paymentCompleted` CustomEvent on `window`

### JS Payment Events Protocol

All events dispatched on `window` via `CustomEvent` with `bubbles: true`.

| Event Name | Detail | Direction | Description |
|------------|--------|-----------|-------------|
| `paymentSuccess` | `null` | Gateway → Frontend | Payment succeeded |
| `paymentCanceled` | `null` | Gateway → Frontend | User canceled payment |
| `paymentCompleted` | `null` | Gateway → Frontend | Terminal event; fires after success or canceled |
| `paymentError` | `{error: string}` | Gateway → Frontend | Payment error occurred |
| `paymentCleanupRegister` | `{cleanup: fn}` | Gateway → Frontend | Gateway registers cleanup callback |

**Cleanup pattern:** The gateway registers a cleanup function via `paymentCleanupRegister`. The frontend stores it and invokes it on `paymentCompleted` or component unmount. Cleanup:
- Unmounts the Stripe embedded checkout page
- Clears the container DOM
- Removes Stripe script tags
- Deletes `globalThis.Stripe`

### Frontend Integration Flow

```
1. Fetch GET /api/account/billing/checkout/ui/:planId?period_id=X
2. Render fragments: inject script_url, mount HTML container, execute script
3. Listen for paymentCompleted event on window
4. On paymentCompleted → call registered cleanup function
5. Optionally redirect or poll session status
```

---

## Phase 3: Webhook-Driven Subscription Activation

Subscription activation is a **two-step process** driven by two sequential Stripe webhooks:

### Step 1: `checkout.session.completed` → Create PENDING Subscriber

**Handler:** `handleCheckoutSessionCompleted()` (`stripe.go:1044-1143`)

Triggered when the Stripe Checkout Session completes (user finishes payment form).

```
1. Unmarshal session event data
2. Verify session.Mode == "subscription" (skip one-time payments)
3. Parse userID from session.ClientReferenceID
4. Extract subscriptionID from session.Subscription.ID
5. Extract customerID from session.Customer.ID
6. Fetch expanded subscription from Stripe (to read period_id from price metadata)
7. CreateOrUpdateSubscriber(userID, "stripe", customerID, subscriptionID, isActive=false, periodID)
   → Creates subscriber record in PENDING state (isActive = false)
```

**Key detail:** At this point, the subscriber exists but is **not active**. The subscription is not yet usable.

### Step 2: `invoice.paid` → Activate Subscriber

**Handler:** `handleInvoicePaid()` (`stripe.go:1175-1418`)

Triggered when Stripe confirms payment for the first invoice. This also handles subsequent renewal invoices.

```
1.  Unmarshal invoice event data
2.  Extract customerID and subscriptionID from invoice line items
3.  Look up subscriber by subscriptionID → get userID
4.  Fetch expanded subscription from Stripe for validation
5.  determineOperationType() → classify as new / renewal / upgrade / downgrade / cancellation
6.  validateAndCalculateCreditAmount() → compute credit with proration comparison
7.  If validatedAmount > 0:
      IssueCreditWithIdempotency(TransactionTypeCharge, invoiceID as idempotency key)
      Fire PaymentCompletedEvent
8.  Check user balance ≥ 0
9.  If sufficient balance:
      IssueUsageCredit(TransactionTypeTime) → period debit
      AssignUserToPlan() → quota assignment
      activateSubscription() → final activation
10. activateSubscription():
      findPeriodIDFromSubscription() → resolve period from Stripe price metadata
      CreateOrUpdateSubscriber(isActive=true, periodID, billing period start/end dates)
```

#### Operation Types

| Type | Trigger |
|------|---------|
| `new` | First invoice for a new subscription |
| `renewal` | Subsequent billing cycle invoice |
| `upgrade` | Plan change to higher tier |
| `downgrade` | Plan change to lower tier |
| `cancellation` | Final invoice after cancellation |

#### Credit & Idempotency

- Credits are issued with **invoice ID as idempotency key** — prevents double-processing on webhook retries
- `TransactionTypeCharge` = credit issued from payment
- `TransactionTypeTime` = period-based debit (subscription time credit)
- Payment validation includes proration comparison via `validateAndCalculateCreditAmount`

### `invoice.payment_failed` Handler

- **Logs only** — no state change on payment failure
- Does not deactivate or modify the subscriber record

---

## Session Status Verification

### Endpoint

```
GET /api/account/billing/checkout/session/:sessionId/status?gateway=stripe
```

### `StripeGateway.GetSessionStatus()` (`stripe.go:3525-3560`)

Retrieves session status from the Stripe API for the return page:

```
1. Retrieve session from Stripe API (expand customer_details, customer)
2. Return SessionStatus{
     Status:        session.Status (open/complete/expired),
     CustomerEmail: from CustomerDetails or Customer,
     SessionID:     session.ID,
     UserID:        from ClientReferenceID
   }
```

### API Handler Security

- **IDOR protection**: Verifies `status.UserID == authenticated userID`. Returns 404 on mismatch.
- Returns `CheckoutSessionStatusResponse` DTO

### Use Cases

- **Return page**: After Stripe redirect, frontend polls to confirm checkout completed
- **SSE status**: Real-time subscription status updates after payment

---

## Complete Data Flow Diagram

```
 User                Frontend              API/BillingService         StripeGateway           Stripe API      billing_subscribers
  │                     │                        │                        │                      │                   │
  │  Click "Subscribe"  │                        │                        │                      │                   │
  │────────────────────►│                        │                        │                      │                   │
  │                     │  GET /checkout/ui/:planId                       │                      │                   │
  │                     │───────────────────────►│                        │                      │                   │
  │                     │                        │  GetCheckoutUI()       │                      │                   │
  │                     │                        │───────────────────────►│                      │                   │
  │                     │                        │                        │  getOrCreateCustomer │                   │
  │                     │                        │                        │─────────────────────►│                   │
  │                     │                        │                        │◄───── customerID ───│  (or fetch existing from DB)
  │                     │                        │                        │                      │                   │
  │                     │                        │                        │  Create Session      │                   │
  │                     │                        │                        │─────────────────────►│                   │
  │                     │                        │                        │◄── clientSecret ─────│                   │
  │                     │                        │◄──────────────────────│                      │                   │
  │                     │◄─── 3 UI fragments ───│                        │                      │                   │
  │                     │                        │                        │                      │                   │
  │  Render embedded    │                        │                        │                      │                   │
  │◄────────────────────│                        │                        │                      │                   │
  │  Stripe form        │                        │                        │                      │                   │
  │────────────────────►│                        │                        │                      │                   │
  │  (payment details)  │                        │                        │                      │                   │
  │                     │                        │                        │ checkout.session.     │                   │
  │                     │                        │                        │ completed webhook     │                   │
  │                     │                        │                        │◄─────────────────────│                   │
  │                     │                        │                        │                      │  Create PENDING   │
  │                     │                        │                        │─────────────────────────────────────────►│
  │                     │                        │                        │                      │  subscriber       │
  │                     │                        │                        │                      │  (isActive=false) │
  │                     │                        │                        │  invoice.paid        │                   │
  │                     │                        │                        │  webhook             │                   │
  │                     │                        │                        │◄─────────────────────│                   │
  │                     │                        │                        │  IssueCredit,        │                   │
  │                     │                        │                        │  AssignUserToPlan,   │                   │
  │                     │                        │                        │  ActivateSub         │                   │
  │                     │                        │                        │─────────────────────────────────────────►│
  │                     │                        │                        │                      │  isActive=true    │
  │                     │                        │                        │                      │  periodID set     │
  │                     │                        │                        │                      │  dates set        │
  │                     │                        │                        │                      │                   │
  │  paymentCompleted   │                        │                        │                      │                   │
  │◄────────────────────│                        │                        │                      │                   │
  │  event fired        │                        │                        │                      │                   │
```

---

## Source Files

| File | Role |
|------|------|
| `internal/api/api_extension.go` | API route handlers, request/response marshalling |
| `internal/api/dto/checkout.go` | Checkout DTOs (`CheckoutUIRequest`, `CheckoutSessionStatusResponse`) |
| `internal/service/billing/checkout.go` | Business validation layer |
| `internal/service/billing/billing.go` | Gateway registry lookup |
| `core/gateway.go` | `CheckoutProvider`, `SessionStatusProvider` interfaces, `CheckoutUIResponse`, `CheckoutUIFragment`, `SessionStatus` types |
| `internal/gateway/stripe/stripe.go` | Stripe gateway implementation — `GetCheckoutUI()`, `GetSessionStatus()`, webhook handlers |
| `internal/gateway/stripe/templates/embedded_checkout.tpl` | Stripe Embedded Checkout JS template |
| `internal/gateway/js_payment_events.md` | JS payment event protocol specification |

---

## Key Design Decisions

1. **Two-step activation**: `checkout.session.completed` creates a pending subscriber; `invoice.paid` activates it. This ensures the user is only granted access after payment is confirmed, not just after the checkout form is submitted.

2. **Embedded checkout over hosted**: Uses Stripe's embedded checkout (`UIMode=embedded`) rather than redirect-based hosted checkout. This keeps the user on-page and allows the `paymentCompleted` event to fire in the same browser context.

3. **Fragment-based UI**: The gateway returns UI fragments (script_url, html, script) rather than a URL, making the checkout renderer gateway-agnostic. Other gateways could return different fragment types.

4. **Idempotent credit issuing**: Invoice IDs serve as idempotency keys to prevent double-crediting on webhook retries.

5. **IDOR protection on session status**: The session status endpoint verifies the authenticated user owns the session before returning data.
