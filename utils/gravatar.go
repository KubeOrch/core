package utils

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"strings"
)

// GetGravatarURL generates a gravatar URL for the given email
func GetGravatarURL(email string, size int) string {
	// Normalize email: lowercase and trim whitespace
	email = strings.TrimSpace(strings.ToLower(email))

	// Generate MD5 hash
	hash := md5.Sum([]byte(email))

	// Return the Gravatar URL with size and default identicon
	return fmt.Sprintf("https://www.gravatar.com/avatar/%s?s=%d&d=identicon",
		hex.EncodeToString(hash[:]), size)
}