package middleware

import (
	"net/http"
	"strings"

	"github.com/KubeOrch/core/pkg/api"
	"github.com/KubeOrch/core/services"
	"github.com/gin-gonic/gin"
)

func BetaAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			api.AbortProblem(c, http.StatusUnauthorized, "authentication_required", "Authentication required", "Provide a bearer token in the Authorization header.")
			return
		}

		claims, err := services.ValidateJWTToken(strings.TrimPrefix(authHeader, "Bearer "))
		if err != nil {
			api.AbortProblem(c, http.StatusUnauthorized, "invalid_token", "Invalid token", "The bearer token is invalid or expired.")
			return
		}

		c.Set("userID", claims.UserID)
		c.Set("email", claims.Email)
		c.Set("userRole", string(claims.Role))
		c.Next()
	}
}
