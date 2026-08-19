package middleware

import (
	"github.com/KubeOrch/core/pkg/api"
	"github.com/gin-gonic/gin"
)

func RequestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		api.RequestID(c)
		c.Next()
	}
}
