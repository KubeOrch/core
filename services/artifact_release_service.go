package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/KubeOrch/core/models"
	"github.com/KubeOrch/core/repositories"
	"github.com/distribution/reference"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

var (
	ErrArtifactNotFound    = errors.New("artifact not found")
	ErrReleaseNotFound     = errors.New("release not found")
	ErrInvalidArtifactData = errors.New("invalid artifact data")
	ErrInvalidReleaseData  = errors.New("invalid release data")
)

var (
	sourceSHAPattern           = regexp.MustCompile(`^(?:[a-fA-F0-9]{40}|[a-fA-F0-9]{64})$`)
	applicationRevisionPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/@+\-]{0,127}$`)
)

type ArtifactReleaseStore interface {
	CreateArtifact(context.Context, *models.Artifact) error
	GetArtifactByCreationKey(context.Context, primitive.ObjectID, primitive.ObjectID, string) (*models.Artifact, error)
	GetArtifactByIdentity(context.Context, primitive.ObjectID, string) (*models.Artifact, error)
	ListArtifacts(context.Context, primitive.ObjectID, int, string) ([]models.Artifact, string, error)
	GetArtifact(context.Context, primitive.ObjectID, primitive.ObjectID) (*models.Artifact, error)
	ArtifactsExist(context.Context, primitive.ObjectID, []primitive.ObjectID) (bool, error)
	GetApplication(context.Context, primitive.ObjectID, primitive.ObjectID) (*models.Application, error)
	CreateRelease(context.Context, *models.Release) error
	GetReleaseByCreationKey(context.Context, primitive.ObjectID, primitive.ObjectID, string) (*models.Release, error)
	ListReleases(context.Context, primitive.ObjectID, primitive.ObjectID, int, string) ([]models.Release, string, error)
	GetRelease(context.Context, primitive.ObjectID, primitive.ObjectID, primitive.ObjectID) (*models.Release, error)
}

type ArtifactReleaseService struct {
	store ArtifactReleaseStore
	now   func() time.Time
}

func NewArtifactReleaseService(store ArtifactReleaseStore) *ArtifactReleaseService {
	return &ArtifactReleaseService{store: store, now: func() time.Time { return time.Now().UTC() }}
}

func (s *ArtifactReleaseService) CreateArtifact(
	ctx context.Context,
	actorID, workspaceID primitive.ObjectID,
	request models.CreateArtifactRequest,
	idempotencyKey string,
) (models.ArtifactResponse, bool, error) {
	image, digest, source, evidence, err := validateArtifactRequest(request)
	if err != nil {
		return models.ArtifactResponse{}, false, err
	}
	requestHash, err := hashJSON(struct {
		Image    string                  `json:"image"`
		Source   models.ArtifactSource   `json:"source"`
		Evidence models.ArtifactEvidence `json:"evidence"`
	}{Image: image, Source: source, Evidence: evidence})
	if err != nil {
		return models.ArtifactResponse{}, false, err
	}

	now := s.now()
	artifact := &models.Artifact{
		ID:           primitive.NewObjectIDFromTimestamp(now),
		WorkspaceID:  workspaceID,
		Image:        image,
		Digest:       digest,
		Source:       source,
		Evidence:     evidence,
		IdentityHash: requestHash,
		CreatedBy:    actorID,
		CreationKey:  idempotencyKey,
		CreationHash: requestHash,
		CreatedAt:    now,
	}

	if err := s.store.CreateArtifact(ctx, artifact); err != nil {
		switch {
		case errors.Is(err, repositories.ErrDuplicateArtifactCreationKey):
			existing, getErr := s.store.GetArtifactByCreationKey(ctx, workspaceID, actorID, idempotencyKey)
			if getErr != nil {
				return models.ArtifactResponse{}, false, mapArtifactReleaseRepositoryError(getErr)
			}
			if existing.CreationHash != requestHash {
				return models.ArtifactResponse{}, false, ErrDomainIdempotencyConflict
			}
			return artifactResponse(*existing), true, nil
		case errors.Is(err, repositories.ErrDuplicateArtifactIdentity):
			existing, getErr := s.store.GetArtifactByIdentity(ctx, workspaceID, requestHash)
			if getErr != nil {
				return models.ArtifactResponse{}, false, mapArtifactReleaseRepositoryError(getErr)
			}
			return artifactResponse(*existing), true, nil
		default:
			return models.ArtifactResponse{}, false, err
		}
	}
	return artifactResponse(*artifact), false, nil
}

func (s *ArtifactReleaseService) ListArtifacts(
	ctx context.Context,
	workspaceID primitive.ObjectID,
	limit int,
	cursor string,
) (models.ArtifactListResponse, error) {
	artifacts, nextCursor, err := s.store.ListArtifacts(ctx, workspaceID, limit, cursor)
	if err != nil {
		if errors.Is(err, repositories.ErrInvalidCursor) {
			return models.ArtifactListResponse{}, fmt.Errorf("%w: invalid pagination cursor", ErrInvalidArtifactData)
		}
		return models.ArtifactListResponse{}, mapArtifactReleaseRepositoryError(err)
	}
	items := make([]models.ArtifactResponse, 0, len(artifacts))
	for _, artifact := range artifacts {
		items = append(items, artifactResponse(artifact))
	}
	return models.ArtifactListResponse{Items: items, PageInfo: pageInfo(nextCursor)}, nil
}

func (s *ArtifactReleaseService) GetArtifact(
	ctx context.Context,
	workspaceID, artifactID primitive.ObjectID,
) (models.ArtifactResponse, error) {
	artifact, err := s.store.GetArtifact(ctx, workspaceID, artifactID)
	if err != nil {
		return models.ArtifactResponse{}, mapArtifactReleaseRepositoryError(err)
	}
	return artifactResponse(*artifact), nil
}

func (s *ArtifactReleaseService) CreateRelease(
	ctx context.Context,
	actorID, workspaceID, applicationID primitive.ObjectID,
	request models.CreateReleaseRequest,
	idempotencyKey string,
) (models.ReleaseResponse, bool, error) {
	if _, err := s.store.GetApplication(ctx, workspaceID, applicationID); err != nil {
		return models.ReleaseResponse{}, false, mapArtifactReleaseRepositoryError(err)
	}
	revision, artifactIDs, source, sourceReference, err := validateReleaseRequest(request)
	if err != nil {
		return models.ReleaseResponse{}, false, err
	}
	exist, err := s.store.ArtifactsExist(ctx, workspaceID, artifactIDs)
	if err != nil {
		return models.ReleaseResponse{}, false, err
	}
	if !exist {
		return models.ReleaseResponse{}, false, ErrArtifactNotFound
	}

	requestHash, err := releaseRequestHash(applicationID, revision, artifactIDs, source, sourceReference)
	if err != nil {
		return models.ReleaseResponse{}, false, err
	}
	now := s.now()
	release := &models.Release{
		ID:                  primitive.NewObjectIDFromTimestamp(now),
		WorkspaceID:         workspaceID,
		ApplicationID:       applicationID,
		ApplicationRevision: revision,
		ArtifactIDs:         artifactIDs,
		Source:              source,
		SourceReference:     sourceReference,
		CreatedBy:           actorID,
		CreationKey:         idempotencyKey,
		CreationHash:        requestHash,
		CreatedAt:           now,
	}
	if err := s.store.CreateRelease(ctx, release); err != nil {
		if !errors.Is(err, repositories.ErrDuplicateReleaseCreationKey) {
			return models.ReleaseResponse{}, false, err
		}
		existing, getErr := s.store.GetReleaseByCreationKey(ctx, workspaceID, actorID, idempotencyKey)
		if getErr != nil {
			return models.ReleaseResponse{}, false, mapArtifactReleaseRepositoryError(getErr)
		}
		if existing.CreationHash != requestHash {
			return models.ReleaseResponse{}, false, ErrDomainIdempotencyConflict
		}
		return releaseResponse(*existing), true, nil
	}
	return releaseResponse(*release), false, nil
}

func (s *ArtifactReleaseService) ListReleases(
	ctx context.Context,
	workspaceID, applicationID primitive.ObjectID,
	limit int,
	cursor string,
) (models.ReleaseListResponse, error) {
	if _, err := s.store.GetApplication(ctx, workspaceID, applicationID); err != nil {
		return models.ReleaseListResponse{}, mapArtifactReleaseRepositoryError(err)
	}
	releases, nextCursor, err := s.store.ListReleases(ctx, workspaceID, applicationID, limit, cursor)
	if err != nil {
		if errors.Is(err, repositories.ErrInvalidCursor) {
			return models.ReleaseListResponse{}, fmt.Errorf("%w: invalid pagination cursor", ErrInvalidReleaseData)
		}
		return models.ReleaseListResponse{}, mapArtifactReleaseRepositoryError(err)
	}
	items := make([]models.ReleaseResponse, 0, len(releases))
	for _, release := range releases {
		items = append(items, releaseResponse(release))
	}
	return models.ReleaseListResponse{Items: items, PageInfo: pageInfo(nextCursor)}, nil
}

func (s *ArtifactReleaseService) GetRelease(
	ctx context.Context,
	workspaceID, applicationID, releaseID primitive.ObjectID,
) (models.ReleaseResponse, error) {
	release, err := s.store.GetRelease(ctx, workspaceID, applicationID, releaseID)
	if err != nil {
		return models.ReleaseResponse{}, mapArtifactReleaseRepositoryError(err)
	}
	return releaseResponse(*release), nil
}

func validateArtifactRequest(request models.CreateArtifactRequest) (string, string, models.ArtifactSource, models.ArtifactEvidence, error) {
	image, digest, err := validateDigestPinnedImage(request.Image)
	if err != nil {
		return "", "", models.ArtifactSource{}, models.ArtifactEvidence{}, err
	}
	source, err := validateArtifactSource(request.Source)
	if err != nil {
		return "", "", models.ArtifactSource{}, models.ArtifactEvidence{}, err
	}
	evidence, err := validateArtifactEvidence(request.Evidence)
	if err != nil {
		return "", "", models.ArtifactSource{}, models.ArtifactEvidence{}, err
	}
	return image, digest, source, evidence, nil
}

func validateDigestPinnedImage(value string) (string, string, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 512 || !utf8.ValidString(value) {
		return "", "", fmt.Errorf("%w: image must contain 1 to 512 characters", ErrInvalidArtifactData)
	}
	named, err := reference.ParseNormalizedNamed(value)
	if err != nil {
		return "", "", fmt.Errorf("%w: image must be a valid OCI image reference", ErrInvalidArtifactData)
	}
	digested, ok := named.(reference.Digested)
	if !ok || digested.Digest().Validate() != nil {
		return "", "", fmt.Errorf("%w: image must be pinned by an immutable digest", ErrInvalidArtifactData)
	}
	digest := digested.Digest().String()
	if !strings.HasPrefix(digest, "sha256:") || len(digest) != len("sha256:")+64 {
		return "", "", fmt.Errorf("%w: image digest must use sha256", ErrInvalidArtifactData)
	}
	image := reference.TrimNamed(named).Name() + "@" + digest
	return image, digest, nil
}

func validateArtifactSource(source models.ArtifactSource) (models.ArtifactSource, error) {
	repository, err := validateSafeHTTPSReference(source.Repository, "source.repository", true, ErrInvalidArtifactData)
	if err != nil {
		return models.ArtifactSource{}, err
	}
	ref := strings.TrimSpace(source.Ref)
	if ref == "" || len(ref) > 512 || !utf8.ValidString(ref) {
		return models.ArtifactSource{}, fmt.Errorf("%w: source.ref must contain 1 to 512 characters", ErrInvalidArtifactData)
	}
	sha := strings.ToLower(strings.TrimSpace(source.SHA))
	if !sourceSHAPattern.MatchString(sha) {
		return models.ArtifactSource{}, fmt.Errorf("%w: source.sha must be a full 40 or 64 character hexadecimal commit identifier", ErrInvalidArtifactData)
	}
	return models.ArtifactSource{Repository: repository, Ref: ref, SHA: sha}, nil
}

func validateArtifactEvidence(evidence models.ArtifactEvidence) (models.ArtifactEvidence, error) {
	var err error
	if evidence.SBOM, err = validateSafeHTTPSReference(evidence.SBOM, "evidence.sbom", false, ErrInvalidArtifactData); err != nil {
		return models.ArtifactEvidence{}, err
	}
	if evidence.Provenance, err = validateSafeHTTPSReference(evidence.Provenance, "evidence.provenance", false, ErrInvalidArtifactData); err != nil {
		return models.ArtifactEvidence{}, err
	}
	if evidence.Scan, err = validateSafeHTTPSReference(evidence.Scan, "evidence.scan", false, ErrInvalidArtifactData); err != nil {
		return models.ArtifactEvidence{}, err
	}
	if evidence.CIRun, err = validateSafeHTTPSReference(evidence.CIRun, "evidence.ciRun", false, ErrInvalidArtifactData); err != nil {
		return models.ArtifactEvidence{}, err
	}
	return evidence, nil
}

func validateReleaseRequest(request models.CreateReleaseRequest) (string, []primitive.ObjectID, models.ReleaseSource, string, error) {
	revision := strings.TrimSpace(request.ApplicationRevision)
	if !applicationRevisionPattern.MatchString(revision) {
		return "", nil, "", "", fmt.Errorf("%w: applicationRevision must contain 1 to 128 safe characters", ErrInvalidReleaseData)
	}
	if len(request.ArtifactIDs) == 0 || len(request.ArtifactIDs) > 100 {
		return "", nil, "", "", fmt.Errorf("%w: artifactIds must contain 1 to 100 unique resource identifiers", ErrInvalidReleaseData)
	}
	artifactIDs := make([]primitive.ObjectID, 0, len(request.ArtifactIDs))
	seen := make(map[primitive.ObjectID]struct{}, len(request.ArtifactIDs))
	for _, value := range request.ArtifactIDs {
		artifactID, err := primitive.ObjectIDFromHex(value)
		if err != nil {
			return "", nil, "", "", fmt.Errorf("%w: artifactIds contains an invalid resource identifier", ErrInvalidReleaseData)
		}
		if _, duplicate := seen[artifactID]; duplicate {
			return "", nil, "", "", fmt.Errorf("%w: artifactIds must not contain duplicates", ErrInvalidReleaseData)
		}
		seen[artifactID] = struct{}{}
		artifactIDs = append(artifactIDs, artifactID)
	}
	sort.Slice(artifactIDs, func(i, j int) bool {
		return artifactIDs[i].Hex() < artifactIDs[j].Hex()
	})
	source := models.ReleaseSource(strings.ToLower(strings.TrimSpace(string(request.Source))))
	if !source.IsValid() {
		return "", nil, "", "", fmt.Errorf("%w: source must be external-ci or manual", ErrInvalidReleaseData)
	}
	sourceReference, err := validateSafeHTTPSReference(request.SourceReference, "sourceReference", source == models.ReleaseSourceExternalCI, ErrInvalidReleaseData)
	if err != nil {
		return "", nil, "", "", err
	}
	return revision, artifactIDs, source, sourceReference, nil
}

func validateSafeHTTPSReference(value, field string, required bool, sentinel error) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		if required {
			return "", fmt.Errorf("%w: %s is required", sentinel, field)
		}
		return "", nil
	}
	if len(value) > 2048 || !utf8.ValidString(value) {
		return "", fmt.Errorf("%w: %s must be a valid HTTPS URL", sentinel, field)
	}
	parsed, err := url.ParseRequestURI(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.ForceQuery || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("%w: %s must be an HTTPS URL without credentials, query parameters, or fragments", sentinel, field)
	}
	return parsed.String(), nil
}

func releaseRequestHash(
	applicationID primitive.ObjectID,
	revision string,
	artifactIDs []primitive.ObjectID,
	source models.ReleaseSource,
	sourceReference string,
) (string, error) {
	ids := make([]string, len(artifactIDs))
	for index, artifactID := range artifactIDs {
		ids[index] = artifactID.Hex()
	}
	return hashJSON(struct {
		ApplicationID       string               `json:"applicationId"`
		ApplicationRevision string               `json:"applicationRevision"`
		ArtifactIDs         []string             `json:"artifactIds"`
		Source              models.ReleaseSource `json:"source"`
		SourceReference     string               `json:"sourceReference"`
	}{applicationID.Hex(), revision, ids, source, sourceReference})
}

func hashJSON(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:]), nil
}

func artifactResponse(artifact models.Artifact) models.ArtifactResponse {
	return models.ArtifactResponse{
		ID:          artifact.ID.Hex(),
		WorkspaceID: artifact.WorkspaceID.Hex(),
		Image:       artifact.Image,
		Digest:      artifact.Digest,
		Source:      artifact.Source,
		Evidence:    artifact.Evidence,
		CreatedBy:   artifact.CreatedBy.Hex(),
		CreatedAt:   artifact.CreatedAt,
	}
}

func releaseResponse(release models.Release) models.ReleaseResponse {
	artifactIDs := make([]string, len(release.ArtifactIDs))
	for index, artifactID := range release.ArtifactIDs {
		artifactIDs[index] = artifactID.Hex()
	}
	return models.ReleaseResponse{
		ID:                  release.ID.Hex(),
		WorkspaceID:         release.WorkspaceID.Hex(),
		ApplicationID:       release.ApplicationID.Hex(),
		ApplicationRevision: release.ApplicationRevision,
		ArtifactIDs:         artifactIDs,
		Source:              release.Source,
		SourceReference:     release.SourceReference,
		CreatedBy:           release.CreatedBy.Hex(),
		CreatedAt:           release.CreatedAt,
	}
}

func mapArtifactReleaseRepositoryError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, repositories.ErrArtifactNotFound):
		return ErrArtifactNotFound
	case errors.Is(err, repositories.ErrReleaseNotFound):
		return ErrReleaseNotFound
	case errors.Is(err, repositories.ErrApplicationNotFound):
		return ErrApplicationNotFound
	default:
		return err
	}
}
