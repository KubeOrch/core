package services

import (
	"context"
	"fmt"
	"sync"

	"github.com/KubeOrch/core/models"
	"github.com/KubeOrch/core/repositories"
	"github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type PluginService struct {
	repo   *repositories.PluginRepository
	logger *logrus.Logger
}

func NewPluginService() *PluginService {
	return &PluginService{
		repo:   repositories.NewPluginRepository(),
		logger: logrus.New(),
	}
}

// ListAvailablePlugins returns all available plugins with their enabled status for a user
func (s *PluginService) ListAvailablePlugins(ctx context.Context, userID primitive.ObjectID) ([]repositories.PluginWithStatus, error) {
	return s.repo.GetPluginsWithStatus(ctx, userID)
}

// ListAllPlugins returns all available plugins (without user status)
func (s *PluginService) ListAllPlugins(ctx context.Context) ([]models.Plugin, error) {
	return s.repo.ListAll(ctx)
}

// GetPlugin retrieves a plugin by its ID
func (s *PluginService) GetPlugin(ctx context.Context, id string) (*models.Plugin, error) {
	return s.repo.GetByID(ctx, id)
}

// GetEnabledPlugins returns all plugins enabled for a user
func (s *PluginService) GetEnabledPlugins(ctx context.Context, userID primitive.ObjectID) ([]models.Plugin, error) {
	return s.repo.GetEnabledPlugins(ctx, userID)
}

// EnablePlugin enables a plugin for a user
func (s *PluginService) EnablePlugin(ctx context.Context, userID primitive.ObjectID, pluginID string) error {
	// Verify plugin exists
	plugin, err := s.repo.GetByID(ctx, pluginID)
	if err != nil {
		return err
	}

	if err := s.repo.EnablePlugin(ctx, userID, pluginID); err != nil {
		return err
	}

	s.logger.WithFields(logrus.Fields{
		"user_id":   userID.Hex(),
		"plugin_id": pluginID,
		"plugin":    plugin.DisplayName,
	}).Info("Plugin enabled for user")

	return nil
}

// DisablePlugin disables a plugin for a user
func (s *PluginService) DisablePlugin(ctx context.Context, userID primitive.ObjectID, pluginID string) error {
	if err := s.repo.DisablePlugin(ctx, userID, pluginID); err != nil {
		return err
	}

	s.logger.WithFields(logrus.Fields{
		"user_id":   userID.Hex(),
		"plugin_id": pluginID,
	}).Info("Plugin disabled for user")

	return nil
}

// IsPluginEnabled checks if a plugin is enabled for a user
func (s *PluginService) IsPluginEnabled(ctx context.Context, userID primitive.ObjectID, pluginID string) (bool, error) {
	return s.repo.IsPluginEnabled(ctx, userID, pluginID)
}

// PluginNodeTypeWithSource includes the plugin ID with the node type
type PluginNodeTypeWithSource struct {
	models.PluginNodeType
	PluginID   string `json:"pluginId"`
	PluginName string `json:"pluginName"`
}

// GetEnabledNodeTypes returns all node types from enabled plugins for a user
// This is used by the templates handler to include plugin node types in the response
func (s *PluginService) GetEnabledNodeTypes(ctx context.Context, userID primitive.ObjectID) ([]PluginNodeTypeWithSource, error) {
	plugins, err := s.repo.GetEnabledPlugins(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get enabled plugins: %w", err)
	}

	var nodeTypes []PluginNodeTypeWithSource
	for _, plugin := range plugins {
		for _, nt := range plugin.NodeTypes {
			nodeTypes = append(nodeTypes, PluginNodeTypeWithSource{
				PluginNodeType: nt,
				PluginID:       plugin.ID,
				PluginName:     plugin.Name,
			})
		}
	}

	return nodeTypes, nil
}

// GetPluginCategories returns all unique plugin categories
func (s *PluginService) GetPluginCategories(ctx context.Context) ([]models.PluginCategory, error) {
	plugins, err := s.repo.ListAll(ctx)
	if err != nil {
		return nil, err
	}

	categorySet := make(map[models.PluginCategory]bool)
	for _, plugin := range plugins {
		categorySet[plugin.Category] = true
	}

	var categories []models.PluginCategory
	for category := range categorySet {
		categories = append(categories, category)
	}

	return categories, nil
}

// Singleton instance
var (
	pluginServiceInstance *PluginService
	pluginServiceOnce     sync.Once
)

func GetPluginService() *PluginService {
	pluginServiceOnce.Do(func() {
		pluginServiceInstance = NewPluginService()
	})
	return pluginServiceInstance
}
