package middleware

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// GetUserID extracts the user ID from the context
func GetUserID(c *gin.Context) (primitive.ObjectID, error) {
	userIDVal, exists := c.Get("userID")
	if !exists {
		return primitive.NilObjectID, fmt.Errorf("user ID not found in context")
	}

	userIDStr, ok := userIDVal.(string)
	if !ok {
		return primitive.NilObjectID, fmt.Errorf("user ID in context is not a string")
	}

	userID, err := primitive.ObjectIDFromHex(userIDStr)
	if err != nil {
		return primitive.NilObjectID, fmt.Errorf("invalid user ID format: %w", err)
	}

	return userID, nil
}

// GetUserEmail extracts the user email from the context
func GetUserEmail(c *gin.Context) (string, error) {
	emailVal, exists := c.Get("email")
	if !exists {
		return "", fmt.Errorf("email not found in context")
	}

	email, ok := emailVal.(string)
	if !ok {
		return "", fmt.Errorf("email in context is not a string")
	}

	return email, nil
}
