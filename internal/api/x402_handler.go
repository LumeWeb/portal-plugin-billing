package api

import (
	echo "github.com/labstack/echo/v4"
	billingX402 "go.lumeweb.com/portal-plugin-billing/internal/x402"
)

// handleX402Checkout handles x402 credit purchase (challenge/response).
func (e *APIExtension) handleX402Checkout(c echo.Context) error {
	// Create nonce store from DB
	nonceStore := billingX402.NewDBNonceStore(e.db)

	// Create x402 handler with services
	handler := billingX402.NewHandler(e.billingService, e.creditService, nonceStore, nil) // TODO: inject user service

	// Delegate to handler
	return handler.HandleCheckout(c)
}
