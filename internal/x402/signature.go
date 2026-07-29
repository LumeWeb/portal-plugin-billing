package x402

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/hyperledger-firefly/signer/pkg/secp256k1"
	"golang.org/x/crypto/sha3"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
)

// EIP-712 type hashes for TransferWithAuthorization (EIP-3009).
// keccak256("TransferWithAuthorization(address from,address to,uint256 value,uint256 validAfter,uint256 validBefore,bytes32 nonce)")
var transferWithAuthorizationTypeHash = keccak256([]byte("TransferWithAuthorization(address from,address to,uint256 value,uint256 validAfter,uint256 validBefore,bytes32 nonce)"))

// EIP-712 domain separator type hash.
var eip712DomainTypeHash = keccak256([]byte("EIP712Domain(string name,string version,uint256 chainId,address verifyingContract)"))

// verifyPaymentSignature verifies an EIP-712 TransferWithAuthorization signature
// per the x402 v2 spec. Recovers the signer address from the signature and checks
// it matches the authorization.from field.
//
// Parameters:
//   - auth:     the EIP-3009 authorization from the payment payload
//   - sigHex:   the EIP-712 signature (65-byte RSV, hex-encoded with 0x prefix)
//   - asset:   the ERC-20 contract address (verifying contract in the EIP-712 domain)
//   - chainID:  the EVM chain ID (from the payment requirements network field, e.g. "eip155:84532")
//   - name:    token name from extra (e.g. "USDC")
//   - version: token version from extra (e.g. "2")
//
// Returns the recovered signer address (lowercase 0x-prefixed) if valid.
func verifyPaymentSignature(auth pluginCore.X402Authorization, sigHex string, asset string, chainID int64, tokenName, tokenVersion string) (string, error) {
	if auth.From == "" {
		return "", fmt.Errorf("missing 'from' in authorization")
	}
	if auth.Nonce == "" {
		return "", fmt.Errorf("missing 'nonce' in authorization")
	}
	if sigHex == "" {
		return "", fmt.Errorf("missing payment signature")
	}

	sigBytes, err := decodeHex(sigHex)
	if err != nil {
		return "", fmt.Errorf("invalid signature encoding: %w", err)
	}
	if len(sigBytes) != 65 {
		return "", fmt.Errorf("invalid signature length: expected 65 bytes, got %d", len(sigBytes))
	}

	// Parse the nonce (bytes32, hex-encoded).
	nonceBytes, err := parseNonce(auth.Nonce)
	if err != nil {
		return "", fmt.Errorf("invalid authorization nonce: %w", err)
	}

	// Parse value (uint256, decimal string).
	value, ok := new(big.Int).SetString(auth.Value, 10)
	if !ok {
		return "", fmt.Errorf("invalid authorization value: %q", auth.Value)
	}

	// Parse validAfter and validBefore (uint256, decimal string).
	validAfter, ok := new(big.Int).SetString(auth.ValidAfter, 10)
	if !ok {
		return "", fmt.Errorf("invalid validAfter: %q", auth.ValidAfter)
	}
	validBefore, ok := new(big.Int).SetString(auth.ValidBefore, 10)
	if !ok {
		return "", fmt.Errorf("invalid validBefore: %q", auth.ValidBefore)
	}

	// Enforce the EIP-3009 time window: reject if the current time is
	// before validAfter or after validBefore.
	now := big.NewInt(time.Now().Unix())
	if now.Cmp(validAfter) < 0 {
		return "", fmt.Errorf("authorization is not yet valid")
	}
	if now.Cmp(validBefore) > 0 {
		return "", fmt.Errorf("authorization has expired")
	}

	// Compute the EIP-712 domain separator.
	domainSeparator, err := computeDomainSeparator(asset, chainID, tokenName, tokenVersion)
	if err != nil {
		return "", fmt.Errorf("failed to compute domain separator: %w", err)
	}

	// Compute the TransferWithAuthorization struct hash.
	structHash, err := computeTransferAuthStructHash(auth.From, auth.To, value, validAfter, validBefore, nonceBytes)
	if err != nil {
		return "", fmt.Errorf("failed to compute struct hash: %w", err)
	}

	// Compute the EIP-712 final hash: keccak256(0x1901 || domainSeparator || structHash)
	finalHash := make([]byte, 66)
	finalHash[0] = 0x19
	finalHash[1] = 0x01
	copy(finalHash[2:34], domainSeparator)
	copy(finalHash[34:66], structHash)
	typedDataHash := keccak256(finalHash)

	// Recover the signer address using firefly-signer (same library as dashboard CAIP-122).
	// RecoverDirect reads sigData.V for the recovery ID; chainID is used for
	// EIP-155 V normalization (V = 2*chainID + 35 + yParity).
	sigData, err := secp256k1.DecodeCompactRSV(context.Background(), sigBytes)
	if err != nil {
		return "", fmt.Errorf("failed to decode signature: %w", err)
	}

	recoveredAddr, err := sigData.RecoverDirect(typedDataHash, chainID)
	if err != nil {
		return "", fmt.Errorf("failed to recover public key from signature: %w", err)
	}

	recovered := strings.ToLower(recoveredAddr.String())
	expected := strings.ToLower(auth.From)
	if recovered != expected {
		return recovered, fmt.Errorf("signature wallet %s does not match authorization.from %s", recovered, expected)
	}

	return recovered, nil
}

// computeDomainSeparator computes the EIP-712 domain separator hash for
// TransferWithAuthorization. The domain includes name, version, chainId, and
// the verifying contract (the ERC-20 token address).
func computeDomainSeparator(verifyingContract string, chainID int64, name, version string) ([]byte, error) {
	// keccak256(name || version || chainId || verifyingContract)
	// Each field is padded to 32 bytes and ABI-encoded.
	nameHash := keccak256([]byte(name))
	versionHash := keccak256([]byte(version))

	chainIDBytes, err := bigTo32Bytes(big.NewInt(chainID))
	if err != nil {
		return nil, fmt.Errorf("invalid chain ID: %w", err)
	}
	contractBytes, err := addressTo32Bytes(verifyingContract)
	if err != nil {
		return nil, fmt.Errorf("invalid verifying contract address: %w", err)
	}

	data := make([]byte, 0, 32*4)
	data = append(data, eip712DomainTypeHash...)
	data = append(data, nameHash...)
	data = append(data, versionHash...)
	data = append(data, chainIDBytes...)
	data = append(data, contractBytes...)

	return keccak256(data), nil
}

// computeTransferAuthStructHash computes the EIP-712 struct hash for
// TransferWithAuthorization.
func computeTransferAuthStructHash(from, to string, value, validAfter, validBefore *big.Int, nonce []byte) ([]byte, error) {
	fromBytes, err := addressTo32Bytes(from)
	if err != nil {
		return nil, fmt.Errorf("invalid from address: %w", err)
	}
	toBytes, err := addressTo32Bytes(to)
	if err != nil {
		return nil, fmt.Errorf("invalid to address: %w", err)
	}

	data := make([]byte, 0, 32*7)
	data = append(data, transferWithAuthorizationTypeHash...)
	data = append(data, fromBytes...)
	data = append(data, toBytes...)

	valueBytes, err := bigTo32Bytes(value)
	if err != nil {
		return nil, fmt.Errorf("invalid value: %w", err)
	}
	data = append(data, valueBytes...)

	validAfterBytes, err := bigTo32Bytes(validAfter)
	if err != nil {
		return nil, fmt.Errorf("invalid validAfter: %w", err)
	}
	data = append(data, validAfterBytes...)

	validBeforeBytes, err := bigTo32Bytes(validBefore)
	if err != nil {
		return nil, fmt.Errorf("invalid validBefore: %w", err)
	}
	data = append(data, validBeforeBytes...)

	data = append(data, nonce...)

	return keccak256(data), nil
}

// keccak256 computes the Keccak-256 hash of the given data.
func keccak256(data ...[]byte) []byte {
	hasher := sha3.NewLegacyKeccak256()
	for _, d := range data {
		hasher.Write(d)
	}
	return hasher.Sum(nil)
}

// bigTo32Bytes left-pads a big.Int to 32 bytes. Returns an error if the
// value exceeds 32 bytes, which would indicate an invalid uint256.
func bigTo32Bytes(v *big.Int) ([]byte, error) {
	b := v.Bytes()
	if len(b) > 32 {
		return nil, fmt.Errorf("uint256 value exceeds 32 bytes: %d", len(b))
	}
	result := make([]byte, 32)
	copy(result[32-len(b):], b)
	return result, nil
}

// addressTo32Bytes converts a hex address string to a left-padded 32-byte
// array. Returns an error on invalid hex — callers must propagate it so that
// signature recovery fails on malformed addresses instead of silently using
// a zero address, which would cause recovery in the wrong EIP-712 domain.
func addressTo32Bytes(addr string) ([]byte, error) {
	clean := strings.TrimPrefix(strings.TrimPrefix(addr, "0x"), "0X")
	b, err := hex.DecodeString(clean)
	if err != nil {
		return nil, fmt.Errorf("invalid hex address %q: %w", addr, err)
	}
	if len(b) > 32 {
		return nil, fmt.Errorf("address %q exceeds 32 bytes", addr)
	}
	result := make([]byte, 32)
	copy(result[32-len(b):], b)
	return result, nil
}

// parseNonce parses a 32-byte nonce from a hex string (0x-prefixed or plain).
// Returns left-padded 32 bytes.
func parseNonce(nonce string) ([]byte, error) {
	nonce = strings.TrimSpace(nonce)
	if nonce == "" {
		return nil, fmt.Errorf("empty nonce")
	}

	// Strip 0x prefix if present.
	s := strings.TrimPrefix(strings.TrimPrefix(nonce, "0x"), "0X")
	b, err := hex.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("invalid hex nonce: %w", err)
	}
	if len(b) > 32 {
		return nil, fmt.Errorf("nonce exceeds 32 bytes: got %d", len(b))
	}
	result := make([]byte, 32)
	copy(result[32-len(b):], b)
	return result, nil
}

// decodeHex decodes a hex string, optionally stripping a 0x prefix.
func decodeHex(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "0x")
	s = strings.TrimPrefix(s, "0X")
	return hex.DecodeString(s)
}

// isValidEVMAddressFormat checks that a string is a valid EVM address (0x + 40 hex chars).
func isValidEVMAddressFormat(addr string) bool {
	if len(addr) != 42 || !strings.HasPrefix(addr, "0x") {
		return false
	}
	_, err := hex.DecodeString(addr[2:])
	return err == nil
}
