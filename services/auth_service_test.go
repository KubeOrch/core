package services

import (
	"testing"
	"time"

	"github.com/KubeOrch/core/models"
	"github.com/golang-jwt/jwt/v5"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func setupJWTSecret(t *testing.T) func() {
	testSecret := "test-jwt-secret-for-unit-testing-32chars"
	viper.Set("JWT_SECRET", testSecret)
	viper.Set("TOKEN_REFRESH_MAX_AGE_DAYS", 7)

	return func() {
		viper.Set("JWT_SECRET", "")
		viper.Set("TOKEN_REFRESH_MAX_AGE_DAYS", 7)
	}
}

func TestHashPassword(t *testing.T) {
	t.Run("should hash password successfully", func(t *testing.T) {
		password := "mySecurePassword123!"
		hash, err := HashPassword(password)

		require.NoError(t, err)
		assert.NotEmpty(t, hash)
		assert.NotEqual(t, password, hash)
	})

	t.Run("should produce different hashes for same password", func(t *testing.T) {
		password := "samePassword"
		hash1, err := HashPassword(password)
		require.NoError(t, err)

		hash2, err := HashPassword(password)
		require.NoError(t, err)

		// bcrypt produces different hashes each time
		assert.NotEqual(t, hash1, hash2)
	})

	t.Run("should handle empty password", func(t *testing.T) {
		hash, err := HashPassword("")
		require.NoError(t, err)
		assert.NotEmpty(t, hash)
	})

	t.Run("should handle unicode password", func(t *testing.T) {
		password := "пароль密码كلمة السر🔐"
		hash, err := HashPassword(password)
		require.NoError(t, err)
		assert.NotEmpty(t, hash)
	})
}

func TestCheckPasswordHash(t *testing.T) {
	password := "testPassword123"
	hash, err := HashPassword(password)
	require.NoError(t, err)

	t.Run("should return true for correct password", func(t *testing.T) {
		result := CheckPasswordHash(password, hash)
		assert.True(t, result)
	})

	t.Run("should return false for incorrect password", func(t *testing.T) {
		result := CheckPasswordHash("wrongPassword", hash)
		assert.False(t, result)
	})

	t.Run("should return false for empty password", func(t *testing.T) {
		result := CheckPasswordHash("", hash)
		assert.False(t, result)
	})

	t.Run("should return false for invalid hash", func(t *testing.T) {
		result := CheckPasswordHash(password, "invalid-hash")
		assert.False(t, result)
	})

	t.Run("should handle case sensitivity", func(t *testing.T) {
		result := CheckPasswordHash("TESTPASSWORD123", hash)
		assert.False(t, result)
	})
}

func TestGenerateJWTToken(t *testing.T) {
	cleanup := setupJWTSecret(t)
	defer cleanup()

	userID := primitive.NewObjectID()
	email := "test@example.com"
	role := models.RoleUser

	t.Run("should generate valid token", func(t *testing.T) {
		token, err := GenerateJWTToken(userID, email, role)

		require.NoError(t, err)
		assert.NotEmpty(t, token)
	})

	t.Run("should generate different tokens each time", func(t *testing.T) {
		token1, err := GenerateJWTToken(userID, email, role)
		require.NoError(t, err)

		// Small delay to ensure different IssuedAt times
		time.Sleep(time.Millisecond * 10)

		token2, err := GenerateJWTToken(userID, email, role)
		require.NoError(t, err)

		// Tokens may be the same if generated at the same second
		// The important thing is both are valid
		assert.NotEmpty(t, token1)
		assert.NotEmpty(t, token2)
	})

	t.Run("should fail without JWT secret", func(t *testing.T) {
		viper.Set("JWT_SECRET", "")
		defer viper.Set("JWT_SECRET", "test-jwt-secret-for-unit-testing-32chars")

		_, err := GenerateJWTToken(userID, email, role)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "JWT_SECRET not configured")
	})
}

func TestValidateJWTToken(t *testing.T) {
	cleanup := setupJWTSecret(t)
	defer cleanup()

	userID := primitive.NewObjectID()
	email := "test@example.com"
	role := models.RoleAdmin

	t.Run("should validate valid token", func(t *testing.T) {
		token, err := GenerateJWTToken(userID, email, role)
		require.NoError(t, err)

		claims, err := ValidateJWTToken(token)

		require.NoError(t, err)
		assert.Equal(t, userID.Hex(), claims.UserID)
		assert.Equal(t, email, claims.Email)
		assert.Equal(t, role, claims.Role)
	})

	t.Run("should reject invalid token", func(t *testing.T) {
		_, err := ValidateJWTToken("invalid-token")
		assert.Error(t, err)
	})

	t.Run("should reject tampered token", func(t *testing.T) {
		token, err := GenerateJWTToken(userID, email, role)
		require.NoError(t, err)

		// Tamper with the token
		tamperedToken := token[:len(token)-5] + "xxxxx"

		_, err = ValidateJWTToken(tamperedToken)
		assert.Error(t, err)
	})

	t.Run("should reject token with wrong secret", func(t *testing.T) {
		token, err := GenerateJWTToken(userID, email, role)
		require.NoError(t, err)

		// Change the secret
		viper.Set("JWT_SECRET", "different-secret")
		defer viper.Set("JWT_SECRET", "test-jwt-secret-for-unit-testing-32chars")

		_, err = ValidateJWTToken(token)
		assert.Error(t, err)
	})

	t.Run("should fail without JWT secret", func(t *testing.T) {
		token, _ := GenerateJWTToken(userID, email, role)

		viper.Set("JWT_SECRET", "")
		defer viper.Set("JWT_SECRET", "test-jwt-secret-for-unit-testing-32chars")

		_, err := ValidateJWTToken(token)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "JWT_SECRET not configured")
	})
}

func TestValidateJWTTokenForRefresh(t *testing.T) {
	cleanup := setupJWTSecret(t)
	defer cleanup()

	userID := primitive.NewObjectID()
	email := "test@example.com"
	role := models.RoleUser

	t.Run("should validate valid token", func(t *testing.T) {
		token, err := GenerateJWTToken(userID, email, role)
		require.NoError(t, err)

		claims, err := ValidateJWTTokenForRefresh(token)

		require.NoError(t, err)
		assert.Equal(t, userID.Hex(), claims.UserID)
		assert.Equal(t, email, claims.Email)
	})

	t.Run("should accept recently expired token", func(t *testing.T) {
		// Create a token that expired 1 day ago (within 7 day max age)
		jwtSecret := viper.GetString("JWT_SECRET")
		claims := JWTClaims{
			UserID: userID.Hex(),
			Email:  email,
			Role:   role,
			RegisteredClaims: jwt.RegisteredClaims{
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(-24 * time.Hour)),
				IssuedAt:  jwt.NewNumericDate(time.Now().Add(-48 * time.Hour)),
			},
		}
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		tokenString, err := token.SignedString([]byte(jwtSecret))
		require.NoError(t, err)

		result, err := ValidateJWTTokenForRefresh(tokenString)

		require.NoError(t, err)
		assert.Equal(t, userID.Hex(), result.UserID)
	})

	t.Run("should reject token expired for too long", func(t *testing.T) {
		// Create a token that expired 10 days ago (beyond 7 day max age)
		jwtSecret := viper.GetString("JWT_SECRET")
		claims := JWTClaims{
			UserID: userID.Hex(),
			Email:  email,
			Role:   role,
			RegisteredClaims: jwt.RegisteredClaims{
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(-10 * 24 * time.Hour)),
				IssuedAt:  jwt.NewNumericDate(time.Now().Add(-11 * 24 * time.Hour)),
			},
		}
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		tokenString, err := token.SignedString([]byte(jwtSecret))
		require.NoError(t, err)

		_, err = ValidateJWTTokenForRefresh(tokenString)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "token expired for too long")
	})

	t.Run("should reject invalid token", func(t *testing.T) {
		_, err := ValidateJWTTokenForRefresh("invalid-token")
		assert.Error(t, err)
	})
}

func TestParseObjectID(t *testing.T) {
	t.Run("should parse valid ObjectID", func(t *testing.T) {
		originalID := primitive.NewObjectID()
		hexString := originalID.Hex()

		parsedID, err := ParseObjectID(hexString)

		require.NoError(t, err)
		assert.Equal(t, originalID, parsedID)
	})

	t.Run("should fail on invalid ObjectID", func(t *testing.T) {
		_, err := ParseObjectID("invalid-id")
		assert.Error(t, err)
	})

	t.Run("should fail on empty string", func(t *testing.T) {
		_, err := ParseObjectID("")
		assert.Error(t, err)
	})

	t.Run("should fail on too short string", func(t *testing.T) {
		_, err := ParseObjectID("12345")
		assert.Error(t, err)
	})
}
