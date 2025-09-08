package encryption

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"os"
)

var encryptionKey []byte

func init() {
	key := os.Getenv("ENCRYPTION_KEY")
	if key == "" {
		// Fallback to JWT_SECRET if ENCRYPTION_KEY not set
		key = os.Getenv("JWT_SECRET")
	}
	if key == "" {
		panic("ENCRYPTION_KEY or JWT_SECRET environment variable must be set")
	}
	
	// Try to decode from base64 first (if it's a generated key)
	decoded, err := base64.StdEncoding.DecodeString(key)
	if err == nil && len(decoded) == 32 {
		// Successfully decoded a 32-byte key
		encryptionKey = decoded
	} else {
		// Use the key as-is or pad it to 32 bytes
		keyBytes := []byte(key)
		if len(keyBytes) < 32 {
			padded := make([]byte, 32)
			copy(padded, keyBytes)
			for i := len(keyBytes); i < 32; i++ {
				padded[i] = byte(i % 256)
			}
			encryptionKey = padded
		} else {
			encryptionKey = keyBytes[:32]
		}
	}
}

func Encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
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
	if ciphertext == "" {
		return "", nil
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