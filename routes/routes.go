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

			// Settings routes
			settings := protected.Group("/settings")
			{
				settings.GET("/invite-code", handlers.GetInviteCodeHandler)
				settings.POST("/generate-invite-code", handlers.GenerateInviteCodeHandler)
			}
		}

		// Deployment routes
		deployments := protected.Group("/deployments")
		{
			deployments.POST("/", handlers.CreateDeploymentHandler)
			deployments.GET("/", handlers.ListDeploymentsHandler)
			deployments.GET("/:id", handlers.GetDeploymentHandler)
			deployments.PUT("/:id", handlers.UpdateDeploymentHandler)
			deployments.DELETE("/:id", handlers.DeleteDeploymentHandler)
		}

		// Kubernetes cluster management routes
		clusterHandler := handlers.NewClusterHandler()
		clusters := protected.Group("/clusters")
		{
			clusters.POST("/", clusterHandler.AddCluster)
			clusters.GET("/", clusterHandler.ListClusters)
			clusters.GET("/default", clusterHandler.GetDefaultCluster)
			clusters.GET("/:name", clusterHandler.GetCluster)
			clusters.DELETE("/:name", clusterHandler.RemoveCluster)
			clusters.PUT("/:name/default", clusterHandler.SetDefaultCluster)
			clusters.POST("/:name/test", clusterHandler.TestConnection)
			clusters.POST("/:name/refresh", clusterHandler.RefreshMetadata)
			clusters.GET("/:name/logs", clusterHandler.GetClusterLogs)
			clusters.PUT("/:name/credentials", clusterHandler.UpdateCredentials)
			clusters.POST("/:name/share", clusterHandler.ShareCluster)
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
