package main

import (
	"github.com/KubeOrch/core/database"
	"github.com/KubeOrch/core/routes"
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
	defer database.Close()

	port := config.GetPort()
	ginMode := config.GetGinMode()
	gin.SetMode(ginMode)

	router := routes.SetupRouter()

	logrus.Infof("Starting KubeOrch server on port %s in %s mode...", port, ginMode)
	logrus.Info("MongoDB connection established")
	logrus.Info("First user to register will automatically become admin")
	
	if err := router.Run(":" + port); err != nil {
		logrus.Fatal("Failed to start server:", err)
	}
}