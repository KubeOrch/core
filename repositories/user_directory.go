package repositories

import (
	"context"

	"github.com/KubeOrch/core/database"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type MongoUserDirectory struct{}

func NewMongoUserDirectory() *MongoUserDirectory {
	return &MongoUserDirectory{}
}

func (d *MongoUserDirectory) Exists(ctx context.Context, userID primitive.ObjectID) (bool, error) {
	count, err := database.UserColl.CountDocuments(ctx, bson.M{"_id": userID, "deleted_at": nil})
	return count > 0, err
}
