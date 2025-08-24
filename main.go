package main

import (
	"flag"

	"github.com/KubeOrchestra/core/database"
	"github.com/KubeOrchestra/core/routes"
	"github.com/KubeOrchestra/core/utils/config"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"github.com/sirupsen/logrus"
)

func main() {
	migrate := flag.Bool("migrate", false, "Run database migrations")
	flag.Parse()

	if err := config.Load(); err != nil {
		logrus.Fatalf("Failed to load configuration: %v", err)
	}
	logrus.SetFormatter(&logrus.JSONFormatter{})

	if err := database.Connect(); err != nil {
		logrus.Fatalf("Failed to connect to database: %v", err)
	}

	if *migrate {
		if err := database.Migrate(); err != nil {
			logrus.Fatalf("Failed to run migrations: %v", err)
		}
		logrus.Info("Database migrations completed successfully")
	}

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
		logrus.Warn("Failed loading .env file, will use environment variables")
	}
}
