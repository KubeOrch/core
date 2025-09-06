package routes

import (
	"time"

	"github.com/KubeOrch/core/handlers"
	"github.com/KubeOrch/core/middleware"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

func SetupRouter() *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())

	r.Use(cors.New(cors.Config{
		AllowOrigins:  []string{"*"}, // TODO(naman): restrict this to specific origins
		AllowMethods:  []string{"GET", "POST", "PUT", "DELETE", "PATCH"},
		AllowHeaders:  []string{"Origin", "Content-Type", "Authorization"},
		ExposeHeaders: []string{"Content-Length"},
		MaxAge:        12 * time.Hour,
	}))

	v1 := r.Group("/v1")
	{
		v1.Use(middleware.LogsMiddleware())
		v1.GET("/", handlers.HelloHandler)

		// Auth routes
		auth := v1.Group("/api/auth")
		{
			auth.POST("/register", handlers.RegisterHandler)
			auth.POST("/login", handlers.LoginHandler)
		}

		// Protected routes
		protected := v1.Group("/api")
		protected.Use(middleware.AuthMiddleware())
		{
			protected.GET("/profile", handlers.GetProfileHandler)
		}

		// Admin routes
		admin := v1.Group("/api/admin")
		admin.Use(middleware.AuthMiddleware(), middleware.AdminMiddleware())
		{
			// Add admin-only endpoints here
		}
	}

	return r
}
