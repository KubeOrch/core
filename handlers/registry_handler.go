package handlers

import (
	"net/http"

	"github.com/KubeOrch/core/middleware"
	"github.com/KubeOrch/core/models"
	"github.com/KubeOrch/core/services"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type RegistryHandler struct {
	service *services.RegistryService
	logger  *logrus.Logger
}

func NewRegistryHandler() *RegistryHandler {
	return &RegistryHandler{
		service: services.GetRegistryService(),
		logger:  logrus.New(),
	}
}

// Request/Response types

type CreateRegistryRequest struct {
	Name         string                     `json:"name" binding:"required"`
	RegistryType models.RegistryType        `json:"registryType" binding:"required"`
	RegistryURL  string                     `json:"registryUrl,omitempty"`
	Credentials  models.RegistryCredentials `json:"credentials" binding:"required"`
}

type UpdateRegistryRequest struct {
	Name        string                      `json:"name,omitempty"`
	RegistryURL string                      `json:"registryUrl,omitempty"`
	Credentials *models.RegistryCredentials `json:"credentials,omitempty"`
}

type RegistryResponse struct {
	ID           string                `json:"id"`
	Name         string                `json:"name"`
	RegistryType models.RegistryType   `json:"registryType"`
	RegistryURL  string                `json:"registryUrl"`
	PreviewURL   string                `json:"previewUrl,omitempty"`
	Status       models.RegistryStatus `json:"status"`
	IsDefault    bool                  `json:"isDefault"`
	LastCheck    string                `json:"lastCheck,omitempty"`
	CreatedAt    string                `json:"createdAt"`
	UpdatedAt    string                `json:"updatedAt"`
}

func registryToResponse(r *models.Registry) RegistryResponse {
	// Populate the preview URL before converting
	r.PopulatePreviewURL()

	resp := RegistryResponse{
		ID:           r.ID.Hex(),
		Name:         r.Name,
		RegistryType: r.RegistryType,
		RegistryURL:  r.RegistryURL,
		PreviewURL:   r.PreviewURL,
		Status:       r.Status,
		IsDefault:    r.IsDefault,
		CreatedAt:    r.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:    r.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
	if !r.LastCheck.IsZero() {
		resp.LastCheck = r.LastCheck.Format("2006-01-02T15:04:05Z07:00")
	}
	return resp
}

// Handlers

// CreateRegistry creates a new registry credential (admin only)
func (h *RegistryHandler) CreateRegistry(c *gin.Context) {
	var req CreateRegistryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.WithError(err).Error("Invalid request body")
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body: " + err.Error()})
		return
	}

	userID, err := middleware.GetUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	registry := &models.Registry{
		Name:         req.Name,
		RegistryType: req.RegistryType,
		RegistryURL:  req.RegistryURL,
		Credentials:  req.Credentials,
		CreatedBy:    userID,
	}

	ctx := c.Request.Context()
	if err := h.service.CreateRegistry(ctx, registry); err != nil {
		h.logger.WithError(err).Error("Failed to create registry")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message":  "Registry created successfully",
		"registry": registryToResponse(registry),
	})
}

// ListRegistries returns all registries (all authenticated users)
func (h *RegistryHandler) ListRegistries(c *gin.Context) {
	ctx := c.Request.Context()
	registries, err := h.service.ListRegistries(ctx)
	if err != nil {
		h.logger.WithError(err).Error("Failed to list registries")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	response := make([]RegistryResponse, 0, len(registries))
	for _, r := range registries {
		response = append(response, registryToResponse(r))
	}

	c.JSON(http.StatusOK, gin.H{
		"registries": response,
	})
}

// GetRegistry returns a registry by ID (all authenticated users)
func (h *RegistryHandler) GetRegistry(c *gin.Context) {
	idStr := c.Param("id")
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid registry ID"})
		return
	}

	ctx := c.Request.Context()
	registry, err := h.service.GetRegistry(ctx, id)
	if err != nil {
		h.logger.WithError(err).Error("Failed to get registry")
		c.JSON(http.StatusNotFound, gin.H{"error": "Registry not found"})
		return
	}

	c.JSON(http.StatusOK, registryToResponse(registry))
}

// UpdateRegistry updates a registry (admin only)
func (h *RegistryHandler) UpdateRegistry(c *gin.Context) {
	idStr := c.Param("id")
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid registry ID"})
		return
	}

	var req UpdateRegistryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.WithError(err).Error("Invalid request body")
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	ctx := c.Request.Context()

	// Check registry exists first
	if _, err := h.service.GetRegistry(ctx, id); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Registry not found"})
		return
	}

	// Build updates map
	updates := make(map[string]interface{})
	if req.Name != "" {
		updates["name"] = req.Name
	}
	if req.RegistryURL != "" {
		updates["registry_url"] = req.RegistryURL
	}
	if req.Credentials != nil {
		updates["credentials"] = *req.Credentials
	}

	if len(updates) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No updates provided"})
		return
	}

	if err := h.service.UpdateRegistry(ctx, id, updates); err != nil {
		h.logger.WithError(err).Error("Failed to update registry")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Fetch updated registry
	registry, err := h.service.GetRegistry(ctx, id)
	if err != nil {
		h.logger.WithError(err).Error("Failed to fetch updated registry")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve updated registry"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":  "Registry updated successfully",
		"registry": registryToResponse(registry),
	})
}

// DeleteRegistry deletes a registry (admin only)
func (h *RegistryHandler) DeleteRegistry(c *gin.Context) {
	idStr := c.Param("id")
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid registry ID"})
		return
	}

	ctx := c.Request.Context()
	if err := h.service.DeleteRegistry(ctx, id); err != nil {
		h.logger.WithError(err).Error("Failed to delete registry")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Registry deleted successfully",
	})
}

// TestConnection tests the connection to a registry (admin only)
func (h *RegistryHandler) TestConnection(c *gin.Context) {
	idStr := c.Param("id")
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid registry ID"})
		return
	}

	ctx := c.Request.Context()
	if err := h.service.TestRegistryConnection(ctx, id); err != nil {
		h.logger.WithError(err).Error("Registry connection test failed")
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error":  err.Error(),
			"status": "failed",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Connection test successful",
		"status":  "connected",
	})
}

// SetDefault sets a registry as the default for its type (admin only)
func (h *RegistryHandler) SetDefault(c *gin.Context) {
	idStr := c.Param("id")
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid registry ID"})
		return
	}

	ctx := c.Request.Context()
	if err := h.service.SetDefaultRegistry(ctx, id); err != nil {
		h.logger.WithError(err).Error("Failed to set default registry")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Registry set as default successfully",
	})
}

// GetRegistryForImage finds the appropriate registry for a given image (all authenticated users)
func (h *RegistryHandler) GetRegistryForImage(c *gin.Context) {
	image := c.Query("image")
	if image == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Image parameter is required"})
		return
	}

	ctx := c.Request.Context()
	registry, err := h.service.GetRegistryForImage(ctx, image)
	if err != nil {
		h.logger.WithError(err).Error("Failed to find registry for image")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if registry == nil {
		// Detect the registry type even if no credentials are configured
		registryType := models.DetectRegistryType(image)
		c.JSON(http.StatusOK, gin.H{
			"found":        false,
			"registryType": registryType,
			"message":      "No registry credentials configured for this image type",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"found":    true,
		"registry": registryToResponse(registry),
	})
}
