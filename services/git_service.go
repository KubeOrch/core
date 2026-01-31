package services

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
)

// GitURLInfo contains parsed information from a Git URL
type GitURLInfo struct {
	Host     string // github.com, gitlab.com, etc.
	Owner    string // username or organization
	Repo     string // repository name
	Path     string // optional path within repo
	Branch   string // branch name (if specified in URL)
	IsGitHub bool
	IsGitLab bool
}

// GitCredentials holds authentication credentials for Git operations
type GitCredentials struct {
	Username string
	Password string // PAT token
}

// GitService handles Git repository operations
type GitService struct {
	tempDir      string
	httpClient   *http.Client
	cloneDepth   int
	cloneTimeout time.Duration
	credentials  *GitCredentials
}

// NewGitService creates a new GitService instance
func NewGitService() *GitService {
	return &GitService{
		tempDir: os.TempDir(),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		cloneDepth:   1,               // Shallow clone by default
		cloneTimeout: 60 * time.Second, // Clone timeout
	}
}

// SetCredentials sets the credentials for Git operations
func (s *GitService) SetCredentials(username, password string) {
	s.credentials = &GitCredentials{
		Username: username,
		Password: password,
	}
}

// ParseGitURL parses a Git URL and extracts relevant information
func (s *GitService) ParseGitURL(rawURL string) (*GitURLInfo, error) {
	// Handle .git suffix
	rawURL = strings.TrimSuffix(rawURL, ".git")

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}

	info := &GitURLInfo{
		Host: parsed.Host,
	}

	// Determine platform
	info.IsGitHub = strings.Contains(parsed.Host, "github.com")
	info.IsGitLab = strings.Contains(parsed.Host, "gitlab.com")

	// Parse path: /owner/repo[/tree/branch/path] or /owner/repo[/blob/branch/path]
	pathParts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(pathParts) < 2 {
		return nil, fmt.Errorf("URL must include owner and repository")
	}

	info.Owner = pathParts[0]
	info.Repo = pathParts[1]

	// Check for tree/blob path (e.g., /owner/repo/tree/main/path/to/file)
	if len(pathParts) > 3 && (pathParts[2] == "tree" || pathParts[2] == "blob") {
		info.Branch = pathParts[3]
		if len(pathParts) > 4 {
			info.Path = strings.Join(pathParts[4:], "/")
		}
	}

	return info, nil
}

// CloneRepository clones a Git repository to a temporary directory
func (s *GitService) CloneRepository(ctx context.Context, repoURL, branch string) (string, error) {
	// Clean up URL
	repoURL = strings.TrimSuffix(repoURL, ".git")

	// Parse URL to get repo name
	urlInfo, err := s.ParseGitURL(repoURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse URL: %w", err)
	}

	// Create unique temp directory
	tempDir, err := os.MkdirTemp(s.tempDir, fmt.Sprintf("kubeorch-import-%s-", urlInfo.Repo))
	if err != nil {
		return "", fmt.Errorf("failed to create temp directory: %w", err)
	}

	// Prepare clone options
	cloneOpts := &git.CloneOptions{
		URL:      repoURL + ".git",
		Progress: io.Discard,
		Depth:    s.cloneDepth,
	}

	// Add authentication if credentials are set
	if s.credentials != nil && s.credentials.Username != "" && s.credentials.Password != "" {
		cloneOpts.Auth = &githttp.BasicAuth{
			Username: s.credentials.Username,
			Password: s.credentials.Password,
		}
	}

	// Set branch if specified
	if branch != "" {
		cloneOpts.ReferenceName = plumbing.NewBranchReferenceName(branch)
		cloneOpts.SingleBranch = true
	}

	// Add timeout to clone operation
	cloneCtx, cancel := context.WithTimeout(ctx, s.cloneTimeout)
	defer cancel()

	// Clone the repository
	_, err = git.PlainCloneContext(cloneCtx, tempDir, false, cloneOpts)
	if err != nil {
		// Clean up temp dir on error
		_ = os.RemoveAll(tempDir)
		if cloneCtx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("clone operation timed out after %v", s.cloneTimeout)
		}
		return "", fmt.Errorf("failed to clone repository: %w", err)
	}

	return tempDir, nil
}

// FetchFile fetches a single file from a repository without cloning
// This is more efficient for small files like docker-compose.yml
func (s *GitService) FetchFile(ctx context.Context, repoURL, filePath, branch string) ([]byte, error) {
	urlInfo, err := s.ParseGitURL(repoURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}

	if branch == "" {
		branch = "main"
	}

	var rawURL string
	if urlInfo.IsGitHub {
		// GitHub raw URL: https://raw.githubusercontent.com/owner/repo/branch/path
		rawURL = fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/%s",
			urlInfo.Owner, urlInfo.Repo, branch, filePath)
	} else if urlInfo.IsGitLab {
		// GitLab raw URL: https://gitlab.com/owner/repo/-/raw/branch/path
		rawURL = fmt.Sprintf("https://gitlab.com/%s/%s/-/raw/%s/%s",
			urlInfo.Owner, urlInfo.Repo, branch, filePath)
	} else {
		// For generic Git hosts, we need to clone
		return nil, fmt.Errorf("direct file fetch not supported for this host, use CloneRepository instead")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add authentication header if credentials are set (for private repos)
	if s.credentials != nil && s.credentials.Password != "" {
		req.Header.Set("Authorization", "token "+s.credentials.Password)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch file: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("file not found: %s", filePath)
	}

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("authentication required or access denied for private repository")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	return content, nil
}

// FindComposeFile searches for docker-compose files in a repository
func (s *GitService) FindComposeFile(repoPath string) (string, error) {
	composeFileNames := []string{
		"docker-compose.yml",
		"docker-compose.yaml",
		"compose.yml",
		"compose.yaml",
	}

	for _, name := range composeFileNames {
		filePath := filepath.Join(repoPath, name)
		if _, err := os.Stat(filePath); err == nil {
			return filePath, nil
		}
	}

	return "", fmt.Errorf("no docker-compose file found in repository")
}

// FindDockerfile searches for a Dockerfile in a repository
func (s *GitService) FindDockerfile(repoPath string) (string, error) {
	dockerfileNames := []string{
		"Dockerfile",
		"dockerfile",
		"Dockerfile.dev",
		"Dockerfile.prod",
	}

	for _, name := range dockerfileNames {
		filePath := filepath.Join(repoPath, name)
		if _, err := os.Stat(filePath); err == nil {
			return filePath, nil
		}
	}

	return "", fmt.Errorf("no Dockerfile found in repository")
}

// CleanupRepository removes a cloned repository directory
func (s *GitService) CleanupRepository(repoPath string) error {
	return os.RemoveAll(repoPath)
}

// ValidateGitURL validates that a URL is a valid Git repository URL
func ValidateGitURL(rawURL string) bool {
	// GitHub pattern
	githubPattern := regexp.MustCompile(`^https?://(www\.)?github\.com/[\w-]+/[\w.-]+(/.*)?$`)
	// GitLab pattern
	gitlabPattern := regexp.MustCompile(`^https?://(www\.)?gitlab\.com/[\w-]+/[\w.-]+(/.*)?$`)
	// Generic git pattern
	gitPattern := regexp.MustCompile(`^(https?|git)://.*\.git$`)

	return githubPattern.MatchString(rawURL) ||
		gitlabPattern.MatchString(rawURL) ||
		gitPattern.MatchString(rawURL)
}

// GetRepoCloneURL converts various Git URLs to a proper clone URL
func GetRepoCloneURL(rawURL string) string {
	// Remove tree/blob paths from GitHub/GitLab URLs
	// e.g., https://github.com/user/repo/tree/main/path -> https://github.com/user/repo
	repoPattern := regexp.MustCompile(`^(https?://(?:www\.)?(?:github|gitlab)\.com/[\w-]+/[\w.-]+)(?:/(?:tree|blob)/.*)?$`)
	if matches := repoPattern.FindStringSubmatch(rawURL); len(matches) > 1 {
		return matches[1]
	}
	return rawURL
}
