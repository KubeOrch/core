package repositories

import (
	"context"
	"time"

	"github.com/KubeOrch/core/database"
	"github.com/KubeOrch/core/models"
	"github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type ResourceRepository struct {
	collection        *mongo.Collection
	historyCollection *mongo.Collection
	accessCollection  *mongo.Collection
	logger            *logrus.Logger
}

func NewResourceRepository() *ResourceRepository {
	db := database.GetDB()
	return &ResourceRepository{
		collection:        db.Collection("resources"),
		historyCollection: db.Collection("resource_history"),
		accessCollection:  db.Collection("resource_access"),
		logger:            logrus.New(),
	}
}

// CreateOrUpdate creates a new resource or updates existing one
func (r *ResourceRepository) CreateOrUpdate(ctx context.Context, resource *models.Resource) error {
	filter := bson.M{
		"userId":      resource.UserID,
		"clusterName": resource.ClusterName,
		"namespace":   resource.Namespace,
		"name":        resource.Name,
		"type":        resource.Type,
	}

	// Check if resource exists
	var existing models.Resource
	err := r.collection.FindOne(ctx, filter).Decode(&existing)

	if err == mongo.ErrNoDocuments {
		// New resource - create it
		resource.FirstDiscoveredAt = time.Now()
		resource.LastSyncedAt = time.Now()
		resource.LastSeenAt = time.Now()

		result, err := r.collection.InsertOne(ctx, resource)
		if err != nil {
			return err
		}
		resource.ID = result.InsertedID.(primitive.ObjectID)

		// Record creation in history
		r.recordHistory(ctx, resource.ID, resource.UserID, "created", nil, "", resource.Status, "Resource discovered")
		return nil
	}

	if err != nil {
		return err
	}

	// Update existing resource
	resource.ID = existing.ID
	resource.FirstDiscoveredAt = existing.FirstDiscoveredAt
	resource.LastSyncedAt = time.Now()
	resource.LastSeenAt = time.Now()

	// Keep user-specific fields
	resource.UserTags = existing.UserTags
	resource.UserNotes = existing.UserNotes
	resource.IsFavorite = existing.IsFavorite

	// Track status change
	if existing.Status != resource.Status {
		r.recordHistory(ctx, existing.ID, resource.UserID, "status_changed", nil, existing.Status, resource.Status, "Status changed")
	}

	// Update the resource
	update := bson.M{"$set": resource}
	_, err = r.collection.UpdateOne(ctx, filter, update)

	return err
}

// GetByID retrieves a resource by ID
func (r *ResourceRepository) GetByID(ctx context.Context, id primitive.ObjectID, userID primitive.ObjectID) (*models.Resource, error) {
	var resource models.Resource
	filter := bson.M{
		"_id":    id,
		"userId": userID,
	}

	err := r.collection.FindOne(ctx, filter).Decode(&resource)
	if err != nil {
		return nil, err
	}

	// Record access
	r.recordAccess(ctx, id, userID, "viewed", nil)

	return &resource, nil
}

// List retrieves resources with filtering
func (r *ResourceRepository) List(ctx context.Context, userID primitive.ObjectID, filter bson.M, opts ...*options.FindOptions) ([]*models.Resource, error) {
	queryFilter := make(bson.M, len(filter)+2)
	for k, v := range filter {
		queryFilter[k] = v
	}
	queryFilter["userId"] = userID
	if _, exists := queryFilter["deletedAt"]; !exists {
		queryFilter["deletedAt"] = bson.M{"$exists": false}
	}

	cursor, err := r.collection.Find(ctx, queryFilter, opts...)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = cursor.Close(ctx)
	}()

	var resources []*models.Resource
	if err = cursor.All(ctx, &resources); err != nil {
		return nil, err
	}

	return resources, nil
}

// Count returns the total number of resources matching the filter
func (r *ResourceRepository) Count(ctx context.Context, userID primitive.ObjectID, filter bson.M) (int64, error) {
	queryFilter := make(bson.M, len(filter)+2)
	for k, v := range filter {
		queryFilter[k] = v
	}
	queryFilter["userId"] = userID
	if _, exists := queryFilter["deletedAt"]; !exists {
		queryFilter["deletedAt"] = bson.M{"$exists": false}
	}

	return r.collection.CountDocuments(ctx, queryFilter)
}

// MarkDeleted marks resources as deleted that weren't seen in latest sync
func (r *ResourceRepository) MarkDeleted(ctx context.Context, userID, clusterID primitive.ObjectID, syncTime time.Time) error {
	filter := bson.M{
		"userId":    userID,
		"clusterId": clusterID,
		"lastSeenAt": bson.M{"$lt": syncTime},
		"deletedAt": bson.M{"$exists": false},
	}

	update := bson.M{
		"$set": bson.M{
			"deletedAt": syncTime,
			"status":    models.ResourceStatusDeleted,
		},
	}

	result, err := r.collection.UpdateMany(ctx, filter, update)
	if err != nil {
		return err
	}

	// Record deletion in history for each resource
	if result.ModifiedCount > 0 {
		cursor, err := r.collection.Find(ctx, filter)
		if err != nil {
			return err
		}
		defer func() {
			_ = cursor.Close(ctx)
		}()

		var resources []*models.Resource
		if err = cursor.All(ctx, &resources); err != nil {
			return err
		}

		for _, resource := range resources {
			r.recordHistory(ctx, resource.ID, userID, "deleted", nil, resource.Status, models.ResourceStatusDeleted, "Resource no longer exists in cluster")
		}
	}

	return nil
}

// UpdateUserFields updates user-specific fields (tags, notes, favorites)
func (r *ResourceRepository) UpdateUserFields(ctx context.Context, id, userID primitive.ObjectID, updates bson.M) error {
	filter := bson.M{
		"_id":    id,
		"userId": userID,
	}

	_, err := r.collection.UpdateOne(ctx, filter, bson.M{"$set": updates})
	return err
}

// GetFavorites retrieves user's favorite resources
func (r *ResourceRepository) GetFavorites(ctx context.Context, userID primitive.ObjectID) ([]*models.Resource, error) {
	filter := bson.M{
		"userId":     userID,
		"isFavorite": true,
		"deletedAt":  bson.M{"$exists": false},
	}

	return r.List(ctx, userID, filter)
}

// GetByCluster retrieves all resources for a specific cluster
func (r *ResourceRepository) GetByCluster(ctx context.Context, userID, clusterID primitive.ObjectID) ([]*models.Resource, error) {
	filter := bson.M{
		"userId":    userID,
		"clusterId": clusterID,
		"deletedAt": bson.M{"$exists": false},
	}

	return r.List(ctx, userID, filter)
}

// GetHistory retrieves resource history
func (r *ResourceRepository) GetHistory(ctx context.Context, resourceID primitive.ObjectID, limit int64) ([]*models.ResourceHistory, error) {
	opts := options.Find().
		SetSort(bson.M{"timestamp": -1}).
		SetLimit(limit)

	cursor, err := r.historyCollection.Find(ctx, bson.M{"resourceId": resourceID}, opts)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = cursor.Close(ctx)
	}()

	var history []*models.ResourceHistory
	if err = cursor.All(ctx, &history); err != nil {
		return nil, err
	}

	return history, nil
}

// SearchResources searches resources by name or labels
func (r *ResourceRepository) SearchResources(ctx context.Context, userID primitive.ObjectID, query string) ([]*models.Resource, error) {
	filter := bson.M{
		"userId": userID,
		"deletedAt": bson.M{"$exists": false},
		"$text": bson.M{"$search": query},
	}

	return r.List(ctx, userID, filter)
}

// GetResourceStats gets statistics about resources
func (r *ResourceRepository) GetResourceStats(ctx context.Context, userID primitive.ObjectID) (map[string]interface{}, error) {
	pipeline := []bson.M{
		{"$match": bson.M{
			"userId": userID,
			"deletedAt": bson.M{"$exists": false},
		}},
		{"$group": bson.M{
			"_id": nil,
			"totalResources": bson.M{"$sum": 1},
			"byType": bson.M{"$push": "$type"},
			"byStatus": bson.M{"$push": "$status"},
			"byCluster": bson.M{"$push": "$clusterName"},
		}},
		{"$project": bson.M{
			"_id": 0,
			"total": "$totalResources",
			"types": bson.M{"$setUnion": []string{"$byType"}},
			"statuses": bson.M{"$setUnion": []string{"$byStatus"}},
			"clusters": bson.M{"$setUnion": []string{"$byCluster"}},
		}},
	}

	cursor, err := r.collection.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = cursor.Close(ctx)
	}()

	var results []map[string]interface{}
	if err = cursor.All(ctx, &results); err != nil {
		return nil, err
	}

	if len(results) == 0 {
		return map[string]interface{}{
			"total": 0,
			"types": []string{},
			"statuses": []string{},
			"clusters": []string{},
		}, nil
	}

	return results[0], nil
}

// Helper function to record history
func (r *ResourceRepository) recordHistory(ctx context.Context, resourceID, userID primitive.ObjectID, action string, changes map[string]interface{}, oldStatus, newStatus models.ResourceStatus, message string) {
	history := &models.ResourceHistory{
		ResourceID: resourceID,
		UserID:     userID,
		Action:     action,
		Changes:    changes,
		OldStatus:  oldStatus,
		NewStatus:  newStatus,
		Timestamp:  time.Now(),
		Message:    message,
	}

	if _, err := r.historyCollection.InsertOne(ctx, history); err != nil {
		r.logger.WithError(err).Warn("Failed to insert resource history")
	}
}

// Helper function to record access
func (r *ResourceRepository) recordAccess(ctx context.Context, resourceID, userID primitive.ObjectID, action string, details map[string]string) {
	access := &models.ResourceAccess{
		ResourceID: resourceID,
		UserID:     userID,
		Action:     action,
		Timestamp:  time.Now(),
		Details:    details,
	}

	if _, err := r.accessCollection.InsertOne(ctx, access); err != nil {
		r.logger.WithError(err).Warn("Failed to insert resource access log")
	}
}

// RecordAccess exposes the recordAccess method for external use
func (r *ResourceRepository) RecordAccess(ctx context.Context, resourceID, userID primitive.ObjectID, action string, details map[string]string) {
	r.recordAccess(ctx, resourceID, userID, action, details)
}

// CreateIndexes creates necessary indexes for the resource collections
func (r *ResourceRepository) CreateIndexes(ctx context.Context) error {
	// Resource collection indexes
	resourceIndexes := []mongo.IndexModel{
		{
			Keys: bson.D{
				{Key: "userId", Value: 1},
				{Key: "clusterName", Value: 1},
				{Key: "namespace", Value: 1},
				{Key: "name", Value: 1},
				{Key: "type", Value: 1},
			},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: bson.D{
				{Key: "userId", Value: 1},
				{Key: "deletedAt", Value: 1},
			},
		},
		{
			Keys: bson.D{
				{Key: "userId", Value: 1},
				{Key: "isFavorite", Value: 1},
			},
		},
		{
			Keys: bson.D{
				{Key: "userId", Value: 1},
				{Key: "clusterId", Value: 1},
			},
		},
		{
			Keys: bson.D{
				{Key: "lastSeenAt", Value: 1},
			},
		},
		{
			Keys: bson.D{
				{Key: "name", Value: "text"},
				{Key: "namespace", Value: "text"},
				{Key: "userTags", Value: "text"},
			},
		},
	}

	if _, err := r.collection.Indexes().CreateMany(ctx, resourceIndexes); err != nil {
		return err
	}

	// History collection indexes
	historyIndexes := []mongo.IndexModel{
		{
			Keys: bson.D{
				{Key: "resourceId", Value: 1},
				{Key: "timestamp", Value: -1},
			},
		},
		{
			Keys: bson.D{
				{Key: "userId", Value: 1},
				{Key: "timestamp", Value: -1},
			},
		},
		{
			Keys: bson.D{
				{Key: "timestamp", Value: 1},
			},
			Options: options.Index().SetExpireAfterSeconds(30 * 24 * 60 * 60), // 30 days TTL
		},
	}

	if _, err := r.historyCollection.Indexes().CreateMany(ctx, historyIndexes); err != nil {
		return err
	}

	// Access collection indexes
	accessIndexes := []mongo.IndexModel{
		{
			Keys: bson.D{
				{Key: "resourceId", Value: 1},
				{Key: "timestamp", Value: -1},
			},
		},
		{
			Keys: bson.D{
				{Key: "userId", Value: 1},
				{Key: "timestamp", Value: -1},
			},
		},
		{
			Keys: bson.D{
				{Key: "timestamp", Value: 1},
			},
			Options: options.Index().SetExpireAfterSeconds(7 * 24 * 60 * 60), // 7 days TTL
		},
	}

	if _, err := r.accessCollection.Indexes().CreateMany(ctx, accessIndexes); err != nil {
		return err
	}

	return nil
}