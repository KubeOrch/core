package main

import (
	"github.com/KubeOrchestra/core/routes"
	"github.com/KubeOrchestra/core/utils/config"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"github.com/sirupsen/logrus"
)

func main() {
	if err := config.Load(); err != nil {
		logrus.Fatalf("Failed to load configuration: %v", err)
	}
	logrus.SetFormatter(&logrus.JSONFormatter{})

	port := config.GetPort()
	ginMode := config.GetGinMode()
	gin.SetMode(ginMode)

	router := routes.SetupRouter()

	logrus.Infof("Starting KubeOrchestra server on port %s in %s mode...", port, ginMode)
	if err := router.Run(":" + port); err != nil {
		logrus.Fatal("Failed to start server:", err)
	}
}

func init() {
	if err := godotenv.Load(); err != nil {
		logrus.Error("Failed loading .env file, will use environment variables")
	}
}
