package handlers

import (
	"net/http"

	"github.com/KubeOrch/core/services"
	"github.com/gin-gonic/gin"
)

// DashboardStatsHandler returns platform-wide stats with month-over-month changes
func DashboardStatsHandler(c *gin.Context) {
	stats, err := services.GetDashboardStats()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get dashboard stats"})
		return
	}

	c.JSON(http.StatusOK, stats)
}
