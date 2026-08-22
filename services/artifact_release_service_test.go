package services

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/KubeOrch/core/models"
	"github.com/KubeOrch/core/repositories"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type memoryArtifactReleaseStore struct {
	artifacts      map[primitive.ObjectID]*models.Artifact
	releases       map[primitive.ObjectID]*models.Release
	applications   map[primitive.ObjectID]*models.Application
	artifactWrites int
	releaseWrites  int
	listError      error
}

func newMemoryArtifactReleaseStore() *memoryArtifactReleaseStore {
	return &memoryArtifactReleaseStore{
		artifacts:    make(map[primitive.ObjectID]*models.Artifact),
		releases:     make(map[primitive.ObjectID]*models.Release),
		applications: make(map[primitive.ObjectID]*models.Application),
	}
}

func (s *memoryArtifactReleaseStore) CreateArtifact(_ context.Context, artifact *models.Artifact) error {
	for _, existing := range s.artifacts {
		if existing.WorkspaceID == artifact.WorkspaceID && existing.CreatedBy == artifact.CreatedBy && existing.CreationKey == artifact.CreationKey {
			return repositories.ErrDuplicateArtifactCreationKey
		}
		if existing.WorkspaceID == artifact.WorkspaceID && existing.IdentityHash == artifact.IdentityHash {
			return repositories.ErrDuplicateArtifactIdentity
		}
	}
	s.artifactWrites++
	s.artifacts[artifact.ID] = cloneArtifactForTest(artifact)
	return nil
}

func (s *memoryArtifactReleaseStore) GetArtifactByCreationKey(
	_ context.Context,
	workspaceID, actorID primitive.ObjectID,
	key string,
) (*models.Artifact, error) {
	for _, artifact := range s.artifacts {
		if artifact.WorkspaceID == workspaceID && artifact.CreatedBy == actorID && artifact.CreationKey == key {
			return cloneArtifactForTest(artifact), nil
		}
	}
	return nil, repositories.ErrArtifactNotFound
}

func (s *memoryArtifactReleaseStore) GetArtifactByIdentity(
	_ context.Context,
	workspaceID primitive.ObjectID,
	identityHash string,
) (*models.Artifact, error) {
	for _, artifact := range s.artifacts {
		if artifact.WorkspaceID == workspaceID && artifact.IdentityHash == identityHash {
			return cloneArtifactForTest(artifact), nil
		}
	}
	return nil, repositories.ErrArtifactNotFound
}

func (s *memoryArtifactReleaseStore) ListArtifacts(
	_ context.Context,
	workspaceID primitive.ObjectID,
	_ int,
	_ string,
) ([]models.Artifact, string, error) {
	if s.listError != nil {
		return nil, "", s.listError
	}
	var result []models.Artifact
	for _, artifact := range s.artifacts {
		if artifact.WorkspaceID == workspaceID {
			result = append(result, *cloneArtifactForTest(artifact))
		}
	}
	return result, "", nil
}

func (s *memoryArtifactReleaseStore) GetArtifact(
	_ context.Context,
	workspaceID, artifactID primitive.ObjectID,
) (*models.Artifact, error) {
	artifact, ok := s.artifacts[artifactID]
	if !ok || artifact.WorkspaceID != workspaceID {
		return nil, repositories.ErrArtifactNotFound
	}
	return cloneArtifactForTest(artifact), nil
}

func (s *memoryArtifactReleaseStore) ArtifactsExist(
	_ context.Context,
	workspaceID primitive.ObjectID,
	artifactIDs []primitive.ObjectID,
) (bool, error) {
	for _, artifactID := range artifactIDs {
		artifact, ok := s.artifacts[artifactID]
		if !ok || artifact.WorkspaceID != workspaceID {
			return false, nil
		}
	}
	return true, nil
}

func (s *memoryArtifactReleaseStore) GetApplication(
	_ context.Context,
	workspaceID, applicationID primitive.ObjectID,
) (*models.Application, error) {
	application, ok := s.applications[applicationID]
	if !ok || application.WorkspaceID != workspaceID {
		return nil, repositories.ErrApplicationNotFound
	}
	clone := *application
	return &clone, nil
}

func (s *memoryArtifactReleaseStore) CreateRelease(_ context.Context, release *models.Release) error {
	for _, existing := range s.releases {
		if existing.WorkspaceID == release.WorkspaceID && existing.CreatedBy == release.CreatedBy && existing.CreationKey == release.CreationKey {
			return repositories.ErrDuplicateReleaseCreationKey
		}
	}
	s.releaseWrites++
	s.releases[release.ID] = cloneReleaseForTest(release)
	return nil
}

func (s *memoryArtifactReleaseStore) GetReleaseByCreationKey(
	_ context.Context,
	workspaceID, actorID primitive.ObjectID,
	key string,
) (*models.Release, error) {
	for _, release := range s.releases {
		if release.WorkspaceID == workspaceID && release.CreatedBy == actorID && release.CreationKey == key {
			return cloneReleaseForTest(release), nil
		}
	}
	return nil, repositories.ErrReleaseNotFound
}

func (s *memoryArtifactReleaseStore) ListReleases(
	_ context.Context,
	workspaceID, applicationID primitive.ObjectID,
	_ int,
	_ string,
) ([]models.Release, string, error) {
	if s.listError != nil {
		return nil, "", s.listError
	}
	var result []models.Release
	for _, release := range s.releases {
		if release.WorkspaceID == workspaceID && release.ApplicationID == applicationID {
			result = append(result, *cloneReleaseForTest(release))
		}
	}
	return result, "", nil
}

func (s *memoryArtifactReleaseStore) GetRelease(
	_ context.Context,
	workspaceID, applicationID, releaseID primitive.ObjectID,
) (*models.Release, error) {
	release, ok := s.releases[releaseID]
	if !ok || release.WorkspaceID != workspaceID || release.ApplicationID != applicationID {
		return nil, repositories.ErrReleaseNotFound
	}
	return cloneReleaseForTest(release), nil
}

func TestArtifactRegistrationRequiresImmutableDigestAndSafeEvidence(t *testing.T) {
	store := newMemoryArtifactReleaseStore()
	service := NewArtifactReleaseService(store)
	actorID, workspaceID := primitive.NewObjectID(), primitive.NewObjectID()

	_, _, err := service.CreateArtifact(context.Background(), actorID, workspaceID, models.CreateArtifactRequest{
		Image:  "ghcr.io/kubeorch/core:latest",
		Source: testArtifactSource(),
	}, "register-core-image")
	assert.ErrorIs(t, err, ErrInvalidArtifactData)

	uppercaseDigest := testArtifactRequest()
	uppercaseDigest.Image = "ghcr.io/kubeorch/core@sha256:" + strings.Repeat("A", 64)
	_, _, err = service.CreateArtifact(context.Background(), actorID, workspaceID, uppercaseDigest, "register-uppercase-image")
	assert.ErrorIs(t, err, ErrInvalidArtifactData)

	request := testArtifactRequest()
	request.Evidence.SBOM = "https://evidence.example/sbom.json?token=secret"
	_, _, err = service.CreateArtifact(context.Background(), actorID, workspaceID, request, "register-core-image")
	assert.ErrorIs(t, err, ErrInvalidArtifactData)
	assert.Empty(t, store.artifacts)
}

func TestArtifactRegistrationIsIdempotentByKeyAndArtifactIdentity(t *testing.T) {
	store := newMemoryArtifactReleaseStore()
	service := NewArtifactReleaseService(store)
	actorID, workspaceID := primitive.NewObjectID(), primitive.NewObjectID()
	request := testArtifactRequest()

	created, replayed, err := service.CreateArtifact(context.Background(), actorID, workspaceID, request, "register-core-image")
	require.NoError(t, err)
	assert.False(t, replayed)
	assert.Equal(t, "ghcr.io/kubeorch/core@"+testImageDigest(), created.Image)
	assert.Equal(t, testImageDigest(), created.Digest)
	assert.Equal(t, actorID.Hex(), created.CreatedBy)

	repeated, replayed, err := service.CreateArtifact(context.Background(), actorID, workspaceID, request, "register-core-image")
	require.NoError(t, err)
	assert.True(t, replayed)
	assert.Equal(t, created, repeated)

	identityReplay, replayed, err := service.CreateArtifact(context.Background(), primitive.NewObjectID(), workspaceID, request, "register-same-evidence")
	require.NoError(t, err)
	assert.True(t, replayed)
	assert.Equal(t, created.ID, identityReplay.ID)
	assert.Equal(t, 1, store.artifactWrites)

	request.Evidence.Scan = "https://evidence.example/other-scan.json"
	_, _, err = service.CreateArtifact(context.Background(), actorID, workspaceID, request, "register-core-image")
	assert.ErrorIs(t, err, ErrDomainIdempotencyConflict)
	assert.Len(t, store.artifacts, 1)
}

func TestArtifactOperationsHideCrossWorkspaceRecordsAndMapCursorErrors(t *testing.T) {
	store := newMemoryArtifactReleaseStore()
	service := NewArtifactReleaseService(store)
	actorID, workspaceID := primitive.NewObjectID(), primitive.NewObjectID()
	created, _, err := service.CreateArtifact(context.Background(), actorID, workspaceID, testArtifactRequest(), "register-core-image")
	require.NoError(t, err)
	artifactID, err := primitive.ObjectIDFromHex(created.ID)
	require.NoError(t, err)

	_, err = service.GetArtifact(context.Background(), primitive.NewObjectID(), artifactID)
	assert.ErrorIs(t, err, ErrArtifactNotFound)
	store.listError = repositories.ErrInvalidCursor
	_, err = service.ListArtifacts(context.Background(), workspaceID, 20, "invalid")
	assert.ErrorIs(t, err, ErrInvalidArtifactData)
}

func TestReleaseBindsApplicationRevisionAndWorkspaceArtifactsWithoutDeployment(t *testing.T) {
	store, service, actorID, workspaceID, applicationID, artifactIDs := testArtifactReleaseService(t)
	request := models.CreateReleaseRequest{
		ApplicationRevision: "desired-state:7",
		ArtifactIDs:         []string{artifactIDs[1].Hex(), artifactIDs[0].Hex()},
		Source:              models.ReleaseSourceExternalCI,
		SourceReference:     "https://github.com/kubeorch/core/actions/runs/123",
	}

	created, replayed, err := service.CreateRelease(context.Background(), actorID, workspaceID, applicationID, request, "release-core-0001")
	require.NoError(t, err)
	assert.False(t, replayed)
	assert.Equal(t, applicationID.Hex(), created.ApplicationID)
	assert.Equal(t, "desired-state:7", created.ApplicationRevision)
	expectedIDs := []string{artifactIDs[0].Hex(), artifactIDs[1].Hex()}
	sort.Strings(expectedIDs)
	assert.Equal(t, expectedIDs, created.ArtifactIDs)
	assert.Equal(t, 1, store.releaseWrites)
	assert.Equal(t, 2, store.artifactWrites)

	repeated, replayed, err := service.CreateRelease(context.Background(), actorID, workspaceID, applicationID, request, "release-core-0001")
	require.NoError(t, err)
	assert.True(t, replayed)
	assert.Equal(t, created, repeated)
	assert.Equal(t, 1, store.releaseWrites)

	request.ApplicationRevision = "desired-state:8"
	_, _, err = service.CreateRelease(context.Background(), actorID, workspaceID, applicationID, request, "release-core-0001")
	assert.ErrorIs(t, err, ErrDomainIdempotencyConflict)
}

func TestReleaseRejectsCrossWorkspaceApplicationAndArtifacts(t *testing.T) {
	store, service, actorID, _, applicationID, artifactIDs := testArtifactReleaseService(t)
	request := models.CreateReleaseRequest{
		ApplicationRevision: "revision-1",
		ArtifactIDs:         []string{artifactIDs[0].Hex()},
		Source:              models.ReleaseSourceManual,
	}

	_, _, err := service.CreateRelease(context.Background(), actorID, primitive.NewObjectID(), applicationID, request, "release-core-0001")
	assert.ErrorIs(t, err, ErrApplicationNotFound)

	otherWorkspaceID := primitive.NewObjectID()
	store.applications[applicationID].WorkspaceID = otherWorkspaceID
	_, _, err = service.CreateRelease(context.Background(), actorID, otherWorkspaceID, applicationID, request, "release-core-0002")
	assert.ErrorIs(t, err, ErrArtifactNotFound)
	assert.Empty(t, store.releases)
	assert.Equal(t, 0, store.releaseWrites)
}

func TestReleaseValidationRejectsMissingEvidenceAndDuplicateArtifacts(t *testing.T) {
	_, service, actorID, workspaceID, applicationID, artifactIDs := testArtifactReleaseService(t)

	_, _, err := service.CreateRelease(context.Background(), actorID, workspaceID, applicationID, models.CreateReleaseRequest{
		ApplicationRevision: "revision-1",
		ArtifactIDs:         []string{artifactIDs[0].Hex(), artifactIDs[0].Hex()},
		Source:              models.ReleaseSourceExternalCI,
		SourceReference:     "https://github.com/kubeorch/core/actions/runs/123",
	}, "release-core-0001")
	assert.ErrorIs(t, err, ErrInvalidReleaseData)

	_, _, err = service.CreateRelease(context.Background(), actorID, workspaceID, applicationID, models.CreateReleaseRequest{
		ApplicationRevision: "revision-1",
		ArtifactIDs:         []string{artifactIDs[0].Hex()},
		Source:              models.ReleaseSourceExternalCI,
	}, "release-core-0002")
	assert.ErrorIs(t, err, ErrInvalidReleaseData)
}

func testArtifactReleaseService(t *testing.T) (*memoryArtifactReleaseStore, *ArtifactReleaseService, primitive.ObjectID, primitive.ObjectID, primitive.ObjectID, []primitive.ObjectID) {
	t.Helper()
	store := newMemoryArtifactReleaseStore()
	service := NewArtifactReleaseService(store)
	actorID, workspaceID, applicationID := primitive.NewObjectID(), primitive.NewObjectID(), primitive.NewObjectID()
	store.applications[applicationID] = &models.Application{ID: applicationID, WorkspaceID: workspaceID}

	first, _, err := service.CreateArtifact(context.Background(), actorID, workspaceID, testArtifactRequest(), "register-core-image")
	require.NoError(t, err)
	secondRequest := testArtifactRequest()
	secondRequest.Image = "ghcr.io/kubeorch/ui:v1@sha256:" + strings.Repeat("c", 64)
	secondRequest.Source.Repository = "https://github.com/kubeorch/ui"
	second, _, err := service.CreateArtifact(context.Background(), actorID, workspaceID, secondRequest, "register-ui-image")
	require.NoError(t, err)
	firstID, err := primitive.ObjectIDFromHex(first.ID)
	require.NoError(t, err)
	secondID, err := primitive.ObjectIDFromHex(second.ID)
	require.NoError(t, err)
	return store, service, actorID, workspaceID, applicationID, []primitive.ObjectID{firstID, secondID}
}

func testArtifactRequest() models.CreateArtifactRequest {
	return models.CreateArtifactRequest{
		Image:  "ghcr.io/kubeorch/core:v1@" + testImageDigest(),
		Source: testArtifactSource(),
		Evidence: models.ArtifactEvidence{
			SBOM:       "https://evidence.example/core.sbom.json",
			Provenance: "https://evidence.example/core.provenance.json",
			Scan:       "https://evidence.example/core.scan.json",
			CIRun:      "https://github.com/kubeorch/core/actions/runs/123",
		},
	}
}

func testArtifactSource() models.ArtifactSource {
	return models.ArtifactSource{
		Repository: "https://github.com/kubeorch/core",
		Ref:        "refs/heads/main",
		SHA:        strings.Repeat("b", 40),
	}
}

func testImageDigest() string {
	return "sha256:" + strings.Repeat("a", 64)
}

func cloneArtifactForTest(artifact *models.Artifact) *models.Artifact {
	clone := *artifact
	return &clone
}

func cloneReleaseForTest(release *models.Release) *models.Release {
	clone := *release
	clone.ArtifactIDs = append([]primitive.ObjectID(nil), release.ArtifactIDs...)
	return &clone
}
