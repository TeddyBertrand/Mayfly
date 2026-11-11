package receiver

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

// sign returns the X-Hub-Signature-256 header GitHub would send for body under secret.
func sign(secret, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	return signaturePrefix + hex.EncodeToString(mac.Sum(nil))
}

// TestVerifySignature covers valid, invalid, missing and tampered cases.
func TestVerifySignature(t *testing.T) {
	secret := []byte("test-secret")
	body := []byte(`{"action":"queued"}`)
	valid := sign(secret, body)

	tests := []struct {
		name   string
		body   []byte
		header string
		want   bool
	}{
		{"valid", body, valid, true},
		{"missing header", body, "", false},
		{"missing prefix", body, valid[len(signaturePrefix):], false},
		{"sha1 prefix", body, "sha1=" + valid[len(signaturePrefix):], false},
		{"not hex", body, "sha256=zz", false},
		{"invalid signature", body, sign([]byte("other-secret"), body), false},
		{"truncated signature", body, valid[:len(valid)-2], false},
		{"tampered body", []byte(`{"action":"completed"}`), valid, false},
		{"empty body signed", []byte{}, sign(secret, []byte{}), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := VerifySignature(secret, tt.body, tt.header); got != tt.want {
				t.Errorf("VerifySignature = %v, want %v", got, tt.want)
			}
		})
	}
}
