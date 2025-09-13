package services

import (
	"context"
	"errors"
	"time"

	"github.com/KubeOrch/core/database"
	"github.com/KubeOrch/core/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// CreateWorkflow creates a new workflow
func CreateWorkflow(workflow *models.Workflow) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Set timestamps
	now := time.Now()
	workflow.CreatedAt = now
	workflow.UpdatedAt = now
	workflow.CurrentVersion = 1
	workflow.Status = models.WorkflowStatusDraft
	workflow.RunCount = 0
	workflow.SuccessCount = 0
	workflow.FailureCount = 0

	// Initialize empty nodes and edges if not provided
	if workflow.Nodes == nil {
		workflow.Nodes = []models.WorkflowNode{}
	}
	if workflow.Edges == nil {
		workflow.Edges = []models.WorkflowEdge{}
	}
	if workflow.Versions == nil {
		workflow.Versions = []models.WorkflowVersion{}
	}
	if workflow.Tags == nil {
		workflow.Tags = []string{}
	}

	// Create initial version
	initialVersion := models.WorkflowVersion{
		Version:     1,
		Nodes:       workflow.Nodes,
		Edges:       workflow.Edges,
		Description: "Initial version",
		CreatedAt:   now,
		CreatedBy:   workflow.OwnerID.Hex(),
	}
	workflow.Versions = append(workflow.Versions, initialVersion)

	result, err := database.WorkflowColl.InsertOne(ctx, workflow)
	if err != nil {
		return err
	}

	workflow.ID = result.InsertedID.(primitive.ObjectID)
	return nil
}

// GetWorkflowByID retrieves a workflow by its ID
func GetWorkflowByID(id primitive.ObjectID) (*models.Workflow, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var workflow models.Workflow
	filter := bson.M{"_id": id, "deleted_at": nil}

	err := database.WorkflowColl.FindOne(ctx, filter).Decode(&workflow)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, errors.New("workflow not found")
		}
		return nil, err
	}

	return &workflow, nil
}

// UpdateWorkflow updates a workflow
func UpdateWorkflow(id primitive.ObjectID, updates bson.M) (*models.Workflow, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Add updated timestamp
	updates["updated_at"] = time.Now()

	filter := bson.M{"_id": id, "deleted_at": nil}
	update := bson.M{"$set": updates}

	var workflow models.Workflow
	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
	err := database.WorkflowColl.FindOneAndUpdate(ctx, filter, update, opts).Decode(&workflow)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, errors.New("workflow not found")
		}
		return nil, err
	}

	return &workflow, nil
}

// SaveWorkflowVersion saves the current state as a new version
func SaveWorkflowVersion(id primitive.ObjectID, nodes []models.WorkflowNode, edges []models.WorkflowEdge, description string, userID primitive.ObjectID) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Get current workflow to determine next version number
	workflow, err := GetWorkflowByID(id)
	if err != nil {
		return err
	}

	newVersion := models.WorkflowVersion{
		Version:     workflow.CurrentVersion + 1,
		Nodes:       nodes,
		Edges:       edges,
		Description: description,
		CreatedAt:   time.Now(),
		CreatedBy:   userID,
	}

	filter := bson.M{"_id": id}
	update := bson.M{
		"$set": bson.M{
			"nodes":           nodes,
			"edges":           edges,
			"current_version": newVersion.Version,
			"updated_at":      time.Now(),
		},
		"$push": bson.M{
			"versions": newVersion,
		},
	}

	_, err = database.WorkflowColl.UpdateOne(ctx, filter, update)
	return err
}

// ListWorkflows lists all workflows for a user
func ListWorkflows(ownerID primitive.ObjectID) ([]models.Workflow, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	filter := bson.M{
		"owner_id":   ownerID,
		"deleted_at": nil,
	}

	cursor, err := database.WorkflowColl.Find(ctx, filter, options.Find().SetSort(bson.D{{Key: "updated_at", Value: -1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var workflows []models.Workflow
	if err = cursor.All(ctx, &workflows); err != nil {
		return nil, err
	}

	return workflows, nil
}

// DeleteWorkflow soft deletes a workflow
func DeleteWorkflow(id primitive.ObjectID) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	now := time.Now()
	filter := bson.M{"_id": id}
	update := bson.M{
		"$set": bson.M{
			"deleted_at": now,
			"updated_at": now,
		},
	}

	result, err := database.WorkflowColl.UpdateOne(ctx, filter, update)
	if err != nil {
		return err
	}

	if result.MatchedCount == 0 {
		return errors.New("workflow not found")
	}

	return nil
}

// CloneWorkflow creates a copy of an existing workflow
func CloneWorkflow(id primitive.ObjectID, newName string, ownerID primitive.ObjectID) (*models.Workflow, error) {
	// Get original workflow
	original, err := GetWorkflowByID(id)
	if err != nil {
		return nil, err
	}

	// Create new workflow with copied data
	cloned := &models.Workflow{
		Name:        newName,
		Description: original.Description + " (Cloned)",
		Status:      models.WorkflowStatusDraft,
		Tags:        original.Tags,
		Nodes:       original.Nodes,
		Edges:       original.Edges,
		OwnerID:     ownerID,
		ClusterID:   original.ClusterID,
		TeamID:      original.TeamID,
	}

	err = CreateWorkflow(cloned)
	if err != nil {
		return nil, err
	}

	return cloned, nil
}

// UpdateWorkflowStatus updates the status of a workflow
func UpdateWorkflowStatus(id primitive.ObjectID, status models.WorkflowStatus) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	filter := bson.M{"_id": id}
	update := bson.M{
		"$set": bson.M{
			"status":     status,
			"updated_at": time.Now(),
		},
	}

	result, err := database.WorkflowColl.UpdateOne(ctx, filter, update)
	if err != nil {
		return err
	}

	if result.MatchedCount == 0 {
		return errors.New("workflow not found")
	}

	return nil
}

// RecordWorkflowRun records a workflow execution
func RecordWorkflowRun(run *models.WorkflowRun) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := database.WorkflowRunColl.InsertOne(ctx, run)
	if err != nil {
		return err
	}

	run.ID = result.InsertedID.(primitive.ObjectID)

	// Update workflow statistics
	updateFilter := bson.M{"_id": run.WorkflowID}
	updateDoc := bson.M{
		"$inc": bson.M{
			"run_count": 1,
		},
		"$set": bson.M{
			"last_run_at": run.StartedAt,
		},
	}

	if run.Status == "completed" {
		updateDoc["$inc"].(bson.M)["success_count"] = 1
	} else if run.Status == "failed" {
		updateDoc["$inc"].(bson.M)["failure_count"] = 1
	}

	_, err = database.WorkflowColl.UpdateOne(ctx, updateFilter, updateDoc)
	return err
}

// GetWorkflowRuns gets execution history for a workflow
func GetWorkflowRuns(workflowID primitive.ObjectID, limit int) ([]models.WorkflowRun, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	filter := bson.M{"workflow_id": workflowID}
	opts := options.Find().SetSort(bson.D{{Key: "started_at", Value: -1}}).SetLimit(int64(limit))

	cursor, err := database.WorkflowRunColl.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var runs []models.WorkflowRun
	if err = cursor.All(ctx, &runs); err != nil {
		return nil, err
	}

	return runs, nil
}