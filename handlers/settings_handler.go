package handlers

import (
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v2"
)

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
		if err := updateConfig("INVITE_CODE", inviteCode); err != nil {
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
	if err := updateConfig("INVITE_CODE", inviteCode); err != nil {
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

func GetRegenerateSettingHandler(c *gin.Context) {
	// Check if user is admin
	role, exists := c.Get("userRole")
	if !exists || role != "admin" {
		c.JSON(http.StatusForbidden, gin.H{
			"error": "Admin access required",
		})
		return
	}

	regenerateSetting := viper.GetBool("REGENERATE_INVITE_AFTER_SIGNUP")

	c.JSON(http.StatusOK, gin.H{
		"regenerateAfterSignup": regenerateSetting,
		"inviteCode": viper.GetString("INVITE_CODE"),
	})
}

func UpdateRegenerateSettingHandler(c *gin.Context) {
	// Check if user is admin
	role, exists := c.Get("userRole")
	if !exists || role != "admin" {
		c.JSON(http.StatusForbidden, gin.H{
			"error": "Admin access required",
		})
		return
	}

	var req struct {
		RegenerateAfterSignup bool `json:"regenerateAfterSignup"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid request data",
		})
		return
	}

	if err := updateConfig("REGENERATE_INVITE_AFTER_SIGNUP", req.RegenerateAfterSignup); err != nil {
		logrus.Errorf("Error updating config: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to update setting",
		})
		return
	}

	viper.Set("REGENERATE_INVITE_AFTER_SIGNUP", req.RegenerateAfterSignup)
	logrus.Infof("Regenerate invite code setting updated to: %v", req.RegenerateAfterSignup)

	c.JSON(http.StatusOK, gin.H{
		"regenerateAfterSignup": req.RegenerateAfterSignup,
	})
}

func updateConfig(key string, value interface{}) error {
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
