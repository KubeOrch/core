package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/KubeOrch/core/models"
	"github.com/KubeOrch/core/repositories"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

var (
	ErrEnvironmentNotFound       = errors.New("environment not found")
	ErrApplicationNotFound       = errors.New("application not found")
	ErrEnvironmentNameConflict   = errors.New("environment name already exists")
	ErrDomainIdempotencyConflict = errors.New("idempotency key reused with different input")
	ErrInvalidEnvironmentData    = errors.New("invalid environment data")
	ErrInvalidApplicationData    = errors.New("invalid application data")
)

type EnvironmentApplicationStore interface {
	CreateEnvironment(context.Context, *models.Environment) error
	GetEnvironmentByCreationKey(context.Context, primitive.ObjectID, primitive.ObjectID, string) (*models.Environment, error)
	ListEnvironments(context.Context, primitive.ObjectID, int, string) ([]models.Environment, string, error)
	GetEnvironment(context.Context, primitive.ObjectID, primitive.ObjectID) (*models.Environment, error)
	UpdateEnvironment(context.Context, primitive.ObjectID, primitive.ObjectID, bson.M) (*models.Environment, error)
	CreateApplication(context.Context, *models.Application) error
	GetApplicationByCreationKey(context.Context, primitive.ObjectID, primitive.ObjectID, string) (*models.Application, error)
	ListApplications(context.Context, primitive.ObjectID, *primitive.ObjectID, bool, int, string) ([]models.Application, string, error)
	GetApplication(context.Context, primitive.ObjectID, primitive.ObjectID) (*models.Application, error)
	UpdateApplication(context.Context, primitive.ObjectID, primitive.ObjectID, bson.M) (*models.Application, error)
	ArchiveApplication(context.Context, primitive.ObjectID, primitive.ObjectID, time.Time) (*models.Application, error)
}

type EnvironmentApplicationService struct {
	store EnvironmentApplicationStore
	now   func() time.Time
}

func NewEnvironmentApplicationService(store EnvironmentApplicationStore) *EnvironmentApplicationService {
	return &EnvironmentApplicationService{
		store: store,
		now:   func() time.Time { return time.Now().UTC() },
	}
}

func (s *EnvironmentApplicationService) CreateEnvironment(
	ctx context.Context,
	actorID, workspaceID primitive.ObjectID,
	request models.CreateEnvironmentRequest,
	idempotencyKey string,
) (models.EnvironmentResponse, bool, error) {
	name, normalizedName, description, err := validateEnvironmentFields(request.Name, request.Description)
	if err != nil {
		return models.EnvironmentResponse{}, false, err
	}
	requestHash := environmentRequestHash(name, description)
	now := s.now()
	environment := &models.Environment{
		ID:             primitive.NewObjectIDFromTimestamp(now),
		WorkspaceID:    workspaceID,
		Name:           name,
		NormalizedName: normalizedName,
		Description:    description,
		CreatedBy:      actorID,
		CreationKey:    idempotencyKey,
		CreationHash:   requestHash,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	if err := s.store.CreateEnvironment(ctx, environment); err != nil {
		if errors.Is(err, repositories.ErrEnvironmentNameExists) {
			return models.EnvironmentResponse{}, false, ErrEnvironmentNameConflict
		}
		if !errors.Is(err, repositories.ErrDuplicateEnvironmentCreationKey) {
			return models.EnvironmentResponse{}, false, err
		}
		existing, getErr := s.store.GetEnvironmentByCreationKey(ctx, workspaceID, actorID, idempotencyKey)
		if getErr != nil {
			return models.EnvironmentResponse{}, false, mapDomainRepositoryError(getErr)
		}
		if existing.CreationHash != requestHash {
			return models.EnvironmentResponse{}, false, ErrDomainIdempotencyConflict
		}
		return environmentResponse(*existing), true, nil
	}
	return environmentResponse(*environment), false, nil
}

func (s *EnvironmentApplicationService) ListEnvironments(
	ctx context.Context,
	workspaceID primitive.ObjectID,
	limit int,
	cursor string,
) (models.EnvironmentListResponse, error) {
	environments, nextCursor, err := s.store.ListEnvironments(ctx, workspaceID, limit, cursor)
	if err != nil {
		return models.EnvironmentListResponse{}, mapDomainRepositoryError(err)
	}
	items := make([]models.EnvironmentResponse, 0, len(environments))
	for _, environment := range environments {
		items = append(items, environmentResponse(environment))
	}
	return models.EnvironmentListResponse{Items: items, PageInfo: pageInfo(nextCursor)}, nil
}

func (s *EnvironmentApplicationService) GetEnvironment(
	ctx context.Context,
	workspaceID, environmentID primitive.ObjectID,
) (models.EnvironmentResponse, error) {
	environment, err := s.store.GetEnvironment(ctx, workspaceID, environmentID)
	if err != nil {
		return models.EnvironmentResponse{}, mapDomainRepositoryError(err)
	}
	return environmentResponse(*environment), nil
}

func (s *EnvironmentApplicationService) UpdateEnvironment(
	ctx context.Context,
	workspaceID, environmentID primitive.ObjectID,
	request models.UpdateEnvironmentRequest,
) (models.EnvironmentResponse, error) {
	updates := bson.M{}
	if request.Name != nil {
		name, normalizedName, err := validateEnvironmentName(*request.Name)
		if err != nil {
			return models.EnvironmentResponse{}, err
		}
		updates["name"] = name
		updates["normalized_name"] = normalizedName
	}
	if request.Description != nil {
		description, err := validateDomainDescription(*request.Description, ErrInvalidEnvironmentData)
		if err != nil {
			return models.EnvironmentResponse{}, err
		}
		updates["description"] = description
	}
	if len(updates) == 0 {
		return models.EnvironmentResponse{}, fmt.Errorf("%w: at least one field must be provided", ErrInvalidEnvironmentData)
	}
	environment, err := s.store.UpdateEnvironment(ctx, workspaceID, environmentID, updates)
	if err != nil {
		return models.EnvironmentResponse{}, mapDomainRepositoryError(err)
	}
	return environmentResponse(*environment), nil
}

func (s *EnvironmentApplicationService) CreateApplication(
	ctx context.Context,
	actorID, workspaceID, environmentID primitive.ObjectID,
	request models.CreateApplicationRequest,
	idempotencyKey string,
) (models.ApplicationResponse, bool, error) {
	if _, err := s.store.GetEnvironment(ctx, workspaceID, environmentID); err != nil {
		return models.ApplicationResponse{}, false, mapDomainRepositoryError(err)
	}
	name, description, desiredState, err := validateApplicationFields(request.Name, request.Description, map[string]any(request.DesiredState))
	if err != nil {
		return models.ApplicationResponse{}, false, err
	}
	requestHash, err := applicationRequestHash(environmentID, name, description, desiredState)
	if err != nil {
		return models.ApplicationResponse{}, false, err
	}
	now := s.now()
	application := &models.Application{
		ID:            primitive.NewObjectIDFromTimestamp(now),
		WorkspaceID:   workspaceID,
		EnvironmentID: environmentID,
		Name:          name,
		Description:   description,
		DesiredState:  desiredState,
		CreatedBy:     actorID,
		CreationKey:   idempotencyKey,
		CreationHash:  requestHash,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	if err := s.store.CreateApplication(ctx, application); err != nil {
		if !errors.Is(err, repositories.ErrDuplicateApplicationCreationKey) {
			return models.ApplicationResponse{}, false, err
		}
		existing, getErr := s.store.GetApplicationByCreationKey(ctx, workspaceID, actorID, idempotencyKey)
		if getErr != nil {
			return models.ApplicationResponse{}, false, mapDomainRepositoryError(getErr)
		}
		if existing.CreationHash != requestHash {
			return models.ApplicationResponse{}, false, ErrDomainIdempotencyConflict
		}
		return applicationResponse(*existing), true, nil
	}
	return applicationResponse(*application), false, nil
}

func (s *EnvironmentApplicationService) ListApplications(
	ctx context.Context,
	workspaceID primitive.ObjectID,
	environmentID *primitive.ObjectID,
	includeArchived bool,
	limit int,
	cursor string,
) (models.ApplicationListResponse, error) {
	if environmentID != nil {
		if _, err := s.store.GetEnvironment(ctx, workspaceID, *environmentID); err != nil {
			return models.ApplicationListResponse{}, mapDomainRepositoryError(err)
		}
	}
	applications, nextCursor, err := s.store.ListApplications(ctx, workspaceID, environmentID, includeArchived, limit, cursor)
	if err != nil {
		return models.ApplicationListResponse{}, mapDomainRepositoryError(err)
	}
	items := make([]models.ApplicationResponse, 0, len(applications))
	for _, application := range applications {
		items = append(items, applicationResponse(application))
	}
	return models.ApplicationListResponse{Items: items, PageInfo: pageInfo(nextCursor)}, nil
}

func (s *EnvironmentApplicationService) GetApplication(
	ctx context.Context,
	workspaceID, applicationID primitive.ObjectID,
) (models.ApplicationResponse, error) {
	application, err := s.store.GetApplication(ctx, workspaceID, applicationID)
	if err != nil {
		return models.ApplicationResponse{}, mapDomainRepositoryError(err)
	}
	return applicationResponse(*application), nil
}

func (s *EnvironmentApplicationService) UpdateApplication(
	ctx context.Context,
	workspaceID, applicationID primitive.ObjectID,
	request models.UpdateApplicationRequest,
) (models.ApplicationResponse, error) {
	updates := bson.M{}
	if request.Name != nil {
		name, err := validateDomainName(*request.Name, ErrInvalidApplicationData)
		if err != nil {
			return models.ApplicationResponse{}, err
		}
		updates["name"] = name
	}
	if request.Description != nil {
		description, err := validateDomainDescription(*request.Description, ErrInvalidApplicationData)
		if err != nil {
			return models.ApplicationResponse{}, err
		}
		updates["description"] = description
	}
	if request.DesiredState.Set {
		desiredState, err := validateDesiredState(request.DesiredState.Value)
		if err != nil {
			return models.ApplicationResponse{}, err
		}
		updates["desired_state"] = desiredState
	}
	if len(updates) == 0 {
		return models.ApplicationResponse{}, fmt.Errorf("%w: at least one field must be provided", ErrInvalidApplicationData)
	}
	application, err := s.store.UpdateApplication(ctx, workspaceID, applicationID, updates)
	if err != nil {
		return models.ApplicationResponse{}, mapDomainRepositoryError(err)
	}
	return applicationResponse(*application), nil
}

func (s *EnvironmentApplicationService) ArchiveApplication(
	ctx context.Context,
	workspaceID, applicationID primitive.ObjectID,
) (models.ApplicationResponse, error) {
	application, err := s.store.ArchiveApplication(ctx, workspaceID, applicationID, s.now())
	if err != nil {
		return models.ApplicationResponse{}, mapDomainRepositoryError(err)
	}
	return applicationResponse(*application), nil
}

func validateEnvironmentFields(name, description string) (string, string, string, error) {
	name, normalizedName, err := validateEnvironmentName(name)
	if err != nil {
		return "", "", "", err
	}
	description, err = validateDomainDescription(description, ErrInvalidEnvironmentData)
	if err != nil {
		return "", "", "", err
	}
	return name, normalizedName, description, nil
}

func validateEnvironmentName(name string) (string, string, error) {
	name, err := validateDomainName(name, ErrInvalidEnvironmentData)
	if err != nil {
		return "", "", err
	}
	normalizedName := strings.ToLower(strings.Join(strings.Fields(name), " "))
	return name, normalizedName, nil
}

func validateApplicationFields(name, description string, desiredState map[string]any) (string, string, map[string]any, error) {
	name, err := validateDomainName(name, ErrInvalidApplicationData)
	if err != nil {
		return "", "", nil, err
	}
	description, err = validateDomainDescription(description, ErrInvalidApplicationData)
	if err != nil {
		return "", "", nil, err
	}
	desiredState, err = validateDesiredState(desiredState)
	if err != nil {
		return "", "", nil, err
	}
	return name, description, desiredState, nil
}

func validateDomainName(name string, sentinel error) (string, error) {
	name = strings.TrimSpace(name)
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 100 {
		return "", fmt.Errorf("%w: name must contain 1 to 100 characters", sentinel)
	}
	return name, nil
}

func validateDomainDescription(description string, sentinel error) (string, error) {
	description = strings.TrimSpace(description)
	if !utf8.ValidString(description) || utf8.RuneCountInString(description) > 1000 {
		return "", fmt.Errorf("%w: description must contain at most 1000 characters", sentinel)
	}
	return description, nil
}

func validateDesiredState(desiredState map[string]any) (map[string]any, error) {
	if desiredState == nil {
		return map[string]any{}, nil
	}
	value, err := cloneDesiredValue(desiredState, 0, "/desiredState")
	if err != nil {
		return nil, err
	}
	return value.(map[string]any), nil
}

func cloneDesiredValue(value any, depth int, path string) (any, error) {
	if depth > 20 {
		return nil, fmt.Errorf("%w: desiredState must not exceed 20 nested levels", ErrInvalidApplicationData)
	}
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, child := range typed {
			if key == "" || strings.HasPrefix(key, "$") || strings.Contains(key, ".") {
				return nil, fmt.Errorf("%w: %s contains an unsupported field name", ErrInvalidApplicationData, path)
			}
			if isSensitiveDesiredStateKey(key) {
				return nil, fmt.Errorf("%w: %s/%s must use a non-secret reference field instead", ErrInvalidApplicationData, path, key)
			}
			cloned, err := cloneDesiredValue(child, depth+1, path+"/"+key)
			if err != nil {
				return nil, err
			}
			result[key] = cloned
		}
		return result, nil
	case []any:
		result := make([]any, len(typed))
		for index, child := range typed {
			cloned, err := cloneDesiredValue(child, depth+1, fmt.Sprintf("%s/%d", path, index))
			if err != nil {
				return nil, err
			}
			result[index] = cloned
		}
		return result, nil
	case nil, bool, string, float32, float64, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return typed, nil
	default:
		return nil, fmt.Errorf("%w: %s contains an unsupported value", ErrInvalidApplicationData, path)
	}
}

func isSensitiveDesiredStateKey(key string) bool {
	normalized := strings.ToLower(strings.NewReplacer("_", "", "-", "", " ", "").Replace(key))
	if strings.HasSuffix(normalized, "ref") || strings.HasSuffix(normalized, "reference") {
		return false
	}
	for _, suffix := range []string{
		"password",
		"passwordhash",
		"passwd",
		"secret",
		"secretaccesskey",
		"secretdata",
		"secretvalue",
		"token",
		"credential",
		"credentials",
		"privatekey",
		"apikey",
	} {
		if strings.HasSuffix(normalized, suffix) {
			return true
		}
	}
	switch normalized {
	case "authorization", "authorizationheader", "kubeconfig", "stringdata":
		return true
	default:
		return false
	}
}

func environmentRequestHash(name, description string) string {
	digest := sha256.Sum256([]byte(name + "\x00" + description))
	return hex.EncodeToString(digest[:])
}

func applicationRequestHash(environmentID primitive.ObjectID, name, description string, desiredState map[string]any) (string, error) {
	desiredStateJSON, err := json.Marshal(desiredState)
	if err != nil {
		return "", fmt.Errorf("hash desired state: %w", err)
	}
	payload := append([]byte(environmentID.Hex()+"\x00"+name+"\x00"+description+"\x00"), desiredStateJSON...)
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

func environmentResponse(environment models.Environment) models.EnvironmentResponse {
	return models.EnvironmentResponse{
		ID:          environment.ID.Hex(),
		WorkspaceID: environment.WorkspaceID.Hex(),
		Name:        environment.Name,
		Description: environment.Description,
		CreatedAt:   environment.CreatedAt,
		UpdatedAt:   environment.UpdatedAt,
	}
}

func applicationResponse(application models.Application) models.ApplicationResponse {
	status := models.ApplicationStatusDraft
	if application.ArchivedAt != nil {
		status = models.ApplicationStatusArchived
	}
	desiredState, err := validateDesiredState(application.DesiredState)
	if err != nil {
		desiredState = application.DesiredState
	}
	if desiredState == nil {
		desiredState = map[string]any{}
	}
	return models.ApplicationResponse{
		ID:            application.ID.Hex(),
		WorkspaceID:   application.WorkspaceID.Hex(),
		EnvironmentID: application.EnvironmentID.Hex(),
		Name:          application.Name,
		Description:   application.Description,
		DesiredState:  desiredState,
		Status:        status,
		ArchivedAt:    application.ArchivedAt,
		CreatedAt:     application.CreatedAt,
		UpdatedAt:     application.UpdatedAt,
	}
}

func mapDomainRepositoryError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, repositories.ErrEnvironmentNotFound):
		return ErrEnvironmentNotFound
	case errors.Is(err, repositories.ErrApplicationNotFound):
		return ErrApplicationNotFound
	case errors.Is(err, repositories.ErrEnvironmentNameExists):
		return ErrEnvironmentNameConflict
	case errors.Is(err, repositories.ErrInvalidCursor):
		return fmt.Errorf("%w: invalid pagination cursor", ErrInvalidApplicationData)
	default:
		return err
	}
}
