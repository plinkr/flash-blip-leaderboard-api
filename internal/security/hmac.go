package security

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
)

// VerifyHMAC computes the HMAC-SHA256 signature of the message with the given secret key
// and compares it against the expected hex-encoded signature in constant time.
func VerifyHMAC(message []byte, secretKey string, expectedHexSig string) bool {
	h := hmac.New(sha256.New, []byte(secretKey))
	h.Write(message)
	computedSig := h.Sum(nil)

	expectedSig, err := hex.DecodeString(expectedHexSig)
	if err != nil {
		return false
	}

	return subtle.ConstantTimeCompare(computedSig, expectedSig) == 1
}
