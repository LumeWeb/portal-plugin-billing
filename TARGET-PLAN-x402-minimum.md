# x402 Minimum Support — Target Plan

## Scope
- Tight MVP: x402 as wire protocol only (no signature verification, no on-chain submission)
- ATLOS handles actual payment collection; x402 carries payment proof
- x402 paywall middleware handles signing/submission client-side or via relayer

## Architecture

### Sequence
```
1. Client → POST /api/account/billing/checkout?wallet=&amount=
   (no X-Payment-Response header)

2. Server → 402 Payment Required
   Header: Payment-Required: <base64(challenge)>
   Challenge includes ATLOS payment address as PayTo

3. Client pays ATLOS directly (via wallet or x402 middleware)

4. Client → POST /api/account/billing/checkout
   Header: X-Payment-Response: <base64(payment_payload)>

5. Server extracts nonce from payload, checks nonce store
   Server calls ConfirmPayment (checks ATLOS webhook cache)

6. Server → 200 OK with credit balance
   (or 202 Accepted if still pending)
```

### Key Design Decision
- **No signature verification in server** — x402 paywall middleware handles that
- **No on-chain submission by server** — client or middleware submits
- **Server only** generates challenge + confirms settlement via ATLOS webhook

## Implementation Status

### Done ✅
| File | What |
|------|------|
| `core/gateway.go` | `PaymentProcessor` interface: `ConfirmPayment` only (no `VerifyPaymentSignature`) |
| `internal/x402/handler.go` | Challenge generation, payload parsing, nonce validation, credit issuance |
| `internal/x402/nonce_store.go` | DB-backed nonce store with TTL |
| `internal/api/api_extension.go` | Route wired: `POST /api/account/billing/x402/checkout` |
| `internal/gateway/atlos/x402.go` | `ConfirmPayment` via webhook cache, `isX402Nonce`, webhook handler |
| `internal/gateway/atlos/x402_test.go` | Tests for nonce detection, webhook cache |

### Still Needed

#### 1. ATLOS Payment Address Integration
Currently `PayTo` is hardcoded to `"0x"`. Need to:
- Call ATLOS `Payment/Create` API to get actual receiving wallet address
- Include ATLOS payment ID in nonce store for correlation
- Pass ATLOS payment ID as `OrderId` in webhook handler

#### 2. Webhook Correlation
- ATLOS webhook sends `OrderId` (our nonce) + `TransactionId`
- `handleX402Webhook` stores mapping in `WebhookNonceCache`
- `ConfirmPayment` checks cache → returns `PaymentConfirmation`

#### 3. x402 PayTo Address
Need ATLOS API call in `returnChallenge`:
```go
// TODO: call ATLOS Payment/Create API
// POST /Payment/Create
// Body: { asset, blockchain, amount, metadata: { nonce } }
// Response: { paymentId, walletAddress }
// Use walletAddress as PayTo
```

## Files Modified
- `core/gateway.go` — `PaymentProcessor` interface, `X402PaymentPayload` types
- `internal/x402/handler.go` — Handler implementation
- `internal/x402/nonce_store.go` — DB nonce store
- `internal/api/api_extension.go` — Route registration
- `internal/gateway/atlos/x402.go` — ATLOS `ConfirmPayment`, webhook cache
- `internal/gateway/atlos/x402_test.go` — Unit tests

## Dependencies
- `go-ethereum` (already in go.mod) — kept for future use but not currently used
- No x402 Go library imported — wire format is simple JSON

## Testing
- `go test ./internal/x402/...` ✅
- `go test ./internal/gateway/atlos/...` ✅
- `go build ./...` ✅
