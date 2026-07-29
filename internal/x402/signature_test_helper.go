package x402

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/hyperledger-firefly/signer/pkg/secp256k1"
	"golang.org/x/crypto/sha3"
)

// testWallet generates a secp256k1 keypair for testing and returns the
// keypair and the corresponding Ethereum address (lowercase 0x-prefixed).
func testWallet(t *testing.T) (*secp256k1.KeyPair, string) {
	t.Helper()
	kp, err := secp256k1.GenerateSecp256k1KeyPair()
	if err != nil {
		t.Fatalf("failed to generate keypair: %v", err)
	}
	return kp, kp.Address.String()
}

// signTransferWithAuthorizationForTest produces an EIP-712 signature over a
// TransferWithAuthorization struct, matching the x402 v2 spec. This is what a
// standard x402 client library would produce.
//
// The signed data follows EIP-712 with:
//
//	domain:   { name, version, chainId, verifyingContract }
//	types:    TransferWithAuthorization(address from, address to, uint256 value, uint256 validAfter, uint256 validBefore, bytes32 nonce)
func signTransferWithAuthorizationForTest(
	t *testing.T,
	kp *secp256k1.KeyPair,
	from, to string,
	value int64,
	validAfter, validBefore int64,
	nonceHex string,
	verifyingContract string,
	chainID int64,
	tokenName, tokenVersion string,
) string {
	t.Helper()

	// Compute domain separator.
	domainSeparator, err := computeDomainSeparator(verifyingContract, chainID, tokenName, tokenVersion)
	if err != nil {
		t.Fatalf("failed to compute domain separator: %v", err)
	}

	// Parse nonce to 32 bytes.
	nonceBytes := parseNonceForTest(t, nonceHex)

	// Compute struct hash.
	structHash, err := computeTransferAuthStructHash(
		from, to,
		big.NewInt(value),
		big.NewInt(validAfter),
		big.NewInt(validBefore),
		nonceBytes,
	)
	if err != nil {
		t.Fatalf("failed to compute struct hash: %v", err)
	}

	// EIP-712 final hash: keccak256(0x1901 || domainSeparator || structHash)
	finalHash := make([]byte, 66)
	finalHash[0] = 0x19
	finalHash[1] = 0x01
	copy(finalHash[2:34], domainSeparator)
	copy(finalHash[34:66], structHash)
	typedDataHash := keccak256(finalHash)

	// Sign.
	sigData, err := kp.SignDirect(typedDataHash)
	if err != nil {
		t.Fatalf("failed to sign: %v", err)
	}

	rsv := make([]byte, 65)
	sigData.R.FillBytes(rsv[0:32])
	sigData.S.FillBytes(rsv[32:64])
	rsv[64] = byte(sigData.V.Int64())

	return "0x" + fmt.Sprintf("%x", rsv)
}

// parseNonceForTest parses a hex nonce string to 32 bytes for test use.
func parseNonceForTest(t *testing.T, nonceHex string) []byte {
	t.Helper()
	b, err := decodeHex(nonceHex)
	if err != nil {
		// Fall back to keccak256 hash.
		hasher := sha3.NewLegacyKeccak256()
		hasher.Write([]byte(nonceHex))
		return hasher.Sum(nil)
	}
	if len(b) > 32 {
		t.Fatalf("nonce too long: %d bytes", len(b))
	}
	result := make([]byte, 32)
	copy(result[32-len(b):], b)
	return result
}
