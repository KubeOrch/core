package handlers

import (
	"net/http"

	"github.com/KubeOrch/core/models"
	"github.com/KubeOrch/core/services"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

func RegisterHandler(c *gin.Context) {
	var req struct {
		Email    string `json:"email" binding:"required,email"`
		Password string `json:"password" binding:"required,min=6"`
		Name     string `json:"name" binding:"required"`
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

	logrus.Infof("New user registered: %s with role: %s", user.Email, user.Role)

	c.JSON(http.StatusCreated, gin.H{
		"token": token,
		"user": gin.H{
			"id":    user.ID.Hex(),
			"email": user.Email,
			"name":  user.Name,
			"role":  user.Role,
		},
	})
}

func LoginHandler(c *gin.Context) {
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
			"id":    user.ID.Hex(),
			"email": user.Email,
			"name":  user.Name,
			"role":  user.Role,
		},
	})
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
			"id":    user.ID.Hex(),
			"email": user.Email,
			"name":  user.Name,
			"role":  user.Role,
		},
	})
}