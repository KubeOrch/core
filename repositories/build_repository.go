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

type BuildRepository struct {
	collection *mongo.Collection
}

func NewBuildRepository() *BuildRepository {
	db := database.GetDB()
	repo := &BuildRepository{
		collection: db.Collection("builds"),
	}

	// Create indexes
	repo.initializeIndexes()

	return repo
}

func (r *BuildRepository) initializeIndexes() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Index for user queries (most common)
	userIndex := mongo.IndexModel{
		Keys:    bson.D{{Key: "user_id", Value: 1}, {Key: "created_at", Value: -1}},
		Options: options.Index().SetName("user_builds_index"),
	}

	_, err := r.collection.Indexes().CreateOne(ctx, userIndex)
	if err != nil {
		logrus.WithError(err).Warn("Failed to create user_builds index")
	}

	// Index for workflow queries
	workflowIndex := mongo.IndexModel{
		Keys:    bson.D{{Key: "workflow_id", Value: 1}},
		Options: options.Index().SetName("workflow_builds_index"),
	}

	_, err = r.collection.Indexes().CreateOne(ctx, workflowIndex)
	if err != nil {
		logrus.WithError(err).Warn("Failed to create workflow_builds index")
	}

	// Index for status queries (for monitoring/cleanup)
	statusIndex := mongo.IndexModel{
		Keys:    bson.D{{Key: "status", Value: 1}},
		Options: options.Index().SetName("status_index"),
	}

	_, err = r.collection.Indexes().CreateOne(ctx, statusIndex)
	if err != nil {
		logrus.WithError(err).Warn("Failed to create status index")
	}
}

// Create creates a new build record
func (r *BuildRepository) Create(ctx context.Context, build *models.Build) error {
	build.CreatedAt = time.Now()
	build.Status = models.BuildStatusPending
	build.CurrentStage = "Queued"
	build.Progress = 0

	result, err := r.collection.InsertOne(ctx, build)
	if err != nil {
		return fmt.Errorf("failed to create build: %w", err)
	}

	build.ID = result.InsertedID.(primitive.ObjectID)
	return nil
}

// GetByID retrieves a build by ID
func (r *BuildRepository) GetByID(ctx context.Context, id primitive.ObjectID) (*models.Build, error) {
	var build models.Build
	err := r.collection.FindOne(ctx, bson.M{"_id": id}).Decode(&build)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, fmt.Errorf("build not found")
		}
		return nil, fmt.Errorf("failed to get build: %w", err)
	}
	return &build, nil
}

// GetByWorkflowID retrieves all builds for a workflow
func (r *BuildRepository) GetByWorkflowID(ctx context.Context, workflowID primitive.ObjectID) ([]*models.Build, error) {
	cursor, err := r.collection.Find(ctx, bson.M{"workflow_id": workflowID},
		options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}}))
	if err != nil {
		return nil, fmt.Errorf("failed to find builds by workflow: %w", err)
	}
	defer func() { _ = cursor.Close(ctx) }()

	var builds []*models.Build
	if err := cursor.All(ctx, &builds); err != nil {
		return nil, fmt.Errorf("failed to decode builds: %w", err)
	}

	return builds, nil
}

// ListByUser retrieves builds for a user with pagination
func (r *BuildRepository) ListByUser(ctx context.Context, userID primitive.ObjectID, limit, offset int) ([]*models.Build, int64, error) {
	filter := bson.M{"user_id": userID}

	// Get total count
	total, err := r.collection.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count builds: %w", err)
	}

	// Get builds with pagination
	opts := options.Find().
		SetSort(bson.D{{Key: "created_at", Value: -1}}).
		SetLimit(int64(limit)).
		SetSkip(int64(offset))

	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to find builds: %w", err)
	}
	defer func() { _ = cursor.Close(ctx) }()

	var builds []*models.Build
	if err := cursor.All(ctx, &builds); err != nil {
		return nil, 0, fmt.Errorf("failed to decode builds: %w", err)
	}

	return builds, total, nil
}

// UpdateStatus updates the build status, stage, and progress
func (r *BuildRepository) UpdateStatus(ctx context.Context, id primitive.ObjectID, status models.BuildStatus, stage string, progress int) error {
	update := bson.M{
		"$set": bson.M{
			"status":        status,
			"current_stage": stage,
			"progress":      progress,
		},
	}

	// Set started_at if this is the first non-pending status
	if status != models.BuildStatusPending {
		update["$set"].(bson.M)["started_at"] = time.Now()
	}

	result, err := r.collection.UpdateOne(ctx, bson.M{"_id": id}, update)
	if err != nil {
		return fmt.Errorf("failed to update build status: %w", err)
	}

	if result.MatchedCount == 0 {
		return fmt.Errorf("build not found")
	}

	return nil
}

// SetStarted marks the build as started
func (r *BuildRepository) SetStarted(ctx context.Context, id primitive.ObjectID) error {
	now := time.Now()
	update := bson.M{
		"$set": bson.M{
			"started_at": now,
		},
	}

	result, err := r.collection.UpdateOne(ctx, bson.M{"_id": id}, update)
	if err != nil {
		return fmt.Errorf("failed to set build started: %w", err)
	}

	if result.MatchedCount == 0 {
		return fmt.Errorf("build not found")
	}

	return nil
}

// SetCompleted marks the build as completed with results
func (r *BuildRepository) SetCompleted(ctx context.Context, id primitive.ObjectID, imageRef, digest string, size int64) error {
	now := time.Now()

	// Get the build to calculate duration
	build, err := r.GetByID(ctx, id)
	if err != nil {
		return err
	}

	var duration int64
	if build.StartedAt != nil {
		duration = now.Sub(*build.StartedAt).Milliseconds()
	}

	update := bson.M{
		"$set": bson.M{
			"status":          models.BuildStatusCompleted,
			"current_stage":   "Completed",
			"progress":        100,
			"final_image_ref": imageRef,
			"image_digest":    digest,
			"image_size":      size,
			"completed_at":    now,
			"duration":        duration,
		},
	}

	result, err := r.collection.UpdateOne(ctx, bson.M{"_id": id}, update)
	if err != nil {
		return fmt.Errorf("failed to set build completed: %w", err)
	}

	if result.MatchedCount == 0 {
		return fmt.Errorf("build not found")
	}

	return nil
}

// SetFailed marks the build as failed with error details
func (r *BuildRepository) SetFailed(ctx context.Context, id primitive.ObjectID, errorMsg, errorStage string) error {
	now := time.Now()

	// Get the build to calculate duration
	build, err := r.GetByID(ctx, id)
	if err != nil {
		return err
	}

	var duration int64
	if build.StartedAt != nil {
		duration = now.Sub(*build.StartedAt).Milliseconds()
	}

	update := bson.M{
		"$set": bson.M{
			"status":        models.BuildStatusFailed,
			"current_stage": "Failed",
			"error_message": errorMsg,
			"error_stage":   errorStage,
			"completed_at":  now,
			"duration":      duration,
		},
	}

	result, err := r.collection.UpdateOne(ctx, bson.M{"_id": id}, update)
	if err != nil {
		return fmt.Errorf("failed to set build failed: %w", err)
	}

	if result.MatchedCount == 0 {
		return fmt.Errorf("build not found")
	}

	return nil
}

// SetCancelled marks the build as cancelled
func (r *BuildRepository) SetCancelled(ctx context.Context, id primitive.ObjectID) error {
	now := time.Now()

	// Get the build to calculate duration
	build, err := r.GetByID(ctx, id)
	if err != nil {
		return err
	}

	var duration int64
	if build.StartedAt != nil {
		duration = now.Sub(*build.StartedAt).Milliseconds()
	}

	update := bson.M{
		"$set": bson.M{
			"status":        models.BuildStatusCancelled,
			"current_stage": "Cancelled",
			"completed_at":  now,
			"duration":      duration,
		},
	}

	result, err := r.collection.UpdateOne(ctx, bson.M{"_id": id}, update)
	if err != nil {
		return fmt.Errorf("failed to set build cancelled: %w", err)
	}

	if result.MatchedCount == 0 {
		return fmt.Errorf("build not found")
	}

	return nil
}

// GetInProgressBuilds gets all builds that are currently running
func (r *BuildRepository) GetInProgressBuilds(ctx context.Context) ([]*models.Build, error) {
	filter := bson.M{
		"status": bson.M{
			"$in": []models.BuildStatus{
				models.BuildStatusPending,
				models.BuildStatusCloning,
				models.BuildStatusBuilding,
				models.BuildStatusPushing,
			},
		},
	}

	cursor, err := r.collection.Find(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("failed to find in-progress builds: %w", err)
	}
	defer func() { _ = cursor.Close(ctx) }()

	var builds []*models.Build
	if err := cursor.All(ctx, &builds); err != nil {
		return nil, fmt.Errorf("failed to decode builds: %w", err)
	}

	return builds, nil
}

// Delete removes a build record (for cleanup)
func (r *BuildRepository) Delete(ctx context.Context, id primitive.ObjectID) error {
	result, err := r.collection.DeleteOne(ctx, bson.M{"_id": id})
	if err != nil {
		return fmt.Errorf("failed to delete build: %w", err)
	}

	if result.DeletedCount == 0 {
		return fmt.Errorf("build not found")
	}

	return nil
}
