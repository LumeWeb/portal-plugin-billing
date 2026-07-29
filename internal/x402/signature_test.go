package x402

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
)

func TestVerifyPaymentSignature_ValidSignature(t *testing.T) {
	kp, wallet := testWallet(t)
	nonce := "0x" + fmt.Sprintf("%064x", 0xABCDEF)
	sig := signTransferWithAuthorizationForTest(t, kp, wallet, testPayTo, 10000, 0, 9999999999, nonce, testAssetAddr, testChainID, testErc20Name, testTokenVer)

	auth := pluginCore.X402Authorization{
		From:        wallet,
		To:          testPayTo,
		Value:       "10000",
		ValidAfter:  "0",
		ValidBefore: "9999999999",
		Nonce:       nonce,
	}

	recovered, err := verifyPaymentSignature(auth, sig, testAssetAddr, testChainID, testErc20Name, testTokenVer)
	require.NoError(t, err)
	assert.Equal(t, wallet, recovered)
}

func TestVerifyPaymentSignature_WrongWallet(t *testing.T) {
	kp1, wallet1 := testWallet(t)
	_, wallet2 := testWallet(t)
	nonce := "0x" + fmt.Sprintf("%064x", 0x111)
	sig := signTransferWithAuthorizationForTest(t, kp1, wallet1, testPayTo, 10000, 0, 9999999999, nonce, testAssetAddr, testChainID, testErc20Name, testTokenVer)

	auth := pluginCore.X402Authorization{
		From:        wallet2, // wrong wallet
		To:          testPayTo,
		Value:       "10000",
		ValidAfter:  "0",
		ValidBefore: "9999999999",
		Nonce:       nonce,
	}

	_, err := verifyPaymentSignature(auth, sig, testAssetAddr, testChainID, testErc20Name, testTokenVer)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not match")
}

func TestVerifyPaymentSignature_MissingSignature(t *testing.T) {
	_, wallet := testWallet(t)
	auth := pluginCore.X402Authorization{From: wallet, Nonce: "0xabc"}
	_, err := verifyPaymentSignature(auth, "", testAssetAddr, testChainID, testErc20Name, testTokenVer)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing payment signature")
}

func TestVerifyPaymentSignature_InvalidSignatureLength(t *testing.T) {
	_, wallet := testWallet(t)
	auth := pluginCore.X402Authorization{From: wallet, Nonce: "0xabc"}
	_, err := verifyPaymentSignature(auth, "0xdeadbeef", testAssetAddr, testChainID, testErc20Name, testTokenVer)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid signature length")
}
// --- Regression: validAfter/validBefore time window enforcement (Kody 3669595289) ---

func TestVerifyPaymentSignature_NotYetValid(t *testing.T) {
	kp, wallet := testWallet(t)
	nonce := "0x" + fmt.Sprintf("%064x", 0x666)

	// validAfter is far in the future
	farFuture := time.Now().Unix() + 3600
	sig := signTransferWithAuthorizationForTest(t, kp, wallet, testPayTo, 10000, farFuture, farFuture+60, nonce, testAssetAddr, testChainID, testErc20Name, testTokenVer)

	auth := pluginCore.X402Authorization{
		From:        wallet,
		To:          testPayTo,
		Value:       "10000",
		ValidAfter:  fmt.Sprintf("%d", farFuture),
		ValidBefore: fmt.Sprintf("%d", farFuture+60),
		Nonce:       nonce,
	}

	_, err := verifyPaymentSignature(auth, sig, testAssetAddr, testChainID, testErc20Name, testTokenVer)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not yet valid")
}

func TestVerifyPaymentSignature_Expired(t *testing.T) {
	kp, wallet := testWallet(t)
	nonce := "0x" + fmt.Sprintf("%064x", 0x665)

	// validBefore is in the past
	pastTime := time.Now().Unix() - 3600
	sig := signTransferWithAuthorizationForTest(t, kp, wallet, testPayTo, 10000, pastTime-60, pastTime, nonce, testAssetAddr, testChainID, testErc20Name, testTokenVer)

	auth := pluginCore.X402Authorization{
		From:        wallet,
		To:          testPayTo,
		Value:       "10000",
		ValidAfter:  fmt.Sprintf("%d", pastTime-60),
		ValidBefore: fmt.Sprintf("%d", pastTime),
		Nonce:       nonce,
	}

	_, err := verifyPaymentSignature(auth, sig, testAssetAddr, testChainID, testErc20Name, testTokenVer)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expired")
}

func TestVerifyPaymentSignature_WithinTimeWindow_Succeeds(t *testing.T) {
	kp, wallet := testWallet(t)
	nonce := "0x" + fmt.Sprintf("%064x", 0x664)

	// validAfter=0, validBefore=far future — should be valid now
	sig := signTransferWithAuthorizationForTest(t, kp, wallet, testPayTo, 10000, 0, 9999999999, nonce, testAssetAddr, testChainID, testErc20Name, testTokenVer)

	auth := pluginCore.X402Authorization{
		From:        wallet,
		To:          testPayTo,
		Value:       "10000",
		ValidAfter:  "0",
		ValidBefore: "9999999999",
		Nonce:       nonce,
	}

	recovered, err := verifyPaymentSignature(auth, sig, testAssetAddr, testChainID, testErc20Name, testTokenVer)
	require.NoError(t, err)
	assert.Equal(t, wallet, recovered)
}
