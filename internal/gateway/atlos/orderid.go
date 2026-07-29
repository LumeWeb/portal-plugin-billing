package atlos

import (
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/hkdf"
)

const (
	// orderIDHMACLen is the number of hex characters (64 bits) retained from the
	// HMAC-SHA256 digest when signing an order ID.  16 hex = 64 bits provides
	// ~2^64 brute-force resistance — sufficient for an identifier that is also
	// covered by the ATLOS webhook HMAC.
	orderIDHMACLen = 16

	// orderIDMaxAge controls how old an order ID timestamp can be (in seconds)
	// before it is rejected by the webhook handler as potentially replayed.
	orderIDMaxAge = 3600 // 1 hour

	// hkdfInfo is the purpose-specific info string used when deriving the HMAC
	// key from the Ed25519 private key seed via HKDF-SHA256.
	hkdfInfo = "lumeweb-portal-atlos-orderid-hmac-v1"
)

// deriveHMACSecret derives a 32-byte HMAC-SHA256 key from the portal's
// Ed25519 identity private key using HKDF-SHA256 with a purpose-specific info
// string. This ensures the key is deterministic (same portal → same key) but
// purpose-bound (cannot be used for anything other than order ID signing).
func deriveHMACSecret(pk ed25519.PrivateKey) ([]byte, error) {
	// Ed25519 private key is 64 bytes: seed (32) || public key (32).
	// The seed is the cryptographically independent part.
	seed := pk.Seed()

	reader := hkdf.New(sha256.New, seed, nil, []byte(hkdfInfo))
	secret := make([]byte, 32)
	if _, err := reader.Read(secret); err != nil {
		return nil, fmt.Errorf("failed to derive HMAC secret from identity key: %w", err)
	}
	return secret, nil
}

// computeOrderIDHMAC returns the truncated HMAC-SHA256 hex string for the
// given payload using the supplied secret.
func computeOrderIDHMAC(secret []byte, payload string) string {
	h := hmac.New(sha256.New, secret)
	h.Write([]byte(payload))
	return hex.EncodeToString(h.Sum(nil))[:orderIDHMACLen]
}

// generateOrderID creates a signed order ID for ATLOS checkout.
//
// Format: sub-{userID}-{newPeriodID}-{timestamp}-{hmac}
// Example: sub-123-456-1714612800-a1b2c3d4e5f6a7b8
func generateOrderID(secret []byte, userID uint, newPeriodID uint) string {
	ts := time.Now().Unix()
	payload := fmt.Sprintf("%s-%d-%d-%d", OrderIDPrefixRegular, userID, newPeriodID, ts)
	mac := computeOrderIDHMAC(secret, payload)
	return payload + "-" + mac
}

// generateProratedOrderID creates a signed order ID for prorated plan changes.
//
// Format: sub-{userID}-{oldPeriodID}-{newPeriodID}-prorated-{timestamp}-{hmac}
// Example: sub-123-789-456-prorated-1714612800-a1b2c3d4e5f6a7b8
//
// Both the old and new period IDs are embedded and HMAC-verified so the webhook
// can recalculate the expected prorated amount server-side without trusting
// client-submitted values.
func generateProratedOrderID(secret []byte, userID uint, oldPeriodID uint, newPeriodID uint) string {
	ts := time.Now().Unix()
	payload := fmt.Sprintf("%s-%d-%d-%d-%s-%d", OrderIDPrefixRegular, userID, oldPeriodID, newPeriodID, OrderIDSuffixProrated, ts)
	mac := computeOrderIDHMAC(secret, payload)
	return payload + "-" + mac
}

// ParsedOrderID holds the fields extracted from a verified order ID.
type ParsedOrderID struct {
	UserID      uint
	OldPeriodID uint // Only valid when IsProrated=true
	NewPeriodID uint
	IsProrated  bool
	Timestamp   int64
}

// parseOrderID verifies the HMAC in an order ID and extracts its fields.
// Returns an error if:
//   - the format is unrecognisable
//   - the HMAC is invalid (tampered or forged)
//   - the timestamp is older than orderIDMaxAge seconds
//
// Supported formats:
//   - Regular:  sub-{userID}-{newPeriodID}-{timestamp}-{hmac}
//   - Prorated: sub-{userID}-{oldPeriodID}-{newPeriodID}-prorated-{timestamp}-{hmac}
func parseOrderID(secret []byte, orderID string) (*ParsedOrderID, error) {
	parts := strings.Split(orderID, "-")

	// Last element is the HMAC; everything before it is the payload.
	providedMAC := parts[len(parts)-1]
	payload := strings.Join(parts[:len(parts)-1], "-")

	// Verify HMAC first — if it fails, nothing else matters.
	expectedMAC := computeOrderIDHMAC(secret, payload)
	if !hmac.Equal([]byte(providedMAC), []byte(expectedMAC)) {
		return nil, fmt.Errorf("order ID HMAC verification failed (tampered or forged)")
	}

	if parts[0] != OrderIDPrefixRegular {
		return nil, fmt.Errorf("invalid order ID prefix: expected '%s', got: %s", OrderIDPrefixRegular, parts[0])
	}

	userID, err := strconv.ParseUint(parts[1], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid user ID in order ID: %w", err)
	}

	// Determine format by checking for "prorated" marker
	isProrated := false
	for _, p := range parts {
		if p == OrderIDSuffixProrated {
			isProrated = true
			break
		}
	}

	if isProrated {
		// Format: sub-{userID}-{oldPeriodID}-{newPeriodID}-prorated-{timestamp}-{hmac}
		// Minimum 7 parts: sub-X-Y-Z-prorated-TS-MAC
		if len(parts) < 7 {
			return nil, fmt.Errorf("invalid prorated order ID format: expected 'sub-{userID}-{oldPeriodID}-{newPeriodID}-prorated-{timestamp}-{hmac}', got: %s", orderID)
		}

		oldPeriodID, err := strconv.ParseUint(parts[2], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid old period ID in order ID: %w", err)
		}

		newPeriodID, err := strconv.ParseUint(parts[3], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid new period ID in order ID: %w", err)
		}

		// Timestamp is between "prorated" and the HMAC
		timestampStr := parts[len(parts)-2]
		timestamp, err := strconv.ParseInt(timestampStr, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid timestamp in order ID: %w", err)
		}

		if err := validateTimestamp(timestamp); err != nil {
			return nil, err
		}

		return &ParsedOrderID{
			UserID:      uint(userID),
			OldPeriodID: uint(oldPeriodID),
			NewPeriodID: uint(newPeriodID),
			IsProrated:  true,
			Timestamp:   timestamp,
		}, nil
	}

	// Regular format: sub-{userID}-{newPeriodID}-{timestamp}-{hmac}
	// Minimum 5 parts: sub-X-Y-TS-MAC
	if len(parts) < 5 {
		return nil, fmt.Errorf("invalid order ID format: expected 'sub-{userID}-{newPeriodID}-{timestamp}-{hmac}', got: %s", orderID)
	}

	newPeriodID, err := strconv.ParseUint(parts[2], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid period ID in order ID: %w", err)
	}

	timestampStr := parts[len(parts)-2]
	timestamp, err := strconv.ParseInt(timestampStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid timestamp in order ID: %w", err)
	}

	if err := validateTimestamp(timestamp); err != nil {
		return nil, err
	}

	return &ParsedOrderID{
		UserID:      uint(userID),
		NewPeriodID: uint(newPeriodID),
		IsProrated:  false,
		Timestamp:   timestamp,
	}, nil
}

func validateTimestamp(timestamp int64) error {
	age := time.Now().Unix() - timestamp
	if age > orderIDMaxAge {
		return fmt.Errorf("order ID expired (age %d seconds, max %d)", age, orderIDMaxAge)
	}
	if age < 0 {
		return fmt.Errorf("order ID timestamp is in the future")
	}
	return nil
}

// orderIDSecret retrieves the HMAC secret derived from the portal identity key.
func (g *AtlosGateway) orderIDSecret() ([]byte, error) {
	pk := g.coreCtx.Config().Config().Core.Identity.PrivateKey()
	if pk == nil {
		return nil, fmt.Errorf("portal identity private key is not configured")
	}
	return deriveHMACSecret(pk)
}
