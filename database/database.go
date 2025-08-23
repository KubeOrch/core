package database

import (
	"fmt"

	"github.com/KubeOrchestra/core/model"
	"github.com/KubeOrchestra/core/utils/config"
	"github.com/sirupsen/logrus"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var DB *gorm.DB

func Connect() error {
	host := config.GetDBHost()
	port := config.GetDBPort()
	user := config.GetDBUser()
	password := config.GetDBPassword()
	dbname := config.GetDBName()
	sslmode := config.GetDBSSLMode()

	logrus.Infof("Connecting to database: host=%s, port=%s, user=%s, dbname=%s, sslmode=%s",
		host, port, user, dbname, sslmode)

	dsn := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		host, port, user, password, dbname, sslmode)

	database, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})

	if err != nil {
		logrus.Errorf("Could not connect to database: %v", err)
		return err
	}

	DB = database
	logrus.Info("Database connection established")
	return nil
}

func Migrate() error {
    return DB.AutoMigrate(&model.User{})
}

func Close() error {
	if DB != nil {
		sqlDB, err := DB.DB()
		if err != nil {
			logrus.Errorf("Could not get database connection: %v", err)
			return err
		}
		return sqlDB.Close()
	}
	return nil
}
