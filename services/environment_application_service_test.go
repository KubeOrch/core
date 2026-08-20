package services

import (
	"context"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/KubeOrch/core/models"
	"github.com/KubeOrch/core/repositories"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type memoryEnvironmentApplicationStore struct {
	environments map[primitive.ObjectID]*models.Environment
	applications map[primitive.ObjectID]*models.Application
}

func newMemoryEnvironmentApplicationStore() *memoryEnvironmentApplicationStore {
	return &memoryEnvironmentApplicationStore{
		environments: make(map[primitive.ObjectID]*models.Environment),
		applications: make(map[primitive.ObjectID]*models.Application),
	}
}

func (s *memoryEnvironmentApplicationStore) CreateEnvironment(_ context.Context, environment *models.Environment) error {
	for _, existing := range s.environments {
		if existing.WorkspaceID == environment.WorkspaceID && existing.CreatedBy == environment.CreatedBy && existing.CreationKey == environment.CreationKey {
			return repositories.ErrDuplicateEnvironmentCreationKey
		}
		if existing.WorkspaceID == environment.WorkspaceID && existing.NormalizedName == environment.NormalizedName {
			return repositories.ErrEnvironmentNameExists
		}
	}
	s.environments[environment.ID] = cloneEnvironment(environment)
	return nil
}

func (s *memoryEnvironmentApplicationStore) GetEnvironmentByCreationKey(
	_ context.Context,
	workspaceID, actorID primitive.ObjectID,
	key string,
) (*models.Environment, error) {
	for _, environment := range s.environments {
		if environment.WorkspaceID == workspaceID && environment.CreatedBy == actorID && environment.CreationKey == key {
			return cloneEnvironment(environment), nil
		}
	}
	return nil, repositories.ErrEnvironmentNotFound
}

func (s *memoryEnvironmentApplicationStore) ListEnvironments(
	_ context.Context,
	workspaceID primitive.ObjectID,
	_ int,
	_ string,
) ([]models.Environment, string, error) {
	var result []models.Environment
	for _, environment := range s.environments {
		if environment.WorkspaceID == workspaceID {
			result = append(result, *cloneEnvironment(environment))
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.After(result[j].CreatedAt) })
	return result, "", nil
}

func (s *memoryEnvironmentApplicationStore) GetEnvironment(
	_ context.Context,
	workspaceID, environmentID primitive.ObjectID,
) (*models.Environment, error) {
	environment, ok := s.environments[environmentID]
	if !ok || environment.WorkspaceID != workspaceID {
		return nil, repositories.ErrEnvironmentNotFound
	}
	return cloneEnvironment(environment), nil
}

func (s *memoryEnvironmentApplicationStore) UpdateEnvironment(
	_ context.Context,
	workspaceID, environmentID primitive.ObjectID,
	updates bson.M,
) (*models.Environment, error) {
	environment, ok := s.environments[environmentID]
	if !ok || environment.WorkspaceID != workspaceID {
		return nil, repositories.ErrEnvironmentNotFound
	}
	if normalizedName, ok := updates["normalized_name"].(string); ok {
		for id, existing := range s.environments {
			if id != environmentID && existing.WorkspaceID == workspaceID && existing.NormalizedName == normalizedName {
				return nil, repositories.ErrEnvironmentNameExists
			}
		}
		environment.NormalizedName = normalizedName
	}
	if name, ok := updates["name"].(string); ok {
		environment.Name = name
	}
	if description, ok := updates["description"].(string); ok {
		environment.Description = description
	}
	return cloneEnvironment(environment), nil
}

func (s *memoryEnvironmentApplicationStore) CreateApplication(_ context.Context, application *models.Application) error {
	for _, existing := range s.applications {
		if existing.WorkspaceID == application.WorkspaceID && existing.CreatedBy == application.CreatedBy && existing.CreationKey == application.CreationKey {
			return repositories.ErrDuplicateApplicationCreationKey
		}
	}
	s.applications[application.ID] = cloneApplication(application)
	return nil
}

func (s *memoryEnvironmentApplicationStore) GetApplicationByCreationKey(
	_ context.Context,
	workspaceID, actorID primitive.ObjectID,
	key string,
) (*models.Application, error) {
	for _, application := range s.applications {
		if application.WorkspaceID == workspaceID && application.CreatedBy == actorID && application.CreationKey == key {
			return cloneApplication(application), nil
		}
	}
	return nil, repositories.ErrApplicationNotFound
}

func (s *memoryEnvironmentApplicationStore) ListApplications(
	_ context.Context,
	workspaceID primitive.ObjectID,
	environmentID *primitive.ObjectID,
	includeArchived bool,
	_ int,
	_ string,
) ([]models.Application, string, error) {
	var result []models.Application
	for _, application := range s.applications {
		if application.WorkspaceID != workspaceID || (!includeArchived && application.ArchivedAt != nil) {
			continue
		}
		if environmentID != nil && application.EnvironmentID != *environmentID {
			continue
		}
		result = append(result, *cloneApplication(application))
	}
	return result, "", nil
}

func (s *memoryEnvironmentApplicationStore) GetApplication(
	_ context.Context,
	workspaceID, applicationID primitive.ObjectID,
) (*models.Application, error) {
	application, ok := s.applications[applicationID]
	if !ok || application.WorkspaceID != workspaceID {
		return nil, repositories.ErrApplicationNotFound
	}
	return cloneApplication(application), nil
}

func (s *memoryEnvironmentApplicationStore) UpdateApplication(
	_ context.Context,
	workspaceID, applicationID primitive.ObjectID,
	updates bson.M,
) (*models.Application, error) {
	application, ok := s.applications[applicationID]
	if !ok || application.WorkspaceID != workspaceID {
		return nil, repositories.ErrApplicationNotFound
	}
	if name, ok := updates["name"].(string); ok {
		application.Name = name
	}
	if description, ok := updates["description"].(string); ok {
		application.Description = description
	}
	if desiredState, ok := updates["desired_state"].(map[string]any); ok {
		application.DesiredState = desiredState
	}
	return cloneApplication(application), nil
}

func (s *memoryEnvironmentApplicationStore) ArchiveApplication(
	_ context.Context,
	workspaceID, applicationID primitive.ObjectID,
	archivedAt time.Time,
) (*models.Application, error) {
	application, ok := s.applications[applicationID]
	if !ok || application.WorkspaceID != workspaceID {
		return nil, repositories.ErrApplicationNotFound
	}
	if application.ArchivedAt == nil {
		application.ArchivedAt = &archivedAt
		application.UpdatedAt = archivedAt
	}
	return cloneApplication(application), nil
}

func TestEnvironmentNamesAreUniqueByNormalizedNameWithinWorkspace(t *testing.T) {
	store := newMemoryEnvironmentApplicationStore()
	service := NewEnvironmentApplicationService(store)
	workspaceID := primitive.NewObjectID()
	actorID := primitive.NewObjectID()

	created, replayed, err := service.CreateEnvironment(context.Background(), actorID, workspaceID, models.CreateEnvironmentRequest{
		Name: "  Production  EU  ",
	}, "create-production-eu")

	require.NoError(t, err)
	assert.False(t, replayed)
	assert.Equal(t, "Production  EU", created.Name)
	_, _, err = service.CreateEnvironment(context.Background(), actorID, workspaceID, models.CreateEnvironmentRequest{
		Name: "production eu",
	}, "create-production-eu-again")
	assert.ErrorIs(t, err, ErrEnvironmentNameConflict)
	_, _, err = service.CreateEnvironment(context.Background(), actorID, primitive.NewObjectID(), models.CreateEnvironmentRequest{
		Name: "production eu",
	}, "create-production-eu-other-workspace")
	require.NoError(t, err)
}

func TestEnvironmentCreationReplaysMatchingIdempotencyKeyAndRejectsDifferentPayload(t *testing.T) {
	store := newMemoryEnvironmentApplicationStore()
	service := NewEnvironmentApplicationService(store)
	workspaceID := primitive.NewObjectID()
	actorID := primitive.NewObjectID()
	request := models.CreateEnvironmentRequest{Name: "Production", Description: "Primary environment"}

	created, replayed, err := service.CreateEnvironment(context.Background(), actorID, workspaceID, request, "create-production")
	require.NoError(t, err)
	assert.False(t, replayed)

	repeated, replayed, err := service.CreateEnvironment(context.Background(), actorID, workspaceID, request, "create-production")
	require.NoError(t, err)
	assert.True(t, replayed)
	assert.Equal(t, created, repeated)

	_, _, err = service.CreateEnvironment(context.Background(), actorID, workspaceID, models.CreateEnvironmentRequest{
		Name: "Staging",
	}, "create-production")
	assert.ErrorIs(t, err, ErrDomainIdempotencyConflict)
	assert.Len(t, store.environments, 1)
}

func TestApplicationDraftPreservesExtensibleDesiredStateWithoutClusterAccess(t *testing.T) {
	store, service, actorID, workspaceID, environmentID := testEnvironmentApplicationService(t)
	desiredState := map[string]any{
		"sourceRef":   map[string]any{"kind": "git", "revision": "abc123"},
		"futureField": map[string]any{"enabled": true, "weight": float64(10)},
	}

	created, replayed, err := service.CreateApplication(context.Background(), actorID, workspaceID, environmentID, models.CreateApplicationRequest{
		Name:         "checkout",
		DesiredState: models.DesiredState(desiredState),
	}, "create-checkout")

	require.NoError(t, err)
	assert.False(t, replayed)
	assert.Equal(t, models.ApplicationStatusDraft, created.Status)
	assert.Equal(t, desiredState, created.DesiredState)
	assert.Nil(t, created.ArchivedAt)
	assert.Len(t, store.applications, 1)
}

func TestApplicationCreationReplaysMatchingIdempotencyKeyAndRejectsDifferentPayload(t *testing.T) {
	store, service, actorID, workspaceID, environmentID := testEnvironmentApplicationService(t)
	request := models.CreateApplicationRequest{
		Name:         "checkout",
		DesiredState: models.DesiredState{"sourceRef": map[string]any{"revision": "abc123"}},
	}

	created, replayed, err := service.CreateApplication(context.Background(), actorID, workspaceID, environmentID, request, "create-checkout")
	require.NoError(t, err)
	assert.False(t, replayed)

	repeated, replayed, err := service.CreateApplication(context.Background(), actorID, workspaceID, environmentID, request, "create-checkout")
	require.NoError(t, err)
	assert.True(t, replayed)
	assert.Equal(t, created, repeated)

	request.Name = "payments"
	_, _, err = service.CreateApplication(context.Background(), actorID, workspaceID, environmentID, request, "create-checkout")
	assert.ErrorIs(t, err, ErrDomainIdempotencyConflict)
	assert.Len(t, store.applications, 1)
}

func TestApplicationResponsesDeepCopyDesiredState(t *testing.T) {
	_, service, actorID, workspaceID, environmentID := testEnvironmentApplicationService(t)
	desiredState := models.DesiredState{"sourceRef": map[string]any{"revision": "abc123"}}
	created, _, err := service.CreateApplication(context.Background(), actorID, workspaceID, environmentID, models.CreateApplicationRequest{
		Name:         "checkout",
		DesiredState: desiredState,
	}, "create-checkout")
	require.NoError(t, err)

	desiredState["sourceRef"].(map[string]any)["revision"] = "mutated-input"
	created.DesiredState["sourceRef"].(map[string]any)["revision"] = "mutated-response"
	applicationID, err := primitive.ObjectIDFromHex(created.ID)
	require.NoError(t, err)
	fetched, err := service.GetApplication(context.Background(), workspaceID, applicationID)
	require.NoError(t, err)
	assert.Equal(t, "abc123", fetched.DesiredState["sourceRef"].(map[string]any)["revision"])
}

func TestApplicationRejectsCrossWorkspaceEnvironmentReference(t *testing.T) {
	_, service, actorID, _, environmentID := testEnvironmentApplicationService(t)

	_, _, err := service.CreateApplication(context.Background(), actorID, primitive.NewObjectID(), environmentID, models.CreateApplicationRequest{
		Name: "checkout",
	}, "create-cross-workspace")

	assert.ErrorIs(t, err, ErrEnvironmentNotFound)
}

func TestApplicationOperationsHideCrossWorkspaceResources(t *testing.T) {
	_, service, actorID, workspaceID, environmentID := testEnvironmentApplicationService(t)
	created, _, err := service.CreateApplication(context.Background(), actorID, workspaceID, environmentID, models.CreateApplicationRequest{
		Name: "checkout",
	}, "create-checkout")
	require.NoError(t, err)
	applicationID, err := primitive.ObjectIDFromHex(created.ID)
	require.NoError(t, err)
	otherWorkspaceID := primitive.NewObjectID()

	_, err = service.GetApplication(context.Background(), otherWorkspaceID, applicationID)
	assert.ErrorIs(t, err, ErrApplicationNotFound)
	updatedName := "payments"
	_, err = service.UpdateApplication(context.Background(), otherWorkspaceID, applicationID, models.UpdateApplicationRequest{
		Name: &updatedName,
	})
	assert.ErrorIs(t, err, ErrApplicationNotFound)
	_, err = service.ArchiveApplication(context.Background(), otherWorkspaceID, applicationID)
	assert.ErrorIs(t, err, ErrApplicationNotFound)
}

func TestApplicationArchiveIsIdempotentAndExcludedFromDefaultLists(t *testing.T) {
	_, service, actorID, workspaceID, environmentID := testEnvironmentApplicationService(t)
	created, _, err := service.CreateApplication(context.Background(), actorID, workspaceID, environmentID, models.CreateApplicationRequest{
		Name: "checkout",
	}, "create-checkout")
	require.NoError(t, err)
	applicationID, err := primitive.ObjectIDFromHex(created.ID)
	require.NoError(t, err)

	first, err := service.ArchiveApplication(context.Background(), workspaceID, applicationID)
	require.NoError(t, err)
	second, err := service.ArchiveApplication(context.Background(), workspaceID, applicationID)
	require.NoError(t, err)
	assert.Equal(t, models.ApplicationStatusArchived, first.Status)
	require.NotNil(t, first.ArchivedAt)
	assert.Equal(t, first.ArchivedAt, second.ArchivedAt)

	active, err := service.ListApplications(context.Background(), workspaceID, nil, false, 20, "")
	require.NoError(t, err)
	assert.Empty(t, active.Items)
	all, err := service.ListApplications(context.Background(), workspaceID, nil, true, 20, "")
	require.NoError(t, err)
	require.Len(t, all.Items, 1)
	assert.Equal(t, models.ApplicationStatusArchived, all.Items[0].Status)
}

func TestApplicationDesiredStateRejectsEmbeddedCredentialsButAllowsReferences(t *testing.T) {
	_, service, actorID, workspaceID, environmentID := testEnvironmentApplicationService(t)

	_, _, err := service.CreateApplication(context.Background(), actorID, workspaceID, environmentID, models.CreateApplicationRequest{
		Name: "checkout",
		DesiredState: models.DesiredState{
			"credentials": map[string]any{"username": "admin"},
		},
	}, "create-with-credentials")
	assert.ErrorIs(t, err, ErrInvalidApplicationData)

	_, _, err = service.CreateApplication(context.Background(), actorID, workspaceID, environmentID, models.CreateApplicationRequest{
		Name: "checkout",
		DesiredState: models.DesiredState{
			"credentialsRef": map[string]any{"name": "registry-credentials"},
		},
	}, "create-with-credential-reference")
	require.NoError(t, err)

	for _, key := range []string{"clientSecret", "githubToken", "registryCredentials", "privateKey", "apiKey", "awsSecretAccessKey", "authorization"} {
		t.Run(key, func(t *testing.T) {
			_, _, err := service.CreateApplication(context.Background(), actorID, workspaceID, environmentID, models.CreateApplicationRequest{
				Name:         key,
				DesiredState: models.DesiredState{key: "embedded-value"},
			}, "create-with-"+strings.ToLower(key))
			assert.ErrorIs(t, err, ErrInvalidApplicationData)
		})
	}
}

func testEnvironmentApplicationService(t *testing.T) (*memoryEnvironmentApplicationStore, *EnvironmentApplicationService, primitive.ObjectID, primitive.ObjectID, primitive.ObjectID) {
	t.Helper()
	store := newMemoryEnvironmentApplicationStore()
	service := NewEnvironmentApplicationService(store)
	actorID := primitive.NewObjectID()
	workspaceID := primitive.NewObjectID()
	environment, _, err := service.CreateEnvironment(context.Background(), actorID, workspaceID, models.CreateEnvironmentRequest{
		Name: "Development",
	}, "create-development")
	require.NoError(t, err)
	environmentID, err := primitive.ObjectIDFromHex(environment.ID)
	require.NoError(t, err)
	return store, service, actorID, workspaceID, environmentID
}

func cloneEnvironment(environment *models.Environment) *models.Environment {
	copy := *environment
	return &copy
}

func cloneApplication(application *models.Application) *models.Application {
	copy := *application
	if application.ArchivedAt != nil {
		archivedAt := *application.ArchivedAt
		copy.ArchivedAt = &archivedAt
	}
	copy.DesiredState = cloneDesiredStateForTest(application.DesiredState)
	return &copy
}

func cloneDesiredStateForTest(value map[string]any) map[string]any {
	copy := make(map[string]any, len(value))
	for key, child := range value {
		copy[key] = child
	}
	return copy
}
