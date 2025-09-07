package repositories

import (
	"context"
	"fmt"
	"time"

	"github.com/KubeOrch/core/database"
	"github.com/KubeOrch/core/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type ClusterRepository struct {
	collection *mongo.Collection
	accessCol  *mongo.Collection
	logCol     *mongo.Collection
}

func NewClusterRepository() *ClusterRepository {
	db := database.GetDB()
	return &ClusterRepository{
		collection: db.Collection("clusters"),
		accessCol:  db.Collection("cluster_access"),
		logCol:     db.Collection("cluster_logs"),
	}
}

func (r *ClusterRepository) Create(ctx context.Context, cluster *models.Cluster) error {
	cluster.CreatedAt = time.Now()
	cluster.UpdatedAt = time.Now()
	cluster.Status = models.ClusterStatusUnknown
	
	result, err := r.collection.InsertOne(ctx, cluster)
	if err != nil {
		return fmt.Errorf("failed to create cluster: %w", err)
	}
	
	cluster.ID = result.InsertedID.(primitive.ObjectID)
	return nil
}

func (r *ClusterRepository) GetByID(ctx context.Context, id primitive.ObjectID) (*models.Cluster, error) {
	var cluster models.Cluster
	err := r.collection.FindOne(ctx, bson.M{"_id": id}).Decode(&cluster)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, fmt.Errorf("cluster not found")
		}
		return nil, fmt.Errorf("failed to get cluster: %w", err)
	}
	return &cluster, nil
}

func (r *ClusterRepository) GetByName(ctx context.Context, name string, userID primitive.ObjectID) (*models.Cluster, error) {
	var cluster models.Cluster
	filter := bson.M{
		"name":    name,
		"$or": []bson.M{
			{"user_id": userID},
			{"shared_with": userID},
		},
	}
	
	err := r.collection.FindOne(ctx, filter).Decode(&cluster)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, fmt.Errorf("cluster not found")
		}
		return nil, fmt.Errorf("failed to get cluster: %w", err)
	}
	return &cluster, nil
}

func (r *ClusterRepository) ListByUser(ctx context.Context, userID primitive.ObjectID) ([]*models.Cluster, error) {
	filter := bson.M{
		"$or": []bson.M{
			{"user_id": userID},
			{"shared_with": userID},
		},
	}
	
	cursor, err := r.collection.Find(ctx, filter, options.Find().SetSort(bson.D{{"created_at", -1}}))
	if err != nil {
		return nil, fmt.Errorf("failed to list clusters: %w", err)
	}
	defer cursor.Close(ctx)
	
	var clusters []*models.Cluster
	if err := cursor.All(ctx, &clusters); err != nil {
		return nil, fmt.Errorf("failed to decode clusters: %w", err)
	}
	
	return clusters, nil
}

func (r *ClusterRepository) ListByOrganization(ctx context.Context, orgID primitive.ObjectID) ([]*models.Cluster, error) {
	filter := bson.M{"org_id": orgID}
	
	cursor, err := r.collection.Find(ctx, filter, options.Find().SetSort(bson.D{{"created_at", -1}}))
	if err != nil {
		return nil, fmt.Errorf("failed to list organization clusters: %w", err)
	}
	defer cursor.Close(ctx)
	
	var clusters []*models.Cluster
	if err := cursor.All(ctx, &clusters); err != nil {
		return nil, fmt.Errorf("failed to decode clusters: %w", err)
	}
	
	return clusters, nil
}

func (r *ClusterRepository) Update(ctx context.Context, id primitive.ObjectID, update bson.M) error {
	update["updated_at"] = time.Now()
	
	_, err := r.collection.UpdateOne(
		ctx,
		bson.M{"_id": id},
		bson.M{"$set": update},
	)
	
	if err != nil {
		return fmt.Errorf("failed to update cluster: %w", err)
	}
	
	return nil
}

func (r *ClusterRepository) UpdateStatus(ctx context.Context, id primitive.ObjectID, status models.ClusterStatus) error {
	return r.Update(ctx, id, bson.M{
		"status":     status,
		"last_check": time.Now(),
	})
}

func (r *ClusterRepository) UpdateMetadata(ctx context.Context, id primitive.ObjectID, metadata models.ClusterMetadata) error {
	metadata.LastUpdated = time.Now()
	return r.Update(ctx, id, bson.M{"metadata": metadata})
}

func (r *ClusterRepository) Delete(ctx context.Context, id primitive.ObjectID) error {
	_, err := r.collection.DeleteOne(ctx, bson.M{"_id": id})
	if err != nil {
		return fmt.Errorf("failed to delete cluster: %w", err)
	}
	
	_, _ = r.accessCol.DeleteMany(ctx, bson.M{"cluster_id": id})
	
	return nil
}

func (r *ClusterRepository) SetDefault(ctx context.Context, userID, clusterID primitive.ObjectID) error {
	_, err := r.collection.UpdateMany(
		ctx,
		bson.M{"user_id": userID},
		bson.M{"$set": bson.M{"default": false}},
	)
	if err != nil {
		return fmt.Errorf("failed to unset default clusters: %w", err)
	}
	
	return r.Update(ctx, clusterID, bson.M{"default": true})
}

func (r *ClusterRepository) GetDefault(ctx context.Context, userID primitive.ObjectID) (*models.Cluster, error) {
	var cluster models.Cluster
	filter := bson.M{
		"default": true,
		"$or": []bson.M{
			{"user_id": userID},
			{"shared_with": userID},
		},
	}
	
	err := r.collection.FindOne(ctx, filter).Decode(&cluster)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get default cluster: %w", err)
	}
	
	return &cluster, nil
}

func (r *ClusterRepository) GrantAccess(ctx context.Context, access *models.ClusterAccess) error {
	access.GrantedAt = time.Now()
	
	_, err := r.accessCol.InsertOne(ctx, access)
	if err != nil {
		return fmt.Errorf("failed to grant cluster access: %w", err)
	}
	
	_, err = r.collection.UpdateOne(
		ctx,
		bson.M{"_id": access.ClusterID},
		bson.M{"$addToSet": bson.M{"shared_with": access.UserID}},
	)
	
	return err
}

func (r *ClusterRepository) RevokeAccess(ctx context.Context, clusterID, userID primitive.ObjectID) error {
	_, err := r.accessCol.DeleteMany(ctx, bson.M{
		"cluster_id": clusterID,
		"user_id":    userID,
	})
	if err != nil {
		return fmt.Errorf("failed to revoke cluster access: %w", err)
	}
	
	_, err = r.collection.UpdateOne(
		ctx,
		bson.M{"_id": clusterID},
		bson.M{"$pull": bson.M{"shared_with": userID}},
	)
	
	return err
}

func (r *ClusterRepository) GetUserAccess(ctx context.Context, clusterID, userID primitive.ObjectID) (*models.ClusterAccess, error) {
	var access models.ClusterAccess
	err := r.accessCol.FindOne(ctx, bson.M{
		"cluster_id": clusterID,
		"user_id":    userID,
	}).Decode(&access)
	
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get user access: %w", err)
	}
	
	return &access, nil
}

func (r *ClusterRepository) LogConnection(ctx context.Context, log *models.ClusterConnectionLog) error {
	log.Timestamp = time.Now()
	
	_, err := r.logCol.InsertOne(ctx, log)
	if err != nil {
		return fmt.Errorf("failed to log connection: %w", err)
	}
	
	return nil
}

func (r *ClusterRepository) GetConnectionLogs(ctx context.Context, clusterID primitive.ObjectID, limit int64) ([]*models.ClusterConnectionLog, error) {
	opts := options.Find().
		SetSort(bson.D{{"timestamp", -1}}).
		SetLimit(limit)
	
	cursor, err := r.logCol.Find(ctx, bson.M{"cluster_id": clusterID}, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to get connection logs: %w", err)
	}
	defer cursor.Close(ctx)
	
	var logs []*models.ClusterConnectionLog
	if err := cursor.All(ctx, &logs); err != nil {
		return nil, fmt.Errorf("failed to decode logs: %w", err)
	}
	
	return logs, nil
}