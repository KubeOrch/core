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
	case "job", "core/job":
		v.validateJob(params, result)
	case "cronjob", "core/cronjob":
		v.validateCronJob(params, result)
	case "daemonset", "core/daemonset":
		v.validateDaemonSet(params, result)
	case "hpa", "core/hpa":
		v.validateHPA(params, result)
	case "networkpolicy", "core/networkpolicy":
		v.validateNetworkPolicy(params, result)
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

	// Selector validation - accept SelectorApp, Selector map, or TargetApp (template fallback)
	_, hasSelectorApp := params["SelectorApp"].(string)
	_, hasSelector := params["Selector"].(map[string]interface{})
	targetApp, hasTargetApp := params["TargetApp"].(string)

	if !hasSelectorApp && !hasSelector && (!hasTargetApp || targetApp == "") {
		result.Valid = false
		result.Errors = append(result.Errors, "Selector is required for service (set TargetApp or Selector)")
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
	// Auto-trim whitespace
	image = strings.TrimSpace(image)

	if image == "" {
		return fmt.Errorf("cannot be empty")
	}

	// Check for spaces in the middle of the image name
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

// validateJob validates job-specific fields
func (v *ResourceValidator) validateJob(params map[string]interface{}, result *ValidationResult) {
	// Image validation (required)
	if image, ok := params["Image"].(string); ok {
		if err := v.validateImage(image); err != nil {
			result.Valid = false
			result.Errors = append(result.Errors, fmt.Sprintf("invalid image: %v", err))
		}
	} else {
		result.Valid = false
		result.Errors = append(result.Errors, "Image is required for Job")
	}

	// RestartPolicy validation
	if restartPolicy, ok := params["RestartPolicy"].(string); ok {
		if restartPolicy != "Never" && restartPolicy != "OnFailure" {
			result.Valid = false
			result.Errors = append(result.Errors, "RestartPolicy must be Never or OnFailure for Jobs")
		}
	}

	// Completions validation
	if completions, ok := params["Completions"]; ok {
		if err := v.validatePositiveNumber(completions, "Completions"); err != nil {
			result.Valid = false
			result.Errors = append(result.Errors, err.Error())
		}
	}

	// Parallelism validation
	if parallelism, ok := params["Parallelism"]; ok {
		if err := v.validatePositiveNumber(parallelism, "Parallelism"); err != nil {
			result.Valid = false
			result.Errors = append(result.Errors, err.Error())
		}
	}

	// BackoffLimit validation
	if backoffLimit, ok := params["BackoffLimit"]; ok {
		if err := v.validateNonNegativeNumber(backoffLimit, "BackoffLimit"); err != nil {
			result.Valid = false
			result.Errors = append(result.Errors, err.Error())
		}
	}
}

// validateCronJob validates cronjob-specific fields
func (v *ResourceValidator) validateCronJob(params map[string]interface{}, result *ValidationResult) {
	// Schedule validation (required)
	if schedule, ok := params["Schedule"].(string); ok {
		if schedule == "" {
			result.Valid = false
			result.Errors = append(result.Errors, "Schedule is required for CronJob")
		} else {
			// Basic cron format validation (5 or 6 fields)
			fields := strings.Fields(schedule)
			if len(fields) < 5 || len(fields) > 6 {
				result.Valid = false
				result.Errors = append(result.Errors, "Schedule must be a valid cron expression (5 or 6 fields)")
			}
		}
	} else {
		result.Valid = false
		result.Errors = append(result.Errors, "Schedule is required for CronJob")
	}

	// Image validation (required)
	if image, ok := params["Image"].(string); ok {
		if err := v.validateImage(image); err != nil {
			result.Valid = false
			result.Errors = append(result.Errors, fmt.Sprintf("invalid image: %v", err))
		}
	} else {
		result.Valid = false
		result.Errors = append(result.Errors, "Image is required for CronJob")
	}

	// ConcurrencyPolicy validation
	if policy, ok := params["ConcurrencyPolicy"].(string); ok {
		validPolicies := map[string]bool{
			"Allow":   true,
			"Forbid":  true,
			"Replace": true,
		}
		if !validPolicies[policy] {
			result.Valid = false
			result.Errors = append(result.Errors, "ConcurrencyPolicy must be Allow, Forbid, or Replace")
		}
	}
}

// validateDaemonSet validates daemonset-specific fields
func (v *ResourceValidator) validateDaemonSet(params map[string]interface{}, result *ValidationResult) {
	// Image validation (required)
	if image, ok := params["Image"].(string); ok {
		if err := v.validateImage(image); err != nil {
			result.Valid = false
			result.Errors = append(result.Errors, fmt.Sprintf("invalid image: %v", err))
		}
	} else {
		result.Valid = false
		result.Errors = append(result.Errors, "Image is required for DaemonSet")
	}

	// UpdateStrategy validation
	if strategy, ok := params["UpdateStrategy"].(string); ok {
		validStrategies := map[string]bool{
			"RollingUpdate": true,
			"OnDelete":      true,
		}
		if !validStrategies[strategy] {
			result.Valid = false
			result.Errors = append(result.Errors, "UpdateStrategy must be RollingUpdate or OnDelete")
		}
	}

	// Port validation
	if port, ok := params["Port"]; ok {
		if err := v.validatePort(port); err != nil {
			result.Valid = false
			result.Errors = append(result.Errors, fmt.Sprintf("invalid port: %v", err))
		}
	}
}

// validateHPA validates hpa-specific fields
func (v *ResourceValidator) validateHPA(params map[string]interface{}, result *ValidationResult) {
	// ScaleTargetName validation (required)
	if targetName, ok := params["ScaleTargetName"].(string); ok {
		if targetName == "" {
			result.Valid = false
			result.Errors = append(result.Errors, "ScaleTargetName is required for HPA")
		}
	} else {
		result.Valid = false
		result.Errors = append(result.Errors, "ScaleTargetName is required for HPA")
	}

	// MaxReplicas validation (required)
	maxReplicas := 0
	if max, ok := params["MaxReplicas"]; ok {
		if err := v.validatePositiveNumber(max, "MaxReplicas"); err != nil {
			result.Valid = false
			result.Errors = append(result.Errors, err.Error())
		} else {
			maxReplicas = v.toInt(max)
		}
	} else {
		result.Valid = false
		result.Errors = append(result.Errors, "MaxReplicas is required for HPA")
	}

	// MinReplicas validation
	if min, ok := params["MinReplicas"]; ok {
		if err := v.validatePositiveNumber(min, "MinReplicas"); err != nil {
			result.Valid = false
			result.Errors = append(result.Errors, err.Error())
		} else {
			minReplicas := v.toInt(min)
			if minReplicas > maxReplicas && maxReplicas > 0 {
				result.Valid = false
				result.Errors = append(result.Errors, "MinReplicas cannot be greater than MaxReplicas")
			}
		}
	}

	// At least one metric target required
	_, hasCPU := params["TargetCPUUtilization"]
	_, hasMemory := params["TargetMemoryUtilization"]
	if !hasCPU && !hasMemory {
		result.Valid = false
		result.Errors = append(result.Errors, "At least one of TargetCPUUtilization or TargetMemoryUtilization is required")
	}

	// Utilization range validation
	if cpu, ok := params["TargetCPUUtilization"]; ok {
		if err := v.validatePercentage(cpu, "TargetCPUUtilization"); err != nil {
			result.Valid = false
			result.Errors = append(result.Errors, err.Error())
		}
	}
	if memory, ok := params["TargetMemoryUtilization"]; ok {
		if err := v.validatePercentage(memory, "TargetMemoryUtilization"); err != nil {
			result.Valid = false
			result.Errors = append(result.Errors, err.Error())
		}
	}
}

// validateNetworkPolicy validates networkpolicy-specific fields
func (v *ResourceValidator) validateNetworkPolicy(params map[string]interface{}, result *ValidationResult) {
	// PolicyTypes validation (required)
	if policyTypes, ok := params["PolicyTypes"].([]interface{}); ok {
		if len(policyTypes) == 0 {
			result.Valid = false
			result.Errors = append(result.Errors, "At least one PolicyType (Ingress or Egress) is required")
		} else {
			validTypes := map[string]bool{
				"Ingress": true,
				"Egress":  true,
			}
			for _, pt := range policyTypes {
				if ptStr, ok := pt.(string); ok {
					if !validTypes[ptStr] {
						result.Valid = false
						result.Errors = append(result.Errors, fmt.Sprintf("Invalid PolicyType: %s", ptStr))
					}
				}
			}
		}
	} else {
		result.Valid = false
		result.Errors = append(result.Errors, "PolicyTypes is required for NetworkPolicy")
	}

	// Validate CIDR blocks in rules if present
	if ingressRules, ok := params["IngressRules"].([]interface{}); ok {
		for i, rule := range ingressRules {
			if ruleMap, ok := rule.(map[string]interface{}); ok {
				if from, ok := ruleMap["From"].([]interface{}); ok {
					for j, peer := range from {
						if peerMap, ok := peer.(map[string]interface{}); ok {
							if ipBlock, ok := peerMap["IPBlock"].(map[string]interface{}); ok {
								if cidr, ok := ipBlock["CIDR"].(string); ok {
									if err := v.validateCIDR(cidr); err != nil {
										result.Valid = false
										result.Errors = append(result.Errors, fmt.Sprintf("IngressRule[%d].From[%d].IPBlock.CIDR: %v", i, j, err))
									}
								}
							}
						}
					}
				}
			}
		}
	}
}

// Helper validation functions

func (v *ResourceValidator) validatePositiveNumber(value interface{}, fieldName string) error {
	num := v.toInt(value)
	if num < 1 {
		return fmt.Errorf("%s must be at least 1", fieldName)
	}
	return nil
}

func (v *ResourceValidator) validateNonNegativeNumber(value interface{}, fieldName string) error {
	num := v.toInt(value)
	if num < 0 {
		return fmt.Errorf("%s cannot be negative", fieldName)
	}
	return nil
}

func (v *ResourceValidator) validatePercentage(value interface{}, fieldName string) error {
	num := v.toInt(value)
	if num < 1 || num > 100 {
		return fmt.Errorf("%s must be between 1 and 100", fieldName)
	}
	return nil
}

func (v *ResourceValidator) validateCIDR(cidr string) error {
	// Basic CIDR format validation
	cidrRegex := regexp.MustCompile(`^(\d{1,3}\.){3}\d{1,3}/\d{1,2}$`)
	if !cidrRegex.MatchString(cidr) {
		return fmt.Errorf("invalid CIDR format")
	}
	return nil
}

func (v *ResourceValidator) toInt(value interface{}) int {
	switch val := value.(type) {
	case int:
		return val
	case int32:
		return int(val)
	case int64:
		return int(val)
	case float64:
		return int(val)
	case float32:
		return int(val)
	default:
		return 0
	}
}