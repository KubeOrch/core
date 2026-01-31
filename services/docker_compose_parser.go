package services

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/KubeOrch/core/models"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
)

// DockerComposeParser parses docker-compose files
type DockerComposeParser struct {
	logger *logrus.Logger
}

// NewDockerComposeParser creates a new docker-compose parser
func NewDockerComposeParser() *DockerComposeParser {
	return &DockerComposeParser{
		logger: logrus.New(),
	}
}

// DockerComposeFile represents a docker-compose.yml structure
type DockerComposeFile struct {
	Version  string                           `yaml:"version,omitempty"`
	Services map[string]DockerComposeService  `yaml:"services"`
	Volumes  map[string]DockerComposeVolume   `yaml:"volumes,omitempty"`
	Networks map[string]DockerComposeNetwork  `yaml:"networks,omitempty"`
}

// DockerComposeService represents a service in docker-compose
type DockerComposeService struct {
	Image         string                      `yaml:"image,omitempty"`
	Build         interface{}                 `yaml:"build,omitempty"` // Can be string or object
	Ports         []interface{}               `yaml:"ports,omitempty"` // Can be string or object
	Environment   interface{}                 `yaml:"environment,omitempty"` // Can be list or map
	EnvFile       interface{}                 `yaml:"env_file,omitempty"` // Can be string or list
	Volumes       []interface{}               `yaml:"volumes,omitempty"`
	DependsOn     interface{}                 `yaml:"depends_on,omitempty"` // Can be list or map
	Command       interface{}                 `yaml:"command,omitempty"` // Can be string or list
	Entrypoint    interface{}                 `yaml:"entrypoint,omitempty"` // Can be string or list
	Networks      interface{}                 `yaml:"networks,omitempty"` // Can be list or map
	Deploy        *DockerComposeDeploy        `yaml:"deploy,omitempty"`
	HealthCheck   *DockerComposeHealthCheck   `yaml:"healthcheck,omitempty"`
	Labels        interface{}                 `yaml:"labels,omitempty"` // Can be list or map
	Restart       string                      `yaml:"restart,omitempty"`
	WorkingDir    string                      `yaml:"working_dir,omitempty"`
	ContainerName string                      `yaml:"container_name,omitempty"`
	Hostname      string                      `yaml:"hostname,omitempty"`
	User          string                      `yaml:"user,omitempty"`
	Expose        []interface{}               `yaml:"expose,omitempty"`
	NetworkMode   string                      `yaml:"network_mode,omitempty"`
	Privileged    bool                        `yaml:"privileged,omitempty"`
	CapAdd        []string                    `yaml:"cap_add,omitempty"`
	CapDrop       []string                    `yaml:"cap_drop,omitempty"`
	Stdin         bool                        `yaml:"stdin_open,omitempty"`
	Tty           bool                        `yaml:"tty,omitempty"`
}

// DockerComposeDeploy represents deploy configuration
type DockerComposeDeploy struct {
	Replicas  int                       `yaml:"replicas,omitempty"`
	Resources *DockerComposeResources   `yaml:"resources,omitempty"`
	Mode      string                    `yaml:"mode,omitempty"`
}

// DockerComposeResources represents resource limits
type DockerComposeResources struct {
	Limits       *DockerComposeResourceSpec `yaml:"limits,omitempty"`
	Reservations *DockerComposeResourceSpec `yaml:"reservations,omitempty"`
}

// DockerComposeResourceSpec represents CPU/memory spec
type DockerComposeResourceSpec struct {
	CPUs   string `yaml:"cpus,omitempty"`
	Memory string `yaml:"memory,omitempty"`
}

// DockerComposeHealthCheck represents health check config
type DockerComposeHealthCheck struct {
	Test        interface{} `yaml:"test,omitempty"` // Can be string or list
	Interval    string      `yaml:"interval,omitempty"`
	Timeout     string      `yaml:"timeout,omitempty"`
	Retries     int         `yaml:"retries,omitempty"`
	StartPeriod string      `yaml:"start_period,omitempty"`
	Disable     bool        `yaml:"disable,omitempty"`
}

// DockerComposeVolume represents a named volume
type DockerComposeVolume struct {
	Driver   string            `yaml:"driver,omitempty"`
	External interface{}       `yaml:"external,omitempty"` // Can be bool or object
	Labels   interface{}       `yaml:"labels,omitempty"`
	Name     string            `yaml:"name,omitempty"`
}

// DockerComposeNetwork represents a network
type DockerComposeNetwork struct {
	Driver   string      `yaml:"driver,omitempty"`
	External interface{} `yaml:"external,omitempty"` // Can be bool or object
	Name     string      `yaml:"name,omitempty"`
}

// Parse parses docker-compose YAML content
func (p *DockerComposeParser) Parse(content []byte) (*models.ImportAnalysis, error) {
	var compose DockerComposeFile
	if err := yaml.Unmarshal(content, &compose); err != nil {
		return nil, fmt.Errorf("failed to parse docker-compose YAML: %w", err)
	}

	analysis := &models.ImportAnalysis{
		DetectedType: "docker-compose",
		Services:     make([]models.ImportedService, 0),
		Volumes:      make([]models.ImportedVolume, 0),
		Networks:     make([]models.ImportedNetwork, 0),
		Warnings:     make([]string, 0),
		Errors:       make([]models.ImportError, 0),
	}

	// Parse services
	for name, svc := range compose.Services {
		importedSvc, warnings, errors := p.parseService(name, svc)
		analysis.Services = append(analysis.Services, importedSvc)
		analysis.Warnings = append(analysis.Warnings, warnings...)
		analysis.Errors = append(analysis.Errors, errors...)
	}

	// Parse volumes
	for name, vol := range compose.Volumes {
		importedVol := p.parseVolume(name, vol)
		analysis.Volumes = append(analysis.Volumes, importedVol)
	}

	// Parse networks
	for name, net := range compose.Networks {
		importedNet := p.parseNetwork(name, net)
		analysis.Networks = append(analysis.Networks, importedNet)
	}

	return analysis, nil
}

// parseService parses a single service definition
func (p *DockerComposeParser) parseService(name string, svc DockerComposeService) (models.ImportedService, []string, []models.ImportError) {
	warnings := make([]string, 0)
	errors := make([]models.ImportError, 0)

	imported := models.ImportedService{
		Name:       name,
		Image:      svc.Image,
		Restart:    svc.Restart,
		WorkingDir: svc.WorkingDir,
	}

	// Parse build config
	if svc.Build != nil {
		imported.Build = p.parseBuild(svc.Build)
		if imported.Image == "" {
			warnings = append(warnings, fmt.Sprintf("Service '%s' uses build context but no image name. You'll need to build and push the image first.", name))
		}
	}

	// Check if service has no image and no build
	if imported.Image == "" && imported.Build == nil {
		errors = append(errors, models.ImportError{
			Code:    models.ImportErrorServiceNoImage,
			Message: "Service has no image or build configuration",
			Service: name,
		})
	}

	// Parse ports
	imported.Ports = p.parsePorts(svc.Ports)

	// Parse environment
	imported.Environment = p.parseEnvironment(svc.Environment)

	// Parse env_file
	imported.EnvFile = p.parseEnvFile(svc.EnvFile)

	// Parse volumes
	imported.Volumes, warnings = p.parseVolumes(svc.Volumes, name, warnings)

	// Parse depends_on
	imported.DependsOn = p.parseDependsOn(svc.DependsOn)

	// Parse command
	imported.Command = p.parseStringOrList(svc.Command)

	// Parse entrypoint
	imported.Entrypoint = p.parseStringOrList(svc.Entrypoint)

	// Parse networks
	imported.Networks = p.parseNetworks(svc.Networks)

	// Parse deploy
	if svc.Deploy != nil {
		imported.Replicas = svc.Deploy.Replicas
		if svc.Deploy.Resources != nil {
			imported.Resources = &models.ResourceConfig{}
			if svc.Deploy.Resources.Limits != nil {
				imported.Resources.Limits = models.ImportResourceSpec{
					CPUs:   svc.Deploy.Resources.Limits.CPUs,
					Memory: svc.Deploy.Resources.Limits.Memory,
				}
			}
			if svc.Deploy.Resources.Reservations != nil {
				imported.Resources.Reservations = models.ImportResourceSpec{
					CPUs:   svc.Deploy.Resources.Reservations.CPUs,
					Memory: svc.Deploy.Resources.Reservations.Memory,
				}
			}
		}
	}

	// Parse health check
	if svc.HealthCheck != nil && !svc.HealthCheck.Disable {
		imported.HealthCheck = &models.HealthCheckConfig{
			Test:        p.parseStringOrList(svc.HealthCheck.Test),
			Interval:    svc.HealthCheck.Interval,
			Timeout:     svc.HealthCheck.Timeout,
			Retries:     svc.HealthCheck.Retries,
			StartPeriod: svc.HealthCheck.StartPeriod,
		}
	}

	// Parse labels
	imported.Labels = p.parseLabelsOrEnv(svc.Labels)

	// Check for unsupported features
	if svc.NetworkMode != "" && svc.NetworkMode != "bridge" {
		warnings = append(warnings, fmt.Sprintf("Service '%s' uses network_mode '%s' which is not supported in Kubernetes", name, svc.NetworkMode))
	}

	if svc.Privileged {
		warnings = append(warnings, fmt.Sprintf("Service '%s' runs in privileged mode. Consider using security contexts in Kubernetes.", name))
	}

	return imported, warnings, errors
}

// parseBuild parses build configuration
func (p *DockerComposeParser) parseBuild(build interface{}) *models.BuildConfig {
	switch v := build.(type) {
	case string:
		return &models.BuildConfig{Context: v}
	case map[string]interface{}:
		config := &models.BuildConfig{}
		if ctx, ok := v["context"].(string); ok {
			config.Context = ctx
		}
		if df, ok := v["dockerfile"].(string); ok {
			config.Dockerfile = df
		}
		if target, ok := v["target"].(string); ok {
			config.Target = target
		}
		if args, ok := v["args"].(map[string]interface{}); ok {
			config.Args = make(map[string]string)
			for k, val := range args {
				config.Args[k] = fmt.Sprintf("%v", val)
			}
		}
		return config
	}
	return nil
}

// parsePorts parses port mappings
func (p *DockerComposeParser) parsePorts(ports []interface{}) []models.PortMapping {
	result := make([]models.PortMapping, 0)
	portRegex := regexp.MustCompile(`^(?:(\d+\.\d+\.\d+\.\d+):)?(?:(\d+):)?(\d+)(?:/(tcp|udp))?$`)

	for _, port := range ports {
		switch v := port.(type) {
		case string:
			matches := portRegex.FindStringSubmatch(v)
			if matches != nil {
				pm := models.PortMapping{}
				if matches[1] != "" {
					pm.HostIP = matches[1]
				}
				if matches[2] != "" {
					pm.HostPort, _ = strconv.Atoi(matches[2])
				}
				pm.ContainerPort, _ = strconv.Atoi(matches[3])
				if matches[4] != "" {
					pm.Protocol = matches[4]
				}
				result = append(result, pm)
			}
		case int:
			result = append(result, models.PortMapping{ContainerPort: v})
		case map[string]interface{}:
			pm := models.PortMapping{}
			if target, ok := v["target"].(int); ok {
				pm.ContainerPort = target
			}
			if published, ok := v["published"].(int); ok {
				pm.HostPort = published
			}
			if protocol, ok := v["protocol"].(string); ok {
				pm.Protocol = protocol
			}
			if hostIp, ok := v["host_ip"].(string); ok {
				pm.HostIP = hostIp
			}
			if pm.ContainerPort > 0 {
				result = append(result, pm)
			}
		}
	}
	return result
}

// parseEnvironment parses environment variables
func (p *DockerComposeParser) parseEnvironment(env interface{}) map[string]string {
	result := make(map[string]string)
	switch v := env.(type) {
	case []interface{}:
		for _, item := range v {
			if str, ok := item.(string); ok {
				parts := strings.SplitN(str, "=", 2)
				if len(parts) == 2 {
					result[parts[0]] = parts[1]
				} else if len(parts) == 1 {
					result[parts[0]] = "" // Variable without value
				}
			}
		}
	case map[string]interface{}:
		for k, val := range v {
			if val == nil {
				result[k] = ""
			} else {
				result[k] = fmt.Sprintf("%v", val)
			}
		}
	}
	return result
}

// parseEnvFile parses env_file configuration
func (p *DockerComposeParser) parseEnvFile(envFile interface{}) []string {
	result := make([]string, 0)
	switch v := envFile.(type) {
	case string:
		result = append(result, v)
	case []interface{}:
		for _, item := range v {
			if str, ok := item.(string); ok {
				result = append(result, str)
			}
		}
	}
	return result
}

// parseVolumes parses volume mappings
func (p *DockerComposeParser) parseVolumes(volumes []interface{}, serviceName string, warnings []string) ([]models.VolumeMapping, []string) {
	result := make([]models.VolumeMapping, 0)
	volumeRegex := regexp.MustCompile(`^([^:]+):([^:]+)(?::(ro|rw))?$`)

	for _, vol := range volumes {
		switch v := vol.(type) {
		case string:
			matches := volumeRegex.FindStringSubmatch(v)
			if matches != nil {
				vm := models.VolumeMapping{
					Source:   matches[1],
					Target:   matches[2],
					ReadOnly: matches[3] == "ro",
				}
				// Determine type: bind mount (starts with / or .) vs named volume
				if strings.HasPrefix(vm.Source, "/") || strings.HasPrefix(vm.Source, ".") {
					vm.Type = "bind"
					warnings = append(warnings, fmt.Sprintf("Service '%s' uses bind mount '%s' which is not directly supported in Kubernetes. Consider using a PersistentVolumeClaim.", serviceName, vm.Source))
				} else {
					vm.Type = "volume"
				}
				result = append(result, vm)
			}
		case map[string]interface{}:
			vm := models.VolumeMapping{}
			if source, ok := v["source"].(string); ok {
				vm.Source = source
			}
			if target, ok := v["target"].(string); ok {
				vm.Target = target
			}
			if readOnly, ok := v["read_only"].(bool); ok {
				vm.ReadOnly = readOnly
			}
			if volType, ok := v["type"].(string); ok {
				vm.Type = volType
			}
			if vm.Type == "bind" {
				warnings = append(warnings, fmt.Sprintf("Service '%s' uses bind mount which is not directly supported in Kubernetes. Consider using a PersistentVolumeClaim.", serviceName))
			}
			if vm.Source != "" && vm.Target != "" {
				result = append(result, vm)
			}
		}
	}
	return result, warnings
}

// parseDependsOn parses depends_on configuration
func (p *DockerComposeParser) parseDependsOn(dependsOn interface{}) []string {
	result := make([]string, 0)
	switch v := dependsOn.(type) {
	case []interface{}:
		for _, item := range v {
			if str, ok := item.(string); ok {
				result = append(result, str)
			}
		}
	case map[string]interface{}:
		for k := range v {
			result = append(result, k)
		}
	}
	return result
}

// parseStringOrList parses a value that can be string or list
func (p *DockerComposeParser) parseStringOrList(val interface{}) []string {
	result := make([]string, 0)
	switch v := val.(type) {
	case string:
		// Split by space for command-like strings
		result = strings.Fields(v)
	case []interface{}:
		for _, item := range v {
			if str, ok := item.(string); ok {
				result = append(result, str)
			}
		}
	}
	return result
}

// parseNetworks parses networks configuration
func (p *DockerComposeParser) parseNetworks(networks interface{}) []string {
	result := make([]string, 0)
	switch v := networks.(type) {
	case []interface{}:
		for _, item := range v {
			if str, ok := item.(string); ok {
				result = append(result, str)
			}
		}
	case map[string]interface{}:
		for k := range v {
			result = append(result, k)
		}
	}
	return result
}

// parseLabelsOrEnv parses labels or env that can be list or map
func (p *DockerComposeParser) parseLabelsOrEnv(val interface{}) map[string]string {
	result := make(map[string]string)
	switch v := val.(type) {
	case []interface{}:
		for _, item := range v {
			if str, ok := item.(string); ok {
				parts := strings.SplitN(str, "=", 2)
				if len(parts) == 2 {
					result[parts[0]] = parts[1]
				}
			}
		}
	case map[string]interface{}:
		for k, val := range v {
			if val == nil {
				result[k] = ""
			} else {
				result[k] = fmt.Sprintf("%v", val)
			}
		}
	}
	return result
}

// parseVolume parses a named volume definition
func (p *DockerComposeParser) parseVolume(name string, vol DockerComposeVolume) models.ImportedVolume {
	imported := models.ImportedVolume{
		Name:   name,
		Driver: vol.Driver,
	}

	// Handle external (can be bool or object with name)
	switch v := vol.External.(type) {
	case bool:
		imported.External = v
	case map[string]interface{}:
		imported.External = true
		if extName, ok := v["name"].(string); ok {
			imported.Name = extName
		}
	}

	// Use explicit name if provided
	if vol.Name != "" {
		imported.Name = vol.Name
	}

	// Parse labels
	imported.Labels = p.parseLabelsOrEnv(vol.Labels)

	return imported
}

// parseNetwork parses a network definition
func (p *DockerComposeParser) parseNetwork(name string, net DockerComposeNetwork) models.ImportedNetwork {
	imported := models.ImportedNetwork{
		Name:   name,
		Driver: net.Driver,
	}

	// Handle external
	switch v := net.External.(type) {
	case bool:
		imported.External = v
	case map[string]interface{}:
		imported.External = true
		if extName, ok := v["name"].(string); ok {
			imported.Name = extName
		}
	}

	// Use explicit name if provided
	if net.Name != "" {
		imported.Name = net.Name
	}

	return imported
}
