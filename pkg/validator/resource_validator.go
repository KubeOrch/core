package validator

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/sirupsen/logrus"
)

// ResourceValidator validates parameters for any Kubernetes resource
type ResourceValidator struct {
	logger *logrus.Logger
}

// ValidationResult represents the result of validation
type ValidationResult struct {
	Valid  bool     `json:"valid"`
	Errors []string `json:"errors,omitempty"`
}

// NewResourceValidator creates a new validator
func NewResourceValidator() *ResourceValidator {
	return &ResourceValidator{
		logger: logrus.New(),
	}
}

// ValidateResourceParams validates parameters based on resource type
func (v *ResourceValidator) ValidateResourceParams(resourceType string, params map[string]interface{}) (*ValidationResult, error) {
	result := &ValidationResult{
		Valid:  true,
		Errors: []string{},
	}

	// Common validation for all resources
	if err := v.validateCommonFields(params); err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, err.Error())
	}

	// Resource-specific validation
	switch resourceType {
	case "deployment", "core/deployment":
		v.validateDeployment(params, result)
	case "service", "core/service":
		v.validateService(params, result)
	case "configmap", "core/configmap":
		v.validateConfigMap(params, result)
	case "secret", "core/secret":
		v.validateSecret(params, result)
	default:
		// For unknown types, just do basic validation
		v.logger.WithField("type", resourceType).Debug("No specific validation for resource type")
	}

	return result, nil
}

// validateCommonFields validates fields common to all resources
func (v *ResourceValidator) validateCommonFields(params map[string]interface{}) error {
	// Validate Name (required for all resources)
	name, ok := params["Name"].(string)
	if !ok || name == "" {
		return fmt.Errorf("name is required")
	}

	if err := v.validateDNSName(name); err != nil {
		return fmt.Errorf("invalid name: %v", err)
	}

	// Validate Namespace if provided
	if namespace, ok := params["Namespace"].(string); ok && namespace != "" {
		if err := v.validateDNSName(namespace); err != nil {
			return fmt.Errorf("invalid namespace: %v", err)
		}
	}

	return nil
}

// validateDeployment validates deployment-specific fields
func (v *ResourceValidator) validateDeployment(params map[string]interface{}, result *ValidationResult) {
	// Image validation
	if image, ok := params["Image"].(string); ok {
		if err := v.validateImage(image); err != nil {
			result.Valid = false
			result.Errors = append(result.Errors, fmt.Sprintf("invalid image: %v", err))
		}
	} else {
		result.Valid = false
		result.Errors = append(result.Errors, "Image is required for deployment")
	}

	// Replicas validation
	if replicas, ok := params["Replicas"]; ok {
		if err := v.validateReplicas(replicas); err != nil {
			result.Valid = false
			result.Errors = append(result.Errors, fmt.Sprintf("invalid replicas: %v", err))
		}
	}

	// Port validation
	if port, ok := params["Port"]; ok {
		if err := v.validatePort(port); err != nil {
			result.Valid = false
			result.Errors = append(result.Errors, fmt.Sprintf("invalid port: %v", err))
		}
	}

	// Resources validation
	if resources, ok := params["Resources"].(map[string]interface{}); ok {
		if err := v.validateResources(resources); err != nil {
			result.Valid = false
			result.Errors = append(result.Errors, fmt.Sprintf("invalid resources: %v", err))
		}
	}
}

// validateService validates service-specific fields
func (v *ResourceValidator) validateService(params map[string]interface{}, result *ValidationResult) {
	// ServiceType validation
	if serviceType, ok := params["ServiceType"].(string); ok {
		validTypes := map[string]bool{
			"ClusterIP":    true,
			"NodePort":     true,
			"LoadBalancer": true,
			"ExternalName": true,
		}
		if !validTypes[serviceType] {
			result.Valid = false
			result.Errors = append(result.Errors, fmt.Sprintf("invalid service type: %s", serviceType))
		}
	}

	// Port validation (required for service)
	if port, ok := params["Port"]; ok {
		if err := v.validatePort(port); err != nil {
			result.Valid = false
			result.Errors = append(result.Errors, fmt.Sprintf("invalid port: %v", err))
		}
	} else {
		result.Valid = false
		result.Errors = append(result.Errors, "Port is required for service")
	}

	// Selector validation
	if _, ok := params["SelectorApp"].(string); !ok {
		if _, ok := params["Selector"].(map[string]interface{}); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "Selector is required for service")
		}
	}
}

// validateConfigMap validates configmap-specific fields
func (v *ResourceValidator) validateConfigMap(params map[string]interface{}, result *ValidationResult) {
	// At least one of Data or BinaryData should be present
	_, hasData := params["Data"].(map[string]interface{})
	_, hasBinaryData := params["BinaryData"].(map[string]interface{})

	if !hasData && !hasBinaryData {
		result.Valid = false
		result.Errors = append(result.Errors, "ConfigMap must have either Data or BinaryData")
	}
}

// validateSecret validates secret-specific fields
func (v *ResourceValidator) validateSecret(params map[string]interface{}, result *ValidationResult) {
	// Type validation
	if secretType, ok := params["Type"].(string); ok {
		validTypes := map[string]bool{
			"Opaque":                              true,
			"kubernetes.io/service-account-token": true,
			"kubernetes.io/dockercfg":             true,
			"kubernetes.io/dockerconfigjson":      true,
			"kubernetes.io/basic-auth":            true,
			"kubernetes.io/ssh-auth":              true,
			"kubernetes.io/tls":                   true,
		}
		if !validTypes[secretType] {
			v.logger.WithField("type", secretType).Warn("Unknown secret type")
		}
	}

	// At least one of Data or StringData should be present
	_, hasData := params["Data"].(map[string]interface{})
	_, hasStringData := params["StringData"].(map[string]interface{})

	if !hasData && !hasStringData {
		result.Valid = false
		result.Errors = append(result.Errors, "Secret must have either Data or StringData")
	}
}

// validateDNSName validates DNS-1123 subdomain names
func (v *ResourceValidator) validateDNSName(name string) error {
	if len(name) > 253 {
		return fmt.Errorf("must be no more than 253 characters")
	}

	dnsRegex := regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`)
	if !dnsRegex.MatchString(name) {
		return fmt.Errorf("must consist of lowercase alphanumeric characters, '-' or '.', and must start and end with an alphanumeric character")
	}

	return nil
}

// validateImage validates container image format
func (v *ResourceValidator) validateImage(image string) error {
	if image == "" {
		return fmt.Errorf("cannot be empty")
	}

	if strings.Contains(image, " ") {
		return fmt.Errorf("cannot contain spaces")
	}

	return nil
}

// validatePort validates port number
func (v *ResourceValidator) validatePort(port interface{}) error {
	var portNum int
	switch p := port.(type) {
	case int:
		portNum = p
	case int32:
		portNum = int(p)
	case int64:
		portNum = int(p)
	case float64:
		portNum = int(p)
	case float32:
		portNum = int(p)
	default:
		return fmt.Errorf("must be a number")
	}

	if portNum < 1 || portNum > 65535 {
		return fmt.Errorf("must be between 1 and 65535")
	}

	return nil
}

// validateReplicas validates replica count
func (v *ResourceValidator) validateReplicas(replicas interface{}) error {
	var count int
	switch r := replicas.(type) {
	case int:
		count = r
	case int32:
		count = int(r)
	case int64:
		count = int(r)
	case float64:
		count = int(r)
	case float32:
		count = int(r)
	default:
		return fmt.Errorf("must be a number")
	}

	if count < 0 {
		return fmt.Errorf("cannot be negative")
	}

	return nil
}

// validateResources validates resource specifications
func (v *ResourceValidator) validateResources(resources map[string]interface{}) error {
	// Simplified validation - just check format
	if requests, ok := resources["Requests"].(map[string]interface{}); ok {
		if cpu, ok := requests["CPU"].(string); ok && cpu != "" {
			if !regexp.MustCompile(`^(\d+(\.\d+)?|\d+m)$`).MatchString(cpu) {
				return fmt.Errorf("invalid CPU format")
			}
		}
		if memory, ok := requests["Memory"].(string); ok && memory != "" {
			if !regexp.MustCompile(`^(\d+)(E|P|T|G|M|K|Ei|Pi|Ti|Gi|Mi|Ki)?$`).MatchString(memory) {
				return fmt.Errorf("invalid memory format")
			}
		}
	}

	return nil
}