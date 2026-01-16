package handlers

import (
	"net/http"

	"github.com/KubeOrch/core/services"
	"github.com/gin-gonic/gin"
)

// SearchHandler handles unified search across workflows, resources, and clusters
// GET /api/search?q=query
func SearchHandler(c *gin.Context) {
	userID, ok := getUserIDFromContext(c)
	if !ok {
		return
	}

	query := c.Query("q")

	// Minimum 3 characters required for search
	if len(query) < 3 {
		c.JSON(http.StatusOK, gin.H{
			"workflows": []interface{}{},
			"resources": []interface{}{},
			"clusters":  []interface{}{},
		})
		return
	}

	results, err := services.Search(userID, query)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Search failed"})
		return
	}

	c.JSON(http.StatusOK, results)
}
