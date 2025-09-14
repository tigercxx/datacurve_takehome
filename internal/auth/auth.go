package auth

import (
	"crypto/sha256"
	"encoding/hex"
)

func HashToken(tok string) string {
	sum := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(sum[:])
}
