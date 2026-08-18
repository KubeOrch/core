package openapicheck

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/oasdiff/oasdiff/checker"
	"github.com/oasdiff/oasdiff/diff"
	"github.com/oasdiff/oasdiff/load"
)

const (
	stabilityExtension   = "x-stability-level"
	boundaryExtension    = "x-kubeorch-workspace-boundary"
	scopesExtension      = "x-kubeorch-required-scopes"
	idempotencyExtension = "x-kubeorch-idempotency"
)

var (
	operationIDPattern = regexp.MustCompile(`^[a-z][A-Za-z0-9]*$`)
	scopePattern       = regexp.MustCompile(`^[a-z][a-z0-9-]*:[a-z][a-z0-9-]*$`)
)

// Violation describes a KubeOrch convention that is missing or invalid.
type Violation struct {
	Location string
	Message  string
}

func (v Violation) String() string {
	return fmt.Sprintf("%s: %s", v.Location, v.Message)
}

// BreakingChange is a client-visible incompatibility between two contracts.
type BreakingChange struct {
	Level       string
	ID          string
	Method      string
	Path        string
	Description string
}

// Validate checks OpenAPI structure and KubeOrch-specific contract conventions.
func Validate(data []byte, source string) ([]Violation, error) {
	doc, err := loadAndValidate(data, source)
	if err != nil {
		return nil, err
	}

	return validateConventions(doc), nil
}

// Compare returns WARN- and ERR-level backward incompatibilities.
func Compare(baseData []byte, baseSource string, revisionData []byte, revisionSource string) ([]BreakingChange, error) {
	base, err := loadAndValidate(baseData, baseSource)
	if err != nil {
		return nil, fmt.Errorf("load base contract: %w", err)
	}
	revision, err := loadAndValidate(revisionData, revisionSource)
	if err != nil {
		return nil, fmt.Errorf("load revised contract: %w", err)
	}

	policyChanges := validateNewOperationPolicies(base, revision)
	normalizeAdoptedOperationStability(base, revision)

	baseInfo := &load.SpecInfo{Url: baseSource, Spec: base, Version: base.Info.Version}
	revisionInfo := &load.SpecInfo{Url: revisionSource, Spec: revision, Version: revision.Info.Version}
	diffReport, operationSources, err := diff.GetWithOperationsSourcesMap(diff.NewConfig(), baseInfo, revisionInfo)
	if err != nil {
		return nil, fmt.Errorf("compare contracts: %w", err)
	}

	localizer := checker.NewDefaultLocalizer()
	changes := checker.CheckBackwardCompatibility(
		checker.NewConfig(checker.GetAllChecks()),
		diffReport,
		operationSources,
	)
	result := make([]BreakingChange, 0, len(changes)+len(policyChanges))
	for _, change := range changes {
		result = append(result, BreakingChange{
			Level:       change.GetLevel().String(),
			ID:          change.GetId(),
			Method:      change.GetOperation(),
			Path:        change.GetPath(),
			Description: change.GetUncolorizedText(localizer),
		})
	}
	result = append(result, policyChanges...)

	sort.Slice(result, func(i, j int) bool {
		left := result[i].Path + result[i].Method + result[i].ID
		right := result[j].Path + result[j].Method + result[j].ID
		return left < right
	})
	return result, nil
}

func validateNewOperationPolicies(base, revision *openapi3.T) []BreakingChange {
	var changes []BreakingChange
	for path, revisionPathItem := range revision.Paths.Map() {
		basePathItem := base.Paths.Value(path)
		for method, operation := range revisionPathItem.Operations() {
			if basePathItem != nil && basePathItem.GetOperation(method) != nil {
				continue
			}

			boundary, _ := stringExtension(operation.Extensions, boundaryExtension)
			protected := isProtectedOperation(revision, operation)
			if boundary == "legacy-user" {
				changes = append(changes, BreakingChange{
					Level:       "ERR",
					ID:          "new-operation-uses-legacy-user-boundary",
					Method:      strings.ToUpper(method),
					Path:        path,
					Description: "legacy-user is reserved for operations already present in the base contract",
				})
			} else if protected && !oneOf(boundary, "identity", "workspace") {
				changes = append(changes, BreakingChange{
					Level:       "ERR",
					ID:          "new-protected-operation-invalid-boundary",
					Method:      strings.ToUpper(method),
					Path:        path,
					Description: "a new protected operation must use the workspace or identity boundary",
				})
			}

			if !isMutation(method) || !protected {
				continue
			}
			mode, ok := stringExtension(operation.Extensions, idempotencyExtension)
			if !ok || !oneOf(mode, "required", "inherent", "not-applicable") {
				changes = append(changes, BreakingChange{
					Level:       "ERR",
					ID:          "new-protected-mutation-missing-idempotency",
					Method:      strings.ToUpper(method),
					Path:        path,
					Description: idempotencyExtension + " must be required, inherent, or not-applicable for a new protected mutation",
				})
			} else if mode == "required" && !hasParameter(revisionPathItem, operation, "Idempotency-Key", openapi3.ParameterInHeader) {
				changes = append(changes, BreakingChange{
					Level:       "ERR",
					ID:          "new-protected-mutation-invalid-idempotency",
					Method:      strings.ToUpper(method),
					Path:        path,
					Description: "required idempotency must reference the IdempotencyKey header parameter",
				})
			}
		}
	}
	return changes
}

// Missing stability metadata predates this contract. The first explicit value
// establishes its baseline; subsequent comparisons retain normal downgrade checks.
func normalizeAdoptedOperationStability(base, revision *openapi3.T) {
	for path, basePathItem := range base.Paths.Map() {
		revisionPathItem := revision.Paths.Value(path)
		if revisionPathItem == nil {
			continue
		}
		for method, baseOperation := range basePathItem.Operations() {
			revisionOperation := revisionPathItem.GetOperation(method)
			if revisionOperation == nil {
				continue
			}
			if _, found := stringExtension(baseOperation.Extensions, stabilityExtension); found {
				continue
			}
			stability, found := stringExtension(revisionOperation.Extensions, stabilityExtension)
			if !found {
				continue
			}
			if baseOperation.Extensions == nil {
				baseOperation.Extensions = make(map[string]any)
			}
			baseOperation.Extensions[stabilityExtension] = stability
		}
	}
}

func loadAndValidate(data []byte, source string) (*openapi3.T, error) {
	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = false
	doc, err := loader.LoadFromData(data)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", source, err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		return nil, fmt.Errorf("validate %s: %w", source, err)
	}
	return doc, nil
}

func validateConventions(doc *openapi3.T) []Violation {
	var violations []Violation
	operationIDs := make(map[string]string)
	paths := doc.Paths.Map()
	pathNames := make([]string, 0, len(paths))
	for path := range paths {
		pathNames = append(pathNames, path)
	}
	sort.Strings(pathNames)

	for _, path := range pathNames {
		pathItem := paths[path]
		operations := pathItem.Operations()
		methods := make([]string, 0, len(operations))
		for method := range operations {
			methods = append(methods, method)
		}
		sort.Strings(methods)

		for _, method := range methods {
			operation := operations[method]
			location := strings.ToUpper(method) + " " + path
			violations = append(violations, validateOperation(doc, pathItem, path, method, operation, location)...)

			if operation.OperationID == "" {
				continue
			}
			if previous, found := operationIDs[operation.OperationID]; found {
				violations = append(violations, Violation{
					Location: location,
					Message:  fmt.Sprintf("operationId %q is already used by %s", operation.OperationID, previous),
				})
			} else {
				operationIDs[operation.OperationID] = location
			}
		}
	}

	return violations
}

func validateOperation(doc *openapi3.T, pathItem *openapi3.PathItem, path, method string, operation *openapi3.Operation, location string) []Violation {
	var violations []Violation
	if operation.OperationID == "" {
		violations = append(violations, Violation{Location: location, Message: "operationId is required"})
	} else if !operationIDPattern.MatchString(operation.OperationID) {
		violations = append(violations, Violation{Location: location, Message: "operationId must use lowerCamelCase ASCII letters and digits"})
	}

	stability, found := stringExtension(operation.Extensions, stabilityExtension)
	if !found {
		violations = append(violations, Violation{Location: location, Message: stabilityExtension + " is required"})
	} else if !oneOf(stability, "draft", "alpha", "beta", "stable") {
		violations = append(violations, Violation{Location: location, Message: stabilityExtension + " must be draft, alpha, beta, or stable"})
	}

	boundary, found := stringExtension(operation.Extensions, boundaryExtension)
	if !found {
		violations = append(violations, Violation{Location: location, Message: boundaryExtension + " is required"})
	} else if !oneOf(boundary, "none", "identity", "legacy-user", "workspace") {
		violations = append(violations, Violation{Location: location, Message: boundaryExtension + " must be none, identity, legacy-user, or workspace"})
	}

	protected := isProtectedOperation(doc, operation)
	if protected {
		scopes, ok := stringSliceExtension(operation.Extensions, scopesExtension)
		if !ok || len(scopes) == 0 {
			violations = append(violations, Violation{Location: location, Message: scopesExtension + " must contain at least one scope for a protected operation"})
		} else {
			for _, scope := range scopes {
				if !scopePattern.MatchString(scope) {
					violations = append(violations, Violation{Location: location, Message: fmt.Sprintf("scope %q must use resource:action", scope)})
				}
			}
		}
		if boundary == "none" {
			violations = append(violations, Violation{Location: location, Message: "a protected operation cannot use the none workspace boundary"})
		}
	}

	hasWorkspacePath := strings.Contains(path, "{workspaceId}")
	if hasWorkspacePath && boundary != "workspace" {
		violations = append(violations, Violation{Location: location, Message: "a {workspaceId} route must use the workspace boundary"})
	}
	if boundary == "workspace" {
		if !hasWorkspacePath {
			violations = append(violations, Violation{Location: location, Message: "the workspace boundary requires {workspaceId} in the route"})
		} else if !hasParameter(pathItem, operation, "workspaceId", openapi3.ParameterInPath) {
			violations = append(violations, Violation{Location: location, Message: "workspace routes must reference the WorkspaceId path parameter"})
		}
	}

	if operation.Deprecated {
		violations = append(violations, validateDeprecation(operation, location)...)
	}

	if isMutation(method) && boundary != "" && boundary != "none" && boundary != "legacy-user" {
		mode, ok := stringExtension(operation.Extensions, idempotencyExtension)
		if !ok || !oneOf(mode, "required", "inherent", "not-applicable") {
			violations = append(violations, Violation{Location: location, Message: idempotencyExtension + " must be required, inherent, or not-applicable for new protected mutations"})
		} else if mode == "required" && !hasParameter(pathItem, operation, "Idempotency-Key", openapi3.ParameterInHeader) {
			violations = append(violations, Violation{Location: location, Message: "required idempotency must reference the IdempotencyKey header parameter"})
		}
	}

	return violations
}

func isProtectedOperation(doc *openapi3.T, operation *openapi3.Operation) bool {
	security := doc.Security
	if operation.Security != nil {
		security = *operation.Security
	}
	return len(security) > 0
}

func validateDeprecation(operation *openapi3.Operation, location string) []Violation {
	var violations []Violation
	for _, extension := range []string{"x-kubeorch-deprecated-at", "x-kubeorch-sunset-at"} {
		value, ok := stringExtension(operation.Extensions, extension)
		if !ok {
			violations = append(violations, Violation{Location: location, Message: extension + " is required when deprecated is true"})
			continue
		}
		if _, err := time.Parse(time.DateOnly, value); err != nil {
			violations = append(violations, Violation{Location: location, Message: extension + " must use YYYY-MM-DD"})
		}
	}
	if reason, ok := stringExtension(operation.Extensions, "x-kubeorch-deprecation-reason"); !ok || strings.TrimSpace(reason) == "" {
		violations = append(violations, Violation{Location: location, Message: "x-kubeorch-deprecation-reason is required when deprecated is true"})
	}
	return violations
}

func hasParameter(pathItem *openapi3.PathItem, operation *openapi3.Operation, name, in string) bool {
	parameters := append(openapi3.Parameters{}, pathItem.Parameters...)
	parameters = append(parameters, operation.Parameters...)
	for _, parameterRef := range parameters {
		if parameterRef != nil && parameterRef.Value != nil && parameterRef.Value.Name == name && parameterRef.Value.In == in {
			return true
		}
	}
	return false
}

func stringExtension(extensions map[string]any, name string) (string, bool) {
	value, found := extensions[name]
	if !found {
		return "", false
	}
	result, ok := value.(string)
	return result, ok
}

func stringSliceExtension(extensions map[string]any, name string) ([]string, bool) {
	value, found := extensions[name]
	if !found {
		return nil, false
	}
	switch typed := value.(type) {
	case []string:
		return typed, true
	case []any:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			text, ok := item.(string)
			if !ok {
				return nil, false
			}
			result = append(result, text)
		}
		return result, true
	default:
		return nil, false
	}
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func isMutation(method string) bool {
	switch strings.ToUpper(method) {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}
