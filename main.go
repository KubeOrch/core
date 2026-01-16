package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/KubeOrch/core/database"
	"github.com/KubeOrch/core/pkg/template"
	"github.com/KubeOrch/core/repositories"
	"github.com/KubeOrch/core/routes"
	"github.com/KubeOrch/core/services"
	"github.com/KubeOrch/core/utils/config"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

func main() {
	// Load configuration using Viper
	if err := config.Load(); err != nil {
		logrus.Fatalf("Failed to load configuration: %v", err)
	}
	logrus.SetFormatter(&logrus.JSONFormatter{})

	// Configure log level from config
	logLevel := config.GetLogLevel()
	level, err := logrus.ParseLevel(logLevel)
	if err != nil {
		logrus.Warnf("Invalid log level '%s', defaulting to 'info': %v", logLevel, err)
		level = logrus.InfoLevel
	}
	logrus.SetLevel(level)
	logrus.Infof("Log level set to: %s", level)

	// Connect to MongoDB
	if err := database.Connect(); err != nil {
		logrus.Fatalf("Failed to connect to MongoDB: %v", err)
	}

	// Create resource indexes after database connection
	resourceRepo := repositories.NewResourceRepository()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := resourceRepo.CreateIndexes(ctx); err != nil {
		logrus.Warnf("Failed to create resource indexes: %v", err)
	}

	// Initialize template registry
	templatesDir := viper.GetString("TEMPLATES_DIR")
	if templatesDir == "" {
		templatesDir = "./templates"
	}
	if err := template.InitializeGlobalRegistry(templatesDir); err != nil {
		logrus.Warnf("Failed to initialize template registry: %v", err)
	} else {
		logrus.Infof("Template registry initialized with directory: %s", templatesDir)
	}

	port := config.GetPort()
	ginMode := config.GetGinMode()
	gin.SetMode(ginMode)

	router := routes.SetupRouter()

	// Start cluster health monitor with 60 second interval
	healthMonitor := services.NewClusterHealthMonitor(60 * time.Second)
	healthMonitor.Start()

	// Resource sync monitor disabled - real-time watchers now handle status updates
	// resourceSyncMonitor := services.NewResourceSyncMonitor(5 * time.Minute)
	// resourceSyncMonitor.Start()

	// Initialize unified SSE broadcaster for real-time updates (workflows, pod logs, etc.)
	broadcaster := services.GetSSEBroadcaster()
	defer broadcaster.Close()

	// Create HTTP server with extended timeouts for SSE streaming
	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      router,
		ReadTimeout:  15 * time.Second, // Time to read request
		WriteTimeout: 0,                // Disable write timeout for SSE streams (no timeout)
		IdleTimeout:  120 * time.Second,
	}

	// Start server in a goroutine
	go func() {
		logrus.Infof("Starting KubeOrch server on port %s in %s mode...", port, ginMode)
		logrus.Info("MongoDB connection established")
		logrus.Info("Cluster health monitor started (60s interval)")
		logrus.Info("First user to register will automatically become admin")

		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logrus.Fatalf("listen: %s\n", err)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server
	quit := make(chan os.Signal, 1)
	// kill (no param) default send syscall.SIGTERM
	// kill -2 is syscall.SIGINT
	// kill -9 is syscall.SIGKILL but can't be catch, so don't need add it
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	logrus.Info("Shutting down server...")

	// Stop all resource watchers
	watcherManager := services.GetResourceWatcherManager()
	watcherManager.Shutdown()
	logrus.Info("Resource watchers stopped")

	// Stop SSE broadcaster
	sseBroadcaster := services.GetSSEBroadcaster()
	sseBroadcaster.Close()
	logrus.Info("SSE broadcaster stopped")

	// Stop health monitor
	healthMonitor.Stop()
	logrus.Info("Health monitor stopped")

	// Create a deadline to wait for server shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	// Gracefully shutdown the server with a timeout of 10 seconds
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logrus.Error("Server forced to shutdown:", err)
	}

	// Close database connection
	if err := database.Close(); err != nil {
		logrus.Errorf("Failed to close database connection: %v", err)
	}
	logrus.Info("Database connection closed")

	logrus.Info("Server exited gracefully")
}
