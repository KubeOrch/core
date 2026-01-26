package encryption

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"os"

	"github.com/spf13/viper"
)

var encryptionKey []byte
var initialized bool

func InitializeEncryption() {
	if initialized {
		return
	}
	
	// Try environment variable first
	key := os.Getenv("ENCRYPTION_KEY")
	if key == "" {
		// Try Viper config
		key = viper.GetString("ENCRYPTION_KEY")
	}
	if key == "" {
		// No encryption key available - this is expected before first admin setup
		// The key will be generated and set during admin registration
		return
	}
	
	// Try to decode from base64 first (if it's a generated key)
	decoded, err := base64.StdEncoding.DecodeString(key)
	if err == nil && len(decoded) == 32 {
		// Successfully decoded a 32-byte key
		encryptionKey = decoded
	} else {
		// Use SHA256 to derive a proper 32-byte key from the input
		// This provides consistent key derivation regardless of input length
		hash := sha256.Sum256([]byte(key))
		encryptionKey = hash[:]
	}
	initialized = true
}

// IsConfigured returns true if encryption key is properly configured
func IsConfigured() bool {
	InitializeEncryption()
	return len(encryptionKey) == 32
}

func Encrypt(plaintext string) (string, error) {
	InitializeEncryption() // Ensure initialization
	
	if plaintext == "" {
		return "", nil
	}
	
	if encryptionKey == nil {
		return "", fmt.Errorf("encryption key not configured - cannot encrypt sensitive data")
	}

	block, err := aes.NewCipher(encryptionKey)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	nonce := make([]byte, aesGCM.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	ciphertext := aesGCM.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

func Decrypt(ciphertext string) (string, error) {
	InitializeEncryption() // Ensure initialization
	
	if ciphertext == "" {
		return "", nil
	}
	
	if encryptionKey == nil {
		return "", fmt.Errorf("encryption key not configured - cannot decrypt sensitive data")
	}

	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("failed to decode ciphertext: %w", err)
	}

	block, err := aes.NewCipher(encryptionKey)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	nonceSize := aesGCM.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertextBytes := data[:nonceSize], data[nonceSize:]
	plaintext, err := aesGCM.Open(nil, nonce, ciphertextBytes, nil)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt: %w", err)
	}

	return string(plaintext), nil
}

func EncryptSlice(items []string) ([]string, error) {
	InitializeEncryption() // Ensure initialization
	
	encrypted := make([]string, len(items))
	for i, item := range items {
		enc, err := Encrypt(item)
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt item %d: %w", i, err)
		}
		encrypted[i] = enc
	}
	return encrypted, nil
}

func DecryptSlice(items []string) ([]string, error) {
	InitializeEncryption() // Ensure initialization
	
	decrypted := make([]string, len(items))
	for i, item := range items {
		dec, err := Decrypt(item)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt item %d: %w", i, err)
		}
		decrypted[i] = dec
	}
	return decrypted, nil
}