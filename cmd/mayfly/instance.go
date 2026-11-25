package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// instanceIDBytes is the random part of the instance ID; hex doubles it to 16 characters.
const instanceIDBytes = 8

// newInstanceID returns a random ID for this process; a restart gets a new one, which is how the reaper spots orphans.
func newInstanceID() (string, error) {
	b := make([]byte, instanceIDBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("instance id: %w", err)
	}
	return hex.EncodeToString(b), nil
}
