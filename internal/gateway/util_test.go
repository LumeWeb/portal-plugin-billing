package gateway

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.lumeweb.com/portal/core/testing/mocks"
)

func TestBuildAbsoluteURL_WithHTTPService_Secure(t *testing.T) {
	mockHTTP := mocks.NewMockHTTPService(t)
	mockHTTP.EXPECT().APISubdomain("dashboard", false).Return("account.example.com")

	result := BuildAbsoluteURL(mockHTTP, DashboardPluginID, "/billing/checkout/success", true)
	assert.Equal(t, "https://account.example.com/billing/checkout/success", result)
}

func TestBuildAbsoluteURL_WithHTTPService_Insecure(t *testing.T) {
	mockHTTP := mocks.NewMockHTTPService(t)
	mockHTTP.EXPECT().APISubdomain("dashboard", false).Return("account.example.com")

	result := BuildAbsoluteURL(mockHTTP, DashboardPluginID, "/billing/checkout/success", false)
	assert.Equal(t, "http://account.example.com/billing/checkout/success", result)
}

func TestBuildAbsoluteURL_NilHTTPService(t *testing.T) {
	result := BuildAbsoluteURL(nil, DashboardPluginID, "/billing/checkout/success", true)
	assert.Equal(t, "/billing/checkout/success", result)
}

func TestBuildAbsoluteURL_CancelURL_Secure(t *testing.T) {
	mockHTTP := mocks.NewMockHTTPService(t)
	mockHTTP.EXPECT().APISubdomain("dashboard", false).Return("account.example.com")

	result := BuildAbsoluteURL(mockHTTP, DashboardPluginID, "/billing/checkout/cancel", true)
	assert.Equal(t, "https://account.example.com/billing/checkout/cancel", result)
}

func TestBuildAbsoluteURL_PostbackURL_Secure(t *testing.T) {
	mockHTTP := mocks.NewMockHTTPService(t)
	mockHTTP.EXPECT().APISubdomain("dashboard", false).Return("account.example.com")

	result := BuildAbsoluteURL(mockHTTP, DashboardPluginID, "/api/billing/webhook/atlos", true)
	assert.Equal(t, "https://account.example.com/api/billing/webhook/atlos", result)
}
