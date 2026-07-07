package atlos

import (
	"net/http/httptest"
	"testing"

	"go.lumeweb.com/atlos-sdk"
)

// newMockAtlosServer starts an httptest.Server backed by the ATLOS SDK's mock
// server. The returned server responds 200 OK to /Subscription/Cancel and other
// ATLOS API routes, so tests that exercise cancelSubscription and similar
// calls don't hit the real api.atlos.io endpoint.
//
// The cleanup is registered via t.Cleanup so callers don't need to manage it.
func newMockAtlosServer(t *testing.T) string {
	t.Helper()

	mockServer, err := atlos.NewServer(atlos.WithSharedSecret(TestAPISecret))
	if err != nil {
		t.Fatalf("failed to create ATLOS mock server: %v", err)
	}

	ts := httptest.NewServer(mockServer.Handler())
	t.Cleanup(ts.Close)

	return ts.URL
}
