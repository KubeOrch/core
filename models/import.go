package models

// ImportSource represents the type of import source
type ImportSource string

const (
	ImportSourceFile   ImportSource = "file"
	ImportSourceGitHub ImportSource = "github"
	ImportSourceGitLab ImportSource = "gitlab"
	ImportSourceGitURL ImportSource = "git_url"
)

// ImportRequest represents an import request from the client
type ImportRequest struct {
	Source      ImportSource `json:"source"`
	URL         string       `json:"url,omitempty"`         // For GitHub/GitLab/Git URL
	Branch      string       `json:"branch,omitempty"`      // Git branch (default: main/master)
	FileContent string       `json:"fileContent,omitempty"` // Base64 encoded for file upload
	FileName    string       `json:"fileName,omitempty"`    // Original filename
	Namespace   string       `json:"namespace,omitempty"`   // Target K8s namespace
	WorkflowID  string       `json:"workflowId,omitempty"`  // Existing workflow to add to
}

// ImportAnalysis represents the analysis result of an import
type ImportAnalysis struct {
	DetectedType    string                  `json:"detectedType"`    // docker-compose, nixpacks, dockerfile
	Services        []ImportedService       `json:"services"`        // Detected services
	Volumes         []ImportedVolume        `json:"volumes,omitempty"`
	Networks        []ImportedNetwork       `json:"networks,omitempty"`
	Warnings        []string                `json:"warnings,omitempty"`
	Errors          []ImportError           `json:"errors,omitempty"`
	SuggestedNodes  []WorkflowNode          `json:"suggestedNodes"`
	SuggestedEdges  []WorkflowEdge          `json:"suggestedEdges"`
	LayoutPositions map[string]NodePosition `json:"layoutPositions"`
	// Build-related fields
	NeedsBuild        bool               `json:"needsBuild"`                  // True if source needs to be built into an image
	SourceBuildConfig *SourceBuildConfig `json:"sourceBuildConfig,omitempty"` // Build configuration from detection
}

// NodePosition represents x,y coordinates for node layout
type NodePosition struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// ImportedService represents a service detected from docker-compose or Nixpacks
type ImportedService struct {
	Name        string            `json:"name"`
	Image       string            `json:"image,omitempty"`
	Build       *BuildConfig      `json:"build,omitempty"`
	Ports       []PortMapping     `json:"ports,omitempty"`
	Environment map[string]string `json:"environment,omitempty"`
	EnvFile     []string          `json:"envFile,omitempty"`
	Volumes     []VolumeMapping   `json:"volumes,omitempty"`
	DependsOn   []string          `json:"dependsOn,omitempty"`
	Command     []string          `json:"command,omitempty"`
	Entrypoint  []string          `json:"entrypoint,omitempty"`
	Networks    []string          `json:"networks,omitempty"`
	Replicas    int               `json:"replicas,omitempty"`
	HealthCheck *HealthCheckConfig `json:"healthcheck,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	Resources   *ResourceConfig   `json:"resources,omitempty"`
	Restart     string            `json:"restart,omitempty"`
	WorkingDir  string            `json:"workingDir,omitempty"`
}

// BuildConfig represents build configuration for a service
type BuildConfig struct {
	Context    string            `json:"context"`
	Dockerfile string            `json:"dockerfile,omitempty"`
	Args       map[string]string `json:"args,omitempty"`
	Target     string            `json:"target,omitempty"`
}

// PortMapping represents a port mapping configuration
type PortMapping struct {
	HostIP        string `json:"hostIp,omitempty"`
	HostPort      int    `json:"hostPort,omitempty"`
	ContainerPort int    `json:"containerPort"`
	Protocol      string `json:"protocol,omitempty"` // tcp/udp
}

// VolumeMapping represents a volume mount configuration
type VolumeMapping struct {
	Source   string `json:"source"`   // Host path or volume name
	Target   string `json:"target"`   // Container path
	ReadOnly bool   `json:"readonly,omitempty"`
	Type     string `json:"type"` // bind, volume, tmpfs
}

// ImportedNetwork represents a network from docker-compose
type ImportedNetwork struct {
	Name     string `json:"name"`
	Driver   string `json:"driver,omitempty"`
	External bool   `json:"external,omitempty"`
}

// ImportedVolume represents a named volume from docker-compose
type ImportedVolume struct {
	Name     string            `json:"name"`
	Driver   string            `json:"driver,omitempty"`
	External bool              `json:"external,omitempty"`
	Labels   map[string]string `json:"labels,omitempty"`
}

// HealthCheckConfig represents health check configuration
type HealthCheckConfig struct {
	Test        []string `json:"test,omitempty"`
	Interval    string   `json:"interval,omitempty"`
	Timeout     string   `json:"timeout,omitempty"`
	Retries     int      `json:"retries,omitempty"`
	StartPeriod string   `json:"startPeriod,omitempty"`
}

// ResourceConfig represents resource limits and reservations
type ResourceConfig struct {
	Limits       ImportResourceSpec `json:"limits,omitempty"`
	Reservations ImportResourceSpec `json:"reservations,omitempty"`
}

// ImportResourceSpec represents CPU and memory specifications
type ImportResourceSpec struct {
	CPUs   string `json:"cpus,omitempty"`
	Memory string `json:"memory,omitempty"`
}

// ImportError represents an error during import
type ImportError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Service string `json:"service,omitempty"`
	Field   string `json:"field,omitempty"`
}

// Import error codes
const (
	ImportErrorInvalidSyntax          = "INVALID_COMPOSE_SYNTAX"
	ImportErrorUnsupportedVersion     = "UNSUPPORTED_COMPOSE_VERSION"
	ImportErrorServiceNoImage         = "SERVICE_NO_IMAGE"
	ImportErrorBindMountNotSupported  = "BIND_MOUNT_NOT_SUPPORTED"
	ImportErrorCircularDependency     = "CIRCULAR_DEPENDENCY"
	ImportErrorGitCloneFailed         = "GIT_CLONE_FAILED"
	ImportErrorNixpacksDetectionFailed = "NIXPACKS_DETECTION_FAILED"
	ImportErrorInvalidURL             = "INVALID_URL"
	ImportErrorFileNotFound           = "FILE_NOT_FOUND"
	ImportErrorNetworkModeNotSupported = "NETWORK_MODE_NOT_SUPPORTED"
)

// NixpacksResult represents detection result from Nixpacks
type NixpacksResult struct {
	Provider     string            `json:"provider"`     // node, python, go, rust, etc.
	Language     string            `json:"language"`
	Framework    string            `json:"framework,omitempty"`
	BuildCommand string            `json:"buildCommand,omitempty"`
	StartCommand string            `json:"startCommand,omitempty"`
	InstallCommand string          `json:"installCommand,omitempty"`
	EnvVars      map[string]string `json:"envVars,omitempty"`
	Ports        []int             `json:"ports,omitempty"`
	StaticFiles  bool              `json:"staticFiles,omitempty"`
}

// SourceBuildConfig represents build configuration detected from source analysis
// This is passed to the build service when NeedsBuild is true
type SourceBuildConfig struct {
	RepoURL      string            `json:"repoUrl"`                // Git repository URL
	Branch       string            `json:"branch"`                 // Git branch
	BuildContext string            `json:"buildContext,omitempty"` // Subdirectory for build (default ".")
	UseNixpacks  bool              `json:"useNixpacks"`            // Whether to use Nixpacks auto-detection
	Dockerfile   string            `json:"dockerfile,omitempty"`   // Dockerfile path if not using Nixpacks
	Provider     string            `json:"provider,omitempty"`     // Detected provider (node, go, python, etc.)
	Framework    string            `json:"framework,omitempty"`    // Detected framework (nextjs, express, etc.)
	BuildArgs    map[string]string `json:"buildArgs,omitempty"`    // Build-time arguments
}

// GitURLInfo represents parsed git URL information
type GitURLInfo struct {
	Provider string `json:"provider"` // github, gitlab, generic
	Owner    string `json:"owner"`
	Repo     string `json:"repo"`
	Branch   string `json:"branch"`
	Path     string `json:"path,omitempty"`
	RawURL   string `json:"rawUrl"`
	CloneURL string `json:"cloneUrl"`
}

// ApplyImportRequest represents a request to apply analyzed import to a workflow
type ApplyImportRequest struct {
	WorkflowID string              `json:"workflowId"`
	Nodes      []WorkflowNode      `json:"nodes"`
	Edges      []WorkflowEdge      `json:"edges"`
	Positions  map[string]NodePosition `json:"positions"`
}

// SensitiveEnvPatterns contains patterns that indicate sensitive environment variables
var SensitiveEnvPatterns = []string{
	"PASSWORD",
	"SECRET",
	"KEY",
	"TOKEN",
	"API_KEY",
	"APIKEY",
	"CREDENTIALS",
	"AUTH",
	"PRIVATE",
	"CERT",
	"CERTIFICATE",
	"DATABASE_URL",
	"DB_PASSWORD",
	"REDIS_PASSWORD",
	"MONGO_PASSWORD",
	"POSTGRES_PASSWORD",
	"MYSQL_PASSWORD",
	"AWS_SECRET",
	"GCP_KEY",
	"AZURE_KEY",
}
