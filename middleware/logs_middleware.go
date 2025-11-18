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
		if c.GetHeader("Upgrade") == "websocket" || c.Request.URL.Path == "/v1/api/resources/:id/exec/terminal" || c.Request.URL.Path == "/v1/api/resources/691c5d47331a05f81bec7f6d/exec/terminal" {
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
		logrus.WithFields(logrus.Fields{
			"method":   c.Request.Method,
			"path":     c.Request.URL.Path,
			"duration": fmt.Sprintf("%.2fms", float64(duration.Microseconds())/1000),
		}).Info("Request processed")
	}
}
