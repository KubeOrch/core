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

			// Public: discover available auth methods
			auth.GET("/methods", handlers.GetAuthMethodsHandler)

			// OAuth2/OIDC routes
			auth.GET("/oauth/:provider/authorize", handlers.OAuthAuthorizeHandler)
			auth.GET("/oauth/:provider/callback", handlers.OAuthCallbackHandler)
			auth.POST("/oauth/exchange", handlers.OAuthExchangeHandler)
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
			// Version control routes (with ownership middleware)
			versions := workflows.Group("/:id/versions")
			versions.Use(middleware.WorkflowOwnershipMiddleware())
			{
				versions.GET("", handlers.ListVersionsHandler)
				versions.GET("/compare", handlers.CompareVersionsHandler) // ?v1=X&v2=Y
				versions.GET("/:version", handlers.GetVersionHandler)
				versions.POST("", handlers.CreateVersionHandler)
				versions.PUT("/:version", handlers.UpdateVersionHandler)
				versions.POST("/:version/restore", handlers.RestoreVersionHandler)
			}
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
			clusters.GET("/:name/metrics", clusterHandler.GetClusterMetrics)
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
			resources.GET("/:id/stream", resourcesHandler.StreamResourceStatus)
			resources.GET("/:id/logs/stream", resourcesHandler.StreamPodLogs)
			resources.GET("/:id/exec/terminal", resourcesHandler.HandleTerminalSession)
			resources.GET("/:id/pods", resourcesHandler.GetDeploymentPods)
		}

		// Registry routes (read operations for all authenticated users)
		registryHandler := handlers.NewRegistryHandler()
		registries := protected.Group("/registries")
		{
			registries.GET("", registryHandler.ListRegistries)
			registries.GET("/lookup", registryHandler.GetRegistryForImage) // ?image=ghcr.io/org/app:v1
			registries.GET("/:id", registryHandler.GetRegistry)
		}

		// Plugin routes (CRD extension plugins)
		pluginHandler := handlers.NewPluginHandler()
		plugins := protected.Group("/plugins")
		{
			plugins.GET("", pluginHandler.ListPlugins)
			plugins.GET("/enabled", pluginHandler.GetEnabledPlugins)
			plugins.GET("/categories", pluginHandler.GetCategories)
			plugins.GET("/:id", pluginHandler.GetPlugin)
			plugins.POST("/:id/enable", pluginHandler.EnablePlugin)
			plugins.POST("/:id/disable", pluginHandler.DisablePlugin)
		}

		// Import routes (docker-compose, git repos)
		importHandler := handlers.NewImportHandler()
		imports := protected.Group("/import")
		{
			imports.POST("/analyze", importHandler.AnalyzeImportHandler)
			imports.POST("/apply", importHandler.ApplyImportHandler)
			imports.POST("/upload", importHandler.UploadComposeHandler)
			imports.POST("/create-workflow", importHandler.CreateWorkflowFromImportHandler)
			imports.GET("/:id", importHandler.GetImportSessionHandler)
			imports.GET("/:id/stream", importHandler.StreamImportLogsHandler)
		}

		// Build routes (container image building)
		builds := protected.Group("/builds")
		{
			builds.POST("/start", handlers.StartBuildHandler)
			builds.GET("", handlers.ListBuildsHandler)
			builds.GET("/:id", handlers.GetBuildHandler)
			builds.GET("/:id/stream", handlers.StreamBuildLogsHandler)
			builds.POST("/:id/cancel", handlers.CancelBuildHandler)
		}

		// Admin routes
		admin := v1.Group("/api/admin")
		admin.Use(middleware.AuthMiddleware(), middleware.AdminMiddleware())
		{
			// Registry management (admin only)
			adminRegistries := admin.Group("/registries")
			{
				adminRegistries.POST("", registryHandler.CreateRegistry)
				adminRegistries.PUT("/:id", registryHandler.UpdateRegistry)
				adminRegistries.DELETE("/:id", registryHandler.DeleteRegistry)
				adminRegistries.POST("/:id/test", registryHandler.TestConnection)
				adminRegistries.PUT("/:id/default", registryHandler.SetDefault)
			}
		}
	}

	return r
}
