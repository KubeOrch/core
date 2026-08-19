package middleware

import (
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

func LogsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Log incoming WebSocket/Terminal requests
		if c.GetHeader("Upgrade") == "websocket" {
			logrus.WithFields(logrus.Fields{
				"method":     c.Request.Method,
				"path":       c.Request.URL.Path,
				"upgrade":    c.GetHeader("Upgrade"),
				"connection": c.GetHeader("Connection"),
				"origin":     c.GetHeader("Origin"),
			}).Info("Incoming WebSocket/Terminal request in LogsMiddleware")
		}

		start := time.Now()
		c.Next()
		duration := time.Since(start)
		fields := logrus.Fields{
			"method":   c.Request.Method,
			"path":     c.Request.URL.Path,
			"duration": fmt.Sprintf("%.2fms", float64(duration.Microseconds())/1000),
		}
		if authorization, ok := WorkspaceAuthorizationFromContext(c.Request.Context()); ok {
			fields["workspace_id"] = authorization.WorkspaceID().Hex()
			fields["membership_id"] = authorization.MembershipID().Hex()
			fields["actor_id"] = authorization.UserID().Hex()
			fields["workspace_role"] = authorization.Role()
		}
		logrus.WithFields(fields).Info("Request processed")
	}
}
