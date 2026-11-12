package github

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"strings"
	"sync"
	"testing"
	"time"
)

var (
	testKeyOnce sync.Once
	testKeyVal  *rsa.PrivateKey
)

// testKey returns a throwaway 2048-bit RSA key shared by the tests in this package.
func testKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	testKeyOnce.Do(func() {
		k, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			panic(err)
		}
		testKeyVal = k
	})
	return testKeyVal
}

// verifyJWT checks the RS256 signature of token with pub and returns its decoded claims.
func verifyJWT(t *testing.T, token string, pub *rsa.PublicKey) map[string]any {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("jwt has %d parts, want 3", len(parts))
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatalf("decode signature: %v", err)
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(pub, crypto.SHA256, digest[:], sig); err != nil {
		t.Fatalf("signature invalid: %v", err)
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode claims: %v", err)
	}
	var claims map[string]any
	if err := json.Unmarshal(raw, &claims); err != nil {
		t.Fatalf("unmarshal claims: %v", err)
	}
	return claims
}

// TestAppJWT checks the signature and the iss, iat and exp claims.
func TestAppJWT(t *testing.T) {
	key := testKey(t)
	now := time.Unix(1_700_000_000, 0)

	token, err := appJWT(5714458, key, now)
	if err != nil {
		t.Fatal(err)
	}
	claims := verifyJWT(t, token, &key.PublicKey)

	if claims["iss"] != "5714458" {
		t.Errorf("iss = %v, want \"5714458\"", claims["iss"])
	}
	if got := int64(claims["iat"].(float64)); got != now.Unix()-60 {
		t.Errorf("iat = %d, want now-60", got)
	}
	exp := int64(claims["exp"].(float64))
	if exp <= now.Unix() || exp-now.Unix() > 600 {
		t.Errorf("exp = now+%ds, want within (0, 600]", exp-now.Unix())
	}
}

// TestParsePrivateKey checks PKCS#1, PKCS#8 and invalid inputs.
func TestParsePrivateKey(t *testing.T) {
	key := testKey(t)
	pkcs8, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		pem     []byte
		wantErr bool
	}{
		{"pkcs1", pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}), false},
		{"pkcs8", pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8}), false},
		{"not pem", []byte("hello"), true},
		{"garbage block", pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: []byte("junk")}), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParsePrivateKey(tt.pem)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && !got.Equal(key) {
				t.Error("parsed key differs from original")
			}
		})
	}
}
