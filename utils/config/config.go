package config

import (
	"os"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

func Load() error {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	viper.AutomaticEnv()

	viper.SetDefault("PORT", "3000")
	viper.SetDefault("GIN_MODE", "debug")

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			log.Info("No config file found, using environment variables")
		} else {
			return err
		}
	}
	return nil
}

func GetPort() string {
	return viper.GetString("PORT")
}

func GetGinMode() string {
	return viper.GetString("GIN_MODE")
}

func GetEnv(key string) string {
	return os.Getenv(key)
}
