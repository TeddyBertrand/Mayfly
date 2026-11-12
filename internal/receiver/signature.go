package receiver

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

const signaturePrefix = "sha256="

// VerifySignature reports whether header is a valid X-Hub-Signature-256 value for body under secret.
func VerifySignature(secret, body []byte, header string) bool {
	hexSig, ok := strings.CutPrefix(header, signaturePrefix)
	if !ok {
		return false
	}
	got, err := hex.DecodeString(hexSig)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	// hmac.Equal is constant-time, so response timing does not leak how many bytes matched.
	return hmac.Equal(got, mac.Sum(nil))
}
