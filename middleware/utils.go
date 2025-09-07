package middleware

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// GetUserID extracts the user ID from the context
func GetUserID(c *gin.Context) (primitive.ObjectID, error) {
	userIDStr, exists := c.Get("userID")
	if !exists {
		return primitive.NilObjectID, fmt.Errorf("user ID not found in context")
	}
	
	userID, err := primitive.ObjectIDFromHex(userIDStr.(string))
	if err != nil {
		return primitive.NilObjectID, fmt.Errorf("invalid user ID format")
	}
	
	return userID, nil
}

// GetUserEmail extracts the user email from the context
func GetUserEmail(c *gin.Context) (string, error) {
	email, exists := c.Get("email")
	if !exists {
		return "", fmt.Errorf("email not found in context")
	}
	
	return email.(string), nil
}

// IsAdmin checks if the user is an admin
func IsAdmin(c *gin.Context) bool {
	isAdmin, exists := c.Get("isAdmin")
	if !exists {
		return false
	}
	
	return isAdmin.(bool)
}