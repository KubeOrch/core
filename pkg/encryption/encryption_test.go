package encryption

import (
	"encoding/base64"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestKey(t *testing.T) func() {
	// Generate a valid 32-byte key for testing
	testKey := make([]byte, 32)
	for i := range testKey {
		testKey[i] = byte(i)
	}
	encodedKey := base64.StdEncoding.EncodeToString(testKey)

	// Set the environment variable
	os.Setenv("ENCRYPTION_KEY", encodedKey)

	// Reset the initialized flag and key to force re-initialization
	initialized = false
	encryptionKey = nil

	// Return cleanup function
	return func() {
		os.Unsetenv("ENCRYPTION_KEY")
		initialized = false
		encryptionKey = nil
	}
}

func TestEncryptDecrypt(t *testing.T) {
	cleanup := setupTestKey(t)
	defer cleanup()

	testCases := []struct {
		name      string
		plaintext string
	}{
		{"empty string", ""},
		{"simple string", "hello world"},
		{"special characters", "!@#$%^&*()_+-=[]{}|;':\",./<>?"},
		{"unicode", "こんにちは世界 🌍"},
		{"long string", "Lorem ipsum dolor sit amet, consectetur adipiscing elit. Sed do eiusmod tempor incididunt ut labore et dolore magna aliqua."},
		{"whitespace", "   spaces   and\ttabs\nand\nnewlines   "},
		{"json", `{"key": "value", "number": 123, "array": [1, 2, 3]}`},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			encrypted, err := Encrypt(tc.plaintext)
			require.NoError(t, err)

			// Empty string should return empty string
			if tc.plaintext == "" {
				assert.Equal(t, "", encrypted)
				return
			}

			// Encrypted should be different from plaintext
			assert.NotEqual(t, tc.plaintext, encrypted)

			// Should be valid base64
			_, err = base64.StdEncoding.DecodeString(encrypted)
			assert.NoError(t, err)

			// Decrypt should return original
			decrypted, err := Decrypt(encrypted)
			require.NoError(t, err)
			assert.Equal(t, tc.plaintext, decrypted)
		})
	}
}

func TestEncryptDifferentOutputs(t *testing.T) {
	cleanup := setupTestKey(t)
	defer cleanup()

	// Same plaintext should produce different ciphertexts (due to random nonce)
	plaintext := "test string"
	encrypted1, err := Encrypt(plaintext)
	require.NoError(t, err)

	encrypted2, err := Encrypt(plaintext)
	require.NoError(t, err)

	// Ciphertexts should be different
	assert.NotEqual(t, encrypted1, encrypted2)

	// But both should decrypt to the same value
	decrypted1, err := Decrypt(encrypted1)
	require.NoError(t, err)
	decrypted2, err := Decrypt(encrypted2)
	require.NoError(t, err)

	assert.Equal(t, plaintext, decrypted1)
	assert.Equal(t, plaintext, decrypted2)
}

func TestDecryptInvalidInput(t *testing.T) {
	cleanup := setupTestKey(t)
	defer cleanup()

	testCases := []struct {
		name       string
		ciphertext string
	}{
		{"invalid base64", "not-valid-base64!!!"},
		{"too short", base64.StdEncoding.EncodeToString([]byte("short"))},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Decrypt(tc.ciphertext)
			assert.Error(t, err)
		})
	}
}

func TestEncryptSlice(t *testing.T) {
	cleanup := setupTestKey(t)
	defer cleanup()

	items := []string{"item1", "item2", "item3"}
	encrypted, err := EncryptSlice(items)
	require.NoError(t, err)

	assert.Len(t, encrypted, len(items))

	// Each item should be encrypted
	for i, enc := range encrypted {
		assert.NotEqual(t, items[i], enc)

		// Decrypt each item
		dec, err := Decrypt(enc)
		require.NoError(t, err)
		assert.Equal(t, items[i], dec)
	}
}

func TestDecryptSlice(t *testing.T) {
	cleanup := setupTestKey(t)
	defer cleanup()

	items := []string{"item1", "item2", "item3"}

	// First encrypt
	encrypted, err := EncryptSlice(items)
	require.NoError(t, err)

	// Then decrypt
	decrypted, err := DecryptSlice(encrypted)
	require.NoError(t, err)

	assert.Equal(t, items, decrypted)
}

func TestEmptySlice(t *testing.T) {
	cleanup := setupTestKey(t)
	defer cleanup()

	items := []string{}

	encrypted, err := EncryptSlice(items)
	require.NoError(t, err)
	assert.Len(t, encrypted, 0)

	decrypted, err := DecryptSlice(encrypted)
	require.NoError(t, err)
	assert.Len(t, decrypted, 0)
}

func TestIsConfigured(t *testing.T) {
	// Test when not configured
	os.Unsetenv("ENCRYPTION_KEY")
	initialized = false
	encryptionKey = nil

	assert.False(t, IsConfigured())

	// Test when configured
	cleanup := setupTestKey(t)
	defer cleanup()

	assert.True(t, IsConfigured())
}

func TestEncryptWithoutKey(t *testing.T) {
	// Clear the key
	os.Unsetenv("ENCRYPTION_KEY")
	initialized = false
	encryptionKey = nil

	_, err := Encrypt("test")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "encryption key not configured")
}

func TestDecryptWithoutKey(t *testing.T) {
	// Clear the key
	os.Unsetenv("ENCRYPTION_KEY")
	initialized = false
	encryptionKey = nil

	// Some valid-looking base64
	_, err := Decrypt("dGVzdA==")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "encryption key not configured")
}

func TestInitializeWithShortKey(t *testing.T) {
	// Set a short key (less than 32 bytes)
	os.Setenv("ENCRYPTION_KEY", "shortkey")
	initialized = false
	encryptionKey = nil

	defer func() {
		os.Unsetenv("ENCRYPTION_KEY")
		initialized = false
		encryptionKey = nil
	}()

	// Should still work - key gets padded
	plaintext := "test message"
	encrypted, err := Encrypt(plaintext)
	require.NoError(t, err)

	decrypted, err := Decrypt(encrypted)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decrypted)
}
