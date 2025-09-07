package config

import (
	"os"

	"github.com/fsnotify/fsnotify"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

func Load() error {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	viper.AutomaticEnv()
	viper.SetEnvPrefix("KUBEORCH")

	// Set defaults
	viper.SetDefault("PORT", "3000")
	viper.SetDefault("GIN_MODE", "debug")
	viper.SetDefault("MONGODB.HOST", "localhost")
	viper.SetDefault("MONGODB.PORT", "27017")
	viper.SetDefault("MONGODB.NAME", "kubeorch")

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			log.Info("No config file found, using environment variables and defaults")
		} else {
			return err
		}
	}

	// Watch for config file changes
	viper.WatchConfig()
	viper.OnConfigChange(func(e fsnotify.Event) {
		log.Infof("Config file changed: %s", e.Name)
	})

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

func GetMongoHost() string {
	return viper.GetString("MONGODB.HOST")
}

func GetMongoPort() string {
	return viper.GetString("MONGODB.PORT")
}

func GetMongoDBName() string {
	return viper.GetString("MONGODB.NAME")
}

func GetJWTSecret() string {
	return viper.GetString("JWT_SECRET")
}
