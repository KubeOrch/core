package services

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/KubeOrch/core/models"
	"github.com/KubeOrch/core/repositories"
	"github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// ImportService handles importing external configs into workflows
type ImportService struct {
	composeParser      *DockerComposeParser
	nodeConverter      *NodeConverter
	layoutEngine       *LayoutEngine
	gitService         *GitService
	nixpacksService    *NixpacksService
	registryRepository *repositories.RegistryRepository
	importRepository   *repositories.ImportRepository
	broadcaster        *SSEBroadcaster
	logger             *logrus.Logger
}

var importServiceInstance *ImportService

// NewImportService creates a new import service
func NewImportService() *ImportService {
	return &ImportService{
		composeParser:      NewDockerComposeParser(),
		nodeConverter:      NewNodeConverter(),
		layoutEngine:       NewLayoutEngine(),
		gitService:         NewGitService(),
		nixpacksService:    NewNixpacksService(),
		registryRepository: repositories.NewRegistryRepository(),
		importRepository:   repositories.NewImportRepository(),
		broadcaster:        GetSSEBroadcaster(),
		logger:             logrus.New(),
	}
}

// GetImportService returns the singleton import service instance
func GetImportService() *ImportService {
	if importServiceInstance == nil {
		importServiceInstance = NewImportService()
	}
	return importServiceInstance
}

// SetImportService sets the import service instance
func SetImportService(svc *ImportService) {
	importServiceInstance = svc
}

// AnalyzeImport analyzes the import source and returns suggested nodes
func (s *ImportService) AnalyzeImport(ctx context.Context, req *models.ImportRequest) (*models.ImportAnalysis, error) {
	var content []byte
	var filename string
	var repoPath string
	var err error

	switch req.Source {
	case models.ImportSourceFile:
		// Decode base64 file content
		content, err = base64.StdEncoding.DecodeString(req.FileContent)
		if err != nil {
			return nil, fmt.Errorf("failed to decode file content: %w", err)
		}
		filename = req.FileName

	case models.ImportSourceGitHub, models.ImportSourceGitLab, models.ImportSourceGitURL:
		// Clone the repository
		content, filename, repoPath, err = s.fetchFromGit(ctx, req)
		if err != nil {
			return nil, err
		}
		// Clean up after processing
		if repoPath != "" {
			defer s.gitService.CleanupRepository(repoPath)
		}

	default:
		return nil, fmt.Errorf("unsupported import source: %s", req.Source)
	}

	// Determine file type and parse accordingly
	analysis, err := s.parseContent(content, filename)
	if err != nil {
		return nil, err
	}

	// Set namespace
	namespace := req.Namespace
	if namespace == "" {
		namespace = "default"
	}

	// Convert to workflow nodes
	conversionResult := s.nodeConverter.Convert(analysis, namespace)

	// Apply conversion results
	analysis.SuggestedNodes = conversionResult.Nodes
	analysis.SuggestedEdges = conversionResult.Edges
	analysis.Warnings = append(analysis.Warnings, conversionResult.Warnings...)

	// Calculate layout positions
	positions := s.layoutEngine.CalculateLayout(analysis.SuggestedNodes, analysis.SuggestedEdges)
	analysis.LayoutPositions = positions

	// Apply positions to nodes
	analysis.SuggestedNodes = s.layoutEngine.ApplyPositions(analysis.SuggestedNodes, positions)

	// Set build configuration if this was detected via Nixpacks (needs building)
	if filename == "nixpacks-generated.yml" && analysis.DetectedType == "nixpacks" {
		analysis.NeedsBuild = true
		branch := req.Branch
		if branch == "" {
			branch = "main"
		}
		// Extract provider/framework from detected services
		var provider, framework string
		if len(analysis.Services) > 0 && analysis.Services[0].Build != nil {
			// The build context can contain nixpacks info
			provider = analysis.DetectedType
		}
		analysis.SourceBuildConfig = &models.SourceBuildConfig{
			RepoURL:      req.URL,
			Branch:       branch,
			BuildContext: ".",
			UseNixpacks:  true,
			Provider:     provider,
			Framework:    framework,
		}
	}

	return analysis, nil
}

// fetchFromGit fetches content from a Git repository
func (s *ImportService) fetchFromGit(ctx context.Context, req *models.ImportRequest) ([]byte, string, string, error) {
	repoURL := GetRepoCloneURL(req.URL)
	branch := req.Branch
	if branch == "" {
		branch = "main"
	}

	// Try to get GitHub credentials from GHCR registry
	urlInfo, err := s.gitService.ParseGitURL(repoURL)
	if err == nil && urlInfo.IsGitHub {
		s.loadGitHubCredentials(ctx)
	}

	// First, try to fetch docker-compose file directly without cloning
	if err == nil && (urlInfo.IsGitHub || urlInfo.IsGitLab) {
		// Try common docker-compose filenames
		composeFiles := []string{
			"docker-compose.yml",
			"docker-compose.yaml",
			"compose.yml",
			"compose.yaml",
		}

		for _, filename := range composeFiles {
			content, err := s.gitService.FetchFile(ctx, repoURL, filename, branch)
			if err == nil {
				s.logger.Infof("Found %s via direct fetch", filename)
				return content, filename, "", nil
			}
		}
	}

	// If direct fetch failed, clone the repository
	s.logger.Info("Direct file fetch failed, cloning repository...")
	repoPath, err := s.gitService.CloneRepository(ctx, repoURL, branch)
	if err != nil {
		return nil, "", "", fmt.Errorf("failed to clone repository: %w", err)
	}

	// Look for docker-compose file
	composePath, err := s.gitService.FindComposeFile(repoPath)
	if err != nil {
		// No docker-compose found, try Nixpacks detection
		s.logger.Info("No docker-compose found, trying Nixpacks detection...")
		nixpacksResult, nixErr := s.nixpacksService.Detect(ctx, repoPath)
		if nixErr == nil && nixpacksResult != nil {
			// Extract repo name from URL
			urlInfo, _ := s.gitService.ParseGitURL(req.URL)
			repoName := "app"
			if urlInfo != nil {
				repoName = urlInfo.Repo
			}

			// Convert to import analysis format
			analysis := s.nixpacksService.ConvertToImportAnalysis(nixpacksResult, repoName)

			// Marshal to YAML-like content for consistency
			// We'll use a special marker to indicate this is a nixpacks result
			content := fmt.Sprintf("# Auto-detected by Nixpacks\n# Provider: %s\nservices:\n  %s:\n    image: %s:latest\n    ports:\n      - \"%d:%d\"\n",
				nixpacksResult.Provider,
				repoName,
				strings.ToLower(repoName),
				nixpacksResult.DefaultPort,
				nixpacksResult.DefaultPort,
			)

			// Store the analysis in a way that parseContent can use
			_ = analysis // We'll return the content directly

			return []byte(content), "nixpacks-generated.yml", repoPath, nil
		}

		// Check for Dockerfile as last resort
		_, dockerErr := s.gitService.FindDockerfile(repoPath)
		if dockerErr != nil {
			return nil, "", repoPath, fmt.Errorf("no docker-compose.yml or Dockerfile found, and Nixpacks detection failed")
		}

		// Dockerfile found but not docker-compose
		return nil, "", repoPath, fmt.Errorf("only Dockerfile found; please use docker-compose format or ensure project structure is detectable")
	}

	// Read the compose file
	content, err := os.ReadFile(composePath)
	if err != nil {
		return nil, "", repoPath, fmt.Errorf("failed to read docker-compose file: %w", err)
	}

	// Extract filename from path
	filename := composePath[strings.LastIndex(composePath, "/")+1:]

	return content, filename, repoPath, nil
}

// loadGitHubCredentials loads GitHub credentials from GHCR registry if available
func (s *ImportService) loadGitHubCredentials(ctx context.Context) {
	// Try to get GHCR registry credentials (GitHub uses same PAT for both)
	registries, err := s.registryRepository.GetByType(ctx, models.RegistryTypeGHCR)
	if err != nil {
		s.logger.WithError(err).Debug("Failed to get GHCR registries")
		return
	}

	// Use the first available GHCR registry with credentials
	for _, registry := range registries {
		if registry.Credentials.Password != "" {
			s.logger.Info("Using GHCR credentials for GitHub repository access")
			// Use x-access-token as username for git operations (works with both classic and fine-grained PATs)
			s.gitService.SetCredentials("x-access-token", registry.Credentials.Password)
			return
		}
	}

	s.logger.Debug("No GHCR credentials found for GitHub repository access")
}

// parseContent determines the content type and parses it
func (s *ImportService) parseContent(content []byte, filename string) (*models.ImportAnalysis, error) {
	// Check if this is a nixpacks-generated file
	if filename == "nixpacks-generated.yml" {
		analysis, err := s.composeParser.Parse(content)
		if err != nil {
			return nil, err
		}
		analysis.DetectedType = "nixpacks"
		return analysis, nil
	}

	// Determine file type based on filename or content
	isDockerCompose := s.isDockerComposeFile(filename, content)

	if isDockerCompose {
		return s.composeParser.Parse(content)
	}

	// TODO: Add Nixpacks detection for source code directories

	return nil, fmt.Errorf("unable to determine file type for import")
}

// isDockerComposeFile checks if the content is a docker-compose file
func (s *ImportService) isDockerComposeFile(filename string, content []byte) bool {
	// Check filename
	lowerFilename := strings.ToLower(filename)
	if strings.Contains(lowerFilename, "docker-compose") ||
		strings.Contains(lowerFilename, "compose.yml") ||
		strings.Contains(lowerFilename, "compose.yaml") {
		return true
	}

	// Check content for docker-compose indicators
	contentStr := string(content)
	if strings.Contains(contentStr, "services:") &&
		(strings.Contains(contentStr, "version:") || strings.Contains(contentStr, "image:") || strings.Contains(contentStr, "build:")) {
		return true
	}

	return false
}

// ApplyImport applies the analyzed import to a workflow
func (s *ImportService) ApplyImport(ctx context.Context, req *models.ApplyImportRequest, userID primitive.ObjectID) (*models.Workflow, error) {
	workflowID, err := primitive.ObjectIDFromHex(req.WorkflowID)
	if err != nil {
		return nil, fmt.Errorf("invalid workflow ID: %w", err)
	}

	// Get existing workflow
	workflow, err := GetWorkflowByID(workflowID)
	if err != nil {
		return nil, fmt.Errorf("failed to get workflow: %w", err)
	}

	// Verify ownership
	if workflow.OwnerID != userID {
		return nil, fmt.Errorf("not authorized to modify this workflow")
	}

	// Apply positions to nodes
	for i := range req.Nodes {
		if pos, ok := req.Positions[req.Nodes[i].ID]; ok {
			req.Nodes[i].Position = models.Position{
				X: pos.X,
				Y: pos.Y,
			}
		}
	}

	// Merge or replace nodes
	// For simplicity, we're adding to existing nodes
	// A more sophisticated approach would detect duplicates
	newNodes := append(workflow.Nodes, req.Nodes...)
	newEdges := append(workflow.Edges, req.Edges...)

	// Update workflow
	updatedWorkflow, err := UpdateWorkflow(workflowID, bson.M{
		"nodes":      newNodes,
		"edges":      newEdges,
		"updated_at": time.Now(),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to update workflow: %w", err)
	}

	return updatedWorkflow, nil
}

// CreateWorkflowFromImport creates a new workflow from imported content
func (s *ImportService) CreateWorkflowFromImport(ctx context.Context, name string, analysis *models.ImportAnalysis, userID primitive.ObjectID, clusterID string) (*models.Workflow, error) {
	// Apply layout positions
	nodes := s.layoutEngine.ApplyPositions(analysis.SuggestedNodes, analysis.LayoutPositions)

	workflow := &models.Workflow{
		Name:        name,
		Description: fmt.Sprintf("Imported from %s", analysis.DetectedType),
		ClusterID:   clusterID,
		Status:      models.WorkflowStatusDraft,
		Nodes:       nodes,
		Edges:       analysis.SuggestedEdges,
		OwnerID:     userID,
		Tags:        []string{"imported", analysis.DetectedType},
	}

	err := CreateWorkflow(workflow)
	if err != nil {
		return nil, fmt.Errorf("failed to create workflow: %w", err)
	}

	return workflow, nil
}

// AnalyzeImportResult represents the result of an import analysis
// It can be either sync (immediate result) or async (session ID for streaming)
type AnalyzeImportResult struct {
	Async     bool                   `json:"async"`
	Analysis  *models.ImportAnalysis `json:"analysis,omitempty"`
	SessionID string                 `json:"sessionId,omitempty"`
	Message   string                 `json:"message,omitempty"`
}

// TryFastImport attempts a fast import (direct file fetch) and returns nil if clone is needed
func (s *ImportService) TryFastImport(ctx context.Context, req *models.ImportRequest) (*models.ImportAnalysis, error) {
	// File imports are always fast
	if req.Source == models.ImportSourceFile {
		return s.AnalyzeImport(ctx, req)
	}

	// For Git sources, try direct file fetch first
	if req.Source == models.ImportSourceGitHub || req.Source == models.ImportSourceGitLab || req.Source == models.ImportSourceGitURL {
		repoURL := GetRepoCloneURL(req.URL)
		branch := req.Branch
		if branch == "" {
			branch = "main"
		}

		// Try to get GitHub credentials from GHCR registry
		urlInfo, err := s.gitService.ParseGitURL(repoURL)
		if err == nil && urlInfo.IsGitHub {
			s.loadGitHubCredentials(ctx)
		}

		// Try to fetch docker-compose file directly
		if err == nil && (urlInfo.IsGitHub || urlInfo.IsGitLab) {
			composeFiles := []string{
				"docker-compose.yml",
				"docker-compose.yaml",
				"compose.yml",
				"compose.yaml",
			}

			for _, filename := range composeFiles {
				content, fetchErr := s.gitService.FetchFile(ctx, repoURL, filename, branch)
				if fetchErr == nil {
					s.logger.Infof("Found %s via direct fetch (fast path)", filename)
					// Parse and return immediately
					analysis, parseErr := s.parseContent(content, filename)
					if parseErr != nil {
						return nil, parseErr
					}
					return s.processAnalysis(analysis, req)
				}
			}
		}
	}

	// Fast path failed, return nil to indicate async is needed
	return nil, nil
}

// processAnalysis converts parsed content to workflow nodes
func (s *ImportService) processAnalysis(analysis *models.ImportAnalysis, req *models.ImportRequest) (*models.ImportAnalysis, error) {
	namespace := req.Namespace
	if namespace == "" {
		namespace = "default"
	}

	// Convert to workflow nodes
	conversionResult := s.nodeConverter.Convert(analysis, namespace)

	// Apply conversion results
	analysis.SuggestedNodes = conversionResult.Nodes
	analysis.SuggestedEdges = conversionResult.Edges
	analysis.Warnings = append(analysis.Warnings, conversionResult.Warnings...)

	// Calculate layout positions
	positions := s.layoutEngine.CalculateLayout(analysis.SuggestedNodes, analysis.SuggestedEdges)
	analysis.LayoutPositions = positions

	// Apply positions to nodes
	analysis.SuggestedNodes = s.layoutEngine.ApplyPositions(analysis.SuggestedNodes, positions)

	return analysis, nil
}

// StartAsyncImport creates an import session and starts async analysis
func (s *ImportService) StartAsyncImport(ctx context.Context, req *models.ImportRequest, userID primitive.ObjectID) (*models.ImportSession, error) {
	session := &models.ImportSession{
		UserID:    userID,
		Source:    req.Source,
		URL:       req.URL,
		Branch:    req.Branch,
		FileName:  req.FileName,
		Namespace: req.Namespace,
		Status:    models.ImportStatusPending,
	}

	if err := s.importRepository.Create(ctx, session); err != nil {
		return nil, fmt.Errorf("failed to create import session: %w", err)
	}

	s.logger.WithField("session_id", session.ID.Hex()).Info("Starting async import")

	// Execute import in background
	go s.ExecuteAsyncImport(session.ID, req)

	return session, nil
}

// ExecuteAsyncImport runs the import analysis in the background with streaming logs
func (s *ImportService) ExecuteAsyncImport(sessionID primitive.ObjectID, req *models.ImportRequest) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	var repoPath string

	// Stage 1: Cloning
	s.publishImportProgress(sessionID, models.ImportStatusCloning, "Cloning repository", 10)
	s.publishImportLog(sessionID, "clone", fmt.Sprintf("Cloning %s (branch: %s)", req.URL, req.Branch), "info")

	repoURL := GetRepoCloneURL(req.URL)
	branch := req.Branch
	if branch == "" {
		branch = "main"
	}

	// Load credentials if GitHub
	urlInfo, _ := s.gitService.ParseGitURL(repoURL)
	if urlInfo != nil && urlInfo.IsGitHub {
		s.loadGitHubCredentials(ctx)
	}

	var err error
	repoPath, err = s.gitService.CloneRepository(ctx, repoURL, branch)
	if err != nil {
		s.handleImportError(ctx, sessionID, "clone", fmt.Errorf("failed to clone repository: %w", err))
		return
	}
	defer s.gitService.CleanupRepository(repoPath)

	s.publishImportLog(sessionID, "clone", "Repository cloned successfully", "info")
	s.publishImportProgress(sessionID, models.ImportStatusAnalyzing, "Analyzing repository", 40)

	// Stage 2: Find and parse content
	var content []byte
	var filename string

	// Look for docker-compose file
	composePath, err := s.gitService.FindComposeFile(repoPath)
	if err != nil {
		// No docker-compose found, try Nixpacks detection
		s.publishImportLog(sessionID, "analyze", "No docker-compose found, detecting project type...", "info")

		nixpacksResult, nixErr := s.nixpacksService.Detect(ctx, repoPath)
		if nixErr != nil || nixpacksResult == nil {
			// Check for Dockerfile
			_, dockerErr := s.gitService.FindDockerfile(repoPath)
			if dockerErr != nil {
				s.handleImportError(ctx, sessionID, "analyze", fmt.Errorf("no docker-compose.yml or Dockerfile found, and project type detection failed"))
				return
			}
			s.handleImportError(ctx, sessionID, "analyze", fmt.Errorf("only Dockerfile found; please use docker-compose format or ensure project structure is detectable"))
			return
		}

		s.publishImportLog(sessionID, "analyze", fmt.Sprintf("Detected: %s", nixpacksResult.Provider), "info")

		repoName := "app"
		if urlInfo != nil {
			repoName = urlInfo.Repo
		}

		content = []byte(fmt.Sprintf("# Auto-detected by Nixpacks\n# Provider: %s\nservices:\n  %s:\n    image: %s:latest\n    ports:\n      - \"%d:%d\"\n",
			nixpacksResult.Provider,
			repoName,
			strings.ToLower(repoName),
			nixpacksResult.DefaultPort,
			nixpacksResult.DefaultPort,
		))
		filename = "nixpacks-generated.yml"
	} else {
		s.publishImportLog(sessionID, "analyze", fmt.Sprintf("Found: %s", composePath), "info")
		content, err = os.ReadFile(composePath)
		if err != nil {
			s.handleImportError(ctx, sessionID, "analyze", fmt.Errorf("failed to read compose file: %w", err))
			return
		}
		filename = composePath[strings.LastIndex(composePath, "/")+1:]
	}

	s.publishImportProgress(sessionID, models.ImportStatusAnalyzing, "Parsing configuration", 60)

	// Parse content
	analysis, err := s.parseContent(content, filename)
	if err != nil {
		s.handleImportError(ctx, sessionID, "analyze", fmt.Errorf("failed to parse content: %w", err))
		return
	}

	s.publishImportProgress(sessionID, models.ImportStatusAnalyzing, "Converting to workflow nodes", 80)
	s.publishImportLog(sessionID, "analyze", fmt.Sprintf("Found %d services", len(analysis.Services)), "info")

	// Process analysis (convert to nodes)
	// Create a temporary request for processing
	tempReq := &models.ImportRequest{
		Source:    req.Source,
		URL:       req.URL,
		Branch:    req.Branch,
		Namespace: req.Namespace,
	}
	analysis, err = s.processAnalysis(analysis, tempReq)
	if err != nil {
		s.handleImportError(ctx, sessionID, "convert", fmt.Errorf("failed to convert to workflow: %w", err))
		return
	}

	// Set build configuration if nixpacks detected
	if filename == "nixpacks-generated.yml" && analysis.DetectedType == "nixpacks" {
		analysis.NeedsBuild = true
		analysis.SourceBuildConfig = &models.SourceBuildConfig{
			RepoURL:      req.URL,
			Branch:       branch,
			BuildContext: ".",
			UseNixpacks:  true,
		}
	}

	// Complete
	s.publishImportProgress(sessionID, models.ImportStatusCompleted, "Analysis complete", 100)
	s.publishImportLog(sessionID, "complete", fmt.Sprintf("Generated %d nodes and %d edges", len(analysis.SuggestedNodes), len(analysis.SuggestedEdges)), "info")

	// Save completed analysis
	if err := s.importRepository.SetCompleted(ctx, sessionID, analysis); err != nil {
		s.logger.WithError(err).Error("Failed to save completed import session")
	}

	// Publish completion event with analysis
	s.publishImportComplete(sessionID, analysis)
}

// GetImportSession retrieves an import session by ID
func (s *ImportService) GetImportSession(ctx context.Context, sessionID primitive.ObjectID) (*models.ImportSession, error) {
	return s.importRepository.GetByID(ctx, sessionID)
}

// publishImportProgress publishes a progress update
func (s *ImportService) publishImportProgress(sessionID primitive.ObjectID, status models.ImportSessionStatus, stage string, progress int) {
	ctx := context.Background()
	s.importRepository.UpdateStatus(ctx, sessionID, status, stage, progress)

	s.broadcaster.Publish(StreamEvent{
		Type:      "import",
		StreamKey: models.GetImportStreamKey(sessionID),
		EventType: "progress",
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"status":        status,
			"current_stage": stage,
			"progress":      progress,
		},
	})
}

// publishImportLog publishes a log message
func (s *ImportService) publishImportLog(sessionID primitive.ObjectID, stage, message, level string) {
	s.broadcaster.Publish(StreamEvent{
		Type:      "import",
		StreamKey: models.GetImportStreamKey(sessionID),
		EventType: "log",
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"stage":   stage,
			"message": message,
			"level":   level,
		},
	})
}

// publishImportComplete publishes a completion event with analysis
func (s *ImportService) publishImportComplete(sessionID primitive.ObjectID, analysis *models.ImportAnalysis) {
	s.broadcaster.Publish(StreamEvent{
		Type:      "import",
		StreamKey: models.GetImportStreamKey(sessionID),
		EventType: "complete",
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"analysis": analysis,
		},
	})
}

// handleImportError handles import failures
func (s *ImportService) handleImportError(ctx context.Context, sessionID primitive.ObjectID, stage string, err error) {
	s.importRepository.SetFailed(ctx, sessionID, err.Error(), stage)

	s.broadcaster.Publish(StreamEvent{
		Type:      "import",
		StreamKey: models.GetImportStreamKey(sessionID),
		EventType: "failed",
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"error_message": err.Error(),
			"error_stage":   stage,
		},
	})

	s.logger.WithFields(logrus.Fields{
		"session_id": sessionID.Hex(),
		"stage":      stage,
		"error":      err.Error(),
	}).Error("Import failed")
}
