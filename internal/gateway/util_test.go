package gateway

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.lumeweb.com/portal/core/testing/mocks"
)

func TestBuildAbsoluteURL_WithHTTPService(t *testing.T) {
	mockHTTP := mocks.NewMockHTTPService(t)
	mockHTTP.EXPECT().APISubdomain("account", false).Return("account.example.com")

	result := BuildAbsoluteURL(mockHTTP, AccountSubdomain, "/billing/checkout/success")
	assert.Equal(t, "https://account.example.com/billing/checkout/success", result)
}

func TestBuildAbsoluteURL_NilHTTPService(t *testing.T) {
	result := BuildAbsoluteURL(nil, AccountSubdomain, "/billing/checkout/success")
	assert.Equal(t, "/billing/checkout/success", result)
}

func TestBuildAbsoluteURL_CancelURL(t *testing.T) {
	mockHTTP := mocks.NewMockHTTPService(t)
	mockHTTP.EXPECT().APISubdomain("account", false).Return("account.example.com")

	result := BuildAbsoluteURL(mockHTTP, AccountSubdomain, "/billing/checkout/cancel")
	assert.Equal(t, "https://account.example.com/billing/checkout/cancel", result)
}

func TestBuildAbsoluteURL_PostbackURL(t *testing.T) {
	mockHTTP := mocks.NewMockHTTPService(t)
	mockHTTP.EXPECT().APISubdomain("account", false).Return("account.example.com")

	result := BuildAbsoluteURL(mockHTTP, AccountSubdomain, "/api/billing/webhook/atlos")
	assert.Equal(t, "https://account.example.com/api/billing/webhook/atlos", result)
}
