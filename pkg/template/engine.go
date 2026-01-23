package template

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
)

// Engine handles template rendering for Kubernetes resources
type Engine struct {
	templatesDir string
	logger       *logrus.Logger
}

// NewEngine creates a new template engine
func NewEngine(templatesDir string) *Engine {
	return &Engine{
		templatesDir: templatesDir,
		logger:       logrus.New(),
	}
}

// RenderTemplate renders a template with given values
func (e *Engine) RenderTemplate(templateID string, values map[string]interface{}) ([]byte, error) {
	// First try template.yaml (generic), then fall back to deployment.yaml (legacy)
	templatePath := filepath.Join(e.templatesDir, templateID, "template.yaml")
	if _, err := os.Stat(templatePath); os.IsNotExist(err) {
		// Try deployment.yaml for backward compatibility
		templatePath = filepath.Join(e.templatesDir, templateID, "deployment.yaml")
		if _, err := os.Stat(templatePath); os.IsNotExist(err) {
			// Try the templateID as a direct file path (e.g., "core/service.yaml")
			templatePath = filepath.Join(e.templatesDir, templateID+".yaml")
		}
	}

	e.logger.WithFields(logrus.Fields{
		"templateID":   templateID,
		"templatePath": templatePath,
		"values":       values,
	}).Debug("Rendering template")

	// Read template file
	templateContent, err := os.ReadFile(templatePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read template file: %w", err)
	}

	// Create template with helper functions
	tmpl, err := template.New("deployment").Funcs(e.getHelperFunctions()).Parse(string(templateContent))
	if err != nil {
		return nil, fmt.Errorf("failed to parse template: %w", err)
	}

	// Render template
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, values); err != nil {
		return nil, fmt.Errorf("failed to execute template: %w", err)
	}

	return buf.Bytes(), nil
}

// RenderToManifests renders template and returns parsed Kubernetes manifests
func (e *Engine) RenderToManifests(templateID string, values map[string]interface{}) ([]map[string]interface{}, error) {
	rendered, err := e.RenderTemplate(templateID, values)
	if err != nil {
		return nil, err
	}

	// Parse YAML to validate and split multiple documents
	var manifests []map[string]interface{}
	decoder := yaml.NewDecoder(bytes.NewReader(rendered))

	for {
		var manifest map[string]interface{}
		err := decoder.Decode(&manifest)
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("failed to decode YAML: %w", err)
		}
		if manifest != nil {
			manifests = append(manifests, manifest)
		}
	}

	return manifests, nil
}

// getHelperFunctions returns template helper functions
func (e *Engine) getHelperFunctions() template.FuncMap {
	return template.FuncMap{
		"default": func(def interface{}, value interface{}) interface{} {
			if value == nil || value == "" || value == 0 {
				return def
			}
			return value
		},
		"quote": func(s interface{}) string {
			return fmt.Sprintf("%q", s)
		},
		"base64encode": func(s interface{}) string {
			str := fmt.Sprintf("%v", s)
			return base64.StdEncoding.EncodeToString([]byte(str))
		},
	}
}

// ValidateTemplate checks if a template exists and is valid
func (e *Engine) ValidateTemplate(templateID string) error {
	// Try multiple possible paths
	possiblePaths := []string{
		filepath.Join(e.templatesDir, templateID, "template.yaml"),
		filepath.Join(e.templatesDir, templateID, "deployment.yaml"),
		filepath.Join(e.templatesDir, templateID+".yaml"),
	}

	var templatePath string
	var found bool
	for _, path := range possiblePaths {
		if _, err := os.Stat(path); err == nil {
			templatePath = path
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("template not found: %s", templateID)
	}

	// Try to parse the template to validate syntax
	templateContent, err := os.ReadFile(templatePath)
	if err != nil {
		return fmt.Errorf("failed to read template: %w", err)
	}

	_, err = template.New("validate").Funcs(e.getHelperFunctions()).Parse(string(templateContent))
	if err != nil {
		return fmt.Errorf("invalid template syntax: %w", err)
	}

	return nil
}

// ListTemplates returns available templates
func (e *Engine) ListTemplates() ([]string, error) {
	var templates []string

	// Walk through templates directory
	err := filepath.Walk(e.templatesDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			// Check for template files in this directory
			templateFiles := []string{"template.yaml", "deployment.yaml"}
			for _, filename := range templateFiles {
				templatePath := filepath.Join(path, filename)
				if _, err := os.Stat(templatePath); err == nil {
					// Get relative path from templates directory
					relPath, _ := filepath.Rel(e.templatesDir, path)
					if relPath != "." {
						templates = append(templates, relPath)
						break // Found a template, no need to check other filenames
					}
				}
			}
		} else if filepath.Ext(path) == ".yaml" || filepath.Ext(path) == ".yml" {
			// Also include standalone YAML files as templates
			relPath, _ := filepath.Rel(e.templatesDir, path)
			// Remove the extension to get template ID
			templateID := strings.TrimSuffix(relPath, filepath.Ext(relPath))
			templates = append(templates, templateID)
		}

		return nil
	})

	return templates, err
}