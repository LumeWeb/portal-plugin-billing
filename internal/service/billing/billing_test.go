package billing

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	"go.lumeweb.com/portal-plugin-billing/internal/gateway"
	coreTesting "go.lumeweb.com/portal/core/testing"
)

func TestBillingService_GetSignatureHeader(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		// Setup
		registry := gateway.NewRegistry()
		mockGateway := new(pluginCore.MockPaymentGateway)
		mockGateway.On("ID").Return("stripe")
		mockGateway.On("SignatureHeader").Return("Stripe-Signature")

		err := registry.Register(mockGateway)
		assert.NoError(tb, err)

		svc, _, err := NewBillingServiceWithRegistry(registry)
		assert.NoError(tb, err)
		service := svc.(pluginCore.BillingService)

		// Test cases
		tests := []struct {
			name          string
			gatewayType   string
			expected      string
			expectedError error
		}{
			{
				name:        "valid gateway",
				gatewayType: "stripe",
				expected:    "Stripe-Signature",
			},
			{
				name:          "invalid gateway",
				gatewayType:   "invalid",
				expectedError: pluginCore.ErrGatewayNotFound,
			},
		}

		t := tb.(*testing.T)

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				header, err := service.GetSignatureHeader(tt.gatewayType)
				if tt.expectedError != nil {
					assert.ErrorIs(t, err, tt.expectedError)
					return
				}
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, header)
			})
		}

		mockGateway.AssertExpectations(tb)
	})
}

func TestBillingService_ProcessWebhook(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		// Setup fresh registry for each test

		// Setup mock gateway that will be used by multiple test cases
		var mockGateway *pluginCore.MockPaymentGateway

		t := tb.(*testing.T)

		var service pluginCore.BillingService

		// Test cases
		tests := []struct {
			name          string
			gatewayType   string
			signature     string
			payload       []byte
			expectedError error
			setup         func()
		}{
			{
				name:        "valid webhook",
				gatewayType: "test",
				signature:   "test_sig",
				payload:     []byte("test_payload"),
				setup: func() {
					mockGateway.On("ValidateWebhook", mock.Anything, "test_sig", []byte("test_payload")).
						Return(nil)
					mockGateway.On("HandleWebhook", mock.Anything, []byte("test_payload")).
						Return(nil)
				},
			},
			{
				name:          "invalid gateway",
				gatewayType:   "invalid",
				signature:     "test_sig",
				payload:       []byte("test_payload"),
				expectedError: pluginCore.ErrGatewayNotFound,
			},
			{
				name:          "validation failed",
				gatewayType:   "test",
				signature:     "invalid_sig",
				payload:       []byte("test_payload"),
				expectedError: errors.New("webhook validation failed"),
				setup: func() {
					mockGateway.On("ValidateWebhook", mock.Anything, "invalid_sig", []byte("test_payload")).
						Return(errors.New("invalid signature"))
				},
			},
			{
				name:          "handle failed",
				gatewayType:   "test",
				signature:     "test_sig",
				payload:       []byte("test_payload"),
				expectedError: errors.New("failed to handle webhook"),
				setup: func() {
					mockGateway.On("ValidateWebhook", mock.Anything, "test_sig", []byte("test_payload")).
						Return(nil)
					mockGateway.On("HandleWebhook", mock.Anything, []byte("test_payload")).
						Return(errors.New("processing error"))
				},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {

				mockGateway = pluginCore.NewMockPaymentGateway(t)
				mockGateway.On("ID").Return("test")

				registry := gateway.NewRegistry()
				err := registry.Register(mockGateway)
				svc, _, err := NewBillingServiceWithRegistry(registry)
				assert.NoError(tb, err)
				service = svc.(pluginCore.BillingService)

				if tt.setup != nil {
					tt.setup()
				}

				err = service.ProcessWebhook(context.Background(), tt.gatewayType, tt.signature, tt.payload)
				if tt.expectedError != nil {
					assert.ErrorContains(t, err, tt.expectedError.Error())
					return
				}
				assert.NoError(t, err)
			})
		}

		mockGateway.AssertExpectations(tb)
	})
}

func TestBillingService_ID(t *testing.T) {
	svc, _, err := NewBillingServiceWithRegistry(gateway.NewRegistry())
	assert.NoError(t, err)
	service := svc.(pluginCore.BillingService)
	assert.Equal(t, pluginCore.BILLING_SERVICE, service.ID())
}

func TestBillingService_GetSignatureHeader_UninitializedRegistry(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		// Create service with nil registry - this should return an error
		svc, _, err := NewBillingServiceWithRegistry(nil)
		assert.Error(t, err)
		assert.Nil(t, svc)
		assert.Contains(t, err.Error(), "gateway registry is nil")
	})
}
