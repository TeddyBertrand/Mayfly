// Package github is a small REST client that authenticates as the Mayfly GitHub App.
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
	"errors"
	"fmt"
	"strconv"
	"time"
)

const (
	// jwtBackdate absorbs clock skew: GitHub rejects tokens issued "in the future".
	jwtBackdate = 60 * time.Second
	// jwtLifetime stays under GitHub's 10-minute maximum.
	jwtLifetime = 9 * time.Minute
)

// ParsePrivateKey decodes a PEM RSA private key in PKCS#1 (GitHub's format) or PKCS#8 form.
// Errors never include key material.
func ParsePrivateKey(pemBytes []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("private key: no PEM block found")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, errors.New("private key: not a PKCS#1 or PKCS#8 RSA key")
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("private key: not an RSA key")
	}
	return key, nil
}

// appJWT returns an RS256 JWT identifying appID, signed with key and valid from now.
func appJWT(appID int64, key *rsa.PrivateKey, now time.Time) (string, error) {
	header := `{"alg":"RS256","typ":"JWT"}`
	claims, err := json.Marshal(struct {
		IssuedAt  int64  `json:"iat"`
		ExpiresAt int64  `json:"exp"`
		Issuer    string `json:"iss"`
	}{
		IssuedAt:  now.Add(-jwtBackdate).Unix(),
		ExpiresAt: now.Add(jwtLifetime).Unix(),
		Issuer:    strconv.FormatInt(appID, 10),
	})
	if err != nil {
		return "", fmt.Errorf("jwt claims: %w", err)
	}

	enc := base64.RawURLEncoding
	signingInput := enc.EncodeToString([]byte(header)) + "." + enc.EncodeToString(claims)
	digest := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		return "", fmt.Errorf("jwt sign: %w", err)
	}
	return signingInput + "." + enc.EncodeToString(sig), nil
}
