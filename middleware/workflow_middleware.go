package middleware

import (
	"net/http"

	"github.com/KubeOrch/core/services"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// WorkflowOwnershipMiddleware validates workflow ownership before allowing access
// It expects the :id parameter in the route and sets "workflow" in the context
func WorkflowOwnershipMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get user ID from context (set by AuthMiddleware)
		userIDStr, exists := c.Get("userID")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
			c.Abort()
			return
		}

		userID, err := primitive.ObjectIDFromHex(userIDStr.(string))
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid user ID"})
			c.Abort()
			return
		}

		// Parse workflow ID from URL parameter
		workflowID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid workflow ID"})
			c.Abort()
			return
		}

		// Get workflow and verify ownership
		workflow, err := services.GetWorkflowByID(workflowID)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Workflow not found"})
			c.Abort()
			return
		}

		if workflow.OwnerID != userID {
			c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
			c.Abort()
			return
		}

		// Set workflow in context for handlers to use
		c.Set("workflow", workflow)
		c.Set("workflowID", workflowID)
		c.Set("ownerID", userID)

		c.Next()
	}
}
