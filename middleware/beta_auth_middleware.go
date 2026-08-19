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
		token, ok := bearerCredential(c.GetHeader("Authorization"))
		if !ok {
			api.AbortProblem(c, http.StatusUnauthorized, "authentication_required", "Authentication required", "Provide a bearer token in the Authorization header.")
			return
		}

		claims, err := services.ValidateJWTToken(token)
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

func bearerCredential(header string) (string, bool) {
	scheme, credential, found := strings.Cut(header, " ")
	if !found || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	credential = strings.TrimLeft(credential, " ")
	if credential == "" || strings.ContainsAny(credential, " \t\r\n") {
		return "", false
	}
	return credential, true
}
