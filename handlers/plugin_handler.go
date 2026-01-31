package handlers

import (
	"net/http"

	"github.com/KubeOrch/core/middleware"
	"github.com/KubeOrch/core/services"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

type PluginHandler struct {
	service *services.PluginService
	logger  *logrus.Logger
}

func NewPluginHandler() *PluginHandler {
	return &PluginHandler{
		service: services.GetPluginService(),
		logger:  logrus.New(),
	}
}

// ListPlugins returns all available plugins with their enabled status for the current user
// GET /plugins
func (h *PluginHandler) ListPlugins(c *gin.Context) {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	ctx := c.Request.Context()
	plugins, err := h.service.ListAvailablePlugins(ctx, userID)
	if err != nil {
		h.logger.WithError(err).Error("Failed to list plugins")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list plugins"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"plugins": plugins,
	})
}

// GetPlugin returns a specific plugin by ID
// GET /plugins/:id
func (h *PluginHandler) GetPlugin(c *gin.Context) {
	pluginID := c.Param("id")
	if pluginID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Plugin ID is required"})
		return
	}

	ctx := c.Request.Context()
	plugin, err := h.service.GetPlugin(ctx, pluginID)
	if err != nil {
		h.logger.WithError(err).WithField("plugin_id", pluginID).Error("Failed to get plugin")
		c.JSON(http.StatusNotFound, gin.H{"error": "Plugin not found"})
		return
	}

	// Also check if enabled for current user
	userID, err := middleware.GetUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	enabled, _ := h.service.IsPluginEnabled(ctx, userID, pluginID)

	c.JSON(http.StatusOK, gin.H{
		"plugin":  plugin,
		"enabled": enabled,
	})
}

// GetEnabledPlugins returns all plugins enabled for the current user
// GET /plugins/enabled
func (h *PluginHandler) GetEnabledPlugins(c *gin.Context) {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	ctx := c.Request.Context()
	plugins, err := h.service.GetEnabledPlugins(ctx, userID)
	if err != nil {
		h.logger.WithError(err).Error("Failed to get enabled plugins")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get enabled plugins"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"plugins": plugins,
	})
}

// EnablePlugin enables a plugin for the current user
// POST /plugins/:id/enable
func (h *PluginHandler) EnablePlugin(c *gin.Context) {
	pluginID := c.Param("id")
	if pluginID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Plugin ID is required"})
		return
	}

	userID, err := middleware.GetUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	ctx := c.Request.Context()
	if err := h.service.EnablePlugin(ctx, userID, pluginID); err != nil {
		h.logger.WithError(err).WithField("plugin_id", pluginID).Error("Failed to enable plugin")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Plugin enabled successfully",
	})
}

// DisablePlugin disables a plugin for the current user
// POST /plugins/:id/disable
func (h *PluginHandler) DisablePlugin(c *gin.Context) {
	pluginID := c.Param("id")
	if pluginID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Plugin ID is required"})
		return
	}

	userID, err := middleware.GetUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	ctx := c.Request.Context()
	if err := h.service.DisablePlugin(ctx, userID, pluginID); err != nil {
		h.logger.WithError(err).WithField("plugin_id", pluginID).Error("Failed to disable plugin")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Plugin disabled successfully",
	})
}

// GetCategories returns all available plugin categories
// GET /plugins/categories
func (h *PluginHandler) GetCategories(c *gin.Context) {
	ctx := c.Request.Context()
	categories, err := h.service.GetPluginCategories(ctx)
	if err != nil {
		h.logger.WithError(err).Error("Failed to get plugin categories")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get categories"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"categories": categories,
	})
}
