package repositories

import (
	"context"
	"fmt"
	"time"

	"github.com/KubeOrch/core/database"
	"github.com/KubeOrch/core/models"
	"github.com/KubeOrch/core/pkg/encryption"
	"github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type RegistryRepository struct {
	collection *mongo.Collection
}

func NewRegistryRepository() *RegistryRepository {
	db := database.GetDB()
	repo := &RegistryRepository{
		collection: db.Collection("registries"),
	}

	// Create indexes
	repo.initializeIndexes()

	return repo
}

func (r *RegistryRepository) initializeIndexes() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create unique index on name
	indexModel := mongo.IndexModel{
		Keys:    bson.M{"name": 1},
		Options: options.Index().SetUnique(true).SetName("unique_name"),
	}

	_, err := r.collection.Indexes().CreateOne(ctx, indexModel)
	if err != nil {
		logrus.WithError(err).Warn("Failed to create unique index on registry name")
	}

	// Create index on registry_type for faster lookups
	typeIndexModel := mongo.IndexModel{
		Keys:    bson.M{"registry_type": 1},
		Options: options.Index().SetName("registry_type_index"),
	}

	_, err = r.collection.Indexes().CreateOne(ctx, typeIndexModel)
	if err != nil {
		logrus.WithError(err).Warn("Failed to create index on registry_type")
	}
}

func (r *RegistryRepository) Create(ctx context.Context, registry *models.Registry) error {
	// Ensure encryption is properly configured before storing sensitive credentials
	if !encryption.IsConfigured() {
		return fmt.Errorf("encryption key not configured - cannot store registry credentials securely")
	}

	registry.CreatedAt = time.Now()
	registry.UpdatedAt = time.Now()
	registry.Status = models.RegistryStatusUnknown

	// Encrypt credentials before storing
	encryptedCreds, err := r.encryptCredentials(&registry.Credentials)
	if err != nil {
		return fmt.Errorf("failed to encrypt credentials: %w", err)
	}

	// Create a copy for insertion with encrypted credentials
	registryToStore := *registry
	registryToStore.Credentials = *encryptedCreds

	result, err := r.collection.InsertOne(ctx, registryToStore)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return fmt.Errorf("registry with name '%s' already exists", registry.Name)
		}
		return fmt.Errorf("failed to create registry: %w", err)
	}

	registry.ID = result.InsertedID.(primitive.ObjectID)
	return nil
}

func (r *RegistryRepository) GetByID(ctx context.Context, id primitive.ObjectID) (*models.Registry, error) {
	var registry models.Registry
	err := r.collection.FindOne(ctx, bson.M{"_id": id}).Decode(&registry)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, fmt.Errorf("registry not found")
		}
		return nil, fmt.Errorf("failed to get registry: %w", err)
	}

	// Decrypt credentials after retrieval
	decryptedCreds, err := r.decryptCredentials(&registry.Credentials)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt credentials: %w", err)
	}
	registry.Credentials = *decryptedCreds

	return &registry, nil
}

func (r *RegistryRepository) GetByName(ctx context.Context, name string) (*models.Registry, error) {
	var registry models.Registry
	err := r.collection.FindOne(ctx, bson.M{"name": name}).Decode(&registry)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, fmt.Errorf("registry not found")
		}
		return nil, fmt.Errorf("failed to get registry: %w", err)
	}

	// Decrypt credentials after retrieval
	decryptedCreds, err := r.decryptCredentials(&registry.Credentials)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt credentials: %w", err)
	}
	registry.Credentials = *decryptedCreds

	return &registry, nil
}

func (r *RegistryRepository) GetByType(ctx context.Context, registryType models.RegistryType) ([]*models.Registry, error) {
	cursor, err := r.collection.Find(ctx, bson.M{"registry_type": registryType},
		options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}}))
	if err != nil {
		return nil, fmt.Errorf("failed to find registries by type: %w", err)
	}
	defer func() { _ = cursor.Close(ctx) }()

	var registries []*models.Registry
	if err := cursor.All(ctx, &registries); err != nil {
		return nil, fmt.Errorf("failed to decode registries: %w", err)
	}

	// Decrypt credentials for each registry
	for i := range registries {
		decryptedCreds, err := r.decryptCredentials(&registries[i].Credentials)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt credentials for registry %s: %w", registries[i].Name, err)
		}
		registries[i].Credentials = *decryptedCreds
	}

	return registries, nil
}

func (r *RegistryRepository) GetDefaultByType(ctx context.Context, registryType models.RegistryType) (*models.Registry, error) {
	var registry models.Registry
	filter := bson.M{
		"registry_type": registryType,
		"is_default":    true,
	}

	err := r.collection.FindOne(ctx, filter).Decode(&registry)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil // No default found is not an error
		}
		return nil, fmt.Errorf("failed to get default registry: %w", err)
	}

	// Decrypt credentials after retrieval
	decryptedCreds, err := r.decryptCredentials(&registry.Credentials)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt credentials: %w", err)
	}
	registry.Credentials = *decryptedCreds

	return &registry, nil
}

func (r *RegistryRepository) List(ctx context.Context) ([]*models.Registry, error) {
	cursor, err := r.collection.Find(ctx, bson.M{},
		options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}}))
	if err != nil {
		return nil, fmt.Errorf("failed to list registries: %w", err)
	}
	defer func() { _ = cursor.Close(ctx) }()

	var registries []*models.Registry
	if err := cursor.All(ctx, &registries); err != nil {
		return nil, fmt.Errorf("failed to decode registries: %w", err)
	}

	// Decrypt credentials for each registry
	for i := range registries {
		decryptedCreds, err := r.decryptCredentials(&registries[i].Credentials)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt credentials for registry %s: %w", registries[i].Name, err)
		}
		registries[i].Credentials = *decryptedCreds
	}

	return registries, nil
}

func (r *RegistryRepository) Update(ctx context.Context, id primitive.ObjectID, update bson.M) error {
	update["updated_at"] = time.Now()

	// Check if credentials are being updated
	if creds, ok := update["credentials"]; ok {
		if credsModel, ok := creds.(models.RegistryCredentials); ok {
			encryptedCreds, err := r.encryptCredentials(&credsModel)
			if err != nil {
				return fmt.Errorf("failed to encrypt credentials: %w", err)
			}
			update["credentials"] = *encryptedCreds
		}
	}

	result, err := r.collection.UpdateOne(
		ctx,
		bson.M{"_id": id},
		bson.M{"$set": update},
	)

	if err != nil {
		return fmt.Errorf("failed to update registry: %w", err)
	}

	if result.MatchedCount == 0 {
		return fmt.Errorf("registry not found")
	}

	return nil
}

func (r *RegistryRepository) UpdateStatus(ctx context.Context, id primitive.ObjectID, status models.RegistryStatus) error {
	return r.Update(ctx, id, bson.M{
		"status":     status,
		"last_check": time.Now(),
	})
}

func (r *RegistryRepository) SetDefault(ctx context.Context, id primitive.ObjectID, registryType models.RegistryType) error {
	// Unset all defaults for this registry type
	_, err := r.collection.UpdateMany(
		ctx,
		bson.M{"registry_type": registryType},
		bson.M{"$set": bson.M{"is_default": false}},
	)
	if err != nil {
		return fmt.Errorf("failed to unset default registries: %w", err)
	}

	// Set the new default
	_, err = r.collection.UpdateOne(
		ctx,
		bson.M{"_id": id},
		bson.M{"$set": bson.M{"is_default": true, "updated_at": time.Now()}},
	)
	if err != nil {
		return fmt.Errorf("failed to set default registry: %w", err)
	}

	return nil
}

func (r *RegistryRepository) Delete(ctx context.Context, id primitive.ObjectID) error {
	result, err := r.collection.DeleteOne(ctx, bson.M{"_id": id})
	if err != nil {
		return fmt.Errorf("failed to delete registry: %w", err)
	}

	if result.DeletedCount == 0 {
		return fmt.Errorf("registry not found")
	}

	return nil
}

// Encryption/Decryption helpers

func (r *RegistryRepository) encryptCredentials(creds *models.RegistryCredentials) (*models.RegistryCredentials, error) {
	encrypted := &models.RegistryCredentials{
		Region: creds.Region, // Region is not sensitive
	}

	var err error

	// Common fields
	if creds.Username != "" {
		encrypted.Username, err = encryption.Encrypt(creds.Username)
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt username: %w", err)
		}
	}

	if creds.Password != "" {
		encrypted.Password, err = encryption.Encrypt(creds.Password)
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt password: %w", err)
		}
	}

	// AWS ECR fields
	if creds.AccessKeyID != "" {
		encrypted.AccessKeyID, err = encryption.Encrypt(creds.AccessKeyID)
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt access key ID: %w", err)
		}
	}

	if creds.SecretAccessKey != "" {
		encrypted.SecretAccessKey, err = encryption.Encrypt(creds.SecretAccessKey)
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt secret access key: %w", err)
		}
	}

	// Google Artifact Registry / GCR fields
	if creds.ServiceAccountJSON != "" {
		encrypted.ServiceAccountJSON, err = encryption.Encrypt(creds.ServiceAccountJSON)
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt service account JSON: %w", err)
		}
	}

	// Azure ACR fields
	if creds.TenantID != "" {
		encrypted.TenantID, err = encryption.Encrypt(creds.TenantID)
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt tenant ID: %w", err)
		}
	}

	if creds.ClientID != "" {
		encrypted.ClientID, err = encryption.Encrypt(creds.ClientID)
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt client ID: %w", err)
		}
	}

	if creds.ClientSecret != "" {
		encrypted.ClientSecret, err = encryption.Encrypt(creds.ClientSecret)
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt client secret: %w", err)
		}
	}

	return encrypted, nil
}

func (r *RegistryRepository) decryptCredentials(creds *models.RegistryCredentials) (*models.RegistryCredentials, error) {
	decrypted := &models.RegistryCredentials{
		Region: creds.Region, // Region is not sensitive
	}

	var err error

	// Common fields
	if creds.Username != "" {
		decrypted.Username, err = encryption.Decrypt(creds.Username)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt username: %w", err)
		}
	}

	if creds.Password != "" {
		decrypted.Password, err = encryption.Decrypt(creds.Password)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt password: %w", err)
		}
	}

	// AWS ECR fields
	if creds.AccessKeyID != "" {
		decrypted.AccessKeyID, err = encryption.Decrypt(creds.AccessKeyID)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt access key ID: %w", err)
		}
	}

	if creds.SecretAccessKey != "" {
		decrypted.SecretAccessKey, err = encryption.Decrypt(creds.SecretAccessKey)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt secret access key: %w", err)
		}
	}

	// Google Artifact Registry / GCR fields
	if creds.ServiceAccountJSON != "" {
		decrypted.ServiceAccountJSON, err = encryption.Decrypt(creds.ServiceAccountJSON)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt service account JSON: %w", err)
		}
	}

	// Azure ACR fields
	if creds.TenantID != "" {
		decrypted.TenantID, err = encryption.Decrypt(creds.TenantID)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt tenant ID: %w", err)
		}
	}

	if creds.ClientID != "" {
		decrypted.ClientID, err = encryption.Decrypt(creds.ClientID)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt client ID: %w", err)
		}
	}

	if creds.ClientSecret != "" {
		decrypted.ClientSecret, err = encryption.Decrypt(creds.ClientSecret)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt client secret: %w", err)
		}
	}

	return decrypted, nil
}
