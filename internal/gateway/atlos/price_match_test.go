package atlos

import (
	"fmt"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func TestPriceMatchesExpected(t *testing.T) {
	tests := []struct {
		name          string
		paidAmount    string
		expectedPrice string
		shouldMatch   bool
		explanation   string
	}{
		// --- Exact matches ---
		{
			name:          "exact match — whole dollar",
			paidAmount:    "1",
			expectedPrice: "1",
			shouldMatch:   true,
			explanation:   "identical amounts always match",
		},
		{
			name:          "exact match — with cents",
			paidAmount:    "1.50",
			expectedPrice: "1.50",
			shouldMatch:   true,
			explanation:   "identical cent-level amounts match",
		},
		{
			name:          "both zero",
			paidAmount:    "0",
			expectedPrice: "0",
			shouldMatch:   true,
			explanation:   "zero amounts match via min threshold",
		},

		// --- Low-price plan: $1/m ---
		{
			name:          "$1/m plan — real production drift",
			paidAmount:    "1",
			expectedPrice: "0.9998450567502987",
			shouldMatch:   true,
			explanation:   "sub-cent drift from timestamp difference on $1 plan",
		},
		{
			name:          "$1/m plan — drift in other direction",
			paidAmount:    "1",
			expectedPrice: "1.0001549432497013",
			shouldMatch:   true,
			explanation:   "sub-cent drift in other direction on $1 plan",
		},
		{
			name:          "$1/m plan — half cent drift",
			paidAmount:    "1",
			expectedPrice: "1.005",
			shouldMatch:   true,
			explanation:   "half cent drift within proportional threshold",
		},
		{
			name:          "$1/m plan — 2 cent drift within proportional threshold",
			paidAmount:    "1",
			expectedPrice: "0.98",
			shouldMatch:   true,
			explanation:   "2 cents on $1 plan: proportional threshold = 0.98×24/720 = $0.033, diff $0.02 within threshold",
		},
		{
			name:          "$1/m plan — large mismatch",
			paidAmount:    "1",
			expectedPrice: "0.50",
			shouldMatch:   false,
			explanation:   "50 cent difference is not timing drift",
		},

		// --- Low-price plan: $2/m ---
		{
			name:          "$2/m plan — prorated drift",
			paidAmount:    "2",
			expectedPrice: "1.9998450567502987",
			shouldMatch:   true,
			explanation:   "sub-cent drift on $2 plan",
		},

		// --- Mid-price plan: $300/m ---
		{
			name:          "$300/m plan — 3hr late webhook (~$1.25 drift)",
			paidAmount:    "150",
			expectedPrice: "148.75",
			shouldMatch:   true,
			explanation:   "3hr drift on $300/m: threshold = 300×24/720 = $10, diff = $1.25 within threshold",
		},
		{
			name:          "$300/m plan — 6hr late webhook (~$2.50 drift)",
			paidAmount:    "150",
			expectedPrice: "147.50",
			shouldMatch:   true,
			explanation:   "6hr drift on $300/m: threshold = $10, diff = $2.50 within threshold",
		},
		{
			name:          "$300/m plan — 30hr late webhook (~$12.50 drift) exceeds threshold",
			paidAmount:    "150",
			expectedPrice: "137.50",
			shouldMatch:   false,
			explanation:   "30hr drift on $300/m: threshold = $10, diff = $12.50 exceeds threshold",
		},
		{
			name:          "$300/m plan — completely wrong payment",
			paidAmount:    "50",
			expectedPrice: "150",
			shouldMatch:   false,
			explanation:   "obvious mismatch",
		},

		// --- High-price plan: $3000/m ---
		{
			name:          "$3000/m plan — 3hr late webhook (~$12.50 drift)",
			paidAmount:    "1500",
			expectedPrice: "1487.50",
			shouldMatch:   true,
			explanation:   "3hr drift on $3000/m: threshold = 3000×24/720 = $100, diff = $12.50 within threshold",
		},
		{
			name:          "$3000/m plan — proportional threshold based on expected, not full price",
			paidAmount:    "1500",
			expectedPrice: "1400",
			shouldMatch:   false,
			explanation:   "threshold = 1400×24/720 = $46.67, diff = $100 exceeds proportional threshold",
		},
		{
			name:          "$3000/m plan — 6hr late with proportional proration price",
			paidAmount:    "1500",
			expectedPrice: "1487.50",
			shouldMatch:   true,
			explanation:   "6hr drift: threshold = 1487.50×24/720 = $49.58, diff = $12.50 within threshold",
		},
		{
			name:          "$3000/m plan — exceeds threshold",
			paidAmount:    "1500",
			expectedPrice: "1300",
			shouldMatch:   false,
			explanation:   "diff = $200 exceeds $100 threshold",
		},

		// --- Near-zero / negative expected (downgrades) ---
		{
			name:          "negative expected — downgrade credit",
			paidAmount:    "0",
			expectedPrice: "-2.50",
			shouldMatch:   false,
			explanation:   "paid 0 for a negative expected (credit owed) — different transaction",
		},
		{
			name:          "tiny negative near zero",
			paidAmount:    "0",
			expectedPrice: "-0.005",
			shouldMatch:   true,
			explanation:   "tiny negative is within min threshold of zero",
		},
		{
			name:          "1 cent payment with sub-cent drift",
			paidAmount:    "0.01",
			expectedPrice: "0.009999",
			shouldMatch:   true,
			explanation:   "1 cent payment with sub-cent drift",
		},

		// --- Boundary: min threshold vs proportional ---
		{
			name:          "very small expected — min threshold applies",
			paidAmount:    "0.02",
			expectedPrice: "0.01",
			shouldMatch:   true,
			explanation:   "diff = $0.01, min threshold = $0.01 → at boundary",
		},
		{
			name:          "very small expected — exceeds min threshold",
			paidAmount:    "0.03",
			expectedPrice: "0.01",
			shouldMatch:   false,
			explanation:   "diff = $0.02 exceeds min threshold of $0.01",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			paid := decimal.RequireFromString(tc.paidAmount)
			expected := decimal.RequireFromString(tc.expectedPrice)
			diff := paid.Sub(expected).Abs()

			// Compute expected threshold for diagnostic output
			absExpected := expected.Abs()
			proportional := absExpected.Mul(decimal.NewFromInt(priceMatchMaxDriftHours)).Div(decimal.NewFromInt(720))
			threshold := proportional
			if threshold.LessThan(priceMatchMinThreshold) {
				threshold = priceMatchMinThreshold
			}

			result := priceMatchesExpected(paid, expected)
			if tc.shouldMatch {
				assert.True(t, result,
					fmt.Sprintf("expected match: paid=%s expected=%s diff=%s threshold=%s — %s",
						paid.String(), expected.String(), diff.StringFixed(6), threshold.StringFixed(6),
						tc.explanation),
				)
			} else {
				assert.False(t, result,
					fmt.Sprintf("expected no match: paid=%s expected=%s diff=%s threshold=%s — %s",
						paid.String(), expected.String(), diff.StringFixed(6), threshold.StringFixed(6),
						tc.explanation),
				)
			}
		})
	}
}
