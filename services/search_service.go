package services

import (
	"context"
	"time"

	"github.com/KubeOrch/core/database"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// SearchResults contains the unified search results
type SearchResults struct {
	Workflows []WorkflowSearchResult `json:"workflows"`
	Resources []ResourceSearchResult `json:"resources"`
	Clusters  []ClusterSearchResult  `json:"clusters"`
}

// WorkflowSearchResult represents a workflow in search results
type WorkflowSearchResult struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Status      string `json:"status"`
}

// ResourceSearchResult represents a resource in search results
type ResourceSearchResult struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Namespace   string `json:"namespace"`
	Type        string `json:"type"`
	ClusterName string `json:"clusterName"`
}

// ClusterSearchResult represents a cluster in search results
type ClusterSearchResult struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Status      string `json:"status"`
}

// Search performs a unified search across workflows, resources, and clusters
func Search(userID primitive.ObjectID, query string) (*SearchResults, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	results := &SearchResults{
		Workflows: []WorkflowSearchResult{},
		Resources: []ResourceSearchResult{},
		Clusters:  []ClusterSearchResult{},
	}

	// Search workflows
	workflows, err := searchWorkflows(ctx, userID, query)
	if err == nil {
		results.Workflows = workflows
	}

	// Search resources
	resources, err := searchResources(ctx, userID, query)
	if err == nil {
		results.Resources = resources
	}

	// Search clusters
	clusters, err := searchClusters(ctx, userID, query)
	if err == nil {
		results.Clusters = clusters
	}

	return results, nil
}

func searchWorkflows(ctx context.Context, userID primitive.ObjectID, query string) ([]WorkflowSearchResult, error) {
	// Case-insensitive regex search on name and description
	filter := bson.M{
		"owner_id":   userID,
		"deleted_at": nil,
		"$or": []bson.M{
			{"name": bson.M{"$regex": query, "$options": "i"}},
			{"description": bson.M{"$regex": query, "$options": "i"}},
		},
	}

	opts := options.Find().
		SetLimit(5).
		SetSort(bson.D{{Key: "updated_at", Value: -1}}).
		SetProjection(bson.M{
			"_id":         1,
			"name":        1,
			"description": 1,
			"status":      1,
		})

	cursor, err := database.WorkflowColl.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var results []WorkflowSearchResult
	for cursor.Next(ctx) {
		var doc struct {
			ID          primitive.ObjectID `bson:"_id"`
			Name        string             `bson:"name"`
			Description string             `bson:"description"`
			Status      string             `bson:"status"`
		}
		if err := cursor.Decode(&doc); err != nil {
			continue
		}
		results = append(results, WorkflowSearchResult{
			ID:          doc.ID.Hex(),
			Name:        doc.Name,
			Description: doc.Description,
			Status:      doc.Status,
		})
	}

	if results == nil {
		results = []WorkflowSearchResult{}
	}
	return results, nil
}

func searchResources(ctx context.Context, userID primitive.ObjectID, query string) ([]ResourceSearchResult, error) {
	db := database.GetDB()
	resourceColl := db.Collection("resources")

	// Case-insensitive regex search on name and namespace
	filter := bson.M{
		"userId": userID,
		"deletedAt": bson.M{"$exists": false},
		"$or": []bson.M{
			{"name": bson.M{"$regex": query, "$options": "i"}},
			{"namespace": bson.M{"$regex": query, "$options": "i"}},
		},
	}

	opts := options.Find().
		SetLimit(5).
		SetSort(bson.D{{Key: "lastSyncedAt", Value: -1}}).
		SetProjection(bson.M{
			"_id":         1,
			"name":        1,
			"namespace":   1,
			"type":        1,
			"clusterName": 1,
		})

	cursor, err := resourceColl.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var results []ResourceSearchResult
	for cursor.Next(ctx) {
		var doc struct {
			ID          primitive.ObjectID `bson:"_id"`
			Name        string             `bson:"name"`
			Namespace   string             `bson:"namespace"`
			Type        string             `bson:"type"`
			ClusterName string             `bson:"clusterName"`
		}
		if err := cursor.Decode(&doc); err != nil {
			continue
		}
		results = append(results, ResourceSearchResult{
			ID:          doc.ID.Hex(),
			Name:        doc.Name,
			Namespace:   doc.Namespace,
			Type:        doc.Type,
			ClusterName: doc.ClusterName,
		})
	}

	if results == nil {
		results = []ResourceSearchResult{}
	}
	return results, nil
}

func searchClusters(ctx context.Context, userID primitive.ObjectID, query string) ([]ClusterSearchResult, error) {
	db := database.GetDB()
	clusterColl := db.Collection("clusters")

	// Case-insensitive regex search on name and displayName
	filter := bson.M{
		"user_id": userID,
		"$or": []bson.M{
			{"name": bson.M{"$regex": query, "$options": "i"}},
			{"display_name": bson.M{"$regex": query, "$options": "i"}},
		},
	}

	opts := options.Find().
		SetLimit(5).
		SetSort(bson.D{{Key: "updated_at", Value: -1}}).
		SetProjection(bson.M{
			"name":         1,
			"display_name": 1,
			"status":       1,
		})

	cursor, err := clusterColl.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var results []ClusterSearchResult
	for cursor.Next(ctx) {
		var doc struct {
			Name        string `bson:"name"`
			DisplayName string `bson:"display_name"`
			Status      string `bson:"status"`
		}
		if err := cursor.Decode(&doc); err != nil {
			continue
		}
		results = append(results, ClusterSearchResult{
			Name:        doc.Name,
			DisplayName: doc.DisplayName,
			Status:      doc.Status,
		})
	}

	if results == nil {
		results = []ClusterSearchResult{}
	}
	return results, nil
}
