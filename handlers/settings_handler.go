package handlers

import (
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v2"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

func GetInviteCodeHandler(c *gin.Context) {
	// Check if user is admin
	role, exists := c.Get("userRole")
	if !exists || role != "admin" {
		c.JSON(http.StatusForbidden, gin.H{
			"error": "Admin access required",
		})
		return
	}

	inviteCode := viper.GetString("INVITE_CODE")
	if inviteCode == "" {
		// Generate new code if it doesn't exist
		inviteCode = generateInviteCode()
		if err := updateConfigFile("INVITE_CODE", inviteCode); err != nil {
			logrus.Errorf("Error updating config: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "Failed to generate invite code",
			})
			return
		}
		viper.Set("INVITE_CODE", inviteCode)
	}

	c.JSON(http.StatusOK, gin.H{
		"inviteCode": inviteCode,
	})
}

func GenerateInviteCodeHandler(c *gin.Context) {
	// Check if user is admin
	role, exists := c.Get("userRole")
	if !exists || role != "admin" {
		c.JSON(http.StatusForbidden, gin.H{
			"error": "Admin access required",
		})
		return
	}

	inviteCode := generateInviteCode()
	if err := updateConfigFile("INVITE_CODE", inviteCode); err != nil {
		logrus.Errorf("Error updating config: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to generate invite code",
		})
		return
	}

	viper.Set("INVITE_CODE", inviteCode)
	logrus.Infof("New invite code generated: %s", inviteCode)

	c.JSON(http.StatusOK, gin.H{
		"inviteCode": inviteCode,
	})
}

func generateInviteCode() string {
	return fmt.Sprintf("%06d", rand.Intn(1000000))
}

func updateConfigFile(key, value string) error {
	// Read the config file
	configPath := "config.yaml"
	data, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}

	// Parse YAML
	var config map[string]interface{}
	if err := yaml.Unmarshal(data, &config); err != nil {
		return err
	}

	// Update the value
	config[key] = value

	// Marshal back to YAML
	updatedData, err := yaml.Marshal(config)
	if err != nil {
		return err
	}

	// Write back to file
	return os.WriteFile(configPath, updatedData, 0644)
}