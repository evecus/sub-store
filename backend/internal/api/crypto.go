package api

import (
	"crypto/rand"
	"encoding/hex"
)

// generateToken returns a cryptographically random hex string of the given byte length.
func generateToken(bytes int) string {
	b := make([]byte, bytes)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
