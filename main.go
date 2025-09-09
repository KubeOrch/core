package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/KubeOrch/core/database"
	"github.com/KubeOrch/core/routes"
	"github.com/KubeOrch/core/services"
	"github.com/KubeOrch/core/utils/config"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

func main() {
	// Load configuration using Viper
	if err := config.Load(); err != nil {
		logrus.Fatalf("Failed to load configuration: %v", err)
	}
	logrus.SetFormatter(&logrus.JSONFormatter{})

	// Connect to MongoDB
	if err := database.Connect(); err != nil {
		logrus.Fatalf("Failed to connect to MongoDB: %v", err)
	}

	port := config.GetPort()
	ginMode := config.GetGinMode()
	gin.SetMode(ginMode)

	router := routes.SetupRouter()

	// Start cluster health monitor with 60 second interval
	healthMonitor := services.NewClusterHealthMonitor(60 * time.Second)
	healthMonitor.Start()

	// Create HTTP server
	srv := &http.Server{
		Addr:    ":" + port,
		Handler: router,
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

	// Stop health monitor first
	healthMonitor.Stop()
	logrus.Info("Health monitor stopped")

	// Create a deadline to wait for server shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Gracefully shutdown the server with a timeout of 10 seconds
	if err := srv.Shutdown(ctx); err != nil {
		logrus.Error("Server forced to shutdown:", err)
	}

	// Close database connection
	database.Close()
	logrus.Info("Database connection closed")

	logrus.Info("Server exited gracefully")
}
