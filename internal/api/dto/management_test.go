package dto

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
)

func TestPlanChangeResultResponse_FromModel(t *testing.T) {
	effectiveDate := time.Now().UTC()

	tests := []struct {
		name     string
		input    *pluginCore.PlanChangeResult
		expected PlanChangeResultResponse
	}{
		{
			name:  "nil input returns empty response",
			input: nil,
			expected: PlanChangeResultResponse{
				CreditApplied: decimal.Zero,
				ChargeDue:     decimal.Zero,
			},
		},
		{
			name: "basic plan change result without fragments",
			input: &pluginCore.PlanChangeResult{
				Action:        pluginCore.PlanChangeActionComplete,
				CheckoutLink:  "",
				CreditApplied: decimal.NewFromFloat(10.50),
				ChargeDue:     decimal.NewFromFloat(0),
				EffectiveDate: &effectiveDate,
				Fragments:     nil,
			},
			expected: PlanChangeResultResponse{
				Action:        "complete",
				CheckoutLink:  "",
				CreditApplied: decimal.NewFromFloat(10.50),
				ChargeDue:     decimal.NewFromFloat(0),
				EffectiveDate: &effectiveDate,
				Fragments:     nil,
			},
		},
		{
			name: "checkout required result with fragments",
			input: &pluginCore.PlanChangeResult{
				Action:        pluginCore.PlanChangeActionCheckoutRequired,
				CheckoutLink:  "checkout-session-123",
				CreditApplied: decimal.NewFromFloat(5.00),
				ChargeDue:     decimal.NewFromFloat(15.00),
				EffectiveDate: &effectiveDate,
				Fragments: []pluginCore.CheckoutUIFragment{
					{
						Type:   pluginCore.FragmentTypeScriptURL,
						Script: "https://widget.atlos.net/pay.js",
					},
					{
						Type: pluginCore.FragmentTypeHTML,
						HTML: "<button id='pay-btn'>Pay</button>",
					},
					{
						Type:     pluginCore.FragmentTypeScript,
						Script:   "console.log('init')",
						Metadata: map[string]any{"orderId": "order-123"},
					},
				},
			},
			expected: PlanChangeResultResponse{
				Action:        "checkout_required",
				CheckoutLink:  "checkout-session-123",
				CreditApplied: decimal.NewFromFloat(5.00),
				ChargeDue:     decimal.NewFromFloat(15.00),
				EffectiveDate: &effectiveDate,
				Fragments: []CheckoutUIFragmentResponse{
					{
						Type:   "script_url",
						Script: "https://widget.atlos.net/pay.js",
					},
					{
						Type: "html",
						HTML: "<button id='pay-btn'>Pay</button>",
					},
					{
						Type:     "script",
						Script:   "console.log('init')",
						Metadata: map[string]interface{}{"orderId": "order-123"},
					},
				},
			},
		},
		{
			name: "empty fragments slice",
			input: &pluginCore.PlanChangeResult{
				Action:        pluginCore.PlanChangeActionComplete,
				CreditApplied: decimal.NewFromFloat(0),
				ChargeDue:     decimal.NewFromFloat(0),
				Fragments:     []pluginCore.CheckoutUIFragment{},
			},
			expected: PlanChangeResultResponse{
				Action:        "complete",
				CreditApplied: decimal.NewFromFloat(0),
				ChargeDue:     decimal.NewFromFloat(0),
				Fragments:     []CheckoutUIFragmentResponse{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var response PlanChangeResultResponse
			err := response.FromModel(tt.input)

			assert.NoError(t, err)
			assert.Equal(t, tt.expected.Action, response.Action)
			assert.Equal(t, tt.expected.CheckoutLink, response.CheckoutLink)
			assert.True(t, tt.expected.CreditApplied.Equal(response.CreditApplied))
			assert.True(t, tt.expected.ChargeDue.Equal(response.ChargeDue))
			assert.Equal(t, tt.expected.EffectiveDate, response.EffectiveDate)
			assert.Equal(t, len(tt.expected.Fragments), len(response.Fragments))

			for i, expectedFrag := range tt.expected.Fragments {
				assert.Equal(t, expectedFrag.Type, response.Fragments[i].Type)
				assert.Equal(t, expectedFrag.HTML, response.Fragments[i].HTML)
				assert.Equal(t, expectedFrag.Script, response.Fragments[i].Script)
				assert.Equal(t, expectedFrag.Link, response.Fragments[i].Link)
				assert.Equal(t, expectedFrag.CSS, response.Fragments[i].CSS)
				assert.Equal(t, expectedFrag.Metadata, response.Fragments[i].Metadata)
			}
		})
	}
}
