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

    viper.SetDefault("DATABASE.HOST", "localhost")
    viper.SetDefault("DATABASE.PORT", "5432")
    viper.SetDefault("DATABASE.USER", "kubeorch_user")
    viper.SetDefault("DATABASE.PASSWORD", "kubeorch_password")
    viper.SetDefault("DATABASE.NAME", "kubeorch_db")
    viper.SetDefault("DATABASE.SSL_MODE", "disable")

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

func GetDBHost() string {
    return viper.GetString("DATABASE.HOST")
}

func GetDBPort() string {
    return viper.GetString("DATABASE.PORT")
}

func GetDBUser() string {
    return viper.GetString("DATABASE.USER")
}

func GetDBPassword() string {
    return viper.GetString("DATABASE.PASSWORD")
}

func GetDBName() string {
    return viper.GetString("DATABASE.NAME")
}

func GetDBSSLMode() string {
    return viper.GetString("DATABASE.SSL_MODE")
}
