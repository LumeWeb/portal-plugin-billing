package atlos

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// generateTestKey creates a new Ed25519 key pair for testing.
func generateTestKey(t *testing.T) ed25519.PrivateKey {
	_, pk, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	return pk
}

func TestDeriveHMACSecret(t *testing.T) {
	pk := generateTestKey(t)

	// Derive secret twice from same key — should be identical
	secret1, err := deriveHMACSecret(pk)
	require.NoError(t, err)
	require.Len(t, secret1, 32)

	secret2, err := deriveHMACSecret(pk)
	require.NoError(t, err)
	assert.Equal(t, secret1, secret2, "same key should derive same secret")

	// Different key should derive different secret
	_, pk2, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	secret3, err := deriveHMACSecret(pk2)
	require.NoError(t, err)
	assert.NotEqual(t, secret1, secret3, "different key should derive different secret")
}

func TestComputeOrderIDHMAC(t *testing.T) {
	pk := generateTestKey(t)
	secret, err := deriveHMACSecret(pk)
	require.NoError(t, err)

	// Same payload should produce same HMAC
	payload := "sub-123-456-1714612800"
	hmac1 := computeOrderIDHMAC(secret, payload)
	hmac2 := computeOrderIDHMAC(secret, payload)
	assert.Equal(t, hmac1, hmac2)
	assert.Len(t, hmac1, orderIDHMACLen, "HMAC should be truncated to %d hex chars", orderIDHMACLen)

	// Different payload should produce different HMAC (almost certainly)
	payload2 := "sub-123-456-1714612801"
	hmac3 := computeOrderIDHMAC(secret, payload2)
	assert.NotEqual(t, hmac1, hmac3)

	// Different secret should produce different HMAC
	_, pk2, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	secret2, err := deriveHMACSecret(pk2)
	require.NoError(t, err)
	hmac4 := computeOrderIDHMAC(secret2, payload)
	assert.NotEqual(t, hmac1, hmac4)
}

func TestGenerateOrderID(t *testing.T) {
	pk := generateTestKey(t)
	secret, err := deriveHMACSecret(pk)
	require.NoError(t, err)

	userID := uint(123)
	periodID := uint(456)

	orderID := generateOrderID(secret, userID, periodID)
	require.NotEmpty(t, orderID)

	// Check format: sub-{userID}-{periodID}-{timestamp}-{hmac}
	parts := strings.Split(orderID, "-")
	require.GreaterOrEqual(t, len(parts), 5, "order ID should have at least 5 parts")

	assert.Equal(t, OrderIDPrefixRegular, parts[0])
	assert.Equal(t, "123", parts[1])
	assert.Equal(t, "456", parts[2])

	// Timestamp should be recent
	ts, err := strconv.ParseInt(parts[len(parts)-2], 10, 64)
	require.NoError(t, err)
	age := time.Now().Unix() - ts
	assert.Less(t, age, int64(10), "timestamp should be within 10 seconds")

	// HMAC should be present
	hmac := parts[len(parts)-1]
	assert.Len(t, hmac, orderIDHMACLen)
}

func TestGenerateProratedOrderID(t *testing.T) {
	pk := generateTestKey(t)
	secret, err := deriveHMACSecret(pk)
	require.NoError(t, err)

	userID := uint(123)
	oldPeriodID := uint(789)
	newPeriodID := uint(456)

	orderID := generateProratedOrderID(secret, userID, oldPeriodID, newPeriodID)
	require.NotEmpty(t, orderID)

	// Check format: sub-{userID}-{oldPeriodID}-{newPeriodID}-prorated-{timestamp}-{hmac}
	parts := strings.Split(orderID, "-")
	require.GreaterOrEqual(t, len(parts), 7, "prorated order ID should have at least 7 parts")

	assert.Equal(t, OrderIDPrefixRegular, parts[0])
	assert.Equal(t, "123", parts[1])
	assert.Equal(t, "789", parts[2])
	assert.Equal(t, "456", parts[3])

	// Check for prorated marker
	foundProrated := false
	for _, p := range parts {
		if p == OrderIDSuffixProrated {
			foundProrated = true
			break
		}
	}
	assert.True(t, foundProrated, "prorated order ID should contain 'prorated' marker")
}

func TestParseOrderID_Regular(t *testing.T) {
	pk := generateTestKey(t)
	secret, err := deriveHMACSecret(pk)
	require.NoError(t, err)

	userID := uint(123)
	periodID := uint(456)

	orderID := generateOrderID(secret, userID, periodID)

	parsed, err := parseOrderID(secret, orderID)
	require.NoError(t, err)
	require.NotNil(t, parsed)

	assert.Equal(t, userID, parsed.UserID)
	assert.Equal(t, periodID, parsed.NewPeriodID)
	assert.False(t, parsed.IsProrated)
	assert.NotZero(t, parsed.Timestamp)
}

func TestParseOrderID_Prorated(t *testing.T) {
	pk := generateTestKey(t)
	secret, err := deriveHMACSecret(pk)
	require.NoError(t, err)

	userID := uint(123)
	oldPeriodID := uint(789)
	newPeriodID := uint(456)

	orderID := generateProratedOrderID(secret, userID, oldPeriodID, newPeriodID)

	parsed, err := parseOrderID(secret, orderID)
	require.NoError(t, err)
	require.NotNil(t, parsed)

	assert.Equal(t, userID, parsed.UserID)
	assert.Equal(t, oldPeriodID, parsed.OldPeriodID)
	assert.Equal(t, newPeriodID, parsed.NewPeriodID)
	assert.True(t, parsed.IsProrated)
	assert.NotZero(t, parsed.Timestamp)
}

func TestParseOrderID_InvalidHMAC(t *testing.T) {
	pk := generateTestKey(t)
	secret, err := deriveHMACSecret(pk)
	require.NoError(t, err)

	orderID := generateOrderID(secret, 123, 456)
	
	// Tamper with the order ID (flip last character of HMAC)
	tampered := orderID[:len(orderID)-1] + "x"

	_, err = parseOrderID(secret, tampered)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "HMAC verification failed")
}

func TestParseOrderID_WrongSecret(t *testing.T) {
	pk1 := generateTestKey(t)
	secret1, err := deriveHMACSecret(pk1)
	require.NoError(t, err)

	pk2 := generateTestKey(t)
	secret2, err := deriveHMACSecret(pk2)
	require.NoError(t, err)

	orderID := generateOrderID(secret1, 123, 456)

	// Parse with different secret should fail
	_, err = parseOrderID(secret2, orderID)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "HMAC verification failed")
}

func TestParseOrderID_InvalidFormat(t *testing.T) {
	pk := generateTestKey(t)
	secret, err := deriveHMACSecret(pk)
	require.NoError(t, err)

	// For malformed order IDs without valid HMACs, HMAC verification fails first
	tests := []struct {
		name    string
		orderID string
		wantErr string
	}{
		{
			name:    "empty string",
			orderID: "",
			wantErr: "HMAC verification failed",
		},
		{
			name:    "invalid HMAC",
			orderID: "sub-123-456-1714612800-invalidhmac",
			wantErr: "HMAC verification failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseOrderID(secret, tt.orderID)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestParseOrderID_InvalidStructure_WithValidHMAC(t *testing.T) {
	pk := generateTestKey(t)
	secret, err := deriveHMACSecret(pk)
	require.NoError(t, err)

	now := time.Now().Unix()

	tests := []struct {
		name    string
		makeID  func() string
		wantErr string
	}{
		{
			name: "invalid prefix",
			makeID: func() string {
				payload := fmt.Sprintf("invalid-123-456-%d", now)
				mac := computeOrderIDHMAC(secret, payload)
				return payload + "-" + mac
			},
			wantErr: "invalid order ID prefix",
		},
		{
			name: "non-numeric user ID",
			makeID: func() string {
				payload := fmt.Sprintf("sub-abc-456-%d", now)
				mac := computeOrderIDHMAC(secret, payload)
				return payload + "-" + mac
			},
			wantErr: "invalid user ID",
		},
		{
			name: "non-numeric period ID",
			makeID: func() string {
				payload := fmt.Sprintf("sub-123-abc-%d", now)
				mac := computeOrderIDHMAC(secret, payload)
				return payload + "-" + mac
			},
			wantErr: "invalid period ID",
		},
		{
			name: "too few parts for prorated",
			makeID: func() string {
				payload := fmt.Sprintf("sub-123-456-prorated-incomplete")
				mac := computeOrderIDHMAC(secret, payload)
				return payload + "-" + mac
			},
			wantErr: "invalid prorated order ID format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orderID := tt.makeID()
			_, err := parseOrderID(secret, orderID)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestValidateTimestamp(t *testing.T) {
	now := time.Now().Unix()

	tests := []struct {
		name      string
		timestamp int64
		wantErr   bool
		errMsg    string
	}{
		{
			name:      "valid recent timestamp",
			timestamp: now,
			wantErr:   false,
		},
		{
			name:      "valid timestamp 30 min ago",
			timestamp: now - 1800,
			wantErr:   false,
		},
		{
			name:      "expired timestamp",
			timestamp: now - orderIDMaxAge - 1,
			wantErr:   true,
			errMsg:    "order ID expired",
		},
		{
			name:      "future timestamp",
			timestamp: now + 100,
			wantErr:   true,
			errMsg:    "timestamp is in the future",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateTimestamp(tt.timestamp)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestParseOrderID_ExpiredTimestamp(t *testing.T) {
	pk := generateTestKey(t)
	secret, err := deriveHMACSecret(pk)
	require.NoError(t, err)

	// Generate order ID with old timestamp
	oldTS := time.Now().Unix() - orderIDMaxAge - 100
	payload := fmt.Sprintf("%s-%d-%d-%d", OrderIDPrefixRegular, 123, 456, oldTS)
	mac := computeOrderIDHMAC(secret, payload)
	orderID := payload + "-" + mac

	_, err = parseOrderID(secret, orderID)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "order ID expired")
}

func TestParseOrderID_FutureTimestamp(t *testing.T) {
	pk := generateTestKey(t)
	secret, err := deriveHMACSecret(pk)
	require.NoError(t, err)

	// Generate order ID with future timestamp
	futureTS := time.Now().Unix() + 1000
	payload := fmt.Sprintf("%s-%d-%d-%d", OrderIDPrefixRegular, 123, 456, futureTS)
	mac := computeOrderIDHMAC(secret, payload)
	orderID := payload + "-" + mac

	_, err = parseOrderID(secret, orderID)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "timestamp is in the future")
}

func TestGenerateAndParseRoundTrip(t *testing.T) {
	pk := generateTestKey(t)
	secret, err := deriveHMACSecret(pk)
	require.NoError(t, err)

	// Test regular order ID round trip
	t.Run("regular", func(t *testing.T) {
		orderID := generateOrderID(secret, 123, 456)
		parsed, err := parseOrderID(secret, orderID)
		require.NoError(t, err)
		assert.Equal(t, uint(123), parsed.UserID)
		assert.Equal(t, uint(456), parsed.NewPeriodID)
		assert.False(t, parsed.IsProrated)
	})

	// Test prorated order ID round trip
	t.Run("prorated", func(t *testing.T) {
		orderID := generateProratedOrderID(secret, 123, 789, 456)
		parsed, err := parseOrderID(secret, orderID)
		require.NoError(t, err)
		assert.Equal(t, uint(123), parsed.UserID)
		assert.Equal(t, uint(789), parsed.OldPeriodID)
		assert.Equal(t, uint(456), parsed.NewPeriodID)
		assert.True(t, parsed.IsProrated)
	})
}
