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
		AllowHeaders:  []string{"Origin", "Content-Type", "Authorization", "Upgrade", "Connection", "Sec-WebSocket-Key", "Sec-WebSocket-Version", "Sec-WebSocket-Extensions"},
		ExposeHeaders: []string{"Content-Length"},
		MaxAge:        12 * time.Hour,
	}))

	v1 := r.Group("/v1")
	{
		v1.Use(middleware.LogsMiddleware())
		v1.GET("", handlers.HelloHandler)

		// Auth routes
		auth := v1.Group("/api/auth")
		{
			auth.POST("/register", handlers.RegisterHandler)
			auth.POST("/login", handlers.LoginHandler)
			auth.POST("/refresh", middleware.RefreshTokenMiddleware(), handlers.RefreshTokenHandler)
		}

		// Protected routes
		protected := v1.Group("/api")
		protected.Use(middleware.AuthMiddleware())
		{
			protected.GET("/profile", handlers.GetProfileHandler)
			protected.PUT("/profile", handlers.UpdateProfileHandler)

			// Search route
			protected.GET("/search", handlers.SearchHandler)

			// Settings routes
			settings := protected.Group("/settings")
			{
				settings.GET("/invite-code", handlers.GetInviteCodeHandler)
				settings.POST("/generate-invite-code", handlers.GenerateInviteCodeHandler)
				settings.GET("/regenerate-setting", handlers.GetRegenerateSettingHandler)
				settings.PUT("/regenerate-setting", handlers.UpdateRegenerateSettingHandler)
			}
		}


		// Workflow routes
		workflows := protected.Group("/workflows")
		{
			workflows.POST("", handlers.CreateWorkflowHandler)
			workflows.GET("", handlers.ListWorkflowsHandler)
			workflows.GET("/:id", handlers.GetWorkflowHandler)
			workflows.PUT("/:id", handlers.UpdateWorkflowHandler)
			workflows.DELETE("/:id", handlers.DeleteWorkflowHandler)
			workflows.POST("/:id/clone", handlers.CloneWorkflowHandler)
			workflows.PUT("/:id/status", handlers.UpdateWorkflowStatusHandler)
			workflows.POST("/:id/save", handlers.SaveWorkflowHandler)
			workflows.POST("/:id/run", handlers.RunWorkflowHandler)
			workflows.GET("/:id/runs", handlers.GetWorkflowRunsHandler)
			// Node diagnostics and auto-fix routes
			workflows.GET("/:id/nodes/:nodeId/diagnostics", handlers.GetNodeDiagnosticsHandler)
			workflows.GET("/:id/nodes/:nodeId/fix-template/:fixType", handlers.GetFixTemplateHandler)
			workflows.POST("/:id/nodes/:nodeId/fix", handlers.ApplyNodeFixHandler)
			// Real-time status streaming via SSE
			workflows.GET("/:id/status/stream", handlers.StreamWorkflowStatusHandler)
			// Version control routes
			workflows.GET("/:id/versions", handlers.ListVersionsHandler)
			workflows.GET("/:id/versions/:version", handlers.GetVersionHandler)
			workflows.POST("/:id/versions", handlers.CreateVersionHandler)
			workflows.PUT("/:id/versions/:version", handlers.UpdateVersionHandler)
			workflows.POST("/:id/versions/:version/restore", handlers.RestoreVersionHandler)
			workflows.GET("/:id/versions/compare", handlers.CompareVersionsHandler) // ?v1=X&v2=Y
		}

		// Template routes
		protected.GET("/templates", handlers.GetTemplatesHandler)

		// Kubernetes cluster management routes
		clusterHandler := handlers.NewClusterHandler()
		clusters := protected.Group("/clusters")
		{
			clusters.POST("", clusterHandler.AddCluster)
			clusters.GET("", clusterHandler.ListClusters)
			clusters.GET("/default", clusterHandler.GetDefaultCluster)
			clusters.GET("/:name", clusterHandler.GetCluster)
			clusters.PUT("/:name", clusterHandler.UpdateCluster)
			clusters.GET("/:name/status", clusterHandler.GetClusterStatus)
			clusters.DELETE("/:name", clusterHandler.RemoveCluster)
			clusters.PUT("/:name/default", clusterHandler.SetDefaultCluster)
			clusters.POST("/:name/test", clusterHandler.TestConnection)
			clusters.POST("/:name/refresh", clusterHandler.RefreshMetadata)
			clusters.GET("/:name/logs", clusterHandler.GetClusterLogs)
			clusters.PUT("/:name/credentials", clusterHandler.UpdateCredentials)
			clusters.POST("/:name/share", clusterHandler.ShareCluster)
		}

		// Kubernetes resources routes
		resourcesHandler := handlers.NewResourcesHandler()
		resources := protected.Group("/resources")
		{
			resources.GET("", resourcesHandler.GetResources)
			resources.POST("/sync", resourcesHandler.SyncResources)
			resources.GET("/:id", resourcesHandler.GetResourceByID)
			resources.PATCH("/:id", resourcesHandler.UpdateResourceUserFields)
			resources.GET("/:id/logs/stream", resourcesHandler.StreamPodLogs)
			resources.GET("/:id/exec/terminal", resourcesHandler.HandleTerminalSession)
			resources.GET("/:id/pods", resourcesHandler.GetDeploymentPods)
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
