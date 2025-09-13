package services

import (
	"context"
	"errors"
	"time"

	"github.com/KubeOrch/core/database"
	"github.com/KubeOrch/core/models"
	"github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func CreateUser(user *models.User) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Check if this is the first user (make them admin)
	isFirst, err := database.IsFirstUser()
	if err != nil {
		return err
	}

	if isFirst {
		user.Role = models.RoleAdmin
	} else {
		// Default role for non-first users
		if user.Role == "" {
			user.Role = models.RoleUser
		}
	}

	// Set timestamps
	now := time.Now()
	user.CreatedAt = now
	user.UpdatedAt = now

	result, err := database.UserColl.InsertOne(ctx, user)
	if err != nil {
		return err
	}

	user.ID = result.InsertedID.(primitive.ObjectID)
	return nil
}

func GetUserByEmail(email string) (*models.User, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var user models.User
	filter := bson.M{"email": email, "deleted_at": nil}

	err := database.UserColl.FindOne(ctx, filter).Decode(&user)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, errors.New("user not found")
		}
		return nil, err
	}

	return &user, nil
}

func GetUserByID(id primitive.ObjectID) (*models.User, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var user models.User
	filter := bson.M{"_id": id, "deleted_at": nil}

	err := database.UserColl.FindOne(ctx, filter).Decode(&user)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, errors.New("user not found")
		}
		return nil, err
	}

	return &user, nil
}

func UserExistsByEmail(email string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	count, err := database.UserColl.CountDocuments(ctx, bson.M{"email": email, "deleted_at": nil})
	if err != nil {
		return false, err
	}

	return count > 0, nil
}

func UpdateUser(user *models.User) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	user.UpdatedAt = time.Now()

	filter := bson.M{"_id": user.ID}
	update := bson.M{"$set": user}

	_, err := database.UserColl.UpdateOne(ctx, filter, update)
	return err
}

func DeleteUser(id primitive.ObjectID) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Soft delete - set deleted_at timestamp
	now := time.Now()
	filter := bson.M{"_id": id}
	update := bson.M{
		"$set": bson.M{
			"deleted_at": now,
			"updated_at": now,
		},
	}

	_, err := database.UserColl.UpdateOne(ctx, filter, update)
	return err
}

func CountUsers() (int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	return database.UserColl.CountDocuments(ctx, bson.M{"deleted_at": nil})
}

func GetUserCount() (int64, error) {
	return CountUsers()
}

func GetAllUsers() ([]models.User, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var users []models.User
	cursor, err := database.UserColl.Find(ctx, bson.M{"deleted_at": nil})
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := cursor.Close(ctx); err != nil {
			logrus.WithError(err).Warn("Failed to close cursor")
		}
	}()

	if err = cursor.All(ctx, &users); err != nil {
		return nil, err
	}

	return users, nil
}

func UpdateUserName(id primitive.ObjectID, name string) (*models.User, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	filter := bson.M{"_id": id, "deleted_at": nil}
	update := bson.M{
		"$set": bson.M{
			"name":       name,
			"updated_at": time.Now(),
		},
	}

	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
	var user models.User
	err := database.UserColl.FindOneAndUpdate(ctx, filter, update, opts).Decode(&user)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, errors.New("user not found")
		}
		return nil, err
	}
	
	return &user, nil
}
