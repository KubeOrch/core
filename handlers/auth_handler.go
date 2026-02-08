package handlers

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"time"

	"github.com/KubeOrch/core/models"
	"github.com/KubeOrch/core/services"
	"github.com/KubeOrch/core/utils"
	"github.com/KubeOrch/core/utils/config"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

func RegisterHandler(c *gin.Context) {
	if !config.GetAuthBuiltinEnabled() || !config.GetAuthSignupEnabled() {
		c.JSON(http.StatusForbidden, gin.H{"error": "Registration is disabled"})
		return
	}

	var req struct {
		Email      string `json:"email" binding:"required,email"`
		Password   string `json:"password" binding:"required,min=6"`
		Name       string `json:"name" binding:"required"`
		InviteCode string `json:"inviteCode"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid request data",
		})
		return
	}

	// Check if user already exists
	exists, err := services.UserExistsByEmail(req.Email)
	if err != nil {
		logrus.Errorf("Error checking user existence: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Registration failed",
		})
		return
	}

	if exists {
		c.JSON(http.StatusConflict, gin.H{
			"error": "User already exists",
		})
		return
	}

	// Check if this is the first user (admin) or validate invite code
	userCount, err := services.GetUserCount()
	if err != nil {
		logrus.Errorf("Error checking user count: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Registration failed",
		})
		return
	}

	var role models.UserRole
	if userCount == 0 {
		// First user becomes admin
		role = models.RoleAdmin

		// Generate JWT secret if not already set
		if viper.GetString("JWT_SECRET") == "" {
			jwtSecret, err := generateJWTSecret()
			if err != nil {
				logrus.Errorf("Error generating JWT secret: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{
					"error": "Registration failed",
				})
				return
			}

			if err := updateConfig("JWT_SECRET", jwtSecret); err != nil {
				logrus.Errorf("Error saving JWT secret: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{
					"error": "Registration failed",
				})
				return
			}
			viper.Set("JWT_SECRET", jwtSecret)
			logrus.Info("JWT secret generated and saved")
		}

		// Generate ENCRYPTION_KEY if not already set
		if viper.GetString("ENCRYPTION_KEY") == "" {
			encryptionKey, err := generateEncryptionKey()
			if err != nil {
				logrus.Errorf("Error generating encryption key: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{
					"error": "Registration failed",
				})
				return
			}

			if err := updateConfig("ENCRYPTION_KEY", encryptionKey); err != nil {
				logrus.Errorf("Error saving encryption key: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{
					"error": "Registration failed",
				})
				return
			}
			viper.Set("ENCRYPTION_KEY", encryptionKey)
			logrus.Info("Encryption key generated and saved")
		}

// Generate invite code for the organization
		inviteCode := generateInviteCode()
		if err := updateConfig("INVITE_CODE", inviteCode); err != nil {
			logrus.Warnf("Error saving initial invite code: %v", err)
		} else {
			viper.Set("INVITE_CODE", inviteCode)
			logrus.Infof("Initial invite code generated: %s", inviteCode)
		}
	} else {
		// Require valid invite code for non-admin users
		storedInviteCode := viper.GetString("INVITE_CODE")
		if storedInviteCode == "" || req.InviteCode != storedInviteCode {
			c.JSON(http.StatusForbidden, gin.H{
				"error": "Valid invite code required",
			})
			return
		}
		role = models.RoleUser
	}

	// Hash password
	hashedPassword, err := services.HashPassword(req.Password)
	if err != nil {
		logrus.Errorf("Error hashing password: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Registration failed",
		})
		return
	}

	// Create user in database
	user := &models.User{
		Email:    req.Email,
		Name:     req.Name,
		Password: hashedPassword,
		Role:     role,
	}

	if err := services.CreateUser(user); err != nil {
		logrus.Errorf("Error creating user: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Registration failed",
		})
		return
	}

	// Generate JWT token
	token, err := services.GenerateJWTToken(user.ID, user.Email, user.Role)
	if err != nil {
		logrus.Errorf("Error generating token: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Registration failed",
		})
		return
	}

	// Check if we should regenerate invite code after signup (only for non-admin users)
	if role == models.RoleUser && viper.GetBool("REGENERATE_INVITE_AFTER_SIGNUP") {
		newInviteCode := generateInviteCode()
		if err := updateConfig("INVITE_CODE", newInviteCode); err != nil {
			logrus.Warnf("Failed to regenerate invite code after signup: %v", err)
		} else {
			viper.Set("INVITE_CODE", newInviteCode)
			logrus.Infof("Invite code regenerated after signup: %s", newInviteCode)
		}
	}

	logrus.Infof("New user registered: %s with role: %s", user.Email, user.Role)

	c.JSON(http.StatusCreated, gin.H{
		"token": token,
		"user": gin.H{
			"id":        user.ID.Hex(),
			"email":     user.Email,
			"name":      user.Name,
			"role":      user.Role,
			"avatarUrl": utils.GetGravatarURL(user.Email, 200),
			"createdAt": user.CreatedAt,
		},
	})
}

func LoginHandler(c *gin.Context) {
	if !config.GetAuthBuiltinEnabled() {
		c.JSON(http.StatusForbidden, gin.H{"error": "Password login is disabled"})
		return
	}

	var req struct {
		Email    string `json:"email" binding:"required,email"`
		Password string `json:"password" binding:"required,min=6"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid request data",
		})
		return
	}

	// Get user from database
	user, err := services.GetUserByEmail(req.Email)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "Invalid email or password",
		})
		return
	}

	// Check password
	if !services.CheckPasswordHash(req.Password, user.Password) {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "Invalid email or password",
		})
		return
	}

	// Generate JWT token
	token, err := services.GenerateJWTToken(user.ID, user.Email, user.Role)
	if err != nil {
		logrus.Errorf("Error generating token: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Login failed",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"token": token,
		"user": gin.H{
			"id":        user.ID.Hex(),
			"email":     user.Email,
			"name":      user.Name,
			"role":      user.Role,
			"avatarUrl": utils.GetGravatarURL(user.Email, 200),
			"createdAt": user.CreatedAt,
		},
	})
}

func generateJWTSecret() (string, error) {
	// Generate 32 bytes of random data
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return "", err
	}
	// Encode to base64 for storage
	return base64.StdEncoding.EncodeToString(secret), nil
}

func generateEncryptionKey() (string, error) {
	// Generate 32 bytes of random data for AES-256
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return "", err
	}
	// Encode to base64 for storage
	return base64.StdEncoding.EncodeToString(key), nil
}

func generateInviteCode() string {
	// Generate a cryptographically secure 6-digit code
	// Using 3 bytes gives us values 0-16777215, we'll mod by 1000000 for 6 digits
	b := make([]byte, 3)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp-based code in case of error
		return fmt.Sprintf("%06d", time.Now().UnixNano()%1000000)
	}
	// Convert bytes to integer and ensure 6 digits
	num := int(b[0])<<16 | int(b[1])<<8 | int(b[2])
	return fmt.Sprintf("%06d", num%1000000)
}


func GetProfileHandler(c *gin.Context) {
	userIDStr, exists := c.Get("userID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "Unauthorized",
		})
		return
	}

	userID, err := services.ParseObjectID(userIDStr.(string))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid user ID",
		})
		return
	}

	user, err := services.GetUserByID(userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "User not found",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"user": gin.H{
			"id":         user.ID.Hex(),
			"email":      user.Email,
			"name":       user.Name,
			"role":       user.Role,
			"createdAt":  user.CreatedAt,
			"avatarUrl":  utils.GetGravatarURL(user.Email, 200),
		},
	})
}

func UpdateProfileHandler(c *gin.Context) {
	userIDStr, exists := c.Get("userID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "Unauthorized",
		})
		return
	}

	userID, err := services.ParseObjectID(userIDStr.(string))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid user ID",
		})
		return
	}

	var request struct {
		Name string `json:"name" binding:"required"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Name is required",
		})
		return
	}

	// Update user in database
	user, err := services.UpdateUserName(userID, request.Name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to update profile",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"user": gin.H{
			"id":         user.ID.Hex(),
			"email":      user.Email,
			"name":       user.Name,
			"role":       user.Role,
			"createdAt":  user.CreatedAt,
			"avatarUrl":  utils.GetGravatarURL(user.Email, 200),
		},
	})
}

func RefreshTokenHandler(c *gin.Context) {
	// This endpoint is called when the token is expired
	// It validates the expired token and issues a new one
	userIDStr, exists := c.Get("userID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "Unauthorized",
		})
		return
	}

	userID, err := services.ParseObjectID(userIDStr.(string))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid user ID",
		})
		return
	}

	// Get user from database
	user, err := services.GetUserByID(userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "User not found",
		})
		return
	}

	// Generate new JWT token
	token, err := services.GenerateJWTToken(user.ID, user.Email, user.Role)
	if err != nil {
		logrus.Errorf("Error generating refresh token: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to refresh token",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"token": token,
		"user": gin.H{
			"id":        user.ID.Hex(),
			"email":     user.Email,
			"name":      user.Name,
			"role":      user.Role,
			"avatarUrl": utils.GetGravatarURL(user.Email, 200),
			"createdAt": user.CreatedAt,
		},
	})
}
