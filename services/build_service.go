package services

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/KubeOrch/core/models"
	"github.com/KubeOrch/core/repositories"
	"github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// BuildService handles container image building
type BuildService struct {
	repo             *repositories.BuildRepository
	registryRepo     *repositories.RegistryRepository
	gitService       *GitService
	nixpacksService  *NixpacksService
	broadcaster      *SSEBroadcaster
	logger           *logrus.Entry
	activeBuilds     map[primitive.ObjectID]context.CancelFunc
	activeBuildsMu   sync.RWMutex
}

var (
	buildServiceInstance *BuildService
	buildServiceOnce     sync.Once
)

// GetBuildService returns the singleton build service instance
func GetBuildService() *BuildService {
	buildServiceOnce.Do(func() {
		buildServiceInstance = &BuildService{
			repo:            repositories.NewBuildRepository(),
			registryRepo:    repositories.NewRegistryRepository(),
			gitService:      NewGitService(),
			nixpacksService: NewNixpacksService(),
			broadcaster:     GetSSEBroadcaster(),
			logger:          logrus.WithField("component", "build-service"),
			activeBuilds:    make(map[primitive.ObjectID]context.CancelFunc),
		}
	})
	return buildServiceInstance
}

// GetBuildStreamKey returns the SSE stream key for a build
func GetBuildStreamKey(buildID primitive.ObjectID) string {
	return fmt.Sprintf("build:%s", buildID.Hex())
}

// StartBuild initiates a new build job
func (s *BuildService) StartBuild(ctx context.Context, req models.StartBuildRequest, userID primitive.ObjectID) (*models.Build, error) {
	// Parse registry ID
	registryID, err := primitive.ObjectIDFromHex(req.RegistryID)
	if err != nil {
		return nil, fmt.Errorf("invalid registry ID: %w", err)
	}

	// Verify registry exists
	registry, err := s.registryRepo.GetByID(ctx, registryID)
	if err != nil {
		return nil, fmt.Errorf("registry not found: %w", err)
	}

	// Parse optional workflow ID
	var workflowID *primitive.ObjectID
	if req.WorkflowID != "" {
		wfID, err := primitive.ObjectIDFromHex(req.WorkflowID)
		if err == nil {
			workflowID = &wfID
		}
	}

	// Create build record
	build := &models.Build{
		UserID:       userID,
		WorkflowID:   workflowID,
		RepoURL:      req.RepoURL,
		Branch:       req.Branch,
		BuildContext: req.BuildContext,
		Dockerfile:   req.Dockerfile,
		BuildArgs:    req.BuildArgs,
		UseNixpacks:  req.UseNixpacks,
		RegistryID:   registryID,
		ImageName:    req.ImageName,
		ImageTag:     req.ImageTag,
	}

	if err := s.repo.Create(ctx, build); err != nil {
		return nil, fmt.Errorf("failed to create build record: %w", err)
	}

	s.logger.WithFields(logrus.Fields{
		"build_id":  build.ID.Hex(),
		"repo_url":  req.RepoURL,
		"image":     fmt.Sprintf("%s:%s", req.ImageName, req.ImageTag),
		"registry":  registry.Name,
	}).Info("Starting build")

	// Execute build in background
	go s.ExecuteBuild(build.ID)

	return build, nil
}

// ExecuteBuild runs the full build pipeline
func (s *BuildService) ExecuteBuild(buildID primitive.ObjectID) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	// Track this build for cancellation
	s.activeBuildsMu.Lock()
	s.activeBuilds[buildID] = cancel
	s.activeBuildsMu.Unlock()
	defer func() {
		s.activeBuildsMu.Lock()
		delete(s.activeBuilds, buildID)
		s.activeBuildsMu.Unlock()
	}()

	// Get build record
	build, err := s.repo.GetByID(ctx, buildID)
	if err != nil {
		s.logger.WithError(err).Error("Failed to get build record")
		return
	}

	// Set started time
	_ = s.repo.SetStarted(ctx, buildID)

	var repoPath string
	var dockerfilePath string

	// Stage 1: Clone
	s.publishProgress(buildID, models.BuildStatusCloning, "Cloning repository", 5)
	repoPath, err = s.stageClone(ctx, build)
	if err != nil {
		s.handleBuildError(ctx, buildID, "clone", err)
		return
	}
	defer s.stageCleanup(repoPath, "")

	// Stage 2: Detect/Generate Dockerfile
	s.publishProgress(buildID, models.BuildStatusBuilding, "Preparing Dockerfile", 20)
	dockerfilePath, err = s.stageDetectOrGenerate(ctx, build, repoPath)
	if err != nil {
		s.handleBuildError(ctx, buildID, "dockerfile", err)
		return
	}

	// Stage 3: Docker Build
	s.publishProgress(buildID, models.BuildStatusBuilding, "Building image", 30)
	imageRef, err := s.stageBuild(ctx, build, repoPath, dockerfilePath)
	if err != nil {
		s.handleBuildError(ctx, buildID, "build", err)
		return
	}

	// Stage 4: Docker Push
	s.publishProgress(buildID, models.BuildStatusPushing, "Pushing to registry", 75)
	digest, err := s.stagePush(ctx, build, imageRef)
	if err != nil {
		s.handleBuildError(ctx, buildID, "push", err)
		// Cleanup local image on push failure
		s.stageCleanup("", imageRef)
		return
	}

	// Success
	finalRef := imageRef
	if digest != "" {
		finalRef = digest // Use digest reference if available
	}

	_ = s.repo.SetCompleted(ctx, buildID, finalRef, digest, 0)
	s.publishProgress(buildID, models.BuildStatusCompleted, "Build completed", 100)
	s.publishComplete(buildID, finalRef, digest)

	// Update workflow's deployment node image if this build is linked to a workflow
	if build.WorkflowID != nil {
		if err := UpdateWorkflowDeploymentImage(*build.WorkflowID, finalRef); err != nil {
			s.logger.WithFields(logrus.Fields{
				"build_id":    buildID.Hex(),
				"workflow_id": build.WorkflowID.Hex(),
				"error":       err,
			}).Warn("Failed to update workflow deployment image")
		} else {
			s.logger.WithFields(logrus.Fields{
				"build_id":    buildID.Hex(),
				"workflow_id": build.WorkflowID.Hex(),
				"image_ref":   finalRef,
			}).Info("Updated workflow deployment image")
		}
	}

	s.logger.WithFields(logrus.Fields{
		"build_id":  buildID.Hex(),
		"image_ref": finalRef,
	}).Info("Build completed successfully")

	// Cleanup local image
	s.stageCleanup("", imageRef)
}

// stageClone clones the repository
func (s *BuildService) stageClone(ctx context.Context, build *models.Build) (string, error) {
	// Sanitize the URL (remove /tree/branch or /blob/branch paths)
	repoURL := GetRepoCloneURL(build.RepoURL)

	// Try to get GitHub credentials if it's a GitHub repo
	// Also extract branch from URL if present (e.g., /tree/main)
	urlInfo, err := s.gitService.ParseGitURL(build.RepoURL)
	if err == nil && urlInfo.IsGitHub {
		s.loadGitHubCredentials(ctx)
	}

	// Use branch from URL if build.Branch is empty or default
	branch := build.Branch
	if (branch == "" || branch == "main") && urlInfo != nil && urlInfo.Branch != "" {
		branch = urlInfo.Branch
	}
	if branch == "" {
		branch = "main"
	}

	s.publishLog(build.ID, "clone", fmt.Sprintf("Cloning %s (branch: %s)", repoURL, branch), "info")

	repoPath, err := s.gitService.CloneRepository(ctx, repoURL, branch)
	if err != nil {
		s.publishLog(build.ID, "clone", fmt.Sprintf("Clone failed: %v", err), "error")
		return "", fmt.Errorf("failed to clone repository: %w", err)
	}

	// Get commit SHA
	commitSHA := s.getCommitSHA(repoPath)
	if commitSHA != "" {
		s.publishLog(build.ID, "clone", fmt.Sprintf("Cloned at commit %s", commitSHA[:8]), "info")
	}

	s.publishLog(build.ID, "clone", "Repository cloned successfully", "info")
	return repoPath, nil
}

// stageDetectOrGenerate detects or generates a Dockerfile
func (s *BuildService) stageDetectOrGenerate(ctx context.Context, build *models.Build, repoPath string) (string, error) {
	buildContextPath := filepath.Join(repoPath, build.BuildContext)

	// If using Nixpacks, generate Dockerfile
	if build.UseNixpacks {
		s.publishLog(build.ID, "dockerfile", "Detecting project type with Nixpacks", "info")

		// Detect project
		result, err := s.nixpacksService.Detect(ctx, buildContextPath)
		if err != nil {
			s.publishLog(build.ID, "dockerfile", fmt.Sprintf("Detection failed: %v", err), "error")
			return "", fmt.Errorf("failed to detect project type: %w", err)
		}

		s.publishLog(build.ID, "dockerfile", fmt.Sprintf("Detected: %s", result.Provider), "info")

		// Generate Dockerfile
		if s.nixpacksService.IsAvailable() {
			s.publishLog(build.ID, "dockerfile", "Generating Dockerfile with Nixpacks", "info")
			// GenerateDockerfile outputs to buildContextPath/.nixpacks/ which includes
			// the Dockerfile and supporting files (assets, nix files) that the Dockerfile references
			dockerfilePath, err := s.nixpacksService.GenerateDockerfile(ctx, buildContextPath, buildContextPath)
			if err != nil {
				s.publishLog(build.ID, "dockerfile", fmt.Sprintf("Dockerfile generation failed: %v", err), "warn")
				// Fall back to fallback generator
			} else {
				s.publishLog(build.ID, "dockerfile", "Dockerfile generated successfully", "info")
				return dockerfilePath, nil
			}
		}

		// Fallback: Generate a basic Dockerfile based on detected project type
		s.publishLog(build.ID, "dockerfile", "Nixpacks CLI not available, using fallback Dockerfile generator", "info")
		dockerfile, err := s.nixpacksService.GenerateFallbackDockerfile(result.Provider, result)
		if err != nil {
			s.publishLog(build.ID, "dockerfile", fmt.Sprintf("Fallback generation failed: %v", err), "warn")
			// Continue to look for existing Dockerfile
		} else {
			// Write fallback Dockerfile
			dockerfilePath := filepath.Join(buildContextPath, "Dockerfile.generated")
			if err := os.WriteFile(dockerfilePath, []byte(dockerfile), 0644); err != nil {
				return "", fmt.Errorf("failed to write generated Dockerfile: %w", err)
			}
			s.publishLog(build.ID, "dockerfile", "Fallback Dockerfile generated successfully", "info")
			return dockerfilePath, nil
		}
	}

	// Look for existing Dockerfile
	dockerfilePath := filepath.Join(buildContextPath, "Dockerfile")
	if build.Dockerfile != "" {
		dockerfilePath = filepath.Join(repoPath, build.Dockerfile)
	}

	if _, err := os.Stat(dockerfilePath); err != nil {
		s.publishLog(build.ID, "dockerfile", "No Dockerfile found", "error")
		return "", fmt.Errorf("dockerfile not found at %s", dockerfilePath)
	}

	s.publishLog(build.ID, "dockerfile", fmt.Sprintf("Using Dockerfile: %s", dockerfilePath), "info")
	return dockerfilePath, nil
}

// stageBuild executes docker build
func (s *BuildService) stageBuild(ctx context.Context, build *models.Build, repoPath, dockerfilePath string) (string, error) {
	// Get registry to determine image prefix
	registry, err := s.registryRepo.GetByID(ctx, build.RegistryID)
	if err != nil {
		return "", fmt.Errorf("failed to get registry for image tagging: %w", err)
	}

	// Build full image reference with registry prefix (e.g., "ghcr.io/user/repo:tag")
	imagePrefix := registry.GetImagePrefix()
	imageRef := fmt.Sprintf("%s%s:%s", imagePrefix, build.ImageName, build.ImageTag)
	buildContextPath := filepath.Join(repoPath, build.BuildContext)

	s.publishLog(build.ID, "build", fmt.Sprintf("Building image: %s", imageRef), "info")

	// Prepare docker build command
	args := []string{
		"build",
		"-t", imageRef,
		"-f", dockerfilePath,
	}

	// Add build args
	for k, v := range build.BuildArgs {
		args = append(args, "--build-arg", fmt.Sprintf("%s=%s", k, v))
	}

	args = append(args, buildContextPath)

	cmd := exec.CommandContext(ctx, "docker", args...)

	// Capture and stream output
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("failed to get stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", fmt.Errorf("failed to get stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		s.publishLog(build.ID, "build", fmt.Sprintf("Failed to start build: %v", err), "error")
		return "", fmt.Errorf("failed to start docker build: %w", err)
	}

	// Stream output in goroutines
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		s.streamOutput(build.ID, "build", stdout, "stdout")
	}()
	go func() {
		defer wg.Done()
		s.streamOutput(build.ID, "build", stderr, "stderr")
	}()

	// Wait for output streaming to complete
	wg.Wait()

	// Wait for command to complete
	if err := cmd.Wait(); err != nil {
		s.publishLog(build.ID, "build", fmt.Sprintf("Build failed: %v", err), "error")
		return "", fmt.Errorf("docker build failed: %w", err)
	}

	s.publishLog(build.ID, "build", "Image built successfully", "info")
	return imageRef, nil
}

// stagePush pushes the image to registry
func (s *BuildService) stagePush(ctx context.Context, build *models.Build, imageRef string) (string, error) {
	s.publishLog(build.ID, "push", "Authenticating with registry", "info")

	// Get registry credentials
	registry, err := s.registryRepo.GetByID(ctx, build.RegistryID)
	if err != nil {
		return "", fmt.Errorf("failed to get registry: %w", err)
	}

	// Docker login
	registryDomain := registry.GetRegistryDomain()
	if registry.Credentials.Username != "" && registry.Credentials.Password != "" {
		loginCmd := exec.CommandContext(ctx, "docker", "login",
			"-u", registry.Credentials.Username,
			"--password-stdin",
			registryDomain)
		loginCmd.Stdin = strings.NewReader(registry.Credentials.Password)

		output, err := loginCmd.CombinedOutput()
		if err != nil {
			s.publishLog(build.ID, "push", fmt.Sprintf("Login failed: %v", err), "error")
			return "", fmt.Errorf("docker login failed: %w - %s", err, string(output))
		}
		s.publishLog(build.ID, "push", "Authenticated successfully", "info")
	}

	s.publishLog(build.ID, "push", fmt.Sprintf("Pushing image: %s", imageRef), "info")

	// Docker push
	pushCmd := exec.CommandContext(ctx, "docker", "push", imageRef)

	stdout, err := pushCmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("failed to get stdout pipe: %w", err)
	}

	stderr, err := pushCmd.StderrPipe()
	if err != nil {
		return "", fmt.Errorf("failed to get stderr pipe: %w", err)
	}

	if err := pushCmd.Start(); err != nil {
		return "", fmt.Errorf("failed to start docker push: %w", err)
	}

	// Stream output
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		s.streamOutput(build.ID, "push", stdout, "stdout")
	}()
	go func() {
		defer wg.Done()
		s.streamOutput(build.ID, "push", stderr, "stderr")
	}()

	wg.Wait()

	if err := pushCmd.Wait(); err != nil {
		s.publishLog(build.ID, "push", fmt.Sprintf("Push failed: %v", err), "error")
		return "", fmt.Errorf("docker push failed: %w", err)
	}

	// Get image digest
	digest := s.getImageDigest(ctx, imageRef)
	s.publishLog(build.ID, "push", fmt.Sprintf("Pushed successfully: %s", digest), "info")

	return digest, nil
}

// stageCleanup cleans up temporary files and images
func (s *BuildService) stageCleanup(repoPath, imageRef string) {
	if repoPath != "" {
		_ = os.RemoveAll(repoPath)
	}

	if imageRef != "" {
		// Remove local image to save space
		_ = exec.Command("docker", "rmi", imageRef).Run()
	}
}

// loadGitHubCredentials loads credentials from GHCR registry
func (s *BuildService) loadGitHubCredentials(ctx context.Context) {
	registries, err := s.registryRepo.GetByType(ctx, models.RegistryTypeGHCR)
	if err != nil {
		return
	}

	for _, registry := range registries {
		if registry.Credentials.Password != "" {
			s.gitService.SetCredentials("x-access-token", registry.Credentials.Password)
			return
		}
	}
}

// getCommitSHA gets the current commit SHA from a repo
func (s *BuildService) getCommitSHA(repoPath string) string {
	cmd := exec.Command("git", "-C", repoPath, "rev-parse", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

// getImageDigest gets the digest of a pushed image
func (s *BuildService) getImageDigest(ctx context.Context, imageRef string) string {
	cmd := exec.CommandContext(ctx, "docker", "inspect", "--format={{index .RepoDigests 0}}", imageRef)
	output, err := cmd.Output()
	if err != nil {
		return imageRef
	}
	digest := strings.TrimSpace(string(output))
	if digest == "" || digest == "[]" {
		return imageRef
	}
	return digest
}

// streamOutput streams command output to SSE
func (s *BuildService) streamOutput(buildID primitive.ObjectID, stage string, reader io.Reader, stream string) {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := scanner.Text()
		if line != "" {
			level := "info"
			if stream == "stderr" && (strings.Contains(strings.ToLower(line), "error") || strings.Contains(strings.ToLower(line), "failed")) {
				level = "error"
			} else if stream == "stderr" && strings.Contains(strings.ToLower(line), "warn") {
				level = "warn"
			}
			s.publishLog(buildID, stage, line, level)
		}
	}
}

// handleBuildError handles build failures
func (s *BuildService) handleBuildError(ctx context.Context, buildID primitive.ObjectID, stage string, err error) {
	_ = s.repo.SetFailed(ctx, buildID, err.Error(), stage)
	s.publishProgress(buildID, models.BuildStatusFailed, fmt.Sprintf("Failed at %s stage", stage), 0)

	s.broadcaster.Publish(StreamEvent{
		Type:      "build",
		StreamKey: GetBuildStreamKey(buildID),
		EventType: "failed",
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"error_message": err.Error(),
			"error_stage":   stage,
		},
	})

	s.logger.WithFields(logrus.Fields{
		"build_id": buildID.Hex(),
		"stage":    stage,
		"error":    err.Error(),
	}).Error("Build failed")
}

// publishLog publishes a log event
func (s *BuildService) publishLog(buildID primitive.ObjectID, stage, message, level string) {
	s.broadcaster.Publish(StreamEvent{
		Type:      "build",
		StreamKey: GetBuildStreamKey(buildID),
		EventType: "log",
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"stage":   stage,
			"message": message,
			"level":   level,
		},
	})
}

// publishProgress publishes a progress update
func (s *BuildService) publishProgress(buildID primitive.ObjectID, status models.BuildStatus, stage string, progress int) {
	_ = s.repo.UpdateStatus(context.Background(), buildID, status, stage, progress)

	s.broadcaster.Publish(StreamEvent{
		Type:      "build",
		StreamKey: GetBuildStreamKey(buildID),
		EventType: "progress",
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"status":        status,
			"current_stage": stage,
			"progress":      progress,
		},
	})
}

// publishComplete publishes a completion event
func (s *BuildService) publishComplete(buildID primitive.ObjectID, imageRef, digest string) {
	s.broadcaster.Publish(StreamEvent{
		Type:      "build",
		StreamKey: GetBuildStreamKey(buildID),
		EventType: "complete",
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"final_image_ref": imageRef,
			"image_digest":    digest,
		},
	})
}

// GetBuild retrieves a build by ID, verifying ownership
func (s *BuildService) GetBuild(ctx context.Context, buildID, userID primitive.ObjectID) (*models.Build, error) {
	build, err := s.repo.GetByID(ctx, buildID)
	if err != nil {
		return nil, err
	}

	if build.UserID != userID {
		return nil, fmt.Errorf("build not found")
	}

	return build, nil
}

// ListBuilds lists builds for a user
func (s *BuildService) ListBuilds(ctx context.Context, userID primitive.ObjectID, limit, offset int) ([]*models.Build, int64, error) {
	return s.repo.ListByUser(ctx, userID, limit, offset)
}

// CancelBuild cancels an in-progress build
func (s *BuildService) CancelBuild(ctx context.Context, buildID, userID primitive.ObjectID) error {
	build, err := s.GetBuild(ctx, buildID, userID)
	if err != nil {
		return err
	}

	if build.IsTerminal() {
		return fmt.Errorf("build is already in terminal state: %s", build.Status)
	}

	// Cancel the build context
	s.activeBuildsMu.RLock()
	cancelFunc, exists := s.activeBuilds[buildID]
	s.activeBuildsMu.RUnlock()

	if exists {
		cancelFunc()
	}

	// Update status
	_ = s.repo.SetCancelled(ctx, buildID)
	s.publishProgress(buildID, models.BuildStatusCancelled, "Cancelled by user", 0)

	s.broadcaster.Publish(StreamEvent{
		Type:      "build",
		StreamKey: GetBuildStreamKey(buildID),
		EventType: "cancelled",
		Timestamp: time.Now(),
		Data:      map[string]interface{}{},
	})

	return nil
}
