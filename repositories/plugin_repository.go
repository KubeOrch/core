package repositories

import (
	"context"
	"fmt"
	"time"

	"github.com/KubeOrch/core/database"
	"github.com/KubeOrch/core/models"
	"github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type PluginRepository struct {
	pluginCollection     *mongo.Collection
	userPluginCollection *mongo.Collection
	logger               *logrus.Logger
}

func NewPluginRepository() *PluginRepository {
	db := database.GetDB()
	repo := &PluginRepository{
		pluginCollection:     db.Collection("plugins"),
		userPluginCollection: db.Collection("user_plugins"),
		logger:               logrus.New(),
	}

	// Initialize indexes
	repo.initializeIndexes()

	// Seed built-in plugins
	repo.seedBuiltInPlugins()

	return repo
}

func (r *PluginRepository) initializeIndexes() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Unique index on plugin name
	_, err := r.pluginCollection.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.M{"name": 1},
		Options: options.Index().SetUnique(true).SetName("unique_plugin_name"),
	})
	if err != nil {
		r.logger.WithError(err).Warn("Failed to create unique index on plugin name")
	}

	// Compound index on user_id and plugin_id for user_plugins
	_, err = r.userPluginCollection.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "user_id", Value: 1}, {Key: "plugin_id", Value: 1}},
		Options: options.Index().SetUnique(true).SetName("unique_user_plugin"),
	})
	if err != nil {
		r.logger.WithError(err).Warn("Failed to create unique index on user_plugin")
	}
}

func (r *PluginRepository) seedBuiltInPlugins() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for _, plugin := range models.BuiltInPlugins {
		plugin.CreatedAt = time.Now()
		plugin.UpdatedAt = time.Now()

		// ReplaceOne: completely replace existing document to ensure all fields are updated
		filter := bson.M{"_id": plugin.ID}
		opts := options.Replace().SetUpsert(true)

		_, err := r.pluginCollection.ReplaceOne(ctx, filter, plugin, opts)
		if err != nil {
			r.logger.WithError(err).WithField("plugin", plugin.ID).Warn("Failed to seed plugin")
		}
	}

	r.logger.WithField("count", len(models.BuiltInPlugins)).Info("Seeded built-in plugins")
}

// ListAll returns all available plugins
func (r *PluginRepository) ListAll(ctx context.Context) ([]models.Plugin, error) {
	cursor, err := r.pluginCollection.Find(ctx, bson.M{},
		options.Find().SetSort(bson.D{{Key: "display_name", Value: 1}}))
	if err != nil {
		return nil, fmt.Errorf("failed to list plugins: %w", err)
	}
	defer func() { _ = cursor.Close(ctx) }()

	var plugins []models.Plugin
	if err := cursor.All(ctx, &plugins); err != nil {
		return nil, fmt.Errorf("failed to decode plugins: %w", err)
	}

	return plugins, nil
}

// GetByID returns a plugin by its ID
func (r *PluginRepository) GetByID(ctx context.Context, id string) (*models.Plugin, error) {
	var plugin models.Plugin
	err := r.pluginCollection.FindOne(ctx, bson.M{"_id": id}).Decode(&plugin)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, fmt.Errorf("plugin not found")
		}
		return nil, fmt.Errorf("failed to get plugin: %w", err)
	}
	return &plugin, nil
}

// GetUserPlugins returns all user plugin associations for a user
func (r *PluginRepository) GetUserPlugins(ctx context.Context, userID primitive.ObjectID) ([]models.UserPlugin, error) {
	cursor, err := r.userPluginCollection.Find(ctx, bson.M{"user_id": userID})
	if err != nil {
		return nil, fmt.Errorf("failed to get user plugins: %w", err)
	}
	defer func() { _ = cursor.Close(ctx) }()

	var userPlugins []models.UserPlugin
	if err := cursor.All(ctx, &userPlugins); err != nil {
		return nil, fmt.Errorf("failed to decode user plugins: %w", err)
	}

	return userPlugins, nil
}

// GetEnabledPlugins returns all enabled plugins for a user
func (r *PluginRepository) GetEnabledPlugins(ctx context.Context, userID primitive.ObjectID) ([]models.Plugin, error) {
	// Get enabled user plugins
	userPlugins, err := r.GetUserPlugins(ctx, userID)
	if err != nil {
		return nil, err
	}

	// Filter to only enabled
	var enabledIDs []string
	for _, up := range userPlugins {
		if up.Status == models.PluginStatusEnabled {
			enabledIDs = append(enabledIDs, up.PluginID)
		}
	}

	if len(enabledIDs) == 0 {
		return []models.Plugin{}, nil
	}

	// Get the actual plugins
	cursor, err := r.pluginCollection.Find(ctx, bson.M{"_id": bson.M{"$in": enabledIDs}})
	if err != nil {
		return nil, fmt.Errorf("failed to get enabled plugins: %w", err)
	}
	defer func() { _ = cursor.Close(ctx) }()

	var plugins []models.Plugin
	if err := cursor.All(ctx, &plugins); err != nil {
		return nil, fmt.Errorf("failed to decode plugins: %w", err)
	}

	return plugins, nil
}

// EnablePlugin enables a plugin for a user
func (r *PluginRepository) EnablePlugin(ctx context.Context, userID primitive.ObjectID, pluginID string) error {
	// Verify plugin exists
	_, err := r.GetByID(ctx, pluginID)
	if err != nil {
		return err
	}

	// Upsert user plugin association
	filter := bson.M{"user_id": userID, "plugin_id": pluginID}
	update := bson.M{
		"$set": bson.M{
			"status":     models.PluginStatusEnabled,
			"enabled_at": time.Now(),
			"updated_at": time.Now(),
		},
		"$setOnInsert": bson.M{
			"user_id":   userID,
			"plugin_id": pluginID,
		},
	}
	opts := options.Update().SetUpsert(true)

	_, err = r.userPluginCollection.UpdateOne(ctx, filter, update, opts)
	if err != nil {
		return fmt.Errorf("failed to enable plugin: %w", err)
	}

	return nil
}

// DisablePlugin disables a plugin for a user
func (r *PluginRepository) DisablePlugin(ctx context.Context, userID primitive.ObjectID, pluginID string) error {
	filter := bson.M{"user_id": userID, "plugin_id": pluginID}
	update := bson.M{
		"$set": bson.M{
			"status":     models.PluginStatusDisabled,
			"updated_at": time.Now(),
		},
	}

	result, err := r.userPluginCollection.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("failed to disable plugin: %w", err)
	}

	if result.MatchedCount == 0 {
		return fmt.Errorf("plugin not enabled for this user")
	}

	return nil
}

// IsPluginEnabled checks if a plugin is enabled for a user
func (r *PluginRepository) IsPluginEnabled(ctx context.Context, userID primitive.ObjectID, pluginID string) (bool, error) {
	var userPlugin models.UserPlugin
	err := r.userPluginCollection.FindOne(ctx, bson.M{
		"user_id":   userID,
		"plugin_id": pluginID,
		"status":    models.PluginStatusEnabled,
	}).Decode(&userPlugin)

	if err == mongo.ErrNoDocuments {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to check plugin status: %w", err)
	}

	return true, nil
}

// GetPluginsWithStatus returns all plugins with their enabled status for a user
func (r *PluginRepository) GetPluginsWithStatus(ctx context.Context, userID primitive.ObjectID) ([]PluginWithStatus, error) {
	plugins, err := r.ListAll(ctx)
	if err != nil {
		return nil, err
	}

	userPlugins, err := r.GetUserPlugins(ctx, userID)
	if err != nil {
		return nil, err
	}

	// Build a map for quick lookup
	statusMap := make(map[string]models.UserPlugin)
	for _, up := range userPlugins {
		statusMap[up.PluginID] = up
	}

	var result []PluginWithStatus
	for _, plugin := range plugins {
		status := PluginWithStatus{
			Plugin:  plugin,
			Enabled: false,
		}
		if up, ok := statusMap[plugin.ID]; ok && up.Status == models.PluginStatusEnabled {
			status.Enabled = true
			status.EnabledAt = up.EnabledAt
		}
		result = append(result, status)
	}

	return result, nil
}

// PluginWithStatus represents a plugin along with its enabled status for a user
type PluginWithStatus struct {
	models.Plugin
	Enabled   bool      `json:"enabled"`
	EnabledAt time.Time `json:"enabledAt,omitempty"`
}
