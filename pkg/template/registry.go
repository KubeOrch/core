package template

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
)

var (
	globalRegistry     *Registry
	globalRegistryOnce sync.Once
)

// TemplateParameter represents a parameter in a template
type TemplateParameter struct {
	Name        string                 `yaml:"name" json:"name"`
	Label       string                 `yaml:"label" json:"label"`
	Description string                 `yaml:"description" json:"description"`
	Type        string                 `yaml:"type" json:"type"` // string, number, boolean, select, object, array
	Required    bool                   `yaml:"required" json:"required"`
	Default     interface{}            `yaml:"default,omitempty" json:"default,omitempty"`
	Options     []ParameterOption      `yaml:"options,omitempty" json:"options,omitempty"`
	Min         *int                   `yaml:"min,omitempty" json:"min,omitempty"`
	Max         *int                   `yaml:"max,omitempty" json:"max,omitempty"`
}

// ParameterOption represents a select option
type ParameterOption struct {
	Value string `yaml:"value" json:"value"`
	Label string `yaml:"label" json:"label"`
}

// TemplateExample represents an example configuration
type TemplateExample struct {
	Name        string                 `yaml:"name" json:"name"`
	Description string                 `yaml:"description" json:"description"`
	Config      map[string]interface{} `yaml:"config" json:"config"`
}

// TemplateMetadata contains information about a template
type TemplateMetadata struct {
	ID             string              `yaml:"id" json:"id"`
	Name           string              `yaml:"name" json:"name"`
	DisplayName    string              `yaml:"displayName" json:"displayName"`
	Category       string              `yaml:"category" json:"category"` // core, storage, networking, security, etc.
	Description    string              `yaml:"description" json:"description"`
	Icon           string              `yaml:"icon" json:"icon"`
	Version        string              `yaml:"version" json:"version"`
	Tags           []string            `yaml:"tags" json:"tags"`
	UsageFrequency string              `yaml:"usageFrequency" json:"usageFrequency"` // very-high, high, medium, low
	Difficulty     string              `yaml:"difficulty" json:"difficulty"`         // easy, medium, hard
	Dependencies   []string            `yaml:"dependencies" json:"dependencies"`      // IDs of templates this depends on
	Parameters     []TemplateParameter `yaml:"parameters" json:"parameters"`
	Examples       []TemplateExample   `yaml:"examples" json:"examples"`
}

// Registry manages template metadata
type Registry struct {
	templatesDir string
	templates    map[string]*TemplateMetadata
}

// NewRegistry creates a new template registry
func NewRegistry(templatesDir string) *Registry {
	return &Registry{
		templatesDir: templatesDir,
		templates:    make(map[string]*TemplateMetadata),
	}
}

// LoadTemplates loads all template metadata from the templates directory
func (r *Registry) LoadTemplates() error {
	logrus.Infof("Loading templates from directory: %s", r.templatesDir)

	// Walk through templates directory
	err := filepath.Walk(r.templatesDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			logrus.Errorf("Error walking path %s: %v", path, err)
			return err
		}

		// Look for metadata.yaml files
		if !info.IsDir() && info.Name() == "metadata.yaml" {
			logrus.Debugf("Found metadata file: %s", path)
			metadata, err := r.loadMetadataFile(path)
			if err != nil {
				return fmt.Errorf("failed to load metadata from %s: %w", path, err)
			}
			r.templates[metadata.ID] = metadata
			logrus.Infof("Loaded template: %s (%s)", metadata.DisplayName, metadata.ID)
		}

		return nil
	})

	logrus.Infof("Loaded %d templates total", len(r.templates))
	return err
}

// loadMetadataFile loads a single metadata file
func (r *Registry) loadMetadataFile(path string) (*TemplateMetadata, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var metadata TemplateMetadata
	if err := yaml.Unmarshal(data, &metadata); err != nil {
		return nil, err
	}

	return &metadata, nil
}

// GetTemplate returns metadata for a specific template
func (r *Registry) GetTemplate(id string) (*TemplateMetadata, error) {
	template, exists := r.templates[id]
	if !exists {
		return nil, fmt.Errorf("template not found: %s", id)
	}
	return template, nil
}

// GetAllTemplates returns all registered templates
func (r *Registry) GetAllTemplates() []*TemplateMetadata {
	templates := make([]*TemplateMetadata, 0, len(r.templates))
	for _, t := range r.templates {
		templates = append(templates, t)
	}
	return templates
}

// GetTemplatesByCategory returns templates filtered by category
func (r *Registry) GetTemplatesByCategory(category string) []*TemplateMetadata {
	templates := make([]*TemplateMetadata, 0)
	for _, t := range r.templates {
		if t.Category == category {
			templates = append(templates, t)
		}
	}
	return templates
}

// GetTemplatesByTag returns templates filtered by tag
func (r *Registry) GetTemplatesByTag(tag string) []*TemplateMetadata {
	templates := make([]*TemplateMetadata, 0)
	for _, t := range r.templates {
		for _, tTag := range t.Tags {
			if tTag == tag {
				templates = append(templates, t)
				break
			}
		}
	}
	return templates
}

// InitializeGlobalRegistry initializes the global template registry
// This should be called once at application startup
func InitializeGlobalRegistry(templatesDir string) error {
	var initErr error
	globalRegistryOnce.Do(func() {
		globalRegistry = NewRegistry(templatesDir)
		initErr = globalRegistry.LoadTemplates()
	})
	return initErr
}

// GetGlobalRegistry returns the global template registry instance
// Returns nil if not initialized
func GetGlobalRegistry() *Registry {
	return globalRegistry
}
