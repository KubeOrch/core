package database

import (
	"context"
	"fmt"
	"time"

	"github.com/KubeOrch/core/utils/config"
	"github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var (
	Client           *mongo.Client
	Database         *mongo.Database
	UserColl         *mongo.Collection
	WorkflowColl     *mongo.Collection
	WorkflowRunColl  *mongo.Collection
)

func Connect() error {
	host := config.GetMongoHost()
	port := config.GetMongoPort()
	dbname := config.GetMongoDBName()

	logrus.Infof("Connecting to MongoDB: host=%s, port=%s, dbname=%s", host, port, dbname)

	uri := fmt.Sprintf("mongodb://%s:%s", host, port)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	clientOptions := options.Client().ApplyURI(uri)

	client, err := mongo.Connect(ctx, clientOptions)
	if err != nil {
		logrus.Errorf("Could not connect to MongoDB: %v", err)
		return err
	}

	// Ping the database to verify connection
	if err := client.Ping(ctx, nil); err != nil {
		logrus.Errorf("Could not ping MongoDB: %v", err)
		return err
	}

	Client = client
	Database = client.Database(dbname)
	UserColl = Database.Collection("users")
	WorkflowColl = Database.Collection("workflows")
	WorkflowRunColl = Database.Collection("workflow_runs")

	logrus.Info("MongoDB connection established")

	// Create indexes
	if err := createIndexes(); err != nil {
		logrus.Errorf("Failed to create indexes: %v", err)
		return err
	}

	// Check if this is the first user (admin)
	if err := checkAndCreateFirstAdmin(); err != nil {
		logrus.Errorf("Failed to check/create first admin: %v", err)
		return err
	}

	return nil
}

func createIndexes() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create unique index on email
	emailIndex := mongo.IndexModel{
		Keys:    bson.D{{Key: "email", Value: 1}},
		Options: options.Index().SetUnique(true),
	}

	_, err := UserColl.Indexes().CreateOne(ctx, emailIndex)
	if err != nil {
		return fmt.Errorf("failed to create email index: %v", err)
	}

	logrus.Info("Database indexes created successfully")
	return nil
}

func checkAndCreateFirstAdmin() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Count users in the database
	count, err := UserColl.CountDocuments(ctx, bson.M{})
	if err != nil {
		return err
	}

	if count == 0 {
		logrus.Info("No users found in database. First user to register will be admin.")
	} else {
		logrus.Infof("Found %d existing users in database", count)
	}

	return nil
}

func IsFirstUser() (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	count, err := UserColl.CountDocuments(ctx, bson.M{})
	if err != nil {
		return false, err
	}

	return count == 0, nil
}

func GetDB() *mongo.Database {
	return Database
}

func Close() error {
	if Client != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := Client.Disconnect(ctx); err != nil {
			logrus.Errorf("Could not disconnect from MongoDB: %v", err)
			return err
		}
		logrus.Info("MongoDB connection closed")
	}
	return nil
}
