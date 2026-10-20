// Package token — случайные токены для cookie и ссылок и их хэши для хранения в БД.
package token

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
)

// Bytes — длина токена в байтах.
const Bytes = 32

// New возвращает 32 случайных байта в base64url без паддинга (безопасно в URL и cookie).
func New() (string, error) {
	b := make([]byte, Bytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// Hash — sha256 от токена (sessions.token_hash).
func Hash(t string) []byte {
	h := sha256.Sum256([]byte(t))
	return h[:]
}

// HashHex — hex(sha256) от токена (invites.token, payment_links.token).
func HashHex(t string) string {
	return hex.EncodeToString(Hash(t))
}
