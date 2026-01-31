package repositories

import (
	"context"
	"time"

	"github.com/KubeOrch/core/database"
	"github.com/KubeOrch/core/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ImportRepository handles database operations for import sessions
type ImportRepository struct {
	collection *mongo.Collection
}

// NewImportRepository creates a new import repository
func NewImportRepository() *ImportRepository {
	db := database.GetDB()
	return &ImportRepository{
		collection: db.Collection("import_sessions"),
	}
}

// Create creates a new import session
func (r *ImportRepository) Create(ctx context.Context, session *models.ImportSession) error {
	session.CreatedAt = time.Now()
	session.Status = models.ImportStatusPending
	session.Progress = 0

	result, err := r.collection.InsertOne(ctx, session)
	if err != nil {
		return err
	}

	session.ID = result.InsertedID.(primitive.ObjectID)
	return nil
}

// GetByID retrieves an import session by ID
func (r *ImportRepository) GetByID(ctx context.Context, id primitive.ObjectID) (*models.ImportSession, error) {
	var session models.ImportSession
	err := r.collection.FindOne(ctx, bson.M{"_id": id}).Decode(&session)
	if err != nil {
		return nil, err
	}
	return &session, nil
}

// UpdateStatus updates the status, stage, and progress of an import session
func (r *ImportRepository) UpdateStatus(ctx context.Context, id primitive.ObjectID, status models.ImportSessionStatus, stage string, progress int) error {
	update := bson.M{
		"$set": bson.M{
			"status":        status,
			"current_stage": stage,
			"progress":      progress,
		},
	}
	_, err := r.collection.UpdateByID(ctx, id, update)
	return err
}

// SetCompleted marks the session as completed with the analysis result
func (r *ImportRepository) SetCompleted(ctx context.Context, id primitive.ObjectID, analysis *models.ImportAnalysis) error {
	now := time.Now()
	update := bson.M{
		"$set": bson.M{
			"status":        models.ImportStatusCompleted,
			"current_stage": "completed",
			"progress":      100,
			"analysis":      analysis,
			"completed_at":  now,
		},
	}
	_, err := r.collection.UpdateByID(ctx, id, update)
	return err
}

// SetFailed marks the session as failed with an error message
func (r *ImportRepository) SetFailed(ctx context.Context, id primitive.ObjectID, errorMsg, errorStage string) error {
	now := time.Now()
	update := bson.M{
		"$set": bson.M{
			"status":        models.ImportStatusFailed,
			"error_message": errorMsg,
			"error_stage":   errorStage,
			"completed_at":  now,
		},
	}
	_, err := r.collection.UpdateByID(ctx, id, update)
	return err
}

// ListByUser lists import sessions for a user with pagination
func (r *ImportRepository) ListByUser(ctx context.Context, userID primitive.ObjectID, limit, offset int) ([]*models.ImportSession, int64, error) {
	filter := bson.M{"user_id": userID}

	// Get total count
	total, err := r.collection.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, err
	}

	// Find with pagination
	opts := options.Find().
		SetSort(bson.D{{Key: "created_at", Value: -1}}).
		SetLimit(int64(limit)).
		SetSkip(int64(offset))

	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)

	var sessions []*models.ImportSession
	if err := cursor.All(ctx, &sessions); err != nil {
		return nil, 0, err
	}

	return sessions, total, nil
}

// DeleteOldSessions deletes sessions older than the specified duration
func (r *ImportRepository) DeleteOldSessions(ctx context.Context, olderThan time.Duration) (int64, error) {
	cutoff := time.Now().Add(-olderThan)
	filter := bson.M{
		"created_at": bson.M{"$lt": cutoff},
		"status": bson.M{"$in": []models.ImportSessionStatus{
			models.ImportStatusCompleted,
			models.ImportStatusFailed,
		}},
	}

	result, err := r.collection.DeleteMany(ctx, filter)
	if err != nil {
		return 0, err
	}

	return result.DeletedCount, nil
}
