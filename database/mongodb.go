package database

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/KubeOrch/core/utils/config"
	"github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var (
	Client              *mongo.Client
	Database            *mongo.Database
	UserColl            *mongo.Collection
	WorkflowColl        *mongo.Collection
	WorkflowRunColl     *mongo.Collection
	WorkflowVersionColl *mongo.Collection
	OAuthStateColl      *mongo.Collection
	OAuthCodeColl       *mongo.Collection
)

func Connect() error {
	uri := config.GetMongoURI()

	// Extract database name from URI
	dbname := extractDatabaseFromURI(uri)
	if dbname == "" {
		dbname = "kubeorch" // Default database name
	}

	// Log connection info (without sensitive details)
	if strings.Contains(uri, "@") {
		logrus.Infof("Connecting to MongoDB with authentication, database=%s", dbname)
	} else {
		logrus.Infof("Connecting to MongoDB without authentication, database=%s", dbname)
	}

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
	WorkflowVersionColl = Database.Collection("workflow_versions")
	OAuthStateColl = Database.Collection("oauth_states")
	OAuthCodeColl = Database.Collection("oauth_codes")

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

	// Create indexes for workflow_versions collection
	versionIndexes := []mongo.IndexModel{
		{
			// Unique compound index on workflow_id + version
			Keys:    bson.D{{Key: "workflow_id", Value: 1}, {Key: "version", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
		{
			// Index for listing versions by workflow
			Keys: bson.D{{Key: "workflow_id", Value: 1}, {Key: "created_at", Value: -1}},
		},
	}

	_, err = WorkflowVersionColl.Indexes().CreateMany(ctx, versionIndexes)
	if err != nil {
		return fmt.Errorf("failed to create workflow version indexes: %v", err)
	}

	// Sparse compound index on users for OAuth provider lookup
	providerIndex := mongo.IndexModel{
		Keys: bson.D{
			{Key: "auth_provider", Value: 1},
			{Key: "provider_user_id", Value: 1},
		},
		Options: options.Index().SetSparse(true),
	}
	_, err = UserColl.Indexes().CreateOne(ctx, providerIndex)
	if err != nil {
		return fmt.Errorf("failed to create provider index: %v", err)
	}

	// TTL index on oauth_states — auto-delete after 10 minutes
	oauthStateTTL := int32(600)
	stateIndex := mongo.IndexModel{
		Keys:    bson.D{{Key: "created_at", Value: 1}},
		Options: options.Index().SetExpireAfterSeconds(oauthStateTTL),
	}
	_, err = OAuthStateColl.Indexes().CreateOne(ctx, stateIndex)
	if err != nil {
		return fmt.Errorf("failed to create oauth_states TTL index: %v", err)
	}

	// TTL index on oauth_codes — auto-delete after 30 seconds
	oauthCodeTTL := int32(30)
	codeIndex := mongo.IndexModel{
		Keys:    bson.D{{Key: "created_at", Value: 1}},
		Options: options.Index().SetExpireAfterSeconds(oauthCodeTTL),
	}
	_, err = OAuthCodeColl.Indexes().CreateOne(ctx, codeIndex)
	if err != nil {
		return fmt.Errorf("failed to create oauth_codes TTL index: %v", err)
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

func extractDatabaseFromURI(uri string) string {
	// Parse database name from URI using the standard library for robustness
	// Handles various MongoDB URI formats including mongodb+srv://
	parsedURL, err := url.Parse(uri)
	if err != nil {
		logrus.Warnf("Could not parse MongoDB URI to extract database name: %v", err)
		return ""
	}

	// The path from a valid MongoDB URI will be like "/dbname"
	// We trim the leading slash to get the database name
	dbPath := strings.TrimPrefix(parsedURL.Path, "/")

	// Remove query parameters if present (shouldn't be in path, but extra safety)
	if idx := strings.Index(dbPath, "?"); idx != -1 {
		dbPath = dbPath[:idx]
	}

	return dbPath
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
