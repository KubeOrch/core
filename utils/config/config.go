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
	viper.SetDefault("LOG_LEVEL", "info")
	// Default to local MongoDB without auth for development
	viper.SetDefault("MONGO_URI", "mongodb://localhost:27017/kubeorch")
	viper.SetDefault("CLUSTER_LOG_TTL_HOURS", 24)
	viper.SetDefault("TOKEN_REFRESH_MAX_AGE_DAYS", 7)

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

func GetMongoURI() string {
	return viper.GetString("MONGO_URI")
}

func GetJWTSecret() string {
	return viper.GetString("JWT_SECRET")
}

func GetEncryptionKey() string {
	return viper.GetString("ENCRYPTION_KEY")
}

func GetClusterLogTTLHours() int {
	return viper.GetInt("CLUSTER_LOG_TTL_HOURS")
}

func GetInviteCode() string {
	return viper.GetString("INVITE_CODE")
}

func GetTokenRefreshMaxAgeDays() int {
	return viper.GetInt("TOKEN_REFRESH_MAX_AGE_DAYS")
}

func GetLogLevel() string {
	return viper.GetString("LOG_LEVEL")
}
