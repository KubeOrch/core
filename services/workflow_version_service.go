package services

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/KubeOrch/core/database"
	"github.com/KubeOrch/core/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// CreateVersionInput contains the input parameters for creating a version
type CreateVersionInput struct {
	WorkflowID  primitive.ObjectID
	Nodes       []models.WorkflowNode
	Edges       []models.WorkflowEdge
	Description string
	UserID      primitive.ObjectID
	IsAutomatic bool
	Name        string
	Tag         string
	RunID       *primitive.ObjectID
	RunStatus   string
}

// VersionsResponse represents paginated version list response
type VersionsResponse struct {
	Versions []models.WorkflowVersionDoc `json:"versions"`
	Total    int64                       `json:"total"`
	Page     int                         `json:"page"`
	Limit    int                         `json:"limit"`
}

// CreateVersion creates a new version in the workflow_versions collection
func CreateVersion(input CreateVersionInput) (*models.WorkflowVersionDoc, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Get the next version number
	nextVersion, err := getNextVersionNumber(ctx, input.WorkflowID)
	if err != nil {
		return nil, err
	}

	version := &models.WorkflowVersionDoc{
		WorkflowID:  input.WorkflowID,
		Version:     nextVersion,
		Nodes:       input.Nodes,
		Edges:       input.Edges,
		Name:        input.Name,
		Tag:         input.Tag,
		Description: input.Description,
		CreatedAt:   time.Now(),
		CreatedBy:   input.UserID,
		IsAutomatic: input.IsAutomatic,
		RunID:       input.RunID,
		RunStatus:   input.RunStatus,
	}

	result, err := database.WorkflowVersionColl.InsertOne(ctx, version)
	if err != nil {
		return nil, err
	}

	version.ID = result.InsertedID.(primitive.ObjectID)

	// Update the workflow's current_version field
	_, err = database.WorkflowColl.UpdateOne(
		ctx,
		bson.M{"_id": input.WorkflowID},
		bson.M{
			"$set": bson.M{
				"current_version": nextVersion,
				"updated_at":      time.Now(),
			},
		},
	)
	if err != nil {
		return nil, err
	}

	return version, nil
}

// getNextVersionNumber gets the next version number for a workflow
func getNextVersionNumber(ctx context.Context, workflowID primitive.ObjectID) (int, error) {
	// Find the highest version number in the collection
	opts := options.FindOne().SetSort(bson.D{{Key: "version", Value: -1}})
	filter := bson.M{"workflow_id": workflowID}

	var latestVersion models.WorkflowVersionDoc
	err := database.WorkflowVersionColl.FindOne(ctx, filter, opts).Decode(&latestVersion)

	if err == mongo.ErrNoDocuments {
		// No versions exist yet, check if there are embedded versions to migrate
		var workflow models.Workflow
		err = database.WorkflowColl.FindOne(ctx, bson.M{"_id": workflowID}).Decode(&workflow)
		if err != nil {
			return 0, err
		}
		// Use current_version from workflow as starting point
		return workflow.CurrentVersion + 1, nil
	}

	if err != nil {
		return 0, err
	}

	return latestVersion.Version + 1, nil
}

// GetVersions retrieves paginated versions for a workflow
func GetVersions(workflowID primitive.ObjectID, page, limit int) (*VersionsResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	filter := bson.M{"workflow_id": workflowID}

	// Get total count
	total, err := database.WorkflowVersionColl.CountDocuments(ctx, filter)
	if err != nil {
		return nil, err
	}

	// Calculate skip
	skip := (page - 1) * limit

	// Find versions with pagination, sorted by version descending (newest first)
	opts := options.Find().
		SetSort(bson.D{{Key: "version", Value: -1}}).
		SetSkip(int64(skip)).
		SetLimit(int64(limit))

	cursor, err := database.WorkflowVersionColl.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var versions []models.WorkflowVersionDoc
	if err = cursor.All(ctx, &versions); err != nil {
		return nil, err
	}

	if versions == nil {
		versions = []models.WorkflowVersionDoc{}
	}

	return &VersionsResponse{
		Versions: versions,
		Total:    total,
		Page:     page,
		Limit:    limit,
	}, nil
}

// GetVersionByNumber retrieves a specific version by its version number
func GetVersionByNumber(workflowID primitive.ObjectID, versionNum int) (*models.WorkflowVersionDoc, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	filter := bson.M{
		"workflow_id": workflowID,
		"version":     versionNum,
	}

	var version models.WorkflowVersionDoc
	err := database.WorkflowVersionColl.FindOne(ctx, filter).Decode(&version)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, errors.New("version not found")
		}
		return nil, err
	}

	return &version, nil
}

// UpdateVersionMetadataInput contains input for updating version metadata
type UpdateVersionMetadataInput struct {
	Name        *string
	Tag         *string
	Description *string
}

// UpdateVersionMetadata updates name, tag, or description of a version
func UpdateVersionMetadata(workflowID primitive.ObjectID, versionNum int, input UpdateVersionMetadataInput) (*models.WorkflowVersionDoc, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	filter := bson.M{
		"workflow_id": workflowID,
		"version":     versionNum,
	}

	updates := bson.M{}
	if input.Name != nil {
		updates["name"] = *input.Name
	}
	if input.Tag != nil {
		updates["tag"] = *input.Tag
	}
	if input.Description != nil {
		updates["description"] = *input.Description
	}

	if len(updates) == 0 {
		return GetVersionByNumber(workflowID, versionNum)
	}

	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
	var version models.WorkflowVersionDoc
	err := database.WorkflowVersionColl.FindOneAndUpdate(ctx, filter, bson.M{"$set": updates}, opts).Decode(&version)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, errors.New("version not found")
		}
		return nil, err
	}

	return &version, nil
}

// RestoreVersion restores a workflow to a previous version, creating a new version
func RestoreVersion(workflowID primitive.ObjectID, versionNum int, userID primitive.ObjectID) (*models.WorkflowVersionDoc, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Get the version to restore
	sourceVersion, err := GetVersionByNumber(workflowID, versionNum)
	if err != nil {
		return nil, err
	}

	// Get next version number
	nextVersion, err := getNextVersionNumber(ctx, workflowID)
	if err != nil {
		return nil, err
	}

	// Create new version with the restored content
	restoredVersion := &models.WorkflowVersionDoc{
		WorkflowID:   workflowID,
		Version:      nextVersion,
		Nodes:        sourceVersion.Nodes,
		Edges:        sourceVersion.Edges,
		Description:  fmt.Sprintf("Restored from version %d", versionNum),
		CreatedAt:    time.Now(),
		CreatedBy:    userID,
		RestoredFrom: &versionNum,
		IsAutomatic:  false,
	}

	result, err := database.WorkflowVersionColl.InsertOne(ctx, restoredVersion)
	if err != nil {
		return nil, err
	}

	restoredVersion.ID = result.InsertedID.(primitive.ObjectID)

	// Update the workflow with the restored nodes/edges and new current_version
	_, err = database.WorkflowColl.UpdateOne(
		ctx,
		bson.M{"_id": workflowID},
		bson.M{
			"$set": bson.M{
				"nodes":           sourceVersion.Nodes,
				"edges":           sourceVersion.Edges,
				"current_version": nextVersion,
				"updated_at":      time.Now(),
			},
		},
	)
	if err != nil {
		return nil, err
	}

	return restoredVersion, nil
}

// CompareVersions compares two versions and returns the differences
func CompareVersions(workflowID primitive.ObjectID, v1, v2 int) (*models.VersionDiff, error) {
	// Get both versions
	version1, err := GetVersionByNumber(workflowID, v1)
	if err != nil {
		return nil, err
	}

	version2, err := GetVersionByNumber(workflowID, v2)
	if err != nil {
		return nil, err
	}

	diff := &models.VersionDiff{
		FromVersion:   v1,
		ToVersion:     v2,
		AddedNodes:    []models.NodeDiff{},
		RemovedNodes:  []models.NodeDiff{},
		ModifiedNodes: []models.NodeDiff{},
		AddedEdges:    []models.EdgeDiff{},
		RemovedEdges:  []models.EdgeDiff{},
	}

	// Build node maps for comparison
	v1Nodes := make(map[string]models.WorkflowNode)
	v2Nodes := make(map[string]models.WorkflowNode)

	for _, node := range version1.Nodes {
		v1Nodes[node.ID] = node
	}
	for _, node := range version2.Nodes {
		v2Nodes[node.ID] = node
	}

	// Find added and modified nodes
	for id, node := range v2Nodes {
		if oldNode, exists := v1Nodes[id]; exists {
			// Check if modified
			if !nodesEqual(oldNode, node) {
				diff.ModifiedNodes = append(diff.ModifiedNodes, models.NodeDiff{
					NodeID:  id,
					Type:    node.Type,
					OldData: oldNode.Data,
					NewData: node.Data,
				})
			}
		} else {
			// Added
			diff.AddedNodes = append(diff.AddedNodes, models.NodeDiff{
				NodeID:  id,
				Type:    node.Type,
				NewData: node.Data,
			})
		}
	}

	// Find removed nodes
	for id, node := range v1Nodes {
		if _, exists := v2Nodes[id]; !exists {
			diff.RemovedNodes = append(diff.RemovedNodes, models.NodeDiff{
				NodeID:  id,
				Type:    node.Type,
				OldData: node.Data,
			})
		}
	}

	// Build edge maps for comparison
	v1Edges := make(map[string]models.WorkflowEdge)
	v2Edges := make(map[string]models.WorkflowEdge)

	for _, edge := range version1.Edges {
		v1Edges[edge.ID] = edge
	}
	for _, edge := range version2.Edges {
		v2Edges[edge.ID] = edge
	}

	// Find added edges
	for id, edge := range v2Edges {
		if _, exists := v1Edges[id]; !exists {
			diff.AddedEdges = append(diff.AddedEdges, models.EdgeDiff{
				EdgeID: id,
				Source: edge.Source,
				Target: edge.Target,
			})
		}
	}

	// Find removed edges
	for id, edge := range v1Edges {
		if _, exists := v2Edges[id]; !exists {
			diff.RemovedEdges = append(diff.RemovedEdges, models.EdgeDiff{
				EdgeID: id,
				Source: edge.Source,
				Target: edge.Target,
			})
		}
	}

	return diff, nil
}

// nodesEqual checks if two nodes are equal using deep comparison
func nodesEqual(n1, n2 models.WorkflowNode) bool {
	if n1.ID != n2.ID || n1.Type != n2.Type {
		return false
	}
	if n1.Position.X != n2.Position.X || n1.Position.Y != n2.Position.Y {
		return false
	}
	// Use deep equality for comparing the data maps to correctly detect changes
	return reflect.DeepEqual(n1.Data, n2.Data)
}

// GetLatestVersion retrieves the latest version for a workflow
func GetLatestVersion(workflowID primitive.ObjectID) (*models.WorkflowVersionDoc, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	filter := bson.M{"workflow_id": workflowID}
	opts := options.FindOne().SetSort(bson.D{{Key: "version", Value: -1}})

	var version models.WorkflowVersionDoc
	err := database.WorkflowVersionColl.FindOne(ctx, filter, opts).Decode(&version)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, errors.New("no versions found")
		}
		return nil, err
	}

	return &version, nil
}

// MigrateEmbeddedVersions migrates embedded versions from a workflow to the separate collection
func MigrateEmbeddedVersions(workflowID primitive.ObjectID) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Get workflow with embedded versions
	var workflow models.Workflow
	err := database.WorkflowColl.FindOne(ctx, bson.M{"_id": workflowID}).Decode(&workflow)
	if err != nil {
		return err
	}

	if len(workflow.Versions) == 0 {
		return nil // Nothing to migrate
	}

	// Convert embedded versions to version docs
	var versionDocs []interface{}
	for _, v := range workflow.Versions {
		doc := models.WorkflowVersionDoc{
			WorkflowID:  workflowID,
			Version:     v.Version,
			Nodes:       v.Nodes,
			Edges:       v.Edges,
			Description: v.Description,
			CreatedAt:   v.CreatedAt,
			CreatedBy:   v.CreatedBy,
			IsAutomatic: true, // Assume existing versions were automatic
		}
		versionDocs = append(versionDocs, doc)
	}

	// Insert all versions into the separate collection
	_, err = database.WorkflowVersionColl.InsertMany(ctx, versionDocs)
	if err != nil {
		return err
	}

	// Remove embedded versions from workflow document
	_, err = database.WorkflowColl.UpdateOne(
		ctx,
		bson.M{"_id": workflowID},
		bson.M{"$unset": bson.M{"versions": ""}},
	)

	return err
}

// UpdateVersionRunStatus updates the run status for a version
func UpdateVersionRunStatus(versionID primitive.ObjectID, runID primitive.ObjectID, status string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := database.WorkflowVersionColl.UpdateOne(
		ctx,
		bson.M{"_id": versionID},
		bson.M{
			"$set": bson.M{
				"run_id":     runID,
				"run_status": status,
			},
		},
	)
	return err
}
